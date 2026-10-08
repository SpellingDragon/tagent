package tagent_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"

	tagent "github.com/SpellingDragon/tagent"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/rl"
)

// exportLine 是一行导出快照的可解码视图：FullEvent 的原样拷贝再加父指针。
type exportLine struct {
	memory.FullEvent
	ParentKey string `json:"parent_key"`
}

// exportShapeKeys 是每行必须具备的字段名：存储契约的字段集合加上父指针。
var exportShapeKeys = []string{
	"event_key", "partition_id", "event_type", "event_summary",
	"timestamp", "content", "tool_calls", "tool_results", "metadata", "parent_key",
}

// readExportLines 把快照字节流按行解码，任何一行不是合法 JSON 都直接失败。
func readExportLines(t *testing.T, body string) []exportLine {
	t.Helper()
	var out []exportLine
	for _, line := range splitNonEmptyLines(body) {
		var raw map[string]any
		require.NoErrorf(t, json.Unmarshal([]byte(line), &raw), "每行必须是合法 JSON: %s", line)
		for _, key := range exportShapeKeys {
			require.Containsf(t, raw, key, "导出行缺少存储契约字段 %s：%s", key, line)
		}
		var fact exportLine
		require.NoErrorf(t, json.Unmarshal([]byte(line), &fact), "每行必须能按 FullEvent 解码: %s", line)
		out = append(out, fact)
	}
	return out
}

// splitNonEmptyLines 按换行切开后丢掉空行。
func splitNonEmptyLines(body string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(body); i++ {
		if i == len(body) || body[i] == '\n' {
			if chunk := bytes.TrimSpace([]byte(body[start:i])); len(chunk) > 0 {
				out = append(out, string(chunk))
			}
			start = i + 1
		}
	}
	return out
}

// factsWithCallID 返回某分区里已盖上精确调用关联的事实。
func factsWithCallID(t *testing.T, store memory.MemoryStore, agentName string) []memory.FullEvent {
	t.Helper()
	var out []memory.FullEvent
	for _, evt := range allFacts(t, store, agentName) {
		if evt.Metadata[tagentevent.MetaKeyCallID] != "" {
			out = append(out, evt)
		}
	}
	return out
}

// runCaptureRound 用真实框架管线跑一轮入口与子 agent 的工具往返，返回承载这些事实的存储。
func runCaptureRound(t *testing.T, dir string) memory.MemoryStore {
	t.Helper()
	installCaptureProbe()
	entry, err := tagent.New(captureConfigFor(dir, "capture-entry", "capture-sub"),
		tagent.WithModel(&captureRoundModel{}))
	require.NoError(t, err)
	out, err := entry.StartLoop("export-user", "export-sess")
	require.NoError(t, err)
	entry.InjectMessage(model.NewUserMessage("do the work"))
	drainUntilFinal(t, out, 60*time.Second)
	require.NoError(t, entry.Close())
	store := entry.MemStore()
	require.NotNil(t, store, "导出面要能在同一条真实管线的存储上工作")
	return store
}

// bindReviewFeedback 给一条事实挂上人工反馈，返回 feedback 的事件键。
func bindReviewFeedback(t *testing.T, store memory.MemoryStore, parentKey int64) int64 {
	t.Helper()
	key, err := memory.BindFeedback(store, parentKey, memory.FeedbackPayload{
		Verdict: "approved",
		Rating:  1,
		Note:    "reviewed offline",
		Source:  "offline-review",
	})
	require.NoErrorf(t, err, "反馈必须能绑定到已提交事实 key=%d", parentKey)
	return key
}

// readSpyStore 只暴露读面，并记下每一次 GetEvent 查过哪个键。
type readSpyStore struct {
	memory.MemoryStore

	mu    sync.Mutex
	reads []int64
}

