// 契约: docs/wiki/agent/execution-generations.md#turn-local-execution-face
package agent

import (
	"context"
	"errors"

	"github.com/SpellingDragon/tagent/plugin"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// ErrExecutionCredentialUnverified is returned by the execution gate when a durable
// turn installed an echo credential (its input facts were committed) but the credential
// was never verified — the framework never fed back the exact committed input, or a
// swallowed plugin error downgraded it. : this MUST block the real model call so the
// turn cannot cross the commit gate on unverified input.
var ErrExecutionCredentialUnverified = errors.New("agent: execution credential not verified at model entry (§4.5)")

// executionGateModel wraps the agent's model to enforce : an execution-credential
// verify at the ACTUAL model entry (not just a plugin return value, which the framework
// logs and continues past), plus deferral of the cold-start recovery notice to the real
// invocation. It preserves the wrapped model's capabilities: Model + IterModel +
// Info + close ownership.
type executionGateModel struct {
	inner model.Model
	cm    *ContextManager
}

func newExecutionGateModel(inner model.Model, cm *ContextManager) *executionGateModel {
	return &executionGateModel{inner: inner, cm: cm}
}

// verify returns ErrExecutionCredentialUnverified if the ctx carries a credential that
// is not Verified(). No credential (volatile / sub-agent) → pass.
func (g *executionGateModel) verify(ctx context.Context) error {
	if cred, ok := plugin.EchoCredentialFrom(ctx); ok && !cred.Verified() {
		return ErrExecutionCredentialUnverified
	}
	return nil
}

// withRecoveryNotice consumes the one-shot cold-start notice at the ACTUAL model call and
// returns a request carrying it (copied, so the framework's shared request is untouched).
// Empty notice → the original request, no copy (zero hot-path overhead).
func (g *executionGateModel) withRecoveryNotice(req *model.Request) *model.Request {
	notice := g.cm.TakeRecoveryNotice()
	if notice == "" {
		return req
	}
	cp := *req
	cp.Messages = append(append(make([]model.Message, 0, len(req.Messages)+1), req.Messages...), model.NewUserMessage(notice))
	log.Infof("[executionGate:%s] recovery notice injected at actual model call (§4.5C/D6)", g.cm.name)
	return &cp
}

// GenerateContent enforces the gate then delegates on the channel path.
func (g *executionGateModel) GenerateContent(ctx context.Context, request *model.Request) (<-chan *model.Response, error) {
	if err := g.verify(ctx); err != nil {
		log.Errorf("[executionGate:%s] §4.5 BLOCKING model call — execution credential unverified at model entry", g.cm.name)
		return nil, err
	}
	return g.inner.GenerateContent(ctx, g.withRecoveryNotice(request))
}

// GenerateContentIter keeps the iterator capability AND the laziness C
// requires: creating the returned Seq performs NO verification, notice consumption, or
// base call — all of that happens only when the caller actually starts iterating, so a
// created-then-cancelled iterator neither blocks nor consumes the recovery notice.
//
// Every failure inside the Seq must surface as a Response carrying Error: the
// framework reduces a turn from the event stream, and a zero-output stream would be
// read as a completed turn — acking the durable input with no output at all. This
// mirrors the channel contract (create-failure returns error, in-stream failures are
// encoded into Response.Error) on the iterator path.
func (g *executionGateModel) GenerateContentIter(ctx context.Context, request *model.Request) (model.Seq[*model.Response], error) {
	return func(yield func(*model.Response) bool) {
		if err := g.verify(ctx); err != nil {
			log.Errorf("[executionGate:%s] §4.5 BLOCKING model iterator — execution credential unverified", g.cm.name)
			yield(gateFailureResponse(err.Error()))
			return
		}
		req := g.withRecoveryNotice(request)
		if it, ok := g.inner.(model.IterModel); ok {
			seq, err := it.GenerateContentIter(ctx, req)
			if err != nil {
				log.Errorf("[executionGate:%s] inner iterator creation failed: %v", g.cm.name, err)
				yield(gateFailureResponse(err.Error()))
				return
			}
			seq(yield)
			return
		}
		ch, err := g.inner.GenerateContent(ctx, req)
		if err != nil {
			log.Errorf("[executionGate:%s] inner GenerateContent failed: %v", g.cm.name, err)
			yield(gateFailureResponse(err.Error()))
			return
		}
		if ch == nil {
			log.Errorf("[executionGate:%s] inner GenerateContent returned a nil channel", g.cm.name)
			yield(gateFailureResponse("execution gate: inner model returned nil stream"))
			return
		}
		for resp := range ch {
			if !yield(resp) {
				go func() {
					for range ch {
					}
				}()
				return
			}
		}
	}, nil
}

// gateFailureResponse encodes a model-entry failure as a terminal error response
// so the owning turn reduces to failed instead of a silent completion.
func gateFailureResponse(reason string) *model.Response {
	return &model.Response{
		Done:  true,
		Error: &model.ResponseError{Type: "model_error", Message: reason},
	}
}

// Info preserves the inner model's Info.
func (g *executionGateModel) Info() model.Info { return g.inner.Info() }

// Close preserves the inner model's close ownership: delegate if the inner is a
// Closer, otherwise a no-op.
func (g *executionGateModel) Close() error {
	if c, ok := g.inner.(interface{ Close() error }); ok {
		return c.Close()
	}
	return nil
}
