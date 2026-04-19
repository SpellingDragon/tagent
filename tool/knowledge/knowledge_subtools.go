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

	// Web search: always available via duckduckgo
	tools = append(tools, duckduckgo.NewTool())

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
		function.WithDescription("Search local skill repository for matching skills"),
	)
}

// NewSkillLoadTool creates a tool that loads full skill content by name.
func NewSkillLoadTool(repo tagenttool.SkillRepository) tool.Tool {
	return function.NewFunctionTool(
		func(ctx context.Context, args skillLoadArgs) (skillLoadResult, error) {
			s, err := repo.Get(args.SkillName)
			if err != nil {
				return skillLoadResult{}, fmt.Errorf("skill not found: %s", args.SkillName)
			}

			var contentBuilder strings.Builder
			contentBuilder.WriteString(s.Body)

			if len(s.Docs) > 0 {
				contentBuilder.WriteString("\n\n## Related Docs\n")
				for _, doc := range s.Docs {
					contentBuilder.WriteString(fmt.Sprintf("- [%s]\n", doc.Path))
					if doc.Content != "" {
						cap := 500
						docContent := doc.Content
						if len(docContent) > cap {
							docContent = docContent[:cap] + "..."
						}
						contentBuilder.WriteString(fmt.Sprintf("  %s\n", docContent))
					}
				}
			}

			return skillLoadResult{
				Type:    "skill_content",
				Title:   s.Summary.Name,
				Content: contentBuilder.String(),
				Docs:    len(s.Docs),
			}, nil
		},
		function.WithName("skill_load"),
		function.WithDescription("Load full content of a skill by name. Must be called after skill_search finds a matching skill."),
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
