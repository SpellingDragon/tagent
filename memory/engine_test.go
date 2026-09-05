package memory

import (
	"context"
	"testing"
)

// stubEngine 是 MemoryEngine 的最小参考实现，用于编译期锁定契约 C6，
// 并为 T-A 的 InMemoryEngine/RustVikingEngine 提供接口满足性基线。
type stubEngine struct {
	indexed   int
	removed   int
	closed    bool
	ready     bool
	caps      RetrievalCaps
	hits      []RetrievalHit
	lastQuery RetrievalQuery
}

func (s *stubEngine) Index(_ context.Context, _ IndexableEvent) error {
	s.indexed++
	return nil
}

func (s *stubEngine) Remove(_ context.Context, _ int64) error {
	s.removed++
	return nil
}

func (s *stubEngine) Retrieve(_ context.Context, q RetrievalQuery) ([]RetrievalHit, error) {
	s.lastQuery = q
	if !s.ready && (q.Mode == ModeVector || q.Mode == ModeHybrid || q.Mode == ModeAuto) {
		// 契约：索引未就绪时退化为关键词而非报错（此处 stub 返回空集表示退化）。
		return nil, nil
	}
	return s.hits, nil
}

func (s *stubEngine) Capabilities() RetrievalCaps { return s.caps }
func (s *stubEngine) Ready() bool                 { return s.ready }
func (s *stubEngine) Close() error                { s.closed = true; return nil }

// 编译期锁定 C6：stubEngine 必须满足 MemoryEngine（IndexBuilder + Retriever + Closer）。
var _ MemoryEngine = (*stubEngine)(nil)

// TestMemoryEngineContractDegradation 验证契约的退化语义：
// 索引未就绪时 Retrieve 对 Auto/Vector/Hybrid 退化（返回空、无错误），不 panic。
func TestMemoryEngineContractDegradation(t *testing.T) {
	eng := &stubEngine{ready: false, caps: RetrievalCaps{Keyword: true}}
	for _, mode := range []RetrievalMode{ModeAuto, ModeVector, ModeHybrid} {
		hits, err := eng.Retrieve(context.Background(), RetrievalQuery{Query: "q", Mode: mode})
		if err != nil {
			t.Fatalf("mode=%d: 未就绪应退化而非报错, got err=%v", mode, err)
		}
		if len(hits) != 0 {
			t.Fatalf("mode=%d: 未就绪退化应返回空集, got %d", mode, len(hits))
		}
	}
}

// TestMemoryEngineContractLifecycle 验证索引/移除/关闭的调用面与能力声明。
func TestMemoryEngineContractLifecycle(t *testing.T) {
	eng := &stubEngine{ready: true, caps: RetrievalCaps{Keyword: true, Vector: true, Hybrid: true}}
	ctx := context.Background()

	if err := eng.Index(ctx, IndexableEvent{EventKey: 1, PartitionID: 2, EventType: TypeExternalInputProbe, Text: "hello"}); err != nil {
		t.Fatalf("Index: %v", err)
	}
	if err := eng.Remove(ctx, 1); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if eng.indexed != 1 || eng.removed != 1 {
		t.Fatalf("调用计数错: indexed=%d removed=%d", eng.indexed, eng.removed)
	}
	if !eng.Capabilities().Hybrid {
		t.Fatal("Capabilities 应声明 Hybrid")
	}
	// 就绪后 hybrid 返回预置命中，且透传分区白名单（跨分区泄漏防线由实现遵守）。
	eng.hits = []RetrievalHit{{EventKey: 42, Score: 1.5}}
	hits, err := eng.Retrieve(ctx, RetrievalQuery{Query: "q", PartitionIDs: []int{2}, Mode: ModeHybrid, Limit: 5})
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(hits) != 1 || hits[0].EventKey != 42 {
		t.Fatalf("命中错: %+v", hits)
	}
	if len(eng.lastQuery.PartitionIDs) != 1 || eng.lastQuery.PartitionIDs[0] != 2 {
		t.Fatalf("分区白名单未透传: %+v", eng.lastQuery)
	}
	if err := eng.Close(); err != nil || !eng.closed {
		t.Fatalf("Close 未生效: err=%v closed=%v", err, eng.closed)
	}
}

// TypeExternalInputProbe 是测试用的事件类型字面量，避免 engine_test 反向依赖 event 包
// （memory 包不 import event；真实类型常量由 event 包持有，此处仅占位）。
const TypeExternalInputProbe = "external_input"
