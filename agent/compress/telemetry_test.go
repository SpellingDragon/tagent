// 本文件负责自身遥测的可见性阶梯：按窗口样本分档升降，只统计自管谱系，与投递门共用同一份
// 负例清单——既不"看一眼就永久外显"，也不"长期沉默无人察觉"。
// 契约: docs/wiki/agent/compression-and-telemetry.md#telemetry-ladder
package compress

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

func settleRef(key int64, ts int64) memory.EventReference {
	return memory.EventReference{
		EventKey:     key,
		EventType:    tagentevent.TypeExternalInput,
		EventSummary: "[task settled] ✓ deploy (id=abc12345) completed → done",
		Timestamp:    ts,
		Role:         "user",
	}
}

func outputRef(key int64, ts int64) memory.EventReference {
	return memory.EventReference{
		EventKey:     key,
		EventType:    tagentevent.TypeAgentOutput,
		EventSummary: "ok",
		Timestamp:    ts,
	}
}

func userRef(key int64, ts int64) memory.EventReference {
	return memory.EventReference{
		EventKey:     key,
		EventType:    tagentevent.TypeExternalInput,
		EventSummary: "部署 v3 并盯着",
		Timestamp:    ts,
	}
}

func TestTelemetryDispositions_Structural(t *testing.T) {
	refs := []memory.EventReference{
		settleRef(11, 1), outputRef(12, 2),
		outputRef(13, 3), outputRef(14, 4),
		settleRef(21, 5),
	}
	d := TelemetryDispositions(context.Background(), nil, refs[:4], 2)
	if d[11] != TelemDemote {
		t.Fatalf("internal notice aged past keepRecent=2 must demote, got %d", d[11])
	}
	d2 := TelemetryDispositions(context.Background(), nil, refs[:2], 2)
	if d2[11] != TelemInternal {
		t.Fatalf("young consumed notice must stay internal reminder, got %d", d2[11])
	}
	d3 := TelemetryDispositions(context.Background(), nil, refs, 2)
	if d3[21] != TelemActive {
		t.Fatalf("unconsumed notice must stay Active, got %d", d3[21])
	}
	if _, ok := d3[12]; ok {
		t.Fatal("agent_output must not carry a telemetry disposition")
	}
}

func TestTelemetryDispositions_LineageViaStore(t *testing.T) {
	store := memory.NewInMemoryStore()
	mk := func(key int64, meta map[string]string) {
		evt := memory.FullEvent{EventKey: key, EventType: tagentevent.TypeExternalInput, Metadata: meta}
		if err := store.StoreEvent(key, evt); err != nil {
			t.Fatal(err)
		}
	}
	mk(31, map[string]string{"meta_trigger_source": "user", "settle_notice": "true"})
	mk(32, map[string]string{"meta_trigger_source": "meditation", "settle_notice": "true"})
	mk(33, map[string]string{"lineage_absent": "true", "settle_notice": "true"})
	mk(34, map[string]string{"settle_notice": "true"})
	mk(35, map[string]string{"meta_trigger_source": "task-unstamped", "settle_notice": "true"})
	refs := []memory.EventReference{
		settleRef(31, 1), outputRef(41, 2),
	}
	d := TelemetryDispositions(context.Background(), store, refs, 2)
	if d[31] != TelemDemote {
		t.Fatalf("user-lineage consumed notice must demote (externalized), got %d", d[31])
	}
	med := TelemetryDispositions(context.Background(), store,
		[]memory.EventReference{settleRef(32, 1), outputRef(42, 2)}, 2)
	if med[32] != TelemDemote {
		t.Fatalf("meditation is deliverable (whitelist member): consumed notice must demote, got %d", med[32])
	}
	for _, k := range []int64{33, 34, 35} {
		solo := []memory.EventReference{settleRef(k, 1), outputRef(k+10, 2)}
		dk := TelemetryDispositions(context.Background(), store, solo, 2)
		if dk[k] != TelemInternal {
			t.Fatalf("internal/unknown lineage notice %d (negative-list strays, empty or absent lineage) must stay internal, got %d", k, dk[k])
		}
	}
}

