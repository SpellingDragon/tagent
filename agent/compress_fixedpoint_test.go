package agent

// 4.1/4.2（tagent-compress-event-sourcing 不动点与逐字节一致性验证）：
//
// 4.1 不动点：旧进程 A 活路径压缩一轮（真实 persistSnapshotEvent 落 WAL，
// recordingStore 截获真实快照事件）；新进程 B 经 ReplayProjectionHandler
// 重放同一 WAL 复原三态；随后两侧注入同批增量事件再触发压缩——
// 断言 B 的再压缩决策（retained refs 分区 / 卡片文本 / 边界锚点 / 装配）
// 与 A 同况产出逐项一致（重放不改写历史，压缩确定性可复现）。
//
// 4.2 逐字节一致性：A 活路径 3 轮压缩→WAL 序列化→B 重放复原→
// 终态装配（render）逐字节 diff 为空。
//
// 确定性前提（代码实证）：nil summaryModel 下 curateCards/叙事均走纯工程
// 降级（context_compressor.go:869 / smart_compress.go:331），决策链零 LLM；
// DefaultTokenCounter 字符启发式（内容/2+10），maxTokens=200 可确定性触发
// over-budget。resolveRefs 经 store.GetEvent 取事件正文（:650），故两侧
// store 必须同 content 落库——这是装置的一部分，不是测试自由度。

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/SpellingDragon/tagent/agent/compress"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// recordingStore 截获 persistSnapshotEvent 的真实落盘事件（WAL 仿真）。
type recordingStore struct {
	memory.MemoryStore
	wal *[]memory.FullEvent
}

func (s *recordingStore) StoreEvent(key int64, event memory.FullEvent) error {
	if s.wal != nil {
		*s.wal = append(*s.wal, event)
	}
	// wal=nil（重放侧 B）：生产语义中新进程的后续压缩同样会真实落库，
	// 只是本测试不截获其 WAL——仍透传底层 store 保持行为同构。
	return s.MemoryStore.StoreEvent(key, event)
}

// fixedPointHarness：与 newTestHarness 同源，但压缩预算收窄（200/0.8/keepRecent=2，
// 可确定性触发真压缩），memStore 包 recordingStore 捕获真实快照事件。
func fixedPointHarness(t *testing.T, wal *[]memory.FullEvent) (*TagentAgent, *ContextManager, *memory.InMemoryStore) {
	t.Helper()
	store := memory.NewInMemoryStore()
	rs := &recordingStore{MemoryStore: store, wal: wal}
	cm := &ContextManager{
		name:       "tagent",
		memStore:   rs,
		projection: compress.NewSessionProjection(),
		// 注意：persistSnapshotEvent 走 cm.memStore（recording 层）落 WAL；
		// 压缩器持有底层 store 专管内容渲染，与生产分层一致。
		partitionID: 9,
		contextCompressor: compress.NewContextCompressor(
			compress.NewSmartCompressor(), store, nil, 200, 0.8, 2,
		),
	}
	ta := &TagentAgent{contextManager: cm}
	return ta, cm, store
}

func fpSummary(r, key int) string {
	return fmt.Sprintf("R%d-E%d: 任务现场摘要，包含足够字节量以推进 token 预算判断的确定性测试载荷", r, key)
}

// fpInject 三落点：投影 Append（重放回调）+ store 正文（渲染数据源）+ WAL 序列。
func fpInject(h func(memory.FullEvent), store *memory.InMemoryStore, wal *[]memory.FullEvent, key int64, evType, trigger, summary string) {
	ev := plainEvent(key, evType, trigger)
	ev.EventSummary = summary
	ev.Content = summary
	ev.Timestamp = key
	h(ev)
	if store != nil {
		_ = store.StoreEvent(key, ev)
	}
	if wal != nil {
		*wal = append(*wal, ev)
	}
}

// compressOnce 镜像 assembleRequest 的压缩调用序：GetAll→Compress→Replace→persist。
func compressOnce(t *testing.T, cm *ContextManager) compress.CompressResult {
	t.Helper()
	refs := cm.projection.GetAll()
	result := cm.contextCompressor.Compress(context.Background(), refs)
	cm.projection.Replace(result.RetainedRefs)
	if notice := cm.persistSnapshotEvent(result, refs); notice != nil {
		result.Notices = append(result.Notices, *notice)
		result.Messages = append(result.Messages, *notice)
	}
	return result
}

