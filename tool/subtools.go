package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"

	"github.com/SpellingDragon/tagent/memory"
)

// NewSkillSearchTool creates a tool that searches the skill repository.
func NewSkillSearchTool(repo SkillRepository) tool.Tool {
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
func NewSkillLoadTool(repo SkillRepository) tool.Tool {
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
func NewMemoryQueryTool(memStore MemoryStoreAccessor) tool.Tool {
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
	Query string `json:"query" description:"Search query for skills"`
}

type skillSearchResult struct {
	Results []KnowledgeResult `json:"results"`
	Count   int               `json:"count"`
}

type skillLoadArgs struct {
	SkillName string `json:"skill_name" description:"Name of the skill to load"`
}

type skillLoadResult struct {
	Type    string `json:"type"`
	Title   string `json:"title"`
	Content string `json:"content"`
	Docs    int    `json:"docs"`
}

type mcpDiscoverArgs struct {
	Query string `json:"query" description:"Search query for MCP tools"`
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
	Query string `json:"query" description:"Query for historical knowledge"`
}

type memoryQueryResult struct {
	Results []KnowledgeResult `json:"results"`
	Count   int               `json:"count"`
}

// ==================== Shared search implementations ====================

// searchSkills searches for matching skills in the repository.
// Uses bidirectional matching: skill name/description contains query, or query contains skill name.
func searchSkills(repo SkillRepository, query string) []KnowledgeResult {
	summaries := repo.Summaries()
	queryLower := strings.ToLower(query)

	var results []KnowledgeResult
	for _, s := range summaries {
		nameLower := strings.ToLower(s.Name)
		descLower := strings.ToLower(s.Description)

		found := false
		// Check 1: skill name/description contains query (short query scenario)
		if strings.Contains(descLower, queryLower) || strings.Contains(nameLower, queryLower) {
			found = true
		}
		// Check 2: query contains skill name (long query scenario)
		if !found && strings.Contains(queryLower, nameLower) {
			found = true
		}
		// Check 3: substring matching for CJK queries
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
func queryHistoricalKnowledge(memStore MemoryStoreAccessor, query string) []KnowledgeResult {
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
