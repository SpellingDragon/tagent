package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/duckduckgo"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/memory"
	tagenttool "github.com/SpellingDragon/tagent/tool"
)

// KnowledgeResult represents a single piece of acquired knowledge.
type KnowledgeResult struct {
	// Type "skill", "skill_content", "web", "mcp_tool", "historical_memory"
	Type string `json:"type"`
	// Title Human-readable title
	Title string `json:"title"`
	// Content Knowledge content
	Content string `json:"content"`
	// Source identifier
	Source string `json:"source,omitempty"`
	// ExecutionPlan Translated executable plan
	ExecutionPlan *ExecutionPlan `json:"execution_plan,omitempty"`
}

// ExecutionPlan describes a physical execution plan that ActionTool can directly run.
type ExecutionPlan struct {
	// Function "exec", "tmux_exec", "mcp_call"
	Function string `json:"function"`
	// Command for exec/tmux_exec
	Command string `json:"command,omitempty"`
	// MCPTool MCP tool name for mcp_call
	MCPTool string `json:"mcp_tool,omitempty"`
	// MCPArgs MCP tool arguments for mcp_call
	MCPArgs map[string]any `json:"mcp_args,omitempty"`
	// Environment variables
	Env map[string]string `json:"env,omitempty"`
	// Dir Working directory
	Dir string `json:"dir,omitempty"`
	// Timeout in seconds
	Timeout int `json:"timeout,omitempty"`
	// Description Human-readable description
	Description string `json:"description,omitempty"`
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

	tools = append(tools, duckduckgo.NewTool())
	tools = append(tools, NewWebSearchTool())

	if cfg.MemStore != nil {
		tools = append(tools, NewMemoryQueryTool(cfg.MemStore, cfg.ReadPartitionIDs))
	}

	return tools
}

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
// - Level 1 (skill_search): name + description from YAML front matter
// - Level 2 (skill_load): name + description + usage summary (up to ~2500 chars)
// - Level 3 (command): read full skill file when deeper detail is needed
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

			skillFilePath := "skills/" + args.SkillName + "/SKILL.md"
			if pathProvider, ok := interface{}(repo).(interface{ Path(string) (string, error) }); ok {
				if dir, err := pathProvider.Path(args.SkillName); err == nil && dir != "" {
					skillFilePath = dir + "/SKILL.md"
				}
			}

			var b strings.Builder

			b.WriteString("**[Skill]** ")
			b.WriteString(s.Summary.Name)
			b.WriteString("\n")
			if s.Summary.Description != "" {
				b.WriteString(s.Summary.Description)
				b.WriteString("\n")
			}
			b.WriteString("\n")

			body := s.Body
			if len(body) > maxBodyChars {
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

// NewMCPDiscoverTool creates a tool that discovers available MCP tools
// from a static toolset slice. Kept for the BuildSubTools compatibility
// path; the config-driven path uses NewMCPDiscoverToolWithRegistry.
func NewMCPDiscoverTool(toolSets []tool.ToolSet) tool.Tool {
	return newMCPDiscoverTool(func(context.Context) []tool.ToolSet { return toolSets })
}

// NewMCPDiscoverToolWithRegistry creates a discover tool over the live MCP
// registry: the server set is read at CALL time, so runtime-registered
// servers become discoverable immediately without rebuilding any agent.
func NewMCPDiscoverToolWithRegistry(reg tagenttool.MCPRegistry) tool.Tool {
	return newMCPDiscoverTool(func(context.Context) []tool.ToolSet { return reg.List() })
}

// newMCPDiscoverTool builds the discover tool over a toolset source.
func newMCPDiscoverTool(source func(context.Context) []tool.ToolSet) tool.Tool {
	return function.NewFunctionTool(
		func(ctx context.Context, args mcpDiscoverArgs) (mcpDiscoverResult, error) {
			results := discoverMCPTools(ctx, source(ctx), args.Query)

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
		function.WithDescription("Discover available MCP tools matching a query. Results include the exact mcp_call invocation (server/tool) and the tool's input schema."),
	)
}

// NewMemoryQueryTool creates a tool that queries historical knowledge from memory.
// readPartitionIDs scopes the query to the agent's readable partitions (own
// namespace first + read_namespaces, injected at build time); on partition-
// isolated stores (FileSegmentStore) an empty list would scan nothing.
func NewMemoryQueryTool(memStore tagenttool.MemoryStoreAccessor, readPartitionIDs []int) tool.Tool {
	return function.NewFunctionTool(
		func(ctx context.Context, args memoryQueryArgs) (memoryQueryResult, error) {
			results := queryHistoricalKnowledge(memStore, readPartitionIDs, args.Query)
			return memoryQueryResult{
				Results: results,
				Count:   len(results),
			}, nil
		},
		function.WithName("memory_query"),
		function.WithDescription("Query historical knowledge events from memory to avoid redundant searches"),
	)
}

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

			if !mcpQueryMatches(queryLower, nameLower, descLower) {
				continue
			}

			schemaInfo := ""
			if decl.InputSchema != nil {
				schemaBytes, _ := json.Marshal(decl.InputSchema)
				schemaInfo = string(schemaBytes)
			}

			content := fmt.Sprintf("MCP tool '%s' on server '%s': %s\n\nCallable via mcp_call(server=%q, tool=%q, args={...}).",
				decl.Name, ts.Name(), decl.Description, ts.Name(), decl.Name)
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

	return results
}

// mcpQueryMatches reports whether a lowercased query matches a tool's
// lowercased name/description. Beyond plain substring containment, it falls
// back to token-AND matching (split on space/underscore/hyphen) so natural
// language queries like "web search" match tools named "web_search_prime"
// or described as "Search web information ..." regardless of word order or
// separator style — LLM-issued queries are rarely exact substrings.
func mcpQueryMatches(queryLower, nameLower, descLower string) bool {
	if strings.Contains(nameLower, queryLower) ||
		strings.Contains(descLower, queryLower) ||
		strings.Contains(queryLower, nameLower) {
		return true
	}
	tokens := strings.FieldsFunc(queryLower, func(r rune) bool {
		return r == ' ' || r == '_' || r == '-'
	})
	if len(tokens) == 0 {
		return false
	}
	combined := nameLower + " " + descLower
	for _, tk := range tokens {
		if !strings.Contains(combined, tk) {
			return false
		}
	}
	return true
}

// queryHistoricalKnowledge queries historical knowledge events from memory,
// scoped to the readable partitions (required by partition-isolated stores).
func queryHistoricalKnowledge(memStore tagenttool.MemoryStoreAccessor, readPartitionIDs []int, query string) []KnowledgeResult {
	opts := memory.QueryOptions{
		Limit:   10,
		OrderBy: "timestamp_desc",
		Keyword: query,
	}
	if len(readPartitionIDs) > 0 {
		opts.PartitionIDs = readPartitionIDs
	}

	events, err := memStore.QueryEvents(opts)
	if err != nil {
		return []KnowledgeResult{{
			Type:    "historical_memory",
			Title:   "query_error",
			Content: fmt.Sprintf("memory query failed (relevant history may exist): %v", err),
			Source:  "memory",
		}}
	}

	var results []KnowledgeResult
	for _, evt := range events {
		results = append(results, KnowledgeResult{
			Type:    "historical_memory",
			Title:   evt.EventType,
			Content: evt.EventSummary,
			Source:  "memory",
		})
	}

	return results
}

// RegisterSubTools registers all knowledge sub-tools as plain tools in the
// global tool registry. Called by tagent.RegisterBuiltinTools().
//
// Registered tools:
// - skill_search: search local skill repository
// - skill_load: load skill content with section-aware truncation
// - mcp_discover: discover available MCP tools
// - web_search: HTML scraping for general web content
// - duckduckgo_search: Instant Answer API for factual info
// - memory_query: query historical knowledge from memory
func RegisterSubTools() {
	agent.RegisterPlainTool("skill_search", skillSearchFactory)
	agent.RegisterPlainTool("skill_load", skillLoadFactory)
	agent.RegisterPlainTool("mcp_discover", mcpDiscoverFactory)
	agent.RegisterPlainTool("web_search", webSearchFactory)
	agent.RegisterPlainTool("duckduckgo_search", duckDuckGoSearchFactory)
	agent.RegisterPlainTool("memory_query", memoryQueryFactory)
}

func skillSearchFactory(cfg agent.PlainToolFactoryConfig) (tool.CallableTool, error) {
	if cfg.SkillRepo == nil {
		return nil, fmt.Errorf("skill_search requires SkillRepo (use WithSkillRepo option)")
	}
	return NewSkillSearchTool(cfg.SkillRepo).(tool.CallableTool), nil
}

func skillLoadFactory(cfg agent.PlainToolFactoryConfig) (tool.CallableTool, error) {
	if cfg.SkillRepo == nil {
		return nil, fmt.Errorf("skill_load requires SkillRepo (use WithSkillRepo option)")
	}
	return NewSkillLoadTool(cfg.SkillRepo).(tool.CallableTool), nil
}

func mcpDiscoverFactory(cfg agent.PlainToolFactoryConfig) (tool.CallableTool, error) {
	if cfg.MCPRegistry != nil {
		return NewMCPDiscoverToolWithRegistry(cfg.MCPRegistry).(tool.CallableTool), nil
	}
	if len(cfg.MCPToolSets) == 0 {
		return NewMCPDiscoverTool(nil).(tool.CallableTool), nil
	}
	return NewMCPDiscoverTool(cfg.MCPToolSets).(tool.CallableTool), nil
}

func webSearchFactory(cfg agent.PlainToolFactoryConfig) (tool.CallableTool, error) {
	return NewWebSearchToolWithConfig(webSearchConfigFromProperties(cfg.Properties)), nil
}

// webSearchConfigFromProperties builds a WebSearchConfig from the tool's
// `properties` block, falling back to defaults for any unset field.
// Recognized keys: endpoint, api_key_env, search_engine, count.
func webSearchConfigFromProperties(props map[string]any) WebSearchConfig {
	cfg := DefaultWebSearchConfig()
	if props == nil {
		return cfg
	}
	if v, ok := props["endpoint"].(string); ok && v != "" {
		cfg.Endpoint = v
	}
	if v, ok := props["api_key_env"].(string); ok && v != "" {
		cfg.APIKeyEnv = v
	}
	if v, ok := props["search_engine"].(string); ok && v != "" {
		cfg.SearchEngine = v
	}
	switch v := props["count"].(type) {
	case int:
		cfg.Count = v
	case float64:
		cfg.Count = int(v)
	}
	return cfg
}

func duckDuckGoSearchFactory(cfg agent.PlainToolFactoryConfig) (tool.CallableTool, error) {
	return duckduckgo.NewTool(), nil
}

func memoryQueryFactory(cfg agent.PlainToolFactoryConfig) (tool.CallableTool, error) {
	if cfg.MemStore == nil {
		return nil, fmt.Errorf("memory_query requires MemStore")
	}
	return NewMemoryQueryTool(cfg.MemStore, cfg.ReadPartitionIDs).(tool.CallableTool), nil
}
