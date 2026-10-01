package rl

import (
	"context"
	"sync"
	"sync/atomic"

	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// SwappableModel 是可在运行期替换内层实例的 model.Model 包装器：换模型不重建
// LLMAgent / Runner，也不改事件机制（常驻循环、消息注入、输出通道都不动），只换最
// 底下的模型实例；所有 GenerateContent / GenerateContentIter / Info 都委托当前内层。
//
// 契约: docs/wiki/rl/rl-architecture.md#swappable-model
type SwappableModel struct {
	mu    sync.RWMutex
	inner model.Model

	// inFlight 是在途租约计数：租约覆盖调用与**整条返回流**的生命周期，归零才允许回收。
	inFlight atomic.Int64
	// retired 是换出待回收的模型：无在途租约引用、且不是当前 inner 时被 io.Closer 关闭
	// 恰好一次（model.Model 无 Close，多数模型是无状态客户端，此机制保护有状态包装器）。
	retired []model.Model
}

// NewSwappableModel creates a SwappableModel wrapping the given model.
func NewSwappableModel(m model.Model) *SwappableModel {
	return &SwappableModel{inner: m}
}

// Swap replaces the inner model atomically.
// In-flight GenerateContent calls — INCLUDING their still-open response
// streams — continue with the old model; subsequent calls use the new model.
// The old model is retired: once no in-flight lease (call + full stream)
// references it AND it is not the current inner, it gets an io.Closer Close
// exactly once.
func (m *SwappableModel) Swap(inner model.Model) {
	m.mu.Lock()
	old := m.inner
	m.inner = inner
	if old != nil && old != inner {
		dup := false
		for _, r := range m.retired {
			if r == old {
				dup = true
				break
			}
		}
		if !dup {
			m.retired = append(m.retired, old)
		}
	}
	m.mu.Unlock()
	m.sweepRetired()
}

// sweepRetired closes retired models when no in-flight lease remains and the
// model is not the current inner (A→B→A keeps the reselected instance alive).
func (m *SwappableModel) sweepRetired() {
	if m.inFlight.Load() != 0 {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.inFlight.Load() != 0 {
		return
	}
	// current is read UNDER the write lock: a Swap landing between an early
	// snapshot and this loop could otherwise Close a model that just became
	// the live inner again (A→B→A).
	current := m.inner
	keep := m.retired[:0]
	for _, old := range m.retired {
		if old == current {
			keep = append(keep, old)
			continue
		}
		if c, ok := old.(interface{ Close() error }); ok {
			_ = c.Close()
		}
	}
	m.retired = keep
}

// release drops one in-flight lease (call + stream lifecycle) and sweeps.
func (m *SwappableModel) release() {
	m.inFlight.Add(-1)
	m.sweepRetired()
}

// GenerateContent delegates to the current inner model. The in-flight lease
// now covers the FULL returned-stream lifecycle
// : responses are forwarded to the caller until the upstream channel is
// closed (or context cancellation closes it) — only then is the lease
// released and the model eligible for retirement Close. Error/nil streams
// release immediately. A model that leaks its channel keeps the lease
// (conservative: never close a possibly-live resource).
func (m *SwappableModel) GenerateContent(ctx context.Context, request *model.Request) (<-chan *model.Response, error) {
	m.inFlight.Add(1)
	m.mu.RLock()
	inner := m.inner
	m.mu.RUnlock()

	ch, err := inner.GenerateContent(ctx, request)
	if err != nil {
		m.release()
		return nil, err
	}
	if ch == nil {
		m.release()
		return nil, nil
	}
	out := make(chan *model.Response, cap(ch))
	go func() {
		defer close(out)
		defer m.release()
		cancelled := false
		for {
			select {
			case r, ok := <-ch:
				if !ok {
					return
				}
				if cancelled {
					continue
				}
				select {
				case out <- r:
				case <-ctx.Done():
					cancelled = true
				}
			case <-ctx.Done():
				if cancelled {
					for range ch {
					}
					return
				}
				cancelled = true
			}
		}
	}()
	return out, nil
}

// GenerateContentIter 保真内层真实的 IterModel 能力，而非把它藏起来：只实现
// GenerateContent 的装饰器会把具备迭代能力的底层模型**静默降级**成"通道＋协程"路径。
//
//   - 惰性：构造返回的 Seq 不算调用——在调用方真正开始迭代前不加租约、不碰内层、不起协程。
//   - 内层是 IterModel 时直接委托其迭代入口（真快路径，不做通道桥接）；否则才桥接。
//   - 租约覆盖整个迭代（与 GenerateContent 同构），换出的模型不会在流中被关；提前停止或
//     ctx 取消时排空上游，生产方不被卡住、租约最终必被释放。
//   - 迭代路径不得把错误咽成"空迭代器的成功"：通道形态会把该错误返回给调用方，桥接侧
//     至少必须记录，否则同一模型走两条路径会有一条静默失败。
func (m *SwappableModel) GenerateContentIter(ctx context.Context, request *model.Request) (model.Seq[*model.Response], error) {
	return func(yield func(*model.Response) bool) {
		m.inFlight.Add(1)
		defer m.release()

		m.mu.RLock()
		inner := m.inner
		m.mu.RUnlock()

		if it, ok := inner.(model.IterModel); ok {
			seq, err := it.GenerateContentIter(ctx, request)
			if err != nil {
				log.Errorf("[SwappableModel] inner GenerateContentIter failed: %v", err)
				return
			}
			seq(yield)
			return
		}

		ch, err := inner.GenerateContent(ctx, request)
		if err != nil || ch == nil {
			log.Errorf("[SwappableModel] inner GenerateContent failed for iter bridge (nil channel: %v): %v", ch == nil, err)
			return
		}
		for {
			select {
			case r, ok := <-ch:
				if !ok {
					return
				}
				if !yield(r) {
					go func() {
						for range ch {
						}
					}()
					return
				}
			case <-ctx.Done():
				for range ch {
				}
				return
			}
		}
	}, nil
}

// Info delegates to the current inner model.
func (m *SwappableModel) Info() model.Info {
	m.mu.RLock()
	inner := m.inner
	m.mu.RUnlock()
	return inner.Info()
}
