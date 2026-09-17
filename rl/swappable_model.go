package rl

import (
	"context"
	"sync"
	"sync/atomic"

	"trpc.group/trpc-go/trpc-agent-go/model"
)

// ---------------------------------------------------------------------------
// SwappableModel — 可运行时替换的 model.Model 包装器
//
// 用于 HTTPAPI 接收 AReaL adapter 传入的 llm_base_url 时，
// 将 LLM 请求重定向到 AReaL proxy（端口动态分配）。
// 不改变事件机制（persistent loop / InjectMessage / outputCh 不变），
// 仅替换底层 model.Model 实例。
// ---------------------------------------------------------------------------

// SwappableModel wraps a model.Model, allowing the inner model to be
// swapped at runtime without recreating the LLMAgent or Runner.
// All GenerateContent/Info calls delegate to the current inner model.
type SwappableModel struct {
	mu    sync.RWMutex
	inner model.Model

	// Retired-model recycling (implementation-hardening 5.2): swapped-out
	// models wait here until no in-flight GenerateContent call references any
	// of them, then get an io.Closer Close (model.Model has no Close; most
	// models are stateless clients — the mechanism guards stateful wrappers).
	inFlight atomic.Int64
	retired  []model.Model
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
				dup = true // re-retired after a A→B→A bounce: entry already queued
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
	m.mu.RLock()
	current := m.inner
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.inFlight.Load() != 0 { // re-check under lock (a call may have started)
		return
	}
	keep := m.retired[:0]
	for _, old := range m.retired {
		if old == current {
			keep = append(keep, old) // still in use — never close the current inner
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
// now covers the FULL returned-stream lifecycle (resident-readiness-plan
// 4.8): responses are forwarded to the caller until the upstream channel is
// closed (or context cancellation closes it) — only then is the lease
// released and the model eligible for retirement Close. Error/nil streams
// release immediately. A model that leaks its channel keeps the lease
// (conservative: never close a possibly-live resource).
func (m *SwappableModel) GenerateContent(ctx context.Context, request *model.Request) (<-chan *model.Response, error) {
	m.inFlight.Add(1) // lease acquired HERE — held until the stream fully ends
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
		// cold-eyes R2 Warning 3: a caller that abandons `out` (upstream
		// cancel without draining) must not wedge this goroutine on
		// `out <- r` forever — the lease would never be released and the
		// retired model never closed. On ctx cancel, drain the upstream to
		// its close, then exit: the lease is ALWAYS eventually freed.
		cancelled := false
		for {
			select {
			case r, ok := <-ch:
				if !ok {
					return
				}
				if cancelled {
					continue // keep draining upstream after cancel
				}
				select {
				case out <- r:
				case <-ctx.Done():
					cancelled = true
				}
			case <-ctx.Done():
				if cancelled {
					// ctx already fired before: keep draining via the range
					// below so the upstream sender never blocks either.
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

// Info delegates to the current inner model.
func (m *SwappableModel) Info() model.Info {
	m.mu.RLock()
	inner := m.inner
	m.mu.RUnlock()
	return inner.Info()
}
