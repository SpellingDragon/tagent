// 契约: docs/wiki/rl/rl-architecture.md#live-benchmark
package report

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// runnerEntry is one runner block of the metrics document.
type runnerEntry struct {
	Runner   string `json:"runner"`
	Status   string `json:"status"`
	Caliber  string `json:"caliber"`
	Attempts int    `json:"attempts"`
	APICalls int    `json:"api_calls"`
	Metrics  struct {
		SuccessRate struct {
			Attempts  int     `json:"attempts"`
			Successes int     `json:"successes"`
			Rate      float64 `json:"rate"`
		} `json:"success_rate"`
		DeliveryRate struct {
			Eligible  int     `json:"eligible"`
			Confirmed int     `json:"confirmed"`
			Rate      float64 `json:"rate"`
			Caliber   string  `json:"caliber"`
		} `json:"delivery_rate"`
		Latency struct {
			Cold struct {
				Samples      int     `json:"samples"`
				FirstTokenMS float64 `json:"first_token_ms"`
				CompleteMS   float64 `json:"complete_ms"`
			} `json:"cold"`
			Hot struct {
				Samples      int     `json:"samples"`
				FirstTokenMS float64 `json:"first_token_ms"`
				CompleteMS   float64 `json:"complete_ms"`
			} `json:"hot"`
		} `json:"latency"`
		TokenCost struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"token_cost"`
		Interventions struct {
			Breakdown struct {
				ApprovalCalls  int `json:"approval_calls"`
				BlockedReports int `json:"blocked_reports"`
			} `json:"breakdown"`
			Total int `json:"total"`
		} `json:"interventions"`
	} `json:"metrics"`
}

// document is the run metrics surface the report renders.
type document struct {
	RunID   string         `json:"run_id"`
	Status  string         `json:"status"`
	Reason  string         `json:"reason"`
	Model   map[string]any `json:"model"`
	Runners []runnerEntry  `json:"runners"`
}

// Render turns one run metrics document into the comparison report.
func Render(raw []byte) ([]byte, error) {
	var value document
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("bench report: decode metrics: %w", err)
	}
	var out strings.Builder
	out.WriteString("# Live benchmark run " + value.RunID + "\n\n")
	out.WriteString("- status: `" + value.Status + "`\n")
	if value.Reason != "" {
		out.WriteString("- reason: " + value.Reason + "\n")
	}
	if len(value.Model) > 0 {
		out.WriteString("- model: " + renderModel(value.Model) + "\n")
	}
	out.WriteString("\n")
	if value.Status != "completed" {
		out.WriteString("The run carries no runner results; the reason above is the whole record.\n")
		return []byte(out.String()), nil
	}
	out.WriteString("| runner | success | delivery | cold first token (ms) | hot first token (ms) | prompt tok | completion tok | interventions | api calls |\n")
	out.WriteString("| --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
	runners := append([]runnerEntry(nil), value.Runners...)
	sort.Slice(runners, func(i, j int) bool { return runners[i].Runner < runners[j].Runner })
	for _, runner := range runners {
		hot := "-"
		if runner.Metrics.Latency.Hot.Samples > 0 {
			hot = fmt.Sprintf("%.1f", runner.Metrics.Latency.Hot.FirstTokenMS)
		}
		fmt.Fprintf(&out, "| %s | %d/%d (%.1f%%) | %d/%d (%.1f%%) | %.1f | %s | %d | %d | %d | %d |\n",
			runner.Runner,
			runner.Metrics.SuccessRate.Successes, runner.Metrics.SuccessRate.Attempts, 100*runner.Metrics.SuccessRate.Rate,
			runner.Metrics.DeliveryRate.Confirmed, runner.Metrics.DeliveryRate.Eligible, 100*runner.Metrics.DeliveryRate.Rate,
			runner.Metrics.Latency.Cold.FirstTokenMS, hot,
			runner.Metrics.TokenCost.PromptTokens, runner.Metrics.TokenCost.CompletionTokens,
			runner.Metrics.Interventions.Total, runner.APICalls)
	}
	out.WriteString("\n## Calibers\n\n")
	for _, runner := range runners {
		fmt.Fprintf(&out, "- %s: success = verifier pass; delivery = %s; interventions = %d approvals + %d blocked reports.\n",
			runner.Runner, runner.Metrics.DeliveryRate.Caliber,
			runner.Metrics.Interventions.Breakdown.ApprovalCalls, runner.Metrics.Interventions.Breakdown.BlockedReports)
	}
	out.WriteString("\nCold is the task attempt start to the first model token (process and host startup included); hot is the later model requests of the same attempt.\n")
	return []byte(out.String()), nil
}

// renderModel renders the model block deterministically.
func renderModel(model map[string]any) string {
	keys := make([]string, 0, len(model))
	for key := range model {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", key, model[key]))
	}
	return strings.Join(parts, ", ")
}
