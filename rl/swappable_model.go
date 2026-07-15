package rl

import (
	"context"
	"sync"

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
}

// NewSwappableModel creates a SwappableModel wrapping the given model.
func NewSwappableModel(m model.Model) *SwappableModel {
	return &SwappableModel{inner: m}
}

// Swap replaces the inner model atomically.
// In-flight GenerateContent calls continue with the old model;
// subsequent calls use the new model.
func (m *SwappableModel) Swap(inner model.Model) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.inner = inner
}

// GenerateContent delegates to the current inner model.
func (m *SwappableModel) GenerateContent(ctx context.Context, request *model.Request) (<-chan *model.Response, error) {
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