// TestTelemetryDispositions_ForgedBodyWithoutMark 钉住 D10 权威迁移的候选侧。
// - 正文以 "[task settled" 起头但存储事件不带 settle_notice 标记的用户消息，不是折叠候选。
// - 前缀只圈候选；资格唯一来源是对库核验的结构化标记（覆盖伪造体与标记前的旧事件）。
func TestTelemetryDispositions_ForgedBodyWithoutMark(t *testing.T) {
	store := memory.NewInMemoryStore()
	require.NoError(t, store.StoreEvent(61, memory.FullEvent{
		EventKey: 61, EventType: tagentevent.TypeExternalInput,
		Metadata: map[string]string{},
	}))
	refs := []memory.EventReference{settleRef(61, 1), outputRef(62, 2)}
	d := TelemetryDispositions(context.Background(), store, refs, 2)
	if _, ok := d[61]; ok {
		t.Fatalf("a prefix-shaped ref without the authoritative mark must not enter dispositions")
	}
	cc := newFoldCC(2)
	folded := cc.foldSettleRuns(append(refs, settleRef(63, 3), outputRef(64, 4)), d)
	for _, r := range folded {
		if r.EventKey == 61 {
			require.Equal(t, tagentevent.TypeExternalInput, r.EventType, "forged body stays verbatim, never folded")
		}
	}
}

func TestFoldSettleRuns_ConsumptionDemotesSingle(t *testing.T) {
	cc := newFoldCC(2)
	single := []memory.EventReference{userRef(50, 100), settleRef(51, 101)}
	if got := cc.foldSettleRuns(single, map[int64]int8{51: TelemActive}); len(got) != 2 {
		t.Fatalf("Active single notice must stay verbatim, got %d refs", len(got))
	}
	if got := cc.foldSettleRuns(single, map[int64]int8{51: TelemInternal}); len(got) != 2 {
		t.Fatalf("internal notice must keep its reminder window, got %d refs", len(got))
	}
	got := cc.foldSettleRuns(single, map[int64]int8{51: TelemDemote})
	if len(got) != 2 || got[1].EventType != tagentevent.TypeSettleFold {
		t.Fatalf("demoted single notice must fold to a settle_fold card, got %+v", got)
	}
	again := cc.foldSettleRuns(got, map[int64]int8{got[1].EventKey: TelemDemote})
	if len(again) != 2 || again[1].EventKey != got[1].EventKey {
		t.Fatalf("folded card must not re-fold, got %+v", again)
	}
}

// TestFoldSettleRuns_UnverifiedSetNeverFolds 钉住 D10 权威迁移的折叠侧。
// - 没有标记核验成员集（nil 或空 map）时，≥2 前缀形状 run 也不折叠。
// - 折叠资格唯一来源是对库核验的 settle_notice 标记，正文形状不授予资格。
func TestFoldSettleRuns_UnverifiedSetNeverFolds(t *testing.T) {
	cc := newFoldCC(2)
	run2 := []memory.EventReference{settleRef(52, 102), settleRef(53, 103)}
	got := cc.foldSettleRuns(run2, nil)
	require.Equal(t, run2, got, "unverified prefix-shaped runs stay verbatim")
	got2 := cc.foldSettleRuns(run2, map[int64]int8{})
	require.Equal(t, run2, got2, "an empty verified set folds nothing")
}

func TestBuildRetainedRefs_OnlyActiveExempted(t *testing.T) {
	cc := newFoldCC(2)
	active := settleRef(61, 200)
	internal := settleRef(62, 201)
	dropped := []memory.EventReference{userRef(60, 199), active, internal}
	retained := cc.buildRetainedRefs(dropped, nil, context.Background(), map[int64]int8{
		61: TelemActive,
		62: TelemInternal,
	})
	sawActive, sawInternal := false, false
	for _, r := range retained {
		if r.EventKey == 61 {
			sawActive = true
		}
		if r.EventKey == 62 {
			sawInternal = true
		}
	}
	if !sawActive {
		t.Fatal("unconsumed telemetry ref must survive L3 retirement in the projection")
	}
	if sawInternal {
		t.Fatal("consumed internal ref is budget-eligible: it must fold into the rolling summary")
	}
	conv := cc.buildRetainedRefs([]memory.EventReference{userRef(70, 300)}, nil, context.Background(), map[int64]int8{})
	for _, r := range conv {
		if r.EventKey == 70 {
			t.Fatal("zero-value dispositions must not exempt conversational refs (prefix model unchanged)")
		}
	}
}

