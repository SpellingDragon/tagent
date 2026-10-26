package acceptance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// defaultScenarioContract is the golden scenario contract read by the acceptance
// package: the path is resolved from the package directory at run time.
const defaultScenarioContract = "../../../golden/runtime/scenarios.json"

// ContractEnvVar names the environment variable that overrides the golden
// scenario contract path, so the scenario runner can hand the same file it
// reports against to the tests.
const ContractEnvVar = "TAGENT_SCENARIO_CONTRACT"

// ScenarioContract is the golden contract document: the scope registration plus
// the scenario entries every acceptance observation is reported against.
// 规格: docs/wiki/platform/evaluation-suites.md#design-notes
type ScenarioContract struct {
	SchemaVersion int        `json:"schema_version"`
	Scopes        ScopeBlock `json:"scopes"`
	Scenarios     []Scenario `json:"scenarios"`
}

// ScopeBlock is the unique scope registration of the contract: every scenario
// carries exactly one of these labels.
type ScopeBlock struct {
	Labels []string `json:"labels"`
}

// ContractPath reports the golden scenario contract path this package reads.
func ContractPath() string {
	if path := os.Getenv(ContractEnvVar); path != "" {
		return path
	}
	return filepath.FromSlash(defaultScenarioContract)
}

// LoadScenarioContract reads the golden scenario contract from the named path.
func LoadScenarioContract(path string) (ScenarioContract, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return ScenarioContract{}, fmt.Errorf("read the golden scenario contract %s: %w", path, err)
	}
	var contract ScenarioContract
	if err := json.Unmarshal(content, &contract); err != nil {
		return ScenarioContract{}, fmt.Errorf("parse the golden scenario contract %s: %w", path, err)
	}
	if len(contract.Scenarios) == 0 {
		return ScenarioContract{}, fmt.Errorf("the golden scenario contract %s carries no scenario", path)
	}
	return contract, nil
}

// ScenarioByID reports the scenario entry filed under the named identity.
func (c ScenarioContract) ScenarioByID(id string) (Scenario, bool) {
	for _, scenario := range c.Scenarios {
		if scenario.ID == id {
			return scenario, true
		}
	}
	return Scenario{}, false
}

// ScenarioOf reads the contract this package reports against and reports the one
// scenario entry filed under the named identity. A contract that does not name
// the scenario refuses the run instead of accepting a self-declared entry.
func ScenarioOf(id string) (Scenario, error) {
	contract, err := LoadScenarioContract(ContractPath())
	if err != nil {
		return Scenario{}, err
	}
	scenario, ok := contract.ScenarioByID(id)
	if !ok {
		return Scenario{}, fmt.Errorf("the golden scenario contract %s names no scenario %s", ContractPath(), id)
	}
	return scenario, nil
}
