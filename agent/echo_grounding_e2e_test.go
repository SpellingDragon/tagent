package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/plugin"
	"trpc.group/trpc-go/trpc-agent-go/runner"

	"github.com/stretchr/testify/require"
)

// §4.4 GROUNDING HARNESS (test-only, zero product risk). MemoryPlugin must stop
// skipping on a coarse whole-turn "first envelope / any user" flag and match THIS
// attempt's echo precisely (root invocation, author, normalized message, committed
// keys). Adding each condition is only safe once we KNOW what the real framework
// delivers to a plugin OnEvent for a durable input echo — an under-matching predicate
// would stop skipping and double-store the input on EVERY turn. This harness runs a
// genuine runner turn through a real OnEvent hook and captures the echo's shape.
//
// It also seeds §4.8 (real-framework capture of the batch/echo). Keep it as a
// characterization test: it pins framework behavior §4.4 will depend on.

type echoObs struct {
	role, author, content string
	invocationID          string
	parentInvocationID    string
	isRoot                bool
	sessionID             string
}

type echoRecorder struct {
	mu   sync.Mutex
	seen []echoObs
}

func (rec *echoRecorder) Name() string { return "echo-recorder" }

func (rec *echoRecorder) Register(r *plugin.Registry) {
	r.OnEvent(func(ctx context.Context, inv *agent.Invocation, e *event.Event) (*event.Event, error) {
		if e == nil || e.Response == nil || len(e.Response.Choices) == 0 {
			return e, nil
		}
		m := e.Response.Choices[0].Message
		isRoot := inv == nil || inv.GetParentInvocation() == nil
		sid := ""
		if inv != nil && inv.Session != nil {
			sid = inv.Session.ID
		}
		rec.mu.Lock()
		rec.seen = append(rec.seen, echoObs{
			role: string(m.Role), author: e.Author, content: m.Content,
			invocationID: e.InvocationID, parentInvocationID: e.ParentInvocationID,
			isRoot: isRoot, sessionID: sid,
		})
		rec.mu.Unlock()
		return e, nil
	})
}

func runEchoCapture(t *testing.T, userContent string) []echoObs {
	t.Helper()
	rec := &echoRecorder{}
	mockModel := &requestCapturingModel{resp: &model.Response{ID: "r", Done: true,
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "ok"}}}}}
	agt := llmagent.New("echo-agent", llmagent.WithModel(mockModel), llmagent.WithInstruction("test"))
	r := runner.NewRunner("echo-app", agt, runner.WithPlugins(rec))
	defer r.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ch, err := r.Run(ctx, "u1", "sess-1", model.NewUserMessage(userContent))
	require.NoError(t, err)
	for range ch {
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return append([]echoObs(nil), rec.seen...)
}

// The durable input is the MERGED message the loop builds (BuildInvocation joins with
// "\n\n---\n\n"). §4.4's credential will match this echo precisely — pin what the
// framework actually hands a plugin for it.
func TestEchoGrounding_PluginHookReceivesInput(t *testing.T) {
	const merged = "msg-A\n\n---\n\nmsg-B"
	seen := runEchoCapture(t, merged)
	for i, s := range seen {
		t.Logf("obs[%d] role=%q author=%q root=%v invID=%q parentID=%q content=%q",
			i, s.role, s.author, s.isRoot, s.invocationID, s.parentInvocationID, truncate(s.content, 40))
	}

	// A plugin MUST observe a user-role event for the input, else the whole dedup
	// premise (and §4.4) is void.
	var userEcho *echoObs
	for i := range seen {
		if seen[i].role == string(model.RoleUser) {
			userEcho = &seen[i]
			break
		}
	}
	require.NotNil(t, userEcho,
		"no user-role input event reached the plugin OnEvent — §4.4 dedup premise would be void")

	// §4.4 may add these conditions ONLY if the real echo satisfies them (or it would
	// under-skip → double-write). Pin each:
	require.True(t, userEcho.isRoot, "the input echo must be the ROOT invocation (empty parent) → root check is safe")
	require.Empty(t, userEcho.parentInvocationID, "echo ParentInvocationID empty → root detectable from the event too")
	require.Equal(t, merged, userEcho.content, "echo carries the exact merged input content → message-normalize match is safe")
	t.Logf("§4.4 GROUNDING: author=%q (predicate must key on fields that actually hold)", userEcho.author)
}
