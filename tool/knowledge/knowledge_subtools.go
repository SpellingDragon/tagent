package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/duckduckgo"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"

	"github.com/SpellingDragon/tagent/memory"
	tagenttool "github.com/SpellingDragon/tagent/tool"
)

// KnowledgeResult represents a single piece of acquired knowledge.
type KnowledgeResult struct {
	Type          string         `json:"type"`                     // "skill", "skill_content", "web", "mcp_tool", "historical_memory"
	Title         string         `json:"title"`                    // Human-readable title
	Content       string         `json:"content"`                  // Knowledge content
	Source        string         `json:"source,omitempty"`         // Source identifier
	ExecutionPlan *ExecutionPlan `json:"execution_plan,omitempty"` // Translated executable plan
}

// ExecutionPlan describes a physical execution plan that CommandTool can directly run.
type ExecutionPlan struct {
	Function    string            `json:"function"`              // "exec", "tmux_exec", "mcp_call"
	Command     string            `json:"command,omitempty"`     // Command for exec/tmux_exec
	MCPTool     string            `json:"mcp_tool,omitempty"`    // MCP tool name for mcp_call
	MCPArgs     map[string]any    `json:"mcp_args,omitempty"`    // MCP tool arguments for mcp_call
	Env         map[string]string `json:"env,omitempty"`         // Environment variables
	Dir         string            `json:"dir,omitempty"`         // Working directory
	Timeout     int               `json:"timeout,omitempty"`     // Timeout in seconds
	Description string            `json:"description,omitempty"` // Human-readable description
}

// BuildSubTools assembles the sub-tool set for the Knowledge Agent.
func BuildSubTools(cfg Config) []tool.Tool {
	var tools []tool.Tool

	if cfg.SkillRepo != nil {
		tools = append(tools, NewSkillSearchTool(cfg.SkillRepo))
		tools = append(tools, NewSkillLoadTool(cfg.SkillRepo))
	}

	if len(cfg.MCPToolSets) > 0 {
		tools = append(tools, NewMCPDiscoverTool(cfg.MCPToolSets))
	}

	// Web search: always available via two complementary tools
	// duckduckgo_search: Instant Answer API for factual/encyclopedic info (fast, structured)
	tools = append(tools, duckduckgo.NewTool())
	// web_search: HTML scraping for general web content (current events, tutorials, docs)
	tools = append(tools, NewWebSearchTool())

	if cfg.MemStore != nil {
		tools = append(tools, NewMemoryQueryTool(cfg.MemStore))
	}

	return tools
}

// ==================== Sub-tool implementations ====================

// NewSkillSearchTool creates a tool that searches the skill repository.
func NewSkillSearchTool(repo tagenttool.SkillRepository) tool.Tool {
	return function.NewFunctionTool(
		func(ctx context.Context, args skillSearchArgs) (skillSearchResult, error) {
			results := searchSkills(repo, args.Query)
			return skillSearchResult{
				Results: results,
				Count:   len(results),
			}, nil
		},
		function.WithName("skill_search"),
		function.WithDescription("Search local skill repository for automation skills. Use task-domain keywords describing what you need to do (e.g., 'url', 'fetch', 'deploy', 'git') — the search matches against skill names and descriptions. Always call this first before falling back to web search."),
	)
}