// GetEvent 记录被读取的键后转发给真实存储。
func (s *readSpyStore) GetEvent(key int64) (*memory.FullEvent, error) {
	s.mu.Lock()
	s.reads = append(s.reads, key)
	s.mu.Unlock()
	return s.MemoryStore.GetEvent(key)
}

// snapshotReads 返回被读取过的键副本。
func (s *readSpyStore) snapshotReads() []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int64(nil), s.reads...)
}

// storeReviewFact 在指定分区落一条手写事实，返回它的事件键。
func storeReviewFact(t *testing.T, store memory.MemoryStore, partitionID int, callID string) int64 {
	t.Helper()
	key := memory.NewSnowflakeEventKey(partitionID, 0)
	evt := memory.FullEvent{
		EventKey:     key,
		PartitionID:  partitionID,
		EventType:    tagentevent.TypeAgentOutput,
		EventSummary: "hand-made fact",
		Timestamp:    1,
		Content:      "offline fixture",
		Metadata:     map[string]string{tagentevent.MetaKeyCallID: callID},
	}
	require.NoErrorf(t, store.StoreEvent(key, evt), "写入夹具事实失败 key=%d", key)
	return key
}

// storeOrphanFeedback 落一条只带父指针、没有因果边的反馈，返回它的事件键。
func storeOrphanFeedback(t *testing.T, store memory.MemoryStore, partitionID int, parentKey int64) int64 {
	t.Helper()
	content, err := json.Marshal(memory.FeedbackPayload{
		Verdict:   "rejected",
		Source:    "offline-review",
		ParentKey: tagentevent.FormatEventKey(parentKey),
		Timestamp: 2,
	})
	require.NoError(t, err)
	key := memory.NewSnowflakeEventKey(partitionID, 0)
	evt := memory.FullEvent{
		EventKey:     key,
		PartitionID:  partitionID,
		EventType:    tagentevent.TypeFeedback,
		EventSummary: "feedback fixture",
		Timestamp:    2,
		Content:      string(content),
		Metadata:     map[string]string{tagentevent.MetaKeySubtype: "offline-review"},
	}
	require.NoError(t, store.StoreEvent(key, evt))
	return key
}

