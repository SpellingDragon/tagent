// 契约: docs/wiki/rl/rl-architecture.md#live-benchmark
package metrics

import (
	"encoding/json"
	"fmt"
	"sort"
)

// SchemaVersion names the document shape this package validates.
const SchemaVersion = 1

// KeySuccessRate is the verifier pass-rate key.
const KeySuccessRate = "success_rate"

// KeyDeliveryRate is the sender-confirmed delivery rate key.
const KeyDeliveryRate = "delivery_rate"

// KeyLatency is the cold and hot latency key.
const KeyLatency = "latency"

// KeyTokenCost is the prompt and completion token key.
const KeyTokenCost = "token_cost"

// KeyInterventions is the human intervention key.
const KeyInterventions = "interventions"

// StatusCompleted marks a run that produced all three runner results.
const StatusCompleted = "completed"

// StatusSkipped marks a run that refused to spend quota without a key.
const StatusSkipped = "skipped"

// StatusBlocked marks a run stopped by a missing dependency.
const StatusBlocked = "blocked"

// attempt is one task attempt as the runners record it.
type attempt struct {
	Runner           string   `json:"runner"`
	TaskID           string   `json:"task_id"`
	Category         string   `json:"category"`
	Repeat           int      `json:"repeat"`
	Success          bool     `json:"success"`
	Delivered        bool     `json:"delivered"`
	APICalls         int      `json:"api_calls"`
	PromptTokens     int      `json:"prompt_tokens"`
	CompletionTokens int      `json:"completion_tokens"`
	ApprovalCalls    int      `json:"approval_calls"`
	BlockedReports   int      `json:"blocked_reports"`
	Cold             sample   `json:"cold"`
	Hot              []sample `json:"hot"`
	Error            string   `json:"error"`
	Output           string   `json:"output,omitempty"`
	StartedAt        string   `json:"started_at"`
	FinishedAt       string   `json:"finished_at"`
}

// sample is the first-token and completion timing of one model request.
type sample struct {
	FirstTokenMS float64 `json:"first_token_ms"`
	CompleteMS   float64 `json:"complete_ms"`
}

// runnerMetrics is the five-metric block one runner contributes.
type runnerMetrics struct {
	SuccessRate   rateBlock         `json:"success_rate"`
	DeliveryRate  deliveryBlock     `json:"delivery_rate"`
	Latency       latencyBlock      `json:"latency"`
	TokenCost     tokenBlock        `json:"token_cost"`
	Interventions interventionBlock `json:"interventions"`
}

type rateBlock struct {
	Attempts  int     `json:"attempts"`
	Successes int     `json:"successes"`
	Rate      float64 `json:"rate"`
}

type deliveryBlock struct {
	Eligible  int     `json:"eligible"`
	Confirmed int     `json:"confirmed"`
	Rate      float64 `json:"rate"`
	Caliber   string  `json:"caliber"`
}

type latencyBlock struct {
	Cold latencySample `json:"cold"`
	Hot  latencySample `json:"hot"`
}

type latencySample struct {
	Samples      int     `json:"samples"`
	FirstTokenMS float64 `json:"first_token_ms"`
	CompleteMS   float64 `json:"complete_ms"`
}

type tokenBlock struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type interventionBlock struct {
	Breakdown breakdown `json:"breakdown"`
	Total     int       `json:"total"`
}

type breakdown struct {
	ApprovalCalls  int `json:"approval_calls"`
	BlockedReports int `json:"blocked_reports"`
}

// NewAttempt validates and canonicalizes one attempt record.
func NewAttempt(raw map[string]any) ([]byte, error) {
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("bench metrics: encode attempt: %w", err)
	}
	var value attempt
	if err := json.Unmarshal(encoded, &value); err != nil {
		return nil, fmt.Errorf("bench metrics: attempt shape: %w", err)
	}
	if value.Runner == "" || value.TaskID == "" {
		return nil, fmt.Errorf("bench metrics: attempt runner and task id are required")
	}
	if value.Repeat < 1 {
		return nil, fmt.Errorf("bench metrics: attempt %s/%s repeat must be positive", value.Runner, value.TaskID)
	}
	if value.APICalls < 0 || value.PromptTokens < 0 || value.CompletionTokens < 0 ||
		value.ApprovalCalls < 0 || value.BlockedReports < 0 {
		return nil, fmt.Errorf("bench metrics: attempt %s/%s counts must not be negative", value.Runner, value.TaskID)
	}
	if value.Cold.FirstTokenMS < 0 || value.Cold.CompleteMS < 0 {
		return nil, fmt.Errorf("bench metrics: attempt %s/%s timings must not be negative", value.Runner, value.TaskID)
	}
	return encoded, nil
}

