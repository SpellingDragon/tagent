package acceptance // import "github.com/SpellingDragon/tagent/tests/integration/acceptance"

Package acceptance holds the type skeleton for the acceptance scenario
runner. The runner loads scenario contracts from the golden contract file
(golden/runtime/scenarios.json), drives the official runtime entry points,
derives observations from the official API output, and evaluates
assertions against the expected observations. This skeleton declares
types and interfaces only and carries no runner logic; the phase-2

CONSTANTS

const ContractEnvVar = "TAGENT_SCENARIO_CONTRACT"
    ContractEnvVar names the environment variable that overrides the golden
    scenario contract path, so the scenario runner can hand the same file it
    reports against to the tests.

FUNCTIONS

func ContractPath() string
    ContractPath reports the golden scenario contract path this package reads.

TYPES

type Assertion struct {
	Key      string `json:"key"`
	Kind     string `json:"kind"`
	Expected any    `json:"expected"`
	Actual   any    `json:"actual"`
	Passed   bool   `json:"passed"`
	Message  string `json:"message"`
}
    Assertion is one check that compares a derived observation against the
    scenario expectation. Kind classifies the assertion target so the runner
    asserts values, message role sequences, and identity relations rather than
    boolean keys alone; Passed reports the comparison result.

type Diff struct {
	ScenarioID string      `json:"scenario_id"`
	Missing    []string    `json:"missing"`
	Unexpected []string    `json:"unexpected"`
	Mismatched []Assertion `json:"mismatched"`
	Equivalent bool        `json:"equivalent"`
}
    Diff reports the difference between the derived observations and the
    scenario expectations for one scenario. Equivalent is true only when there
    are no missing, unexpected, or mismatched observations.

type Observation struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}
    Observation is one normalized observation derived from the official API
    output of a scenario run. The runner MUST derive observations from real
    API output instead of copying a scenario's expected values; random ids and
    timestamps may be normalized, but one-to-one relations, order, and causality
    are preserved.

type Result struct {
	ScenarioID   string        `json:"scenario_id"`
	Observations []Observation `json:"observations"`
	Assertions   []Assertion   `json:"assertions"`
	Diff         Diff          `json:"diff"`
	Passed       bool          `json:"passed"`
}
    Result is the outcome of running one acceptance scenario. It carries the
    derived observations, the evaluated assertions, and the resulting diff.

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
    Runner drives acceptance scenarios against the official runtime entry
    points. Load reads scenario contracts, Drive exercises the official entry
    for a scenario, Derive derives normalized observations from the recorded
    official output, and Assert evaluates the derived observations against the
    scenario expectations.

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
    Scenario is one acceptance scenario contract loaded from
    golden/runtime/scenarios.json. The json tags mirror the golden contract
    fields, and Observations holds the expected observation values that a
    scenario run must derive from the official API output.

func ScenarioOf(id string) (Scenario, error)
    ScenarioOf reads the contract this package reports against and reports the
    one scenario entry filed under the named identity. A contract that does
    not name the scenario refuses the run instead of accepting a self-declared
    entry.

type ScenarioContract struct {
	SchemaVersion int        `json:"schema_version"`
	Scopes        ScopeBlock `json:"scopes"`
	Scenarios     []Scenario `json:"scenarios"`
}
    ScenarioContract is the golden contract document: the scope registration
    plus the scenario entries every acceptance observation is reported against.

func LoadScenarioContract(path string) (ScenarioContract, error)
    LoadScenarioContract reads the golden scenario contract from the named path.

func (c ScenarioContract) ScenarioByID(id string) (Scenario, bool)
    ScenarioByID reports the scenario entry filed under the named identity.

type ScopeBlock struct {
	Labels []string `json:"labels"`
}
    ScopeBlock is the unique scope registration of the contract: every scenario
    carries exactly one of these labels.

type Step struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}
    Step is one ordered driving action that exercises the official runtime entry
    for a scenario. The golden scenario contract does not enumerate steps,
    so the concrete per-scenario step catalog is phase-2 work.
