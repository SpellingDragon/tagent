package acceptance

import (
	"encoding/json"
	"fmt"
	"testing"
)

// ScenarioRecordMarker prefixes the machine-readable observation record of one
// scenario in the test log; the scenario runner reads the marker-prefixed line
// out of `go test -json` output.
const ScenarioRecordMarker = "TAGENT-SCENARIO-RECORD:"

// ScenarioRecord is the real observation one scenario run reports: the scenario
// identity of the golden contract, the product identities the drive touched, and
// every observation value derived from the official API output.
// 规格: docs/wiki/platform/evaluation-suites.md#design-notes
type ScenarioRecord struct {
	ScenarioID   string         `json:"scenario_id"`
	TestPackage  string         `json:"test_package"`
	TestName     string         `json:"test_name"`
	EventRefs    []string       `json:"event_refs"`
	Observations map[string]any `json:"observations"`
}

// EmitScenarioRecord reports the scenario record as one marker-prefixed json line
// on the test log. The record carries only derived observations; no expectation
// of the contract is copied into it.
func EmitScenarioRecord(t *testing.T, scenario Scenario, refs []string, observations map[string]any) {
	t.Helper()
	if len(observations) == 0 {
		t.Fatalf("scenario %s reports no observation", scenario.ID)
	}
	record := ScenarioRecord{
		ScenarioID:   scenario.ID,
		TestPackage:  scenario.TestPackage,
		TestName:     scenario.TestName,
		EventRefs:    append([]string{}, refs...),
		Observations: observations,
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("encode the observation record of %s: %v", scenario.ID, err)
	}
	t.Logf("%s%s", ScenarioRecordMarker, encoded)
}

// scenarioLabels maps the product identities of one run onto the stable call,
// task and decision labels of the report: the first identity filed under a prefix
// takes suffix "a", the second "b", and so on. The mapping keeps every real
// identity distinct, and preserves the order and the causal direction of the real
// identities without freezing a random identifier into a report.
type scenarioLabels struct {
	byID    map[string]string
	counter map[string]int
}

// newScenarioLabels reports an empty label mapping.
func newScenarioLabels() *scenarioLabels {
	return &scenarioLabels{byID: map[string]string{}, counter: map[string]int{}}
}

// label reports the stable label of one product identity filed under the prefix.
func (l *scenarioLabels) label(prefix, id string) string {
	if id == "" {
		return ""
	}
	key := prefix + "\x00" + id
	if label, ok := l.byID[key]; ok {
		return label
	}
	label := fmt.Sprintf("%s-%c", prefix, 'a'+l.counter[prefix])
	l.counter[prefix]++
	l.byID[key] = label
	return label
}

// labels reports every stable label filed under one prefix, in labelling order.
func (l *scenarioLabels) labels(prefix string) []string {
	out := make([]string, 0, l.counter[prefix])
	for i := 0; i < l.counter[prefix]; i++ {
		out = append(out, fmt.Sprintf("%s-%c", prefix, 'a'+i))
	}
	return out
}
