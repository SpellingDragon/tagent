// Package knowledge provides tools for the Knowledge Agent (skill search + web search + MCP discovery).
//
// websearch.go implements the web_search tool backed by the Zhipu Web Search
// API (https://open.bigmodel.cn/api/paas/v4/web_search). Compared with the
// former multi-engine HTML-scraping implementation, the API is purpose-built
// for LLM consumption: it returns structured results (title / link / content /
// media / publish_date) plus intent recognition, and is far more robust than
// scraping engine HTML that changes without notice.
//
// Authentication: the API key is read from an environment variable
// (configurable via the tool's `api_key_env` property, default ZAI_API_KEY —
// the same key used by the zhipu model provider).
package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

// SearchResult represents a single search result.
type SearchResult struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	URL         string `json:"url"`
	Source      string `json:"source"` // Media / source name (e.g. "搜狐")
}

// SearchResponse represents the search response.
type SearchResponse struct {
	Results []SearchResult `json:"results"`
	Engine  string         `json:"engine"`
	Query   string         `json:"query"`
	Message string         `json:"message,omitempty"`
}

// WebSearchConfig configures the Zhipu-backed web_search tool.
type WebSearchConfig struct {
	// Endpoint is the Zhipu Web Search API URL.
	Endpoint string
	// APIKeyEnv is the environment variable holding the Zhipu API key.
	APIKeyEnv string
	// SearchEngine selects the Zhipu search engine (e.g. "search_std", "search_pro").
	SearchEngine string
	// Count is the number of results to request (Zhipu accepts 1-50).
	Count int
}

// DefaultWebSearchConfig returns the default configuration, using the public
// Zhipu Web Search endpoint and the ZAI_API_KEY env var shared with the zhipu
// model provider.
func DefaultWebSearchConfig() WebSearchConfig {
	return WebSearchConfig{
		Endpoint:     "https://open.bigmodel.cn/api/paas/v4/web_search",
		APIKeyEnv:    "ZAI_API_KEY",
		SearchEngine: "search_std",
		Count:        10,
	}
}

// webSearchTool wraps the Zhipu web search call as a CallableTool.
type webSearchTool struct {
	cfg        WebSearchConfig
	httpClient *http.Client
}

// NewWebSearchTool creates a web_search tool with the default configuration.
func NewWebSearchTool() tool.CallableTool {
	return NewWebSearchToolWithConfig(DefaultWebSearchConfig())
}