// NewSkillLoadTool creates a tool that loads skill content as a structured summary.
//
// Progressive disclosure design (following trpc-agent-go pattern):
//   - Level 1 (skill_search): name + description from YAML front matter
//   - Level 2 (skill_load): name + description + usage summary (up to ~2500 chars)
//   - Level 3 (command): read full skill file when deeper detail is needed
//
// The tool returns a compact, structured output suitable for the knowledge agent
// to read and synthesize, avoiding the context explosion of dumping the full body.
func NewSkillLoadTool(repo tagenttool.SkillRepository) tool.Tool {
	const maxBodyChars = 2500

	return function.NewFunctionTool(
		func(ctx context.Context, args skillLoadArgs) (skillLoadResult, error) {
			s, err := repo.Get(args.SkillName)
			if err != nil {
				return skillLoadResult{}, fmt.Errorf("skill not found: %s", args.SkillName)
			}

			// Get fully-qualified path for truncation note
			skillFilePath := "skills/" + args.SkillName + "/SKILL.md"
			// Try to use Path() from the framework repo if available
			if pathProvider, ok := interface{}(repo).(interface{ Path(string) (string, error) }); ok {
				if dir, err := pathProvider.Path(args.SkillName); err == nil && dir != "" {
					skillFilePath = dir + "/SKILL.md"
				}
			}

			var b strings.Builder

			// 1. Summary (Name + Description) always at top
			b.WriteString("**[Skill]** ")
			b.WriteString(s.Summary.Name)
			b.WriteString("\n")
			if s.Summary.Description != "" {
				b.WriteString(s.Summary.Description)
				b.WriteString("\n")
			}
			b.WriteString("\n")

			// 2. Body with section-aware truncation
			body := s.Body
			if len(body) > maxBodyChars {
				// Find the last section heading (## ) within the limit to cut cleanly.
				// Only consider headings in the second half of the limit to avoid
				// cutting before the first real section.
				cutoff := maxBodyChars
				searchRange := body[:maxBodyChars]
				if lastHeading := strings.LastIndex(searchRange, "\n## "); lastHeading > maxBodyChars/2 {
					cutoff = lastHeading
				}
				body = body[:cutoff]
				b.WriteString(body)
				b.WriteString("\n\n---\n*(truncated at ")
				b.WriteString(fmt.Sprintf("%d chars) — full file: %s, use command to read if deeper inspection needed)*\n", len(s.Body), skillFilePath))
			} else {
				b.WriteString(body)
			}

			// 3. Docs listed by path only (no content to keep output compact)
			if len(s.Docs) > 0 {
				b.WriteString("\n**Docs** (")
				b.WriteString(fmt.Sprintf("%d", len(s.Docs)))
				b.WriteString("): ")
				var names []string
				for _, doc := range s.Docs {
					names = append(names, doc.Path)
				}
				b.WriteString(strings.Join(names, ", "))
			}

			return skillLoadResult{
				Type:    "skill_content",
				Title:   s.Summary.Name,
				Content: b.String(),
				Docs:    len(s.Docs),
			}, nil
		},
		function.WithName("skill_load"),
		function.WithDescription("Load a skill's usage summary and key parameters. Returns name, description, and upto first ~2500 chars of body with section-aware truncation. Use after skill_search finds a match. For full content, read the skill file via command."),
	)
}

// NewMCPDiscoverTool creates a tool that discovers available MCP tools.
func NewMCPDiscoverTool(toolSets []tool.ToolSet) tool.Tool {
	return function.NewFunctionTool(
		func(ctx context.Context, args mcpDiscoverArgs) (mcpDiscoverResult, error) {
			results := discoverMCPTools(ctx, toolSets, args.Query)

			var tools []mcpToolInfo
			for _, r := range results {
				tools = append(tools, mcpToolInfo{
					Name:        r.Title,
					Description: r.Content,
					Source:      r.Source,
				})
			}

			return mcpDiscoverResult{
				Tools: tools,
				Count: len(tools),
			}, nil
		},
		function.WithName("mcp_discover"),
		function.WithDescription("Discover available MCP tools matching a query"),
	)
}

// NewMemoryQueryTool creates a tool that queries historical knowledge from memory.
func NewMemoryQueryTool(memStore tagenttool.MemoryStoreAccessor) tool.Tool {
	return function.NewFunctionTool(
		func(ctx context.Context, args memoryQueryArgs) (memoryQueryResult, error) {
			results := queryHistoricalKnowledge(memStore, args.Query)
			return memoryQueryResult{
				Results: results,
				Count:   len(results),
			}, nil
		},
		function.WithName("memory_query"),
		function.WithDescription("Query historical knowledge events from memory to avoid redundant searches"),
	)
}

// ==================== Sub-tool argument/result types ====================

type skillSearchArgs struct {
	Query string `json:"query"`
}

type skillSearchResult struct {
	Results []KnowledgeResult `json:"results"`
	Count   int               `json:"count"`
}

