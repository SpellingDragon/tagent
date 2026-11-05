// 契约: docs/wiki/rl/rl-architecture.md#live-benchmark
package main

// request is one task envelope read from stdin.
type request struct {
	Protocol  string   `json:"protocol"`
	TaskID    string   `json:"task_id"`
	Prompts   []string `json:"prompts"`
	Workspace string   `json:"workspace"`
}

// response is the result envelope written to stdout.
type response struct {
	Protocol         string   `json:"protocol"`
	Runner           string   `json:"runner"`
	TaskID           string   `json:"task_id"`
	Status           string   `json:"status"`
	Output           string   `json:"output"`
	Delivered        bool     `json:"delivered"`
	APICalls         int      `json:"api_calls"`
	PromptTokens     int      `json:"prompt_tokens"`
	CompletionTokens int      `json:"completion_tokens"`
	FirstTokenMS     float64  `json:"first_token_ms"`
	CompleteMS       float64  `json:"complete_ms"`
	Hot              []sample `json:"hot"`
	ApprovalCalls    int      `json:"approval_calls"`
	BlockedReports   int      `json:"blocked_reports"`
	Error            string   `json:"error"`
}

// sample is one model request timing.
type sample struct {
	FirstTokenMS float64 `json:"first_token_ms"`
	CompleteMS   float64 `json:"complete_ms"`
}

// describeResponse is the tool face the bridge advertises.
type describeResponse struct {
	Protocol     string              `json:"protocol"`
	Runner       string              `json:"runner"`
	Tools        []string            `json:"tools"`
	Capabilities map[string][]string `json:"capabilities"`
}
