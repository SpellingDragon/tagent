// The environment-block observation checks of the host: the rewrite frequency
// of the block is read through the formal assembly entry, from the counter the
// boundary keeps and the DEBUG line each generation writes.
package host

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/telemetry"
)

// TestEnvironmentBlockGenerationIsObserved checks the rewrite-frequency face of the environment block through the formal entry.
// - every assembled request of a declaring agent moves the generation counter exactly once
// - an agent that declares no environment block moves it never
// - each generation writes one DEBUG line naming the trigger, the agent and the task count
// - the INFO stream carries no generation line, so the rewrite face never inflates it
//
// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
func TestEnvironmentBlockGenerationIsObserved(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	logger := &observationLogger{}
	watchObservationFaces(t, logger, &observedTracer{})
	round := 0
	provider := &entryModel{name: "model-a", answer: func(*model.Request) *model.Response {
		round++
		return entryFinalTurn("回答第" + strconv.Itoa(round) + "轮")
	}}
	decl := &config.Declaration{
		Entry: "agent-a",
		Agents: []config.AgentSpec{
			{ID: "agent-a", Model: "model-a", EnvironmentBlock: true},
			{ID: "agent-b", Model: "model-a"},
		},
	}
	run, err := tagent.Assemble(ctx, decl, map[event.AgentID]tagent.NativeAgent{
		"agent-a": {Model: provider},
		"agent-b": {Model: provider},
	}, tagent.WithGovernance(passPolicy{}), tagent.WithTelemetry(telemetry.NewLogObserver(logger)))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}

	before := tagent.EnvironmentBlockGenerations()
	invokeTurn(t, run, "inv-block-1", event.Address{AgentID: "agent-a", SessionID: "session-block"}, "带上环境块")
	invokeTurn(t, run, "inv-block-2", event.Address{AgentID: "agent-a", SessionID: "session-block"}, "再带一次")
	invokeTurn(t, run, "inv-no-block", event.Address{AgentID: "agent-b", SessionID: "session-no-block"}, "不带环境块")
	if got := tagent.EnvironmentBlockGenerations() - before; got != 2 {
		t.Fatalf("environment block generations = %d, want one per declaring request and none for the undeclared agent", got)
	}

	generations := 0
	for _, line := range logger.at("debug") {
		if strings.Contains(line, "environment block generated") {
			generations++
			if !strings.Contains(line, "reason=request-boundary") || !strings.Contains(line, "agent=agent-a") || !strings.Contains(line, "tasks=0") {
				t.Fatalf("generation line = %q, want the trigger, the agent and the task count", line)
			}
			if strings.Contains(line, "带上环境块") || strings.Contains(line, "不带环境块") {
				t.Fatalf("generation line = %q, want no task or boundary text rendered", line)
			}
		}
	}
	if generations != 2 {
		t.Fatalf("generation lines = %d, want one per declaring request: %v", generations, logger.at("debug"))
	}
	for _, line := range logger.at("info") {
		if strings.Contains(line, "environment block generated") {
			t.Fatalf("info line = %q, want the generation face left at DEBUG", line)
		}
	}
	if requests := provider.recorded(); len(requests) != 3 {
		t.Fatalf("recorded requests = %d, want one per invocation", len(requests))
	}
}