type skillLoadArgs struct {
	SkillName string `json:"skill_name"`
}

type skillLoadResult struct {
	Type    string `json:"type"`
	Title   string `json:"title"`
	Content string `json:"content"`
	Docs    int    `json:"docs"`
}

type mcpDiscoverArgs struct {
	Query string `json:"query"`
}

type mcpDiscoverResult struct {
	Tools []mcpToolInfo `json:"tools"`
	Count int           `json:"count"`
}

type mcpToolInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"`
}

type memoryQueryArgs struct {
	Query string `json:"query"`
}

type memoryQueryResult struct {
	Results []KnowledgeResult `json:"results"`
	Count   int               `json:"count"`
}

// ==================== Shared search implementations ====================

// searchSkills searches for matching skills in the repository.
func searchSkills(repo tagenttool.SkillRepository, query string) []KnowledgeResult {
	summaries := repo.Summaries()
	queryLower := strings.ToLower(query)

	var results []KnowledgeResult
	for _, s := range summaries {
		nameLower := strings.ToLower(s.Name)
		descLower := strings.ToLower(s.Description)

		found := false
		if strings.Contains(descLower, queryLower) || strings.Contains(nameLower, queryLower) {
			found = true
		}
		if !found && strings.Contains(queryLower, nameLower) {
			found = true
		}
		if !found {
			queryRunes := []rune(queryLower)
			for i := 0; i < len(queryRunes); i++ {
				for j := i + 2; j <= len(queryRunes) && j <= i+10; j++ {
					substr := string(queryRunes[i:j])
					if strings.Contains(descLower, substr) {
						found = true
						break
					}
				}
				if found {
					break
				}
			}
		}

		if found {
			results = append(results, KnowledgeResult{
				Type:    "skill",
				Title:   s.Name,
				Content: s.Description,
			})
		}
	}

	return results
}

// discoverMCPTools discovers MCP tools matching the query.
func discoverMCPTools(ctx context.Context, toolSets []tool.ToolSet, query string) []KnowledgeResult {
	queryLower := strings.ToLower(query)
	var results []KnowledgeResult

	for _, ts := range toolSets {
		tools := ts.Tools(ctx)
		for _, tl := range tools {
			decl := tl.Declaration()
			if decl == nil {
				continue
			}
			nameLower := strings.ToLower(decl.Name)
			descLower := strings.ToLower(decl.Description)

			if strings.Contains(nameLower, queryLower) ||
				strings.Contains(descLower, queryLower) ||
				strings.Contains(queryLower, nameLower) {

				schemaInfo := ""
				if decl.InputSchema != nil {
					schemaBytes, _ := json.Marshal(decl.InputSchema)
					schemaInfo = string(schemaBytes)
				}

				content := fmt.Sprintf("MCP tool '%s': %s\n\nCallable via command(mode=\"exec\", command=...).",
					decl.Name, decl.Description)
				if schemaInfo != "" {
					content += fmt.Sprintf("\n\nInput Schema: %s", schemaInfo)
				}

				results = append(results, KnowledgeResult{
					Type:    "mcp_tool",
					Title:   decl.Name,
					Content: content,
					Source:  fmt.Sprintf("mcp:%s", ts.Name()),
				})
			}
		}
	}

	return results
}

// queryHistoricalKnowledge queries historical knowledge events from memory.
func queryHistoricalKnowledge(memStore tagenttool.MemoryStoreAccessor, query string) []KnowledgeResult {
	opts := memory.QueryOptions{
		Limit:   10,
		OrderBy: "timestamp_desc",
	}

	events, err := memStore.QueryEvents(opts)
	if err != nil {
		return nil
	}

	queryLower := strings.ToLower(query)
	var results []KnowledgeResult
	for _, evt := range events {
		if strings.Contains(strings.ToLower(evt.EventSummary), queryLower) {
			results = append(results, KnowledgeResult{
				Type:    "historical_memory",
				Title:   evt.EventType,
				Content: evt.EventSummary,
				Source:  "memory",
			})
		}
	}

	return results
}