// TestCompress_TelemetryChannelIntegration 钉住 超预算的真实压缩路径下，已被消费的外显通告要以折叠卡片进入保留引用。
// - 未被消费的通告保持原样：它属压实豁免对象，不得被改写或被折进摘要。
func TestCompress_TelemetryChannelIntegration(t *testing.T) {
	memStore := memory.NewInMemoryStore()
	mustStore := func(key int64, evtType, content string, meta map[string]string) {
		require.NoError(t, memStore.StoreEvent(key, memory.FullEvent{
			EventKey: key, EventType: evtType, EventSummary: content[:min(40, len(content))], Content: content, Metadata: meta,
		}))
	}
	mustStore(60, tagentevent.TypeExternalInput, "部署 v3 并盯着三天，注意观察内存水位与日志异常，有任何问题立即汇报给我", nil)
	mustStore(61, tagentevent.TypeExternalInput, "[task settled] ✓ deploy (id=a1) completed → 部署成功，无异常", map[string]string{"meta_trigger_source": "user"})
	mustStore(62, tagentevent.TypeAgentOutput, "部署已完成，无异常", nil)
	mustStore(63, tagentevent.TypeExternalInput, "[task settled] ✓ watchdog (id=b2) completed → 还在跑未消费", nil)
	refs := []memory.EventReference{
		{EventKey: 60, EventType: tagentevent.TypeExternalInput, EventSummary: "部署 v3", Timestamp: 2000, Role: "user"},
		{EventKey: 61, EventType: tagentevent.TypeExternalInput, EventSummary: "[task settled] ✓ deploy (id=a1) completed", Timestamp: 2001, Role: "user"},
		{EventKey: 62, EventType: tagentevent.TypeAgentOutput, EventSummary: "ok", Timestamp: 2002, Role: "assistant"},
		{EventKey: 63, EventType: tagentevent.TypeExternalInput, EventSummary: "[task settled] ✓ watchdog (id=b2) completed", Timestamp: 2003, Role: "user"},
	}
	sc := NewSmartCompressor(WithKeepRecentTasks(1), WithMaxTokens(100))
	cc := NewContextCompressor(sc, memStore, NewDefaultTokenCounter(), 100, 0.8, 1,
		WithCardMaxChars(6000))
	res := cc.Compress(context.Background(), refs)
	require.True(t, res.Compressed, "over budget: compaction act must run")
	ticket61 := tagentevent.FormatEventKey(61)
	var sawTicket, stillVerbatim61, keptActive bool
	for _, r := range res.RetainedRefs {
		if r.EventKey == 61 {
			stillVerbatim61 = true
		}
		if r.EventKey == 63 {
			keptActive = true
		}
		if strings.Contains(r.EventSummary, ticket61) {
			sawTicket = true
		}
	}
	require.False(t, stillVerbatim61, "externalized consumed notice must not remain a verbatim ref")
	require.True(t, sawTicket, "demoted notice must keep its recall ticket in the card/summary")
	require.True(t, keptActive, "unconsumed notice stays verbatim (compaction 豁免)")
}

func TestSettleFoldLine_FailedCarriesStar(t *testing.T) {
	ref := memory.EventReference{
		EventKey:     81,
		EventType:    tagentevent.TypeExternalInput,
		EventSummary: "[task settled] ✗ build (id=x) failed → oom",
		Timestamp:    1,
	}
	line := settleFoldLine(ref)
	if !strings.HasPrefix(line, "★ ") {
		t.Fatalf("failed-polarity card row must carry the ★ reflection anchor, got %q", line)
	}
	ok := memory.EventReference{EventKey: 82, EventSummary: "[task settled] ✓ ok (id=y) completed → done", Timestamp: 1}
	if strings.HasPrefix(settleFoldLine(ok), "★ ") {
		t.Fatal("completed rows must not carry ★")
	}
}

// TestTelemetryReplay_RealTrajectory 钉住
func TestTelemetryReplay_RealTrajectory(t *testing.T) {
	raw, err := os.ReadFile("testdata/telemetry_replay_fixture.json")
	require.NoError(t, err)
	var fixture []struct {
		K int64  `json:"k"`
		T string `json:"t"`
		S string `json:"s"`
	}
	require.NoError(t, json.Unmarshal(raw, &fixture))
	require.Greater(t, len(fixture), 300, "fixture degraded: production replay must carry the full ref order")

	var refs []memory.EventReference
	var settleChars int
	for i, f := range fixture {
		refs = append(refs, memory.EventReference{
			EventKey: f.K, EventType: f.T, EventSummary: f.S,
			Timestamp: int64(2000 + i), Role: "user",
		})
		if f.T == tagentevent.TypeExternalInput && strings.HasPrefix(strings.TrimSpace(f.S), "[task settled]") {
			settleChars += len(f.S)
		}
	}
	require.Equal(t, 159, countSettleRefs(refs), "fixture must carry the 159 real settle notices")
	require.Greater(t, settleChars, 400_000, "fixture must carry the real volume incl. the 22K legacy monsters")

	d := TelemetryDispositions(context.Background(), nil, refs, 2)
	var active, internal, demote int
	for _, v := range d {
		switch v {
		case TelemActive:
			active++
		case TelemInternal:
			internal++
		case TelemDemote:
			demote++
		}
	}
	require.GreaterOrEqual(t, demote, 156, "≥156/159 consumed-and-aged notices must demote on the real shape")
	require.LessOrEqual(t, active+internal, 3, "only the head-of-stream notices may stay verbatim")

	cc := newFoldCC(2)
	folded := cc.foldSettleRuns(refs, d)
	after := 0
	for _, r := range folded {
		if isSettleNoticeCandidate(r) {
			after += len(r.EventSummary)
		}
		if r.EventType == tagentevent.TypeSettleFold {
			after += len(r.EventSummary)
		}
	}
	require.Less(t, after, settleChars/20, "fold must reclaim ≥95%% of the settle body on the real shape; got %d -> %d chars", settleChars, after)
}