// narrativeOrCardOf 提取压缩产出的决策轨迹载体：折叠摘要消息（含卡片行
// "- [key] ..." 与 "〔历史综述〕" 叙事线）。纯工程重建，双侧必然同构。
func narrativeOrCardOf(t *testing.T, result compress.CompressResult) string {
	t.Helper()
	for i := range result.Messages {
		c := result.Messages[i].Content
		if strings.HasPrefix(c, "〔历史综述〕") || strings.Contains(c, "- [") {
			return c
		}
	}
	// 兜底：整段装配拼接（无独立摘要消息的折叠形态）。
	var b strings.Builder
	for i := range result.Messages {
		b.WriteString(result.Messages[i].Content)
	}
	return b.String()
}

func assertTriStateEqual(t *testing.T, a, b *ContextManager, phase string) {
	t.Helper()
	ra, rb := a.projection.GetAll(), b.projection.GetAll()
	if !reflect.DeepEqual(ra, rb) {
		t.Fatalf("[%s] retained refs 分区划分重排:\nA=%+v\nB=%+v", phase, ra, rb)
	}
	if a.contextCompressor.FullBoundary() != b.contextCompressor.FullBoundary() {
		t.Fatalf("[%s] FullBoundary: A=%d B=%d", phase,
			a.contextCompressor.FullBoundary(), b.contextCompressor.FullBoundary())
	}
	if a.contextCompressor.Threshold() != b.contextCompressor.Threshold() {
		t.Fatalf("[%s] Threshold: A=%v B=%v", phase,
			a.contextCompressor.Threshold(), b.contextCompressor.Threshold())
	}
	ma, mb := a.contextCompressor.MeditationKeysSnapshot(), b.contextCompressor.MeditationKeysSnapshot()
	if !reflect.DeepEqual(ma, mb) {
		t.Fatalf("[%s] meditationKeys: A=%v B=%v", phase, ma, mb)
	}
}

func diffFirstByte(a, b []model.Message) string {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i].Content != b[i].Content {
			ca, cb := a[i].Content, b[i].Content
			for j := 0; j < len(ca) && j < len(cb); j++ {
				if ca[j] != cb[j] {
					lo := j - 20
					if lo < 0 {
						lo = 0
					}
					hiA, hiB := j+21, j+21
					if hiA > len(ca) {
						hiA = len(ca)
					}
					if hiB > len(cb) {
						hiB = len(cb)
					}
					return fmt.Sprintf("msg[%d] 首个差异字节 @%d: A=%q B=%q", i, j, ca[lo:hiA], cb[lo:hiB])
				}
			}
			return fmt.Sprintf("msg[%d] 长度差异: A=%d B=%d", i, len(ca), len(cb))
		}
	}
	if len(a) != len(b) {
		return fmt.Sprintf("条数差异: A=%d B=%d", len(a), len(b))
	}
	return ""
}

