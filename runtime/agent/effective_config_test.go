package agent

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
	tagentmodel "github.com/SpellingDragon/tagent/api/model"
)

// effectiveRequestModel is the model stand-in that declares generation
// parameters beside the request its provider receives: the loop reads the
// declared values as the effective configuration, and the request the model was
// handed is kept so one test can compare what the two sides really carry.
type effectiveRequestModel struct {
	*invariantLoopModel
	declared tagentmodel.GenerationConfig
	mu       sync.Mutex
	served   tagentmodel.GenerationConfig
}

// EffectiveGenerationConfig reports the declared configuration as the one the
// provider runs under.
func (m *effectiveRequestModel) EffectiveGenerationConfig(*tagentmodel.Request) tagentmodel.GenerationConfig {
	return m.declared
}

// GenerateContent keeps the configuration the request reached the provider with
// and serves the scripted answer.
func (m *effectiveRequestModel) GenerateContent(ctx context.Context, request *tagentmodel.Request) (<-chan *tagentmodel.Response, error) {
	m.mu.Lock()
	m.served = request.GenerationConfig
	m.mu.Unlock()
	return m.invariantLoopModel.GenerateContent(ctx, request)
}

// servedConfig reports the configuration the provider received.
func (m *effectiveRequestModel) servedConfig() tagentmodel.GenerationConfig {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.served
}

// requestEvidenceCapture keeps the parameters of every request the loop fixed
// evidence for, encoded the way the capture persists them.
type requestEvidenceCapture struct {
	mu         sync.Mutex
	parameters [][]byte
}

// Snapshot keeps the generation configuration of one fixed request.
func (c *requestEvidenceCapture) Snapshot(_ context.Context, _ event.DecisionID, _, _ string, _, _ []string, request tagentmodel.Request) error {
	encoded, err := json.Marshal(request.GenerationConfig)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.parameters = append(c.parameters, encoded)
	return nil
}

// Record admits one attempt and keeps nothing of it.
func (c *requestEvidenceCapture) Record(context.Context, event.AttemptID, event.DecisionID, event.EpisodeID, tagentmodel.Outcome) error {
	return nil
}

// Seal closes the turn.
func (c *requestEvidenceCapture) Seal(context.Context) error { return nil }

// fixedParameters reports the parameters of the requests fixed so far.
func (c *requestEvidenceCapture) fixedParameters() [][]byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([][]byte, 0, len(c.parameters))
	out = append(out, c.parameters...)
	return out
}

// TestLoopRequestEvidenceCarriesEffectiveConfig pins the request evidence to the configuration the provider receives.
//   - a model that reports declared parameters fixes evidence carrying them, and the provider receives the same values
//   - a model that reports none leaves the evidence naming no parameter instead of an invented default
//
// 契约: docs/wiki/agent/agent-architecture.md#request-budget
func TestLoopRequestEvidenceCarriesEffectiveConfig(t *testing.T) {
	temperature, effort := 0.3, "high"
	declared := tagentmodel.GenerationConfig{Temperature: &temperature, ReasoningEffort: &effort}
	drive := func(t *testing.T, serving tagentmodel.Model) (*requestEvidenceCapture, tagentmodel.GenerationConfig) {
		t.Helper()
		loop, _, _ := invariantLoop(t, serving)
		capture := &requestEvidenceCapture{}
		loop.SetCapture(capture)
		turn, err := loop.drive(context.Background(), Invocation{
			InvocationID: "invocation-evidence",
			Address:      invariantLoopAddress,
			Instance:     "instance-a",
			Ready:        []event.Event{{Key: "event-evidence", Message: tagentmodel.Message{Role: tagentmodel.RoleUser, Content: "hello"}}},
		})
		if err != nil {
			t.Fatalf("drive one decision: %v", err)
		}
		if turn.DecisionCount != 1 {
			t.Fatalf("decisions = %d, want the one decision of the turn", turn.DecisionCount)
		}
		return capture, declared
	}

	t.Run("declared parameters reach the evidence and the provider alike", func(t *testing.T) {
		serving := &effectiveRequestModel{invariantLoopModel: newInvariantLoopModel("answer-a", nil), declared: declared}
		capture, want := drive(t, serving)
		fixed := capture.fixedParameters()
		if len(fixed) != 1 {
			t.Fatalf("fixed parameters = %d, want the one decision's evidence", len(fixed))
		}
		encoded, err := json.Marshal(want)
		if err != nil {
			t.Fatalf("encode the declared configuration: %v", err)
		}
		if string(fixed[0]) != string(encoded) {
			t.Fatalf("fixed parameters = %s, want the declared %s the provider runs under", fixed[0], encoded)
		}
		served := serving.servedConfig()
		if served.Temperature == nil || *served.Temperature != temperature {
			t.Fatalf("provider temperature = %v, want the declared %v", served.Temperature, temperature)
		}
		if served.ReasoningEffort == nil || *served.ReasoningEffort != effort {
			t.Fatalf("provider reasoning effort = %v, want the declared %q", served.ReasoningEffort, effort)
		}
	})

	t.Run("a model reporting no declaration leaves the evidence naming none", func(t *testing.T) {
		capture, _ := drive(t, newInvariantLoopModel("answer-b", nil))
		fixed := capture.fixedParameters()
		if len(fixed) != 1 {
			t.Fatalf("fixed parameters = %d, want the one decision's evidence", len(fixed))
		}
		var held map[string]any
		if err := json.Unmarshal(fixed[0], &held); err != nil {
			t.Fatalf("decode the fixed parameters %s: %v", fixed[0], err)
		}
		for _, invented := range []string{"temperature", "reasoning_effort", "thinking_enabled"} {
			if _, present := held[invented]; present {
				t.Fatalf("fixed parameters = %s, want no %s invented beside a request that declares none", fixed[0], invented)
			}
		}
	})
}
