// Package acceptance holds the type skeleton for the acceptance scenario
// runner. The runner loads scenario contracts from the golden contract file
// (golden/runtime/scenarios.json), drives the official runtime entry points,
// derives observations from the official API output, and evaluates assertions
// against the expected observations. This skeleton declares types and
// interfaces only and carries no runner logic; the phase-2 implementation
// fills in the
// behavior behind these declarations.
// 规格: docs/wiki/agent/agent-architecture.md#test-support
package acceptance

import "context"

// Scenario is one acceptance scenario contract loaded from
// golden/runtime/scenarios.json. The json tags mirror the golden contract
// fields, and Observations holds the expected observation values that a
// scenario run must derive from the official API output.
type Scenario struct {
	ID           string         `json:"id"`
	Title        string         `json:"title"`
	TaskIDs      []string       `json:"task_ids"`
	TestPackage  string         `json:"test_package"`
	TestName     string         `json:"test_name"`
	Status       string         `json:"status"`
	Scope        string         `json:"scope"`
	AnchorTests  []string       `json:"anchor_tests"`
	Observations map[string]any `json:"observations"`
}

// Step is one ordered driving action that exercises the official runtime entry
// for a scenario. The golden scenario contract does not enumerate steps, so the
// concrete per-scenario step catalog is phase-2 work.
type Step struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Observation is one normalized observation derived from the official API
// output of a scenario run. The runner MUST derive observations from real API
// output instead of copying a scenario's expected values; random ids and
// timestamps may be normalized, but distinct identities stay distinct, and
// their order and causality are preserved.
type Observation struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

// Assertion is one check that compares a derived observation against the
// scenario expectation. Kind classifies the assertion target so the runner
// asserts values, message role sequences, and identity relations rather than
// boolean keys alone; Passed reports the comparison result.
type Assertion struct {
	Key      string `json:"key"`
	Kind     string `json:"kind"`
	Expected any    `json:"expected"`
	Actual   any    `json:"actual"`
	Passed   bool   `json:"passed"`
	Message  string `json:"message"`
}

// Diff reports the difference between the derived observations and the scenario
// expectations for one scenario. Equivalent is true only when there are no
// missing, unexpected, or mismatched observations.
type Diff struct {
	ScenarioID string      `json:"scenario_id"`
	Missing    []string    `json:"missing"`
	Unexpected []string    `json:"unexpected"`
	Mismatched []Assertion `json:"mismatched"`
	Equivalent bool        `json:"equivalent"`
}

// Result is the outcome of running one acceptance scenario. It carries the
// derived observations, the evaluated assertions, and the resulting diff.
type Result struct {
	ScenarioID   string        `json:"scenario_id"`
	Observations []Observation `json:"observations"`
	Assertions   []Assertion   `json:"assertions"`
	Diff         Diff          `json:"diff"`
	Passed       bool          `json:"passed"`
}

// Runner drives acceptance scenarios against the official runtime entry points.
// Load reads scenario contracts, Drive exercises the official entry for a
// scenario, Derive derives normalized observations from the recorded official
// output, and Assert evaluates the derived observations against the scenario
// expectations.
type Runner interface {
	// Load reads the scenario contract set from the given source path.
	Load(source string) ([]Scenario, error)

	// Drive exercises the official runtime entry for the scenario and records
	// the raw official output consumed later by Derive.
	Drive(ctx context.Context, scenario Scenario) error

	// Derive derives normalized observations from the official API output
	// recorded by the most recent Drive call for the scenario.
	Derive(scenario Scenario) ([]Observation, error)

	// Assert evaluates the derived observations against the scenario
	// expectations and returns the result, including the diff.
	Assert(scenario Scenario, observations []Observation) (Result, error)
}