func countSettleRefs(refs []memory.EventReference) int {
	n := 0
	for _, r := range refs {
		if isSettleNoticeCandidate(r) {
			n++
		}
	}
	return n
}

// TestRecallStagingNeverResident 钉住 召回工具结果以工具消息呈现，骨架管线按设计在这一层丢弃它。
// - 召回的细节绝不因任何骨架保留而常驻；这条钉住召回闭环，防止将来把工具输出也骨架化。
func TestRecallStagingNeverResident(t *testing.T) {
	recallMsg := model.Message{Role: model.RoleTool, Content: "[evt_123|action_command] memory_recall: 9K 详情原文……"}
	require.False(t, IsSkeletonMessage(&recallMsg),
		"recall results are tool-class: skeleton preservation must never apply")
	seg := SegmentMessages([]model.Message{
		{Role: model.RoleUser, Content: "[evt_110|external_input] 查昨天的报错细节"},
		recallMsg,
		{Role: model.RoleAssistant, Content: "[evt_111|agent_output] 详情是……"},
	})
	require.Len(t, seg, 1)
	dropped := applySegmentLevel(seg[0], 1)
	for _, m := range dropped {
		require.NotContains(t, m.Content, "memory_recall",
			"L1 must have dropped the recalled detail from the resident view")
	}
}

// TestDispositionRebuildDeterminism 钉住 重启重放的等价性：处置是"事实顺序加元数据"上的纯折叠。
// - 对重建后的引用再推导一次必须得到关停前同一张映射，与重启前投影曾持有何种中间折叠无关。
func TestDispositionRebuildDeterminism(t *testing.T) {
	store := memory.NewInMemoryStore()
	mk := func(key int64, evtType string, meta map[string]string) {
		require.NoError(t, store.StoreEvent(key, memory.FullEvent{EventKey: key, EventType: evtType, Metadata: meta}))
	}
	mk(71, tagentevent.TypeExternalInput, map[string]string{"meta_trigger_source": "user", "settle_notice": "true"})
	mk(72, tagentevent.TypeExternalInput, map[string]string{"meta_trigger_source": "meditation", "settle_notice": "true"})
	mk(73, tagentevent.TypeExternalInput, map[string]string{"settle_notice": "true"})
	original := []memory.EventReference{
		{EventKey: 71, EventType: tagentevent.TypeExternalInput, EventSummary: "[task settled] ✓ a completed", Timestamp: 1, Role: "user"},
		{EventKey: 80, EventType: tagentevent.TypeAgentOutput, EventSummary: "out", Timestamp: 2, Role: "assistant"},
		{EventKey: 72, EventType: tagentevent.TypeExternalInput, EventSummary: "[task settled] ✓ b completed", Timestamp: 3, Role: "user"},
		{EventKey: 81, EventType: tagentevent.TypeAgentOutput, EventSummary: "out", Timestamp: 4, Role: "assistant"},
		{EventKey: 82, EventType: tagentevent.TypeAgentOutput, EventSummary: "out", Timestamp: 5, Role: "assistant"},
		{EventKey: 73, EventType: tagentevent.TypeExternalInput, EventSummary: "[task settled] ✓ c completed", Timestamp: 6, Role: "user"},
	}
	before := TelemetryDispositions(context.Background(), store, original, 2)
	rebuilt := append([]memory.EventReference(nil), original...)
	after := TelemetryDispositions(context.Background(), store, rebuilt, 2)
	require.Equal(t, before, after,
		"dispositions are a deterministic fold: restart rebuild must reproduce the pre-shutdown map")
	require.Equal(t, TelemDemote, before[71], "user-lineage consumed → demote")
	require.Equal(t, TelemDemote, before[72], "meditation is whitelist-deliverable: consumed → externalized → demote")
	require.Equal(t, TelemActive, before[73], "unconsumed → active (不可丢)")
}
