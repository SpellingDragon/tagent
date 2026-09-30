package action

import (
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/task"
)

// TestSpecFromDeclarativeRestoresTTL pins that a restored command task keeps the model explicit ttl.
// - The ttl is persisted in Declarative.Params at spawn and recovered by SpecFromDeclarative on rebuild, so the reaper stays bound across restarts.
// - Without a lifetime a restored long-running service would sit on the board forever, which is the behaviour this test guards.
// - Records written before the ttl key existed fall back to the 10m floor rather than becoming immortal.
func TestSpecFromDeclarativeRestoresTTL(t *testing.T) {
	ct := &ActionTool{}

	t.Run("explicit ttl round-trips through the declarative projection", func(t *testing.T) {
		decl := DeclarativeFromArgs(ActionArgs{Command: "sleep 999", TTL: 45}, "sess-1")
		spec := ct.SpecFromDeclarative(nil, *decl)
		if spec.TTL != 45*time.Second {
			t.Fatalf("restored spec.TTL must equal the persisted ttl, got %s", spec.TTL)
		}
	})

	t.Run("pre-ttl record falls back to the floor", func(t *testing.T) {
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
		spec := ct.SpecFromDeclarative(nil, *decl)
		if spec.TTL != 2*time.Hour {
			t.Fatalf("a 7200s resident service must restore its exact 2h lifetime, got %s", spec.TTL)
		}
	})
}

// TestSubagentSpecFromDeclarativeRestoresTTL 钉住 子 agent 自设的寿命持久在声明式参数里，重启后必须回放进重建的任务规格。
// - 回收器因此保住模型选定的锚点，而不是塌回默认下限；
// - 记录里没有该键时寿命留空，由管理器默认值管辖。
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

// TestSpawnerTTLSourceIsLiveAuthority 钉住 省略寿命的派生，其默认绝对寿命在派生那一刻从属主已提交的权威记录解析。
// - 执行工具不得另持一份可写默认值：一个旋钮只能有一处真值；
// - 属主换源是对被捕获变量的一次赋值，与装配根换记录的方式相同。
// 契约: docs/wiki/agent/task-lifecycle.md#ttl-reclaim
func TestSpawnerTTLSourceIsLiveAuthority(t *testing.T) {
	cur := 2 * time.Hour
	ct := NewActionTool()
	ct.SetDefaultTTLSource(func() time.Duration { return cur })

	if got := ct.resolveTTL(ActionArgs{}); got != 2*time.Hour {
		t.Fatalf("spawn with omitted ttl = %v, want the source's 2h", got)
	}

	cur = 45 * time.Minute
	if got := ct.resolveTTL(ActionArgs{}); got != 45*time.Minute {
		t.Fatalf("after source rotation = %v, want 45m (the source is the authority, no push)", got)
	}

	if got := ct.resolveTTL(ActionArgs{TTL: 10}); got != 10*time.Second {
		t.Fatalf("explicit ttl = %v, want 10s", got)
	}

	cur = 0
	if got := ct.resolveTTL(ActionArgs{}); got != defaultTaskTTL {
		t.Fatalf("zero source reading = %v, want the construction default %v", got, defaultTaskTTL)
	}
}

// TestSpawnerTTLSourceAbsentKeepsConstructionDefault 钉住 没有参数源的边界（工具在热更体系之外构造）由构造期默认值继续应答。
// - 拉取契约不得要求参数源必须存在。
func TestSpawnerTTLSourceAbsentKeepsConstructionDefault(t *testing.T) {
	ct := NewActionTool()
	if got := ct.resolveTTL(ActionArgs{}); got != defaultTaskTTL {
		t.Fatalf("no source = %v, want construction default %v", got, defaultTaskTTL)
	}
}
