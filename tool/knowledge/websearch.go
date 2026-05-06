// Package knowledge provides tools for the Knowledge Agent (skill search + web search + MCP discovery).
//
// websearch.go contains the multi-engine HTML web search implementation,
// formerly in tool/websearch/. Moved here as it is exclusively used as a
// Knowledge Agent subtool.
//
// Supported engines:
//   - DuckDuckGo HTML (global, privacy-focused)
//   - Bing (global + CN)
//   - Baidu (CN)
//   - Brave (global, privacy-focused)
//
// Region auto-detection: queries containing CJK characters default to CN engines,
// otherwise global engines are used.
package knowledge

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

	"golang.org/x/net/html"

	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

// SearchEngine defines a search engine configuration.
type SearchEngine struct {
	Name   string // Engine name, e.g., "baidu", "duckduckgo"
	URL    string // URL template with {keyword} placeholder
	Region string // "cn" or "global"
}

// SearchResult represents a single search result.
type SearchResult struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	URL         string `json:"url"`
	Source      string `json:"source"` // Engine name
}

// SearchResponse represents the search response.
type SearchResponse struct {
	Results []SearchResult `json:"results"`
	Engine  string         `json:"engine"`
	Query   string         `json:"query"`
	Message string         `json:"message,omitempty"`
}

// defaultEngines are the built-in search engine configurations.
var defaultEngines = []SearchEngine{
	// Chinese domestic engines
	{Name: "bing_cn", URL: "https://cn.bing.com/search?q={keyword}", Region: "cn"},
	{Name: "baidu", URL: "https://www.baidu.com/s?wd={keyword}", Region: "cn"},
	// Global engines
	{Name: "duckduckgo", URL: "https://html.duckduckgo.com/html/?q={keyword}", Region: "global"},
	{Name: "bing", URL: "https://www.bing.com/search?q={keyword}", Region: "global"},
	{Name: "brave", URL: "https://search.brave.com/search?q={keyword}", Region: "global"},
}

// webSearchTool wraps the multi-engine search logic as a CallableTool.
type webSearchTool struct {
	engines    []SearchEngine
	httpClient *http.Client
}

// NewWebSearchTool creates a new web_search tool with built-in engine configurations.
func NewWebSearchTool() tool.CallableTool {
	t := &webSearchTool{
		engines: defaultEngines,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
	return function.NewFunctionTool(
		t.search,
		function.WithName("web_search"),
		function.WithDescription(
			"Search the web using multiple search engines. "+
				"Returns titles, URLs, and snippets. "+
				"Automatically selects the best engine based on query language: "+
				"CJK queries use Bing CN or Baidu; English/global queries use DuckDuckGo, Bing, or Brave. "+
				"Best for: current events, tutorials, documentation, news, and general web content. "+
				"For factual/encyclopedic info (definitions, entity details), prefer duckduckgo_search instead.",
		),
	)
}

// searchRequest represents the input for the web search tool.
type searchRequest struct {
	Query string `json:"query" jsonschema:"description=The search query,required"`
}

// search performs the actual search operation.
func (t *webSearchTool) search(ctx context.Context, req searchRequest) (SearchResponse, error) {
	query := strings.TrimSpace(req.Query)
	if query == "" {
		return SearchResponse{Message: "empty query"}, nil
	}

	// Apply 30s timeout for the entire search operation
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	region := detectRegion(query)
	engine := t.selectEngine(region)
	searchURL := buildSearchURL(engine, query)

	log.Debugf("[web_search] query=%q region=%s engine=%s", query, region, engine.Name)

	results, err := t.fetchAndParse(ctx, searchURL, engine.Name)
	if err != nil {
		return SearchResponse{
			Engine:  engine.Name,
			Query:   query,
			Message: fmt.Sprintf("search failed: %v", err),
		}, nil
	}

	log.Debugf("[web_search] found %d results via %s", len(results), engine.Name)

	return SearchResponse{
		Results: results,
		Engine:  engine.Name,
		Query:   query,
	}, nil
}

// detectRegion detects the region based on query language.
func detectRegion(query string) string {
	for _, r := range query {
		if unicode.Is(unicode.Han, r) {
			return "cn"
		}
	}
	return "global"
}

// selectEngine selects the first engine matching the region.
func (t *webSearchTool) selectEngine(region string) SearchEngine {
	for _, e := range t.engines {
		if e.Region == region {
			return e
		}
	}
	// Fallback to first global engine
	for _, e := range t.engines {
		if e.Region == "global" {
			return e
		}
	}
	return t.engines[0]
}

// buildSearchURL builds the search URL by replacing {keyword} placeholder.
func buildSearchURL(engine SearchEngine, keyword string) string {
	return strings.Replace(engine.URL, "{keyword}", url.QueryEscape(keyword), 1)
}

// fetchAndParse fetches the search page and parses results.
func (t *webSearchTool) fetchAndParse(ctx context.Context, searchURL string, engineName string) ([]SearchResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, err
	}

	// Set common headers to mimic browser
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9,zh-CN;q=0.8,zh;q=0.7")

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	const maxBodySize = 1 * 1024 * 1024 // 1MB

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodySize+1))
	if err != nil {
		return nil, err
	}

	truncated := false
	if len(body) > maxBodySize {
		body = body[:maxBodySize]
		truncated = true
	}

	var results []SearchResult
	switch engineName {
	case "duckduckgo":
		results, err = parseDuckDuckGo(body)
	case "bing", "bing_cn":
		results, err = parseBing(body)
	case "baidu":
		results, err = parseBaidu(body)
	default:
		results, err = parseGeneric(body)
	}

	if truncated && err == nil {
		results = append(results, SearchResult{
			Title:       "[truncated at 1MB]",
			Description: "Response body was truncated at 1MB, search results may be incomplete",
			Source:      engineName,
		})
	}

	return results, err
}

