// 契约: docs/wiki/rl/rl-architecture.md#live-benchmark
package metrics

import (
	"encoding/json"
	"fmt"
)

// ValidateRunner checks one runner block for the five-metric contract.
func ValidateRunner(raw []byte) error {
	var entry struct {
		Runner  string                     `json:"runner"`
		Status  string                     `json:"status"`
		Caliber string                     `json:"caliber"`
		Metrics map[string]json.RawMessage `json:"metrics"`
	}
	if err := json.Unmarshal(raw, &entry); err != nil {
		return fmt.Errorf("bench metrics: runner entry: %w", err)
	}
	if entry.Runner == "" {
		return fmt.Errorf("bench metrics: runner name is empty")
	}
	if entry.Status != StatusCompleted {
		return fmt.Errorf("bench metrics: runner %s status %q, want completed", entry.Runner, entry.Status)
	}
	if entry.Caliber == "" {
		return fmt.Errorf("bench metrics: runner %s carries no delivery caliber", entry.Runner)
	}
	for _, key := range []string{KeySuccessRate, KeyDeliveryRate, KeyLatency, KeyTokenCost, KeyInterventions} {
		if _, ok := entry.Metrics[key]; !ok {
			return fmt.Errorf("bench metrics: runner %s misses %s", entry.Runner, key)
		}
	}
	var interventions interventionBlock
	if err := json.Unmarshal(entry.Metrics[KeyInterventions], &interventions); err != nil {
		return fmt.Errorf("bench metrics: runner %s interventions: %w", entry.Runner, err)
	}
	if interventions.Breakdown.ApprovalCalls+interventions.Breakdown.BlockedReports != interventions.Total {
		return fmt.Errorf("bench metrics: runner %s intervention breakdown does not sum to total", entry.Runner)
	}
	var latency latencyBlock
	if err := json.Unmarshal(entry.Metrics[KeyLatency], &latency); err != nil {
		return fmt.Errorf("bench metrics: runner %s latency: %w", entry.Runner, err)
	}
	if latency.Cold.Samples == 0 {
		return fmt.Errorf("bench metrics: runner %s has no cold latency sample", entry.Runner)
	}
	return nil
}
