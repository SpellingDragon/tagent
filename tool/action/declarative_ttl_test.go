package action

import (
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/task"
)

// TestSpecFromDeclarativeRestoresTTL covers §10.5(a): a restored command task must
// carry the model's explicit `ttl` (persisted in Declarative.Params at spawn and
// recovered by SpecFromDeclarative on rebuild) so the reaper stays bound across
// restarts — this is the 56bf24c3 fix (a restored long-running service that used
// to linger on the board forever because restore dropped its lifetime). Pre-TTL
// records (no `ttl` key) fall back to the 10m floor rather than becoming immortal.
func TestSpecFromDeclarativeRestoresTTL(t *testing.T) {
	ct := &ActionTool{} // zero: SpecFromDeclarative only builds closures, never invokes them

	t.Run("explicit ttl round-trips through the declarative projection", func(t *testing.T) {
		decl := DeclarativeFromArgs(ActionArgs{Command: "sleep 999", TTL: 45}, "sess-1")
		spec := ct.SpecFromDeclarative(nil, *decl)
		if spec.TTL != 45*time.Second {
			t.Fatalf("restored spec.TTL must equal the persisted ttl, got %s", spec.TTL)
		}
	})

	t.Run("pre-ttl record falls back to the floor", func(t *testing.T) {
		// Simulate a record written before `ttl` existed: no pTTL key in Params.
		decl := DeclarativeFromArgs(ActionArgs{Command: "sleep 999"}, "sess-2")
		if _, ok := decl.Params[pTTL]; ok {
			t.Fatal("control: ttl=0 must not be persisted")
		}
		spec := ct.SpecFromDeclarative(nil, *decl)
		if spec.TTL != defaultTaskTTL {
			t.Fatalf("restored spec.TTL must fall back to the %s floor, got %s", defaultTaskTTL, spec.TTL)
		}
	})

	t.Run("ttl survives a full Declarative marshal round-trip", func(t *testing.T) {
		decl := DeclarativeFromArgs(ActionArgs{Command: "python -m http.server", Mode: "resident", TTL: 7200}, "sess-3")
		// Rebuild strictly from the persisted projection, as RebuildTaskRegistry does.
		spec := ct.SpecFromDeclarative(nil, *decl)
		if spec.TTL != 2*time.Hour {
			t.Fatalf("a 7200s resident service must restore its exact 2h lifetime, got %s", spec.TTL)
		}
	})
}

// TestSubagentSpecFromDeclarativeRestoresTTL covers resident-review-fixes 4.1:
// the sub-agent's self-set `ttl`, persisted in Declarative.Params by the wrapper,
// must replay back into the rebuilt TaskSpec.TTL across a restart, so the reaper
// keeps the model's chosen anchor instead of collapsing to the default floor.
// A record without the key (pre-ttl) leaves TTL unset → the manager default governs.
func TestSubagentSpecFromDeclarativeRestoresTTL(t *testing.T) {
	t.Run("explicit ttl replays from the persisted projection", func(t *testing.T) {
		decl := task.Declarative{Kind: "subagent", Desc: "plan: x", Key: "plan:x", AgentName: "plan", Params: map[string]string{"ttl": "90"}}
		spec := SubagentSpecFromDeclarative(nil, decl)
		if spec.TTL != 90*time.Second {
			t.Fatalf("replayed subagent TTL = %s, want 90s", spec.TTL)
		}
	})
	t.Run("absent ttl leaves the default floor", func(t *testing.T) {
		decl := task.Declarative{Kind: "subagent", Desc: "plan: y", Key: "plan:y", AgentName: "plan"}
		spec := SubagentSpecFromDeclarative(nil, decl)
		if spec.TTL != 0 {
			t.Fatalf("pre-ttl record must leave TTL unset (→ manager default), got %s", spec.TTL)
		}
	})
	t.Run("malformed ttl value is ignored, not fatal", func(t *testing.T) {
		decl := task.Declarative{Kind: "subagent", Key: "k", Params: map[string]string{"ttl": "soon"}}
		spec := SubagentSpecFromDeclarative(nil, decl)
		if spec.TTL != 0 {
			t.Fatalf("a non-numeric ttl must be ignored (→ default), got %s", spec.TTL)
		}
	})
}