// Aggregate folds one runner's attempts into its five-metric block.
// Latency uses the median across repeats (cold: one sample per attempt; hot: one
// per recorded hot request) rather than the mean, so a single slow outlier does
// not skew the reported first-token and complete latency.
func Aggregate(runner, status, caliber string, attempts [][]byte) ([]byte, error) {
	values := make([]attempt, 0, len(attempts))
	for _, raw := range attempts {
		var value attempt
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, fmt.Errorf("bench metrics: decode attempt for %s: %w", runner, err)
		}
		if value.Runner != runner {
			return nil, fmt.Errorf("bench metrics: attempt of %s passed to aggregate %s", value.Runner, runner)
		}
		values = append(values, value)
	}
	successes, delivered := 0, 0
	block := runnerMetrics{
		DeliveryRate: deliveryBlock{Eligible: len(values), Caliber: caliber},
	}
	coldFirst, coldComplete := make([]float64, 0, len(values)), make([]float64, 0, len(values))
	var hotFirst, hotComplete []float64
	for _, value := range values {
		if value.Success {
			successes++
		}
		if value.Delivered {
			delivered++
		}
		block.TokenCost.PromptTokens += value.PromptTokens
		block.TokenCost.CompletionTokens += value.CompletionTokens
		block.Interventions.Breakdown.ApprovalCalls += value.ApprovalCalls
		block.Interventions.Breakdown.BlockedReports += value.BlockedReports
		coldFirst = append(coldFirst, value.Cold.FirstTokenMS)
		coldComplete = append(coldComplete, value.Cold.CompleteMS)
		for _, hot := range value.Hot {
			hotFirst = append(hotFirst, hot.FirstTokenMS)
			hotComplete = append(hotComplete, hot.CompleteMS)
		}
	}
	block.SuccessRate = rateBlock{Attempts: len(values), Successes: successes}
	if len(values) > 0 {
		block.SuccessRate.Rate = ratio(successes, len(values))
	}
	block.DeliveryRate.Confirmed = delivered
	if len(values) > 0 {
		block.DeliveryRate.Rate = ratio(delivered, len(values))
	}
	block.Latency.Cold = latencySample{Samples: len(values), FirstTokenMS: median(coldFirst), CompleteMS: median(coldComplete)}
	block.Latency.Hot = latencySample{Samples: len(hotFirst), FirstTokenMS: median(hotFirst), CompleteMS: median(hotComplete)}
	block.TokenCost.TotalTokens = block.TokenCost.PromptTokens + block.TokenCost.CompletionTokens
	block.Interventions.Total = block.Interventions.Breakdown.ApprovalCalls + block.Interventions.Breakdown.BlockedReports
	apiCalls := 0
	for _, value := range values {
		apiCalls += value.APICalls
	}
	encoded, err := json.Marshal(map[string]any{
		"runner":    runner,
		"status":    status,
		"caliber":   caliber,
		"attempts":  len(values),
		"api_calls": apiCalls,
		"metrics":   block,
	})
	if err != nil {
		return nil, fmt.Errorf("bench metrics: encode %s: %w", runner, err)
	}
	return encoded, nil
}

// ValidateRun checks one run metrics document for the five-metric contract.
func ValidateRun(raw []byte) error {
	var document struct {
		SchemaVersion int               `json:"schema_version"`
		RunID         string            `json:"run_id"`
		Status        string            `json:"status"`
		Reason        string            `json:"reason"`
		Runners       []json.RawMessage `json:"runners"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		return fmt.Errorf("bench metrics: run document: %w", err)
	}
	if document.SchemaVersion != SchemaVersion {
		return fmt.Errorf("bench metrics: schema_version = %d, want %d", document.SchemaVersion, SchemaVersion)
	}
	if document.RunID == "" {
		return fmt.Errorf("bench metrics: run_id is empty")
	}
	switch document.Status {
	case StatusCompleted, StatusSkipped, StatusBlocked:
	default:
		return fmt.Errorf("bench metrics: status %q is not completed/skipped/blocked", document.Status)
	}
	if document.Status != StatusCompleted {
		if document.Reason == "" {
			return fmt.Errorf("bench metrics: %s run carries no reason", document.Status)
		}
		if len(document.Runners) != 0 {
			return fmt.Errorf("bench metrics: %s run must not carry runner results", document.Status)
		}
		return nil
	}
	if len(document.Runners) != 3 {
		return fmt.Errorf("bench metrics: completed run carries %d runners, want 3", len(document.Runners))
	}
	seen := map[string]bool{}
	for _, runnerRaw := range document.Runners {
		var probe struct {
			Runner string `json:"runner"`
		}
		if err := json.Unmarshal(runnerRaw, &probe); err != nil {
			return fmt.Errorf("bench metrics: runner entry: %w", err)
		}
		if probe.Runner == "" || seen[probe.Runner] {
			return fmt.Errorf("bench metrics: runner %q is empty or duplicated", probe.Runner)
		}
		seen[probe.Runner] = true
		if err := ValidateRunner(runnerRaw); err != nil {
			return err
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	want := []string{"baseline", "dsh", "tagent"}
	for index := range want {
		if names[index] != want[index] {
			return fmt.Errorf("bench metrics: runners = %v, want %v", names, want)
		}
	}
	return nil
}

func ratio(part, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(part) / float64(total)
}

// median returns the middle value of samples (even count: average of the two
// middles). An empty slice yields 0. The input slice is not modified.
func median(samples []float64) float64 {
	if len(samples) == 0 {
		return 0
	}
	sorted := append([]float64(nil), samples...)
	sort.Float64s(sorted)
	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid]
	}
	return (sorted[mid-1] + sorted[mid]) / 2
}
