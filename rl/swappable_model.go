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
// In-flight GenerateContent calls continue with the old model;
// subsequent calls use the new model. The old model is retired: once no
// in-flight call references any retired model, each gets an io.Closer Close
// (implementation-hardening 5.2 — the drain-free "tail" the swap used to
// leave dangling).
func (m *SwappableModel) Swap(inner model.Model) {
	m.mu.Lock()
	old := m.inner
	m.inner = inner
	if old != nil && old != inner { // same instance → no leak, nothing to retire
		m.retired = append(m.retired, old)
	}
	m.mu.Unlock()
	m.sweepRetired()
}

// sweepRetired closes retired models when no in-flight call remains.
func (m *SwappableModel) sweepRetired() {
	if m.inFlight.Load() != 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.inFlight.Load() != 0 { // re-check under lock (a call may have started)
		return
	}
	for _, old := range m.retired {
		if c, ok := old.(interface{ Close() error }); ok {
			_ = c.Close()
		}
	}
	m.retired = nil
}

// GenerateContent delegates to the current inner model, accounting the
// in-flight counter that gates retired-model sweeps.
func (m *SwappableModel) GenerateContent(ctx context.Context, request *model.Request) (<-chan *model.Response, error) {
	m.inFlight.Add(1)
	defer func() {
		m.inFlight.Add(-1)
		m.sweepRetired()
	}()
	m.mu.RLock()
	inner := m.inner
	m.mu.RUnlock()
	return inner.GenerateContent(ctx, request)
}

// Info delegates to the current inner model.
func (m *SwappableModel) Info() model.Info {
	m.mu.RLock()
	inner := m.inner
	m.mu.RUnlock()
	return inner.Info()
}
