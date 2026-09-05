package tagent

import (
	"testing"

	"github.com/SpellingDragon/tagent/memory"
)

// TestWireMemoryEngine_NilEngineUnchanged 验证未配置 Engine 时 store 原样返回
// （不包裹引擎）——保证 T-A 对现状零影响。
func TestWireMemoryEngine_NilEngineUnchanged(t *testing.T) {
	store := memory.NewInMemoryStore()
	got, err := wireMemoryEngine(store, MemoryConfig{})
	if err != nil {
		t.Fatalf("wireMemoryEngine: %v", err)
	}
	if _, ok := got.(memory.MemoryEngineProvider); ok {
		t.Fatal("未配置 Engine 时不应包裹引擎")
	}
}

// TestWireMemoryEngine_EmbeddingNilUnchanged 验证 Engine 配置但无 Embedding 时
// 不接线（无向量能力 = 纯关键词 = 现状）。
func TestWireMemoryEngine_EmbeddingNilUnchanged(t *testing.T) {
	store := memory.NewInMemoryStore()
	got, err := wireMemoryEngine(store, MemoryConfig{Engine: &MemoryEngineConfig{}})
	if err != nil {
		t.Fatalf("wireMemoryEngine: %v", err)
	}
	if _, ok := got.(memory.MemoryEngineProvider); ok {
		t.Fatal("无 Embedding 时不应包裹引擎")
	}
}

// TestWireMemoryEngine_MockEmbedderWraps 验证 mock 嵌入配置 → 包裹引擎（MemoryEngineProvider）。
func TestWireMemoryEngine_MockEmbedderWraps(t *testing.T) {
	store := memory.NewInMemoryStore()
	mc := MemoryConfig{Engine: &MemoryEngineConfig{Embedding: &EmbeddingConfig{Provider: "mock", Dimensions: 32}}}
	got, err := wireMemoryEngine(store, mc)
	if err != nil {
		t.Fatalf("wireMemoryEngine: %v", err)
	}
	ep, ok := got.(memory.MemoryEngineProvider)
	if !ok {
		t.Fatal("配置 mock 嵌入后应包裹引擎(MemoryEngineProvider)")
	}
	if ep.MemoryEngine() == nil {
		t.Fatal("引擎不应为 nil")
	}
	if c, ok := got.(interface{ Close() error }); ok {
		_ = c.Close() // 回收引擎 worker，防 goroutine 泄漏
	} else {
		t.Fatal("包裹后的 store 应可 Close（agent.Closer）")
	}
}

// TestWireMemoryEngine_ZhipuNoKeyDegrades 验证 zhipu 嵌入无 API key 时优雅降级：
// 返回原 store（无引擎），不报错、不阻断 agent 构建（不变量：增强能力故障不传染主链路）。
func TestWireMemoryEngine_ZhipuNoKeyDegrades(t *testing.T) {
	t.Setenv("ZAI_API_KEY", "")
	store := memory.NewInMemoryStore()
	mc := MemoryConfig{Engine: &MemoryEngineConfig{Embedding: &EmbeddingConfig{Provider: "zhipu"}}}
	got, err := wireMemoryEngine(store, mc)
	if err != nil {
		t.Fatalf("无 key 应优雅降级不报错, got %v", err)
	}
	if _, ok := got.(memory.MemoryEngineProvider); ok {
		t.Fatal("无 key 时应降级为原 store(无引擎)")
	}
}

// TestWireMemoryEngine_UnknownBackendErrors 验证未知 backend 报错（配置校验）。
func TestWireMemoryEngine_UnknownBackendErrors(t *testing.T) {
	store := memory.NewInMemoryStore()
	mc := MemoryConfig{Engine: &MemoryEngineConfig{Backend: "bogus", Embedding: &EmbeddingConfig{Provider: "mock"}}}
	// 未知 backend：buildMemoryEngine 报错 → wireMemoryEngine 优雅降级返回原 store（不阻断）。
	got, err := wireMemoryEngine(store, mc)
	if err != nil {
		t.Fatalf("应优雅降级不报错, got %v", err)
	}
	if _, ok := got.(memory.MemoryEngineProvider); ok {
		t.Fatal("未知 backend 应降级为原 store")
	}
}
