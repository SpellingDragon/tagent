package agent

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	trgagent "trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/session/inmemory"
	trgtool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// introduce-durable-workflow-engine §6.3「实际停止能力门」——当前形态：fork 能力门。
//
// 历史（design D6 / upstream-research.md）：官方 v1.11.2 的公开接口**不足**——
// `TestUpstreamStopGate_ProcessedCloseIsNotProducerDone` 曾以同一夹具确定证伪
// 「processed 流关闭＝producer done」：runEventLoop 在 ctx.Done 后直接 return，
// defer 先 close(processedEventCh) 后才 cancel，从不 join 事件生产者。
//
// 本测现在钉**正向契约**：processed 事件流关闭 ⟹ 生产者 goroutine 已退出。
// 在带 producer-done 修复的依赖（trpc-agent-go 分支 tagent/producer-done，
// 经本地 replace / fork tag 接入）下为绿；在官方 v1.11.2 下为**红**——
// 这正是能力门的语义：红＝当前依赖不具备停止凭证，需要 fork。
//
// 对 tagent 的意义（D6）：资源释放门可以挂在「处理后流关闭」上——关闭即生产者
// 已收尾，不存在「在活生产者脚下释放资源」的窗口。

// stopCapProducer is a real agent.Agent whose event-production goroutine parks on
// a test-owned gate and ignores cancellation while parked. It records
// producer-done independently of the runner's consumer lifecycle.
type stopCapProducer struct {
	started      chan struct{} // closed once the producer goroutine begins
	hold         chan struct{} // producer parks here (ignoring ctx) until released
	producerDone atomic.Bool   // true only after the producer goroutine returns
	releaseOnce  sync.Once     // guards the single close of hold
}

func (p *stopCapProducer) Run(ctx context.Context, _ *trgagent.Invocation) (<-chan *event.Event, error) {
	out := make(chan *event.Event) // unbuffered: a post-cancel send has no reader
	go func() {
		defer close(out)
		defer p.producerDone.Store(true)
		close(p.started)
		<-p.hold // still-running producer: blocks here even after ctx is cancelled
		// After release, attempt to emit; the runner is already gone, so this can
		// only complete via ctx cancellation — it never proves an upstream join.
		select {
		case out <- &event.Event{Response: &model.Response{Done: true}}:
		case <-ctx.Done():
		}
	}()
	return out, nil
}

func (p *stopCapProducer) Tools() []trgtool.Tool { return nil }
func (p *stopCapProducer) Info() trgagent.Info {
	return trgagent.Info{Name: "stopcap", Description: "probe"}
}

func (p *stopCapProducer) SubAgents() []trgagent.Agent        { return nil }
func (p *stopCapProducer) FindSubAgent(string) trgagent.Agent { return nil }

// TestUpstreamStopGate_ProcessedCloseImpliesProducerDone is the §6.3 capability
// gate against the ACTIVE dependency: cancelling a run while its producer is
// parked must NOT close the processed stream until the producer has actually
// exited, and once it exits the stream must close.
func TestUpstreamStopGate_ProcessedCloseImpliesProducerDone(t *testing.T) {
	prod := &stopCapProducer{
		started: make(chan struct{}),
		hold:    make(chan struct{}),
	}
	r := runner.NewRunner("stopcap-app", prod,
		runner.WithSessionService(inmemory.NewSessionService()))
	defer func() { _ = r.Close() }()

	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out, err := r.Run(runCtx, "u", "s-stopcap", model.NewUserMessage("go"))
	require.NoError(t, err, "the runner must accept the invocation")

	// Producer is genuinely running (its goroutine started and is parked on hold).
	select {
	case <-prod.started:
	case <-time.After(5 * time.Second):
		t.Fatal("the test producer never started — this probe would measure nothing")
	}

	closed := make(chan struct{})
	go func() {
		for range out {
		}
		close(closed)
	}()

	// Cancel while the producer is parked (it ignores the context).
	cancel()

	// THE GATE: while the producer is still parked, the processed stream must
	// NOT close. On a dependency without the producer-done contract (official
	// v1.11.2) it closes within microseconds of cancellation — deterministic red.
	select {
	case <-closed:
		prod.release()
		t.Fatal("§6.3 gate: processed stream closed while the producer was still running — " +
			"the active dependency lacks the producer-done completion contract (official v1.11.2 " +
			"behavior; see upstream-research.md). Under the producer-done fork this must not happen.")
	case <-time.After(300 * time.Millisecond):
	}

	// Once the producer actually exits, the stream must close — release-gated join.
	prod.release()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("processed stream never closed after the producer exited")
	}
	require.True(t, prod.producerDone.Load(),
		"producer-done must hold at the moment the processed stream closes — this is the "+
			"credential tagent's release gate (D6) hangs on")
}

// release frees the parked producer exactly once, so helper and failure paths
// never double-close the gate.
func (p *stopCapProducer) release() { p.releaseOnce.Do(func() { close(p.hold) }) }