// TestTrainingCapture_OfflineDataset 钉住采集产物被离线训练侧真实消费时的形状与边界：
//   - 授权分区上的导出把每条已提交事实原样写成 JSONL，父子指针与 call_id 一起在场；
//   - 人工绑定的反馈作为独立一行出现，并按父事实的 call_id 精确入账（join bound）；
//   - 封账计数与字节自洽：written 等于行数、缺父计数等于空父指针行数、摘要等于快照字节；
//   - 导出对存储只读，不新增事件、不改既有事实；
//   - 空授权是拒绝而不是全库扫描；按 call_id 选择会把落选者计入 filtered；
//   - 父键不可得时报 expired_or_missing，父键在未授权分区时报 forbidden，且都不越权补读、不猜原因。
//
// 契约: docs/wiki/rl/rl-architecture.md#training-export
func TestTrainingCapture_OfflineDataset(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "capture-export")
	store := runCaptureRound(t, dir)
	partitionID := memory.PartitionIDFromName("capture-entry")

	stamped := factsWithCallID(t, store, "capture-entry")
	require.NotEmptyf(t, stamped, "真实管线里至少要有条事实带上 call_id，否则 join 面无事可查")
	parent := stamped[0]
	callID := parent.Metadata[tagentevent.MetaKeyCallID]
	feedbackKey := bindReviewFeedback(t, store, parent.EventKey)

	before := allFacts(t, store, "capture-entry")

	var buf bytes.Buffer
	manifest, err := rl.ExportTrainingFacts(context.Background(), store,
		rl.ExportOptions{PartitionIDs: []int{partitionID}}, &buf)
	require.NoError(t, err)
	require.Truef(t, manifest.Complete, "无读失败的快照才配得上 complete：%+v", manifest)
	require.Zero(t, manifest.Errs)
	require.Zero(t, manifest.ReadErrors)
	require.Zero(t, manifest.Forbidden)

	body := buf.String()
	lines := readExportLines(t, body)
	require.NotEmpty(t, lines, "授权分区里有事实，快照不得是空的")
	require.Equal(t, len(lines), manifest.EventsWritten, "written 必须等于真正写出的行数")

	after := allFacts(t, store, "capture-entry")
	require.Equal(t, len(before), len(after), "导出对存储只读：不得新增或多留一条事实")
	for i := range before {
		require.Equal(t, before[i].EventKey, after[i].EventKey)
		require.Equal(t, before[i].Metadata, after[i].Metadata, "导出不得改写既有事实的元数据")
	}

	seenCallID := 0
	missingParent := 0
	var feedbackLines []exportLine
	for _, line := range lines {
		require.NotZero(t, line.EventKey)
		require.Equalf(t, partitionID, line.PartitionID, "授权分区之外的内容不得进快照 key=%d", line.EventKey)
		require.Equal(t, memory.PartitionIDFromEventKey(line.EventKey), line.PartitionID,
			"事件键自己的分区声明必须与正文一致")
		require.NotZero(t, line.Timestamp, "离线侧按时间轴分页，缺时间戳的行无法定位")
		if line.Metadata[tagentevent.MetaKeyCallID] != "" {
			seenCallID++
		}
		if line.ParentKey == "" {
			missingParent++
		}
		if line.EventType == tagentevent.TypeFeedback {
			feedbackLines = append(feedbackLines, line)
		}
	}
	require.Equal(t, missingParent, manifest.ParentKeyMissing,
		"缺父计数必须就是真正没有父指针的行数，不得凭空补一个父")

	require.Len(t, feedbackLines, 1, "绑定的反馈必须作为独立一行出现在快照里")
	require.Equal(t, feedbackKey, feedbackLines[0].EventKey)
	require.Equal(t, tagentevent.FormatEventKey(parent.EventKey), feedbackLines[0].ParentKey,
		"反馈行的父指针必须指向被评价的那条事实")
	require.Equal(t, "offline-review", feedbackLines[0].Metadata[tagentevent.MetaKeySubtype],
		"反馈的来源必须随事件在场，离线侧才知道这份评价是谁给的")
	require.Contains(t, feedbackLines[0].Content, "\"verdict\":\"approved\"",
		"反馈正文必须原样带着被评价的结论，不得在导出途中被改写")

	require.GreaterOrEqual(t, seenCallID, 1, "call_id 必须随事实进入快照，离线侧才有可对齐的调用身份")
	require.Equal(t, callID, parent.Metadata[tagentevent.MetaKeyCallID])
	require.Equal(t, 1, manifest.FeedbackTotal)
	require.Equalf(t, 1, manifest.JoinBound, "父事实带着 call_id，反馈就必须精确入账：%+v", manifest)
	require.Zero(t, manifest.JoinMissing, "父指针在场且父带 call_id，不得判成缺关联")
	require.Zero(t, manifest.JoinExpiredOrMissing)
	require.Zero(t, manifest.JoinForbiddenParent)
	require.Zero(t, manifest.JoinAmbiguous)

	sum := sha256.Sum256([]byte(body))
	require.Equal(t, hex.EncodeToString(sum[:]), manifest.SourceSHA256,
		"摘要必须锁住真正写出的字节，否则清单与快照不是同一份东西")
	require.Equal(t, manifest.GeneratedAt, manifest.Cutoff, "未指定上界时截断点就是本次导出时刻")
	require.Equal(t, []int{partitionID}, manifest.Partitions, "清单必须回显生效的授权名单")
	require.Greater(t, manifest.PageLimit, 0)
	require.GreaterOrEqual(t, manifest.Pages, 1)

	t.Run("empty allowlist refuses instead of scanning the store", func(t *testing.T) {
		var refused bytes.Buffer
		m, err := rl.ExportTrainingFacts(context.Background(), store, rl.ExportOptions{}, &refused)
		require.ErrorIs(t, err, rl.ErrExportAuthorization)
		require.False(t, m.Complete, "被拒绝的导出不得伪装成一份完整快照")
		require.Zero(t, m.EventsWritten)
		require.Empty(t, refused.String(), "拒绝必须发生在任何字节写出之前")
	})

	t.Run("nil source and nil sink are refused rather than exported as empty", func(t *testing.T) {
		var sink bytes.Buffer
		_, err := rl.ExportTrainingFacts(context.Background(), nil,
			rl.ExportOptions{PartitionIDs: []int{partitionID}}, &sink)
		require.ErrorIs(t, err, rl.ErrExportNilStore)

		_, err = rl.ExportTrainingFacts(context.Background(), store,
			rl.ExportOptions{PartitionIDs: []int{partitionID}}, nil)
		require.ErrorIs(t, err, rl.ErrExportNilWriter)
	})

	t.Run("call id selection narrows the snapshot and counts what it drops", func(t *testing.T) {
		var filtered bytes.Buffer
		m, err := rl.ExportTrainingFacts(context.Background(), store,
			rl.ExportOptions{PartitionIDs: []int{partitionID}, CallIDs: []string{callID}}, &filtered)
		require.NoError(t, err)
		require.True(t, m.Complete)
		rows := readExportLines(t, filtered.String())
		require.NotEmpty(t, rows, "选中的那次调用必须在快照里")
		for _, line := range rows {
			require.Equal(t, callID, line.Metadata[tagentevent.MetaKeyCallID],
				"按 call_id 选择后，快照里不得混进别的调用的事实")
		}
		require.Equal(t, len(lines)-len(rows), m.Filtered,
			"落选的条数必须计成 filtered，而不是被说成不存在")
	})

	t.Run("unreachable and unauthorized parents are named without extra reads", func(t *testing.T) {
		fixture := memory.NewInMemoryStore()
		authorized := memory.PartitionIDFromName("offline-authorized")
		foreign := memory.PartitionIDFromName("offline-foreign")

		spy := &readSpyStore{MemoryStore: fixture}
		keptKey := storeReviewFact(t, spy, authorized, "call-kept")
		foreignParent := storeReviewFact(t, spy, foreign, "call-foreign")
		ghost := memory.NewSnowflakeEventKey(authorized, 0)
		storeOrphanFeedback(t, spy, authorized, foreignParent)
		storeOrphanFeedback(t, spy, authorized, ghost)

		var body bytes.Buffer
		m, err := rl.ExportTrainingFacts(context.Background(), spy,
			rl.ExportOptions{PartitionIDs: []int{authorized}}, &body)
		require.NoError(t, err)
		require.True(t, m.Complete)
		require.Equal(t, 1, m.JoinForbiddenParent, "父在未授权分区时必须具名为 forbidden")
		require.Equal(t, 1, m.JoinExpiredOrMissing, "父取不到时只报不可得，不猜是过期还是从未存在")
		require.Zero(t, m.JoinBound)
		require.Zero(t, m.Forbidden, "父键未授权不等于本分区的事实越权")

		reads := spy.snapshotReads()
		for _, read := range reads {
			require.NotEqualf(t, foreignParent, read,
				"未授权分区的父键必须在任何读取之前就被拒，越权补读等于把授权名单作废")
		}
		kept := false
		rows := readExportLines(t, body.String())
		for _, line := range rows {
			require.NotEqualf(t, foreignParent, line.EventKey, "未授权分区的内容不得进快照")
			if line.EventKey == keptKey {
				kept = true
			}
		}
		require.Truef(t, kept, "授权分区里带 call_id 的事实必须进快照，否则离线侧少了一条可训练的事实")
	})
}
