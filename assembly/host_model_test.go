package assembly

import (
	"reflect"
	"testing"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/model"
)

// effectiveConfigReporter is the capability the loop asks a model for the
// configuration its provider will receive.
type effectiveConfigReporter interface {
	EffectiveGenerationConfig(request *model.Request) model.GenerationConfig
}

// TestEffectiveGenerationConfig pins the configuration one parameterized model reports to the request evidence against what it hands the provider.
//   - a declared temperature, thinking switch and reasoning effort fill the parameters the request leaves unset, and the provider receives exactly them
//   - a parameter the request names keeps its value while the rest is filled
//   - a parameter neither side declares stays absent, and a spec that declares none adds no wrapper at all
//
// 契约: docs/wiki/agent/agent-architecture.md#request-budget
func TestEffectiveGenerationConfig(t *testing.T) {
	temperature, thinking, effort := 0.3, true, "high"
	wrapped := withHostModelParams(&snapshotTestModel{}, config.AgentSpec{
		Temperature:     &temperature,
		ThinkingEnabled: &thinking,
		ReasoningEffort: effort,
	})
	reporting, ok := wrapped.(effectiveConfigReporter)
	if !ok {
		t.Fatalf("the parameterized model %T reports no effective generation configuration", wrapped)
	}

	t.Run("declared defaults reach the configuration the provider receives", func(t *testing.T) {
		request := model.Request{}
		effective := reporting.EffectiveGenerationConfig(&request)
		if effective.Temperature == nil || *effective.Temperature != temperature {
			t.Fatalf("effective temperature = %v, want the declared %v", effective.Temperature, temperature)
		}
		if effective.ThinkingEnabled == nil || *effective.ThinkingEnabled != thinking {
			t.Fatalf("effective thinking switch = %v, want the declared %v", effective.ThinkingEnabled, thinking)
		}
		if effective.ReasoningEffort == nil || *effective.ReasoningEffort != effort {
			t.Fatalf("effective reasoning effort = %v, want the declared %q", effective.ReasoningEffort, effort)
		}
		served := wrapped.(*hostParameterizedModel).decorate(&request)
		if !reflect.DeepEqual(served.GenerationConfig, effective) {
			t.Fatalf("provider configuration = %+v, want the %+v the evidence reports", served.GenerationConfig, effective)
		}
	})

	t.Run("request_value_not_overridden", func(t *testing.T) {
		named := 0.9
		request := model.Request{GenerationConfig: model.GenerationConfig{Temperature: &named}}
		effective := reporting.EffectiveGenerationConfig(&request)
		if effective.Temperature == nil || *effective.Temperature != named {
			t.Fatalf("effective temperature = %v, want the %v the request declared", effective.Temperature, named)
		}
		if effective.ReasoningEffort == nil || *effective.ReasoningEffort != effort {
			t.Fatalf("effective reasoning effort = %v, want the declared %q beside the named temperature", effective.ReasoningEffort, effort)
		}
		if request.Temperature == nil || *request.Temperature != named {
			t.Fatalf("the caller's request temperature = %v, want the %v it was built with", request.Temperature, named)
		}
	})

	t.Run("undeclared_stays_absent", func(t *testing.T) {
		narrow := &hostParameterizedModel{inner: &snapshotTestModel{}, params: hostModelParams{ReasoningEffort: effort}}
		effective := narrow.EffectiveGenerationConfig(&model.Request{})
		if effective.Temperature != nil || effective.ThinkingEnabled != nil {
			t.Fatalf("effective configuration = %+v, want no temperature or thinking switch invented", effective)
		}
		if effective.ReasoningEffort == nil || *effective.ReasoningEffort != effort {
			t.Fatalf("effective reasoning effort = %v, want the one parameter that was declared", effective.ReasoningEffort)
		}
		bare := withHostModelParams(&snapshotTestModel{}, config.AgentSpec{})
		if _, wrapped := bare.(*hostParameterizedModel); wrapped {
			t.Fatalf("a spec declaring no sampling wrapped the model in %T, want the model as it stands", bare)
		}
		if _, reported := bare.(effectiveConfigReporter); reported {
			t.Fatal("a model beside no declared sampling reports an effective configuration, want the request kept as it stands")
		}
	})
}