// ==================== HTML Parsers ====================

// parseDuckDuckGo parses DuckDuckGo HTML search results.
func parseDuckDuckGo(body []byte) ([]SearchResult, error) {
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}

	var results []SearchResult
	var f func(*html.Node)
	f = func(n *html.Node) {
		if len(results) >= 10 {
			return
		}
		if n.Type == html.ElementNode && n.Data == "a" {
			var hasResultClass bool
			var href string
			for _, attr := range n.Attr {
				if attr.Key == "class" && strings.Contains(attr.Val, "result__a") {
					hasResultClass = true
				}
				if attr.Key == "href" {
					href = attr.Val
				}
			}
			if hasResultClass && href != "" {
				title := extractText(n)
				description := findSnippet(n.Parent)
				actualURL := parseDDGURL(href)
				if title != "" && actualURL != "" {
					results = append(results, SearchResult{
						Title:       strings.TrimSpace(title),
						Description: strings.TrimSpace(description),
						URL:         actualURL,
						Source:      "duckduckgo",
					})
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(doc)
	return results, nil
}

// parseBing parses Bing search results.
func parseBing(body []byte) ([]SearchResult, error) {
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}

	var results []SearchResult
	var f func(*html.Node)
	f = func(n *html.Node) {
		if len(results) >= 10 {
			return
		}
		if n.Type == html.ElementNode && n.Data == "li" {
			var isBAlgo bool
			for _, attr := range n.Attr {
				if attr.Key == "class" && strings.Contains(attr.Val, "b_algo") {
					isBAlgo = true
					break
				}
			}
			if isBAlgo {
				result := extractBingResult(n)
				if result.Title != "" && result.URL != "" {
					results = append(results, result)
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(doc)
	return results, nil
}

// extractBingResult extracts title, URL, and description from a Bing result node.
func extractBingResult(n *html.Node) SearchResult {
	var result SearchResult
	result.Source = "bing"
	var f func(*html.Node)
	f = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "h2" {
			for c := node.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == html.ElementNode && c.Data == "a" {
					result.Title = extractText(c)
					for _, attr := range c.Attr {
						if attr.Key == "href" {
							result.URL = attr.Val
							break
						}
					}
					break
				}
			}
		}
		if node.Type == html.ElementNode && (node.Data == "p" || node.Data == "div") {
			for _, attr := range node.Attr {
				if attr.Key == "class" && (strings.Contains(attr.Val, "b_caption") || strings.Contains(attr.Val, "b_lineclamp")) {
					if result.Description == "" {
						result.Description = extractText(node)
					}
					break
				}
			}
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(n)
	return result
}

// parseBaidu parses Baidu search results.
func parseBaidu(body []byte) ([]SearchResult, error) {
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}

	var results []SearchResult
	var f func(*html.Node)
	f = func(n *html.Node) {
		if len(results) >= 10 {
			return
		}
		if n.Type == html.ElementNode && n.Data == "div" {
			var isResult bool
			for _, attr := range n.Attr {
				if attr.Key == "class" && (strings.Contains(attr.Val, "result ") || strings.Contains(attr.Val, "c-container")) {
					isResult = true
					break
				}
			}
			if isResult {
				result := extractBaiduResult(n)
				if result.Title != "" && result.URL != "" {
					results = append(results, result)
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(doc)
	return results, nil
}

// extractBaiduResult extracts result from a Baidu result node.
func extractBaiduResult(n *html.Node) SearchResult {
	var result SearchResult
	result.Source = "baidu"
	var f func(*html.Node)
	f = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "h3" {
			for c := node.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == html.ElementNode && c.Data == "a" {
					result.Title = extractText(c)
					for _, attr := range c.Attr {
						if attr.Key == "href" {
							result.URL = attr.Val
							break
						}
					}
					break
				}
			}
		}
		if node.Type == html.ElementNode && (node.Data == "span" || node.Data == "div") {
			for _, attr := range node.Attr {
				if attr.Key == "class" && strings.Contains(attr.Val, "content") {
					if result.Description == "" {
						result.Description = extractText(node)
					}
					break
				}
			}
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(n)
	return result
}

// parseGeneric provides a generic HTML parser for other search engines.
func parseGeneric(body []byte) ([]SearchResult, error) {
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}

	var results []SearchResult
	seenURLs := make(map[string]bool)

	var f func(*html.Node)
	f = func(n *html.Node) {
		if len(results) >= 10 {
			return
		}
		if n.Type == html.ElementNode && n.Data == "a" {
			var href string
			for _, attr := range n.Attr {
				if attr.Key == "href" {
					href = attr.Val
					break
				}
			}
			if isSearchResultLink(href) && !seenURLs[href] {
				title := extractText(n)
				if title != "" && len(title) > 5 {
					seenURLs[href] = true
					results = append(results, SearchResult{
						Title:  strings.TrimSpace(title),
						URL:    href,
						Source: "generic",
					})
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(doc)
	return results, nil
}

// ==================== HTML Helper Functions ====================

// extractText extracts all text content from a node.
func extractText(n *html.Node) string {
	var sb strings.Builder
	var f func(*html.Node)
	f = func(node *html.Node) {
		if node.Type == html.TextNode {
			sb.WriteString(node.Data)
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(n)
	return strings.TrimSpace(sb.String())
}

// findSnippet finds the snippet/description text near a result link.
func findSnippet(parent *html.Node) string {
	if parent == nil {
		return ""
	}
	var f func(*html.Node) string
	f = func(n *html.Node) string {
		if n.Type == html.ElementNode {
			for _, attr := range n.Attr {
				if attr.Key == "class" && strings.Contains(attr.Val, "result__snippet") {
					return extractText(n)
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if result := f(c); result != "" {
				return result
			}
		}
		return ""
	}
	for p := parent; p != nil; p = p.Parent {
		if result := f(p); result != "" {
			return result
		}
		if p.Data == "body" || p.Data == "html" {
			break
		}
	}
	return ""
}

// parseDDGURL parses the actual URL from DuckDuckGo redirect URL.
func parseDDGURL(ddgURL string) string {
	if strings.Contains(ddgURL, "uddg=") {
		parts := strings.Split(ddgURL, "uddg=")
		if len(parts) >= 2 {
			decoded, err := url.QueryUnescape(parts[1])
			if err == nil {
				if idx := strings.Index(decoded, "&"); idx > 0 {
					decoded = decoded[:idx]
				}
				return decoded
			}
		}
	}
	if strings.HasPrefix(ddgURL, "http://") || strings.HasPrefix(ddgURL, "https://") {
		return ddgURL
	}
	return ""
}

// isSearchResultLink checks if a URL looks like a search result rather than navigation.
func isSearchResultLink(href string) bool {
	if href == "" || href == "#" {
		return false
	}
	if !strings.HasPrefix(href, "http://") && !strings.HasPrefix(href, "https://") {
		return false
	}
	excludePatterns := []string{
		"google.com/search",
		"bing.com/search",
		"duckduckgo.com",
		"baidu.com/s?",
		"/support",
		"/help",
		"/settings",
		"/preferences",
		"/advanced_search",
	}
	for _, pattern := range excludePatterns {
		if strings.Contains(href, pattern) {
			return false
		}
	}
	return true
}