// TestCompressReplay_FixedPoint 4.1：重放三态后同况再压缩，决策轨迹与旧进程一致。
func TestCompressReplay_FixedPoint(t *testing.T) {
	var walA []memory.FullEvent
	taA, cmA, storeA := fixedPointHarness(t, &walA)
	hA := ReplayProjectionHandler(taA)

	// A 旧进程：初始历史 20 条（含 2 冥想）→ 首轮压缩。
	for i := 1; i <= 20; i++ {
		if i == 7 || i == 15 {
			fpInject(hA, storeA, &walA, int64(i), tagentevent.TypeAgentOutput, "meditation", fpSummary(0, i))
			continue
		}
		fpInject(hA, storeA, &walA, int64(i), tagentevent.TypeExternalInput, "", fpSummary(0, i))
	}
	r1 := compressOnce(t, cmA)
	if !r1.Compressed {
		t.Fatalf("首轮应触发真压缩（over-budget），got Compressed=false")
	}
	boundA1 := cmA.contextCompressor.FullBoundary()
	// 决策轨迹载体（代码实证）：折叠段卡片行 = refs→extractCardLine 纯工程
	// 重建（context_compressor.go:803），叙事线 nil 模型时透传（:916）。
	// 注：persistSnapshotEvent 扫描 "[context_compress" 前缀消息取 CardText
	// 的路径在生产代码中无构造点（死代码，恒空）——不作为判据，已记入报账。
	cardA1 := narrativeOrCardOf(t, r1)
	if cardA1 == "" {
		t.Fatalf("压缩产出缺卡片行/叙事载体，无法验证决策轨迹")
	}

	// B 新进程：重放同一 WAL（20 条普通/冥想事件 + 1 条真实快照，时间序）。
	taB, cmB, storeB := fixedPointHarness(t, nil)
	hB := ReplayProjectionHandler(taB)
	for _, ev := range walA {
		hB(ev)
		if ev.Metadata[compress.SnapshotMetaKey] == "" {
			_ = storeB.StoreEvent(ev.EventKey, ev) // 正文渲染源（快照事件不进投影，落 store 无害）
		}
	}
	assertTriStateEqual(t, cmA, cmB, "首轮重放后")

	// 同批增量事件推入双方 → 同况再压缩（不动点判据）。
	baseKey := int64(100)
	for i := 1; i <= 12; i++ {
		summary := fpSummary(1, i)
		k := baseKey + int64(i)
		fpInject(hA, storeA, &walA, k, tagentevent.TypeExternalInput, "", summary)
		fpInject(hB, storeB, nil, k, tagentevent.TypeExternalInput, "", summary)
	}
	rA2 := compressOnce(t, cmA)
	rB2 := compressOnce(t, cmB)
	if !rA2.Compressed || !rB2.Compressed {
		t.Fatalf("增量后双方均应再触发压缩: A=%v B=%v", rA2.Compressed, rB2.Compressed)
	}

	// 不动点断言：retained refs/分区、决策轨迹载体、边界锚点、装配逐项一致。
	assertTriStateEqual(t, cmA, cmB, "再压缩后")
	if got, want := narrativeOrCardOf(t, rB2), narrativeOrCardOf(t, rA2); got != want {
		t.Fatalf("决策轨迹漂移（卡片行/叙事）:\nA=%q\nB=%q", want, got)
	}
	if got := cmB.contextCompressor.FullBoundary(); got <= boundA1 {
		t.Fatalf("边界锚点未随再压缩推进: B=%d 首轮=%d", got, boundA1)
	}
	if !reflect.DeepEqual(rB2.Messages, rA2.Messages) {
		t.Fatalf("再压缩装配不一致:\n%s", diffFirstByte(rA2.Messages, rB2.Messages))
	}
}

// TestCompressReplay_ByteIdenticalAssembly 4.2：3 轮压缩→重放→终态装配逐字节 diff 为空。
func TestCompressReplay_ByteIdenticalAssembly(t *testing.T) {
	var walA []memory.FullEvent
	taA, cmA, storeA := fixedPointHarness(t, &walA)
	hA := ReplayProjectionHandler(taA)

	key := int64(0)
	for i := 1; i <= 20; i++ {
		key++
		if i == 7 {
			fpInject(hA, storeA, &walA, key, tagentevent.TypeAgentOutput, "meditation", fpSummary(0, i))
			continue
		}
		fpInject(hA, storeA, &walA, key, tagentevent.TypeExternalInput, "", fpSummary(0, i))
	}
	for r := 1; r <= 3; r++ {
		result := compressOnce(t, cmA)
		if !result.Compressed {
			t.Fatalf("第 %d 轮应触发真压缩", r)
		}
		for i := 1; i <= 12; i++ {
			key++
			fpInject(hA, storeA, &walA, key, tagentevent.TypeExternalInput, "", fpSummary(r, int(key)))
		}
	}

	// B 新进程：全量重放（普通事件 + 3 条真实快照，时间序）。
	taB, cmB, storeB := fixedPointHarness(t, nil)
	hB := ReplayProjectionHandler(taB)
	for _, ev := range walA {
		hB(ev)
		if ev.Metadata[compress.SnapshotMetaKey] == "" {
			_ = storeB.StoreEvent(ev.EventKey, ev)
		}
	}
	assertTriStateEqual(t, cmA, cmB, "3 轮重放后")

	// 终态装配（双侧同调 Compress）：预算升级耗尽时可能退化（Compressed=true
	// 但快照降级跳过），逐字节判据落在装配产物本身，不卡 Compressed 标志。
	finalA := compressOnce(t, cmA)
	finalB := compressOnce(t, cmB)
	if !reflect.DeepEqual(finalA.Messages, finalB.Messages) {
		t.Fatalf("终态装配逐字节 diff 非空:\n%s", diffFirstByte(finalA.Messages, finalB.Messages))
	}
	if !reflect.DeepEqual(finalA.RetainedRefs, finalB.RetainedRefs) {
		t.Fatalf("终态 retained refs 不一致:\nA=%+v\nB=%+v", finalA.RetainedRefs, finalB.RetainedRefs)
	}
}