// NewWebSearchToolWithConfig creates a web_search tool with the given config.
func NewWebSearchToolWithConfig(cfg WebSearchConfig) tool.CallableTool {
	if cfg.Endpoint == "" {
		cfg.Endpoint = DefaultWebSearchConfig().Endpoint
	}
	if cfg.APIKeyEnv == "" {
		cfg.APIKeyEnv = DefaultWebSearchConfig().APIKeyEnv
	}
	if cfg.SearchEngine == "" {
		cfg.SearchEngine = DefaultWebSearchConfig().SearchEngine
	}
	if cfg.Count <= 0 {
		cfg.Count = DefaultWebSearchConfig().Count
	}
	t := &webSearchTool{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
	return function.NewFunctionTool(
		t.search,
		function.WithName("web_search"),
		function.WithDescription(
			"Search the web using the Zhipu Web Search API. "+
				"Returns titles, URLs, snippets, sources and publish dates. "+
				"Best for: current events, news, tutorials, documentation, and general web content. "+
				"This is the primary and most reliable web search tool; prefer it over duckduckgo_search.",
		),
	)
}

// searchRequest represents the input for the web search tool.
type searchRequest struct {
	Query string `json:"query" jsonschema:"description=The search query,required"`
}

// zhipuSearchRequest is the request body for the Zhipu Web Search API.
type zhipuSearchRequest struct {
	SearchQuery         string `json:"search_query"`
	SearchEngine        string `json:"search_engine"`
	SearchIntent        bool   `json:"search_intent"`
	Count               int    `json:"count"`
	SearchDomainFilter  string `json:"search_domain_filter,omitempty"`
	SearchRecencyFilter string `json:"search_recency_filter"`
	RequestID           string `json:"request_id,omitempty"`
	UserID              string `json:"user_id,omitempty"`
}

// zhipuSearchResultItem is a single entry in the Zhipu search_result array.
type zhipuSearchResultItem struct {
	Content     string `json:"content"`
	Icon        string `json:"icon"`
	Link        string `json:"link"`
	Media       string `json:"media"`
	PublishDate string `json:"publish_date"`
	Refer       string `json:"refer"`
	Title       string `json:"title"`
}

// zhipuSearchResponse is the Zhipu Web Search API response body.
type zhipuSearchResponse struct {
	Created      int64                   `json:"created"`
	ID           string                  `json:"id"`
	RequestID    string                  `json:"request_id"`
	SearchIntent []zhipuSearchIntentItem `json:"search_intent"`
	SearchResult []zhipuSearchResultItem `json:"search_result"`
}

// zhipuSearchIntentItem is an entry in the search_intent array.
type zhipuSearchIntentItem struct {
	Intent   string `json:"intent"`
	Keywords string `json:"keywords"`
	Query    string `json:"query"`
}

// zhipuAPIError captures the error shape Zhipu returns on failure.
type zhipuAPIError struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// search performs the Zhipu web search.
func (t *webSearchTool) search(ctx context.Context, req searchRequest) (SearchResponse, error) {
	query := strings.TrimSpace(req.Query)
	if query == "" {
		return SearchResponse{Engine: "zhipu", Message: "empty query"}, nil
	}

	apiKey := os.Getenv(t.cfg.APIKeyEnv)
	if apiKey == "" {
		return SearchResponse{
			Engine:  "zhipu",
			Query:   query,
			Message: fmt.Sprintf("web_search unavailable: %s environment variable is not set", t.cfg.APIKeyEnv),
		}, nil
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	results, msg, err := t.callZhipu(ctx, query, apiKey)
	if err != nil {
		return SearchResponse{Engine: "zhipu", Query: query, Message: msg}, nil
	}

	log.Debugf("[web_search] zhipu returned %d results for %q", len(results), query)
	return SearchResponse{Results: results, Engine: "zhipu", Query: query}, nil
}

// callZhipu issues the HTTP request and maps results. On failure it returns a
// human-readable message alongside the error.
func (t *webSearchTool) callZhipu(ctx context.Context, query, apiKey string) ([]SearchResult, string, error) {
	count := t.cfg.Count
	if count < 1 {
		count = 1
	}
	if count > 50 {
		count = 50
	}

	body := zhipuSearchRequest{
		SearchQuery:         query,
		SearchEngine:        t.cfg.SearchEngine,
		SearchIntent:        false,
		Count:               count,
		SearchRecencyFilter: "noLimit",
		RequestID:           uuid.NewString(),
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Sprintf("marshal request: %v", err), err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, t.cfg.Endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Sprintf("build request: %v", err), err
	}
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := t.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Sprintf("request failed: %v", err), err
	}
	defer resp.Body.Close()

	// 无需在此限制读取大小：框架的 OutputLimitTool 已对所有工具的超大返回
	// 输出自动转储为文件；且 Zhipu API 响应受 count≤50 约束、httpClient 30s
	// 超时也已约束读取量。
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Sprintf("read response: %v", err), err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Sprintf("zhipu web_search HTTP %d: %s", resp.StatusCode, errorSnippet(raw)), nil
	}

	// Zhipu signals API-level errors with an error object even on some statuses.
	var apiErr zhipuAPIError
	if err := json.Unmarshal(raw, &apiErr); err == nil && apiErr.Error.Code != "" {
		return nil, fmt.Sprintf("zhipu web_search error %s: %s", apiErr.Error.Code, apiErr.Error.Message), nil
	}

	var parsed zhipuSearchResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Sprintf("parse response: %v", err), err
	}

	results := make([]SearchResult, 0, len(parsed.SearchResult))
	for _, item := range parsed.SearchResult {
		if item.Title == "" && item.Link == "" {
			continue
		}
		results = append(results, SearchResult{
			Title:       strings.TrimSpace(item.Title),
			Description: buildDescription(item),
			URL:         item.Link,
			Source:      sourceName(item),
		})
	}
	return results, "", nil
}

// buildDescription composes the snippet, appending publish date for recency.
func buildDescription(item zhipuSearchResultItem) string {
	desc := strings.TrimSpace(item.Content)
	if item.PublishDate != "" {
		if desc != "" {
			desc += " "
		}
		desc += "（发布于 " + item.PublishDate + "）"
	}
	return desc
}

// sourceName prefers the media name, falling back to a stable label.
func sourceName(item zhipuSearchResultItem) string {
	if item.Media != "" {
		return item.Media
	}
	return "zhipu"
}

// errorSnippet returns a short excerpt of an error body for messages.
func errorSnippet(raw []byte) string {
	s := strings.TrimSpace(string(raw))
	if len(s) > 300 {
		s = s[:300] + "..."
	}
	return s
}
