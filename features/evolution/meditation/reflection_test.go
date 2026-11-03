package meditation

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
)

// reflectionSessionQuery is the live-binding stand-in of the anchors below: the
// complete addresses each named agent currently binds.
type reflectionSessionQuery map[event.AgentID][]event.Address

// ActiveSessions reports the bound addresses of one agent.
func (q reflectionSessionQuery) ActiveSessions(agentID event.AgentID) []event.Address {
	return q[agentID]
}

// TestMeditationSendsViaSendTool drives the one face a reflection turn delivers through.
// - positive: the reflection input names every live session of every declared deliver_to target as the exact agent and session pair a send call carries
// - positive: a declared target binding no live session is reported as such, so the turn never delivers to a session it invented
// - negative: the tool face the reflection holds carries no summary tool, and the package names no deliver_summary symbol at all
// 契约: docs/wiki/reliability/durable-delivery.md#lineage-visibility
func TestMeditationSendsViaSendTool(t *testing.T) {
	t.Run("delivery_targets_name_live_sessions", func(t *testing.T) {
		text := reflectionMaterialOf(t, ReflectionDelivery{
			Targets: []string{"user", "peer"},
			Sessions: reflectionSessionQuery{
				"user": {{AgentID: "user", SessionID: "session-user"}},
				"peer": {
					{AgentID: "peer", SessionID: "session-peer-one"},
					{AgentID: "peer", SessionID: "session-peer-two"},
				},
			},
		})
		for _, want := range []string{
			`- agent "user", session "session-user"`,
			`- agent "peer", session "session-peer-one"`,
			`- agent "peer", session "session-peer-two"`,
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("the reflection input carries no %q, want the live session the send call names:\n%s", want, text)
			}
		}
		if !strings.Contains(text, "issue one send per live session") {
			t.Fatalf("the reflection input names no send, want the delivery instruction:\n%s", text)
		}
	})

	t.Run("target_beside_no_live_session_is_reported", func(t *testing.T) {
		text := reflectionMaterialOf(t, ReflectionDelivery{
			Targets:  []string{"user"},
			Sessions: reflectionSessionQuery{},
		})
		if !strings.Contains(text, `- agent "user": no live session, so a body sent there would be refused`) {
			t.Fatalf("the reflection input omits the target binding no session, want a named gap:\n%s", text)
		}
		if strings.Contains(text, `session ""`) {
			t.Fatalf("the reflection input names an empty session, want no invented address:\n%s", text)
		}
	})

	t.Run("tool_face_has_no_deliver_summary", func(t *testing.T) {
		text := reflectionMaterialOf(t, ReflectionDelivery{
			Targets:  []string{"user"},
			Sessions: reflectionSessionQuery{"user": {{AgentID: "user", SessionID: "session-user"}}},
		})
		lowered := strings.ToLower(text)
		if strings.Contains(lowered, "deliver_summary") || strings.Contains(lowered, "summary") {
			t.Fatalf("the reflection input names a summary tool, want the send face alone:\n%s", text)
		}
		for file, body := range packageSources(t) {
			if strings.Contains(body, "DeliverSummaryTool") || strings.Contains(body, "deliver_summary") {
				t.Fatalf("%s names the removed summary tool, want the tool face to carry none", file)
			}
			if filepath.Base(file) == "deliver.go" {
				t.Fatalf("%s still carries the removed delivery tool", file)
			}
		}
	})
}

// reflectionMaterialOf renders the input body of one reflection configured with
// the given delivery face, so an anchor reads the exact material a reflection
// turn is served instead of the shape this file meant to build.
func reflectionMaterialOf(t *testing.T, delivery ReflectionDelivery) string {
	t.Helper()
	reflection, err := NewReflection(ReflectionConfig{
		Loop:     ReflectionLoop{Agent: "meditator", Session: "session-meditator"},
		Face:     ReflectionFace{ObservedNamespaces: []string{"user"}, ReadNamespaces: []string{"user"}},
		Cadence:  ReflectionCadence{Interval: time.Hour, MinGap: 2 * time.Hour},
		Prompt:   "reflect on the observed face",
		Delivery: delivery,
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("assemble the reflection: %v", err)
	}
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	return reflection.reflectionText(now, now.Add(-time.Hour), "digest: one pass")
}

// packageSources reads the non-test sources of this package, keyed by path, so an
// anchor reads the material the production files really carry.
func packageSources(t *testing.T) map[string]string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("the anchor cannot locate the package directory")
	}
	dir := filepath.Dir(file)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read the package directory %s: %v", dir, err)
	}
	out := make(map[string]string, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		out[name] = string(body)
	}
	if len(out) == 0 {
		t.Fatalf("no production source was read under %s, want the package files", dir)
	}
	return out
}
