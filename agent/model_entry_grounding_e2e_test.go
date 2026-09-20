package agent

import (
	"context"
	"sync"
	"sync/atomic"
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

// §4.5 GROUNDING (test-only, zero product risk). §4.5 adds an execution-credential
// verify at the ACTUAL model entry ("未绑定/不匹配的执行凭据 MUST 在实际模型入口阻断") and
// requires model decorators to preserve the base IterModel capability. Both depend on
// facts about the real framework that must not be guessed:
//
//	(A) ORDERING — does MemoryPlugin's user-echo OnEvent fire BEFORE the model is
//	    entered? A model-entry gate is only safe if the §4.4 credential is already bound
//	    by then; otherwise every turn would false-block (catastrophic).
//	(B) ITERATOR PREFERENCE — when the base model implements model.IterModel, does the
//	    framework actually use GenerateContentIter (so decorators that omit it hide a
//	    real capability) or fall back to the channel path?
//
// This harness runs a genuine turn with a model that implements BOTH entry points and
// logs which is used, plus a plugin that logs when it sees the root user echo, and pins
// their relative order.
type callLog struct {
	mu  sync.Mutex
	seq []string
}

func (l *callLog) add(s string) {
	l.mu.Lock()
	l.seq = append(l.seq, s)
	l.mu.Unlock()
}

func (l *callLog) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.seq...)
}

// dualModel implements model.Model AND model.IterModel, logging which entry the
// framework invokes and whether the lazy iterator is created vs first-iterated.
type dualModel struct {
	log      *callLog
	resp     *model.Response
	iterMade atomic.Bool
	iterRan  atomic.Bool
}

func (m *dualModel) GenerateContent(_ context.Context, _ *model.Request) (<-chan *model.Response, error) {
	m.log.add("model:channel")
	ch := make(chan *model.Response, 1)
	ch <- m.resp
	close(ch)
	return ch, nil
}

func (m *dualModel) GenerateContentIter(_ context.Context, _ *model.Request) (model.Seq[*model.Response], error) {
	m.iterMade.Store(true)
	m.log.add("model:iter-created")
	// Lazy: only touch the "model" when the caller actually starts iterating.
	return func(yield func(*model.Response) bool) {
		m.iterRan.Store(true)
		m.log.add("model:iter-first-next")
		yield(m.resp)
	}, nil
}

func (m *dualModel) Info() model.Info { return model.Info{Name: "dual"} }

// echoMarkPlugin logs the first root user echo it observes at OnEvent.
type echoMarkPlugin struct{ log *callLog }

func (echoMarkPlugin) Name() string { return "echo-mark" }

func (p echoMarkPlugin) Register(r *plugin.Registry) {
	r.OnEvent(func(_ context.Context, inv *agent.Invocation, e *event.Event) (*event.Event, error) {
		if e == nil || e.Response == nil || len(e.Response.Choices) == 0 {
			return e, nil
		}
		if inv != nil && inv.GetParentInvocation() == nil &&
			e.Author == "user" && e.Response.Choices[0].Message.Role == model.RoleUser {
			p.log.add("plugin:user-echo")
		}
		return e, nil
	})
}

func TestModelEntryGrounding_OrderingAndIterator(t *testing.T) {
	log := &callLog{}
	m := &dualModel{log: log, resp: &model.Response{ID: "r", Done: true,
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "ok"}}}}}
	agt := llmagent.New("gate-agent", llmagent.WithModel(m), llmagent.WithInstruction("test"))
	r := runner.NewRunner("gate-app", agt, runner.WithPlugins(echoMarkPlugin{log}))
	defer r.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ch, err := r.Run(ctx, "u1", "sess-1", model.NewUserMessage("hello-input"))
	require.NoError(t, err)
	for range ch {
	}

	seq := log.snapshot()
	t.Logf("§4.5 GROUNDING call order: %v", seq)

	echoIdx := -1
	firstModelIdx := -1
	for i, s := range seq {
		if s == "plugin:user-echo" && echoIdx == -1 {
			echoIdx = i
		}
		switch s {
		case "model:channel", "model:iter-created", "model:iter-first-next":
			if firstModelIdx == -1 {
				firstModelIdx = i
			}
		}
	}
	require.NotEqual(t, -1, echoIdx, "the plugin must observe the root user echo at OnEvent")
	require.NotEqual(t, -1, firstModelIdx, "the model must be entered")

	// (A): the echo is bound BEFORE the model entry → a credential-gate at model entry
	// can rely on the §4.4 bind having already happened (no false-block on happy path).
	require.Less(t, echoIdx, firstModelIdx,
		"§4.5(A): the input echo MUST reach the plugin before the model is entered, "+
			"otherwise a model-entry credential gate would false-block every turn")

	// (B): report which entry point the framework chose.
	if m.iterMade.Load() {
		t.Logf("§4.5(B): framework PREFERRED the IterModel path (iter-created=%v iter-ran=%v)", m.iterMade.Load(), m.iterRan.Load())
	} else {
		t.Logf("§4.5(B): framework used the CHANNEL path even though the base is an IterModel " +
			"(decorators that omit GenerateContentIter do not currently lose an actively-used capability)")
	}
}
