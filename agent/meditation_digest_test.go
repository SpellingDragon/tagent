// 本文件负责自我状态摘要的有界呈现：需关注项限条数、描述按 rune 截断（CJK 安全）、被裁数量
// 显式报出，无控制器时整段缺席而非空壳。
// 契约: docs/wiki/agent/compression-and-telemetry.md#self-state-digest
package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/task"
	tagentevent "github.com/SpellingDragon/tagent/event"
)

// mkDigestTask builds a task.Task in a specific status for digest tests (white-box:
// status is package-private).
func mkDigestTask(id, desc string, st task.TaskStatus, age time.Duration) *task.Task {
	return task.NewTaskFixture(id, desc, st, time.Now().Add(-age))
}

// fakeTaskController implements task.TaskController; only List returns data.
type fakeTaskController struct{ tasks []*task.Task }

func (f *fakeTaskController) Spawn(task.TaskSpec, task.SettleDetector) task.SpawnResult {
	return task.SpawnResult{}
}
func (f *fakeTaskController) List() []*task.Task            { return f.tasks }
func (f *fakeTaskController) Get(string) (*task.Task, bool) { return nil, false }
func (f *fakeTaskController) Cancel(string) bool            { return false }
func (f *fakeTaskController) Relaunch(context.Context, string) (task.SpawnResult, error) {
	return task.SpawnResult{}, nil
}
func (f *fakeTaskController) Resume(context.Context, string, string) (task.SpawnResult, error) {
	return task.SpawnResult{}, nil
}

func (f *fakeTaskController) RenewTTLBySession(string) bool { return false }

func (f *fakeTaskController) DefaultTTL() time.Duration { return 0 }

func TestRenderSelfStateDigest_EmptyDegrades(t *testing.T) {
	if got := renderSelfStateDigest(nil, time.Hour); got != "" {
		t.Errorf("nil tasks → empty digest, got %q", got)
	}
	if got := renderSelfStateDigest([]*task.Task{}, time.Hour); got != "" {
		t.Errorf("empty slice → empty digest, got %q", got)
	}
}

func TestRenderSelfStateDigest_CountsAndAttention(t *testing.T) {
	tasks := []*task.Task{
		mkDigestTask("aaaaaaaa11", "run a", task.TaskRunning, time.Minute),
		mkDigestTask("bbbbbbbb11", "svc b", task.TaskAliveDetached, time.Hour),
		mkDigestTask("cccccccc11", "stuck c", task.TaskSuspect, 2*time.Minute),
		mkDigestTask("dddddddd11", "dead d", task.TaskDead, 5*time.Minute),
	}
	got := renderSelfStateDigest(tasks, 90*time.Minute)

	for _, want := range []string{"running=1", "alive_detached=1", "suspect=1", "dead=1", "空闲时长", "需关注", "stuck c", "dead d"} {
		if !strings.Contains(got, want) {
			t.Errorf("digest missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "run a") {
		t.Errorf("running task should not appear in attention detail:\n%s", got)
	}
}

func TestRenderSelfStateDigest_BoundedAttention(t *testing.T) {
	var tasks []*task.Task
	overflow := 5
	for i := 0; i < digestMaxAttentionDetail+overflow; i++ {
		tasks = append(tasks, mkDigestTask(fmt.Sprintf("id%03d", i), fmt.Sprintf("t%d", i), task.TaskSuspect, time.Duration(i)*time.Minute))
	}
	got := renderSelfStateDigest(tasks, time.Minute)

	if lines := strings.Count(got, "  - ["); lines != digestMaxAttentionDetail {
		t.Errorf("attention detail lines = %d, want %d", lines, digestMaxAttentionDetail)
	}
	if !strings.Contains(got, fmt.Sprintf("另有 %d 条", overflow)) {
		t.Errorf("overflow summary missing:\n%s", got)
	}
}

func TestRenderSelfStateDigest_TruncatesLongDesc(t *testing.T) {
	long := strings.Repeat("字", 100)
	got := renderSelfStateDigest([]*task.Task{mkDigestTask("x", long, task.TaskSuspect, time.Minute)}, time.Minute)
	if !strings.Contains(got, "…") {
		t.Errorf("long desc should be rune-truncated with ellipsis:\n%s", got)
	}
}

// TestMeditation_DigestPresentBeforePrompt 钉住 with a task controller, the meditation message carries the digest before the prompt (task 4.1).
func TestMeditation_DigestPresentBeforePrompt(t *testing.T) {
	mgr := NewMeditationManager(MeditationConfig{PromptText: "REFLECT_NOW"}, &mockMessageInjector{})
	mgr.SetTaskController(&fakeTaskController{tasks: []*task.Task{
		mkDigestTask("id1", "stuck task", task.TaskSuspect, time.Minute),
	}})

	msg := mgr.buildMeditationMessage(time.Now(), time.Hour, nil)

	if !strings.Contains(msg.Content, "自我状态快照") || !strings.Contains(msg.Content, "stuck task") {
		t.Errorf("digest missing from meditation message:\n%s", msg.Content)
	}
	if strings.Index(msg.Content, "自我状态快照") > strings.Index(msg.Content, "REFLECT_NOW") {
		t.Errorf("digest should appear BEFORE the prompt:\n%s", msg.Content)
	}
}

// TestMeditation_NoDigestWhenNoController 钉住 without a task controller, behavior is unchanged — no digest section, prompt intact (task 4.1 / graceful degrade).
func TestMeditation_NoDigestWhenNoController(t *testing.T) {
	mgr := NewMeditationManager(MeditationConfig{PromptText: "REFLECT_NOW"}, &mockMessageInjector{})

	msg := mgr.buildMeditationMessage(time.Now(), time.Hour, nil)

	if strings.Contains(msg.Content, "自我状态快照") {
		t.Errorf("no digest expected without a task controller:\n%s", msg.Content)
	}
	if !strings.Contains(msg.Content, "REFLECT_NOW") {
		t.Errorf("prompt should still be present:\n%s", msg.Content)
	}
}

// observedScanWith builds one external form's collected evidence for the renderer tests.
func observedScanWith(watermarkMs int64, page, hydrated int, parts ...partitionCounts) *observedScan {
	return &observedScan{
		watermarkMs:    watermarkMs,
		partitions:     parts,
		referenceTotal: page,
		hydratedTotal:  hydrated,
	}
}

// requireDigestLines reports which expected line a digest failed to render.
func requireDigestLines(t *testing.T, got string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("digest missing %q:\n%s", w, got)
		}
	}
}

func TestMeditation_DigestObservedScan_PerPartitionLineageCounts(t *testing.T) {
	scan := observedScanWith(time.Date(2026, 6, 30, 10, 0, 0, 0, time.UTC).UnixMilli(), 5, 3,
		partitionCounts{name: "recall", id: 7, references: 3, externalHits: 1, selfManaged: 2},
		partitionCounts{name: "planner", id: 9, references: 2},
	)
	got := renderObservedScanDigest(scan, 90*time.Minute)

	requireDigestLines(t, got,
		"观察分区概况",
		"空闲时长：1h30m0s",
		"自 2026-06-30 10:00:00 起",
		"引用页 5 条，本次水合 3 条",
		"分区 recall（id=7）：引用 3；水合样本 非自管=1 自管/未知=2",
		"分区 planner（id=9）：引用 2；水合样本 非自管=0 自管/未知=0",
	)
	if strings.Contains(got, "最近非自管活动") {
		t.Errorf("a pass without a hit must not claim recent activity:\n%s", got)
	}
}

func TestMeditation_DigestObservedScan_FirstPassReadsFromEpoch(t *testing.T) {
	got := renderObservedScanDigest(observedScanWith(0, 1, 1,
		partitionCounts{name: "recall", id: 7, references: 1, externalHits: 1},
	), time.Minute)

	requireDigestLines(t, got, "首次冥想，自 epoch 起")
}

func TestMeditation_DigestObservedScan_RecentActivityAndUnknownLineage(t *testing.T) {
	scan := observedScanWith(time.Now().Add(-time.Hour).UnixMilli(), 4, 3,
		partitionCounts{name: "recall", id: 7, references: 4, externalHits: 1, selfManaged: 2},
	)
	scan.recent = &recentActivity{
		eventKey: 12345678901, partition: "recall", eventType: "external_input", lineage: "user",
		summary: "用户要求复盘上周的决定", timestampMs: time.Now().Add(-time.Minute).UnixMilli(),
	}
	scan.unknownLineage = 1
	scan.foreignPartition = 1
	got := renderObservedScanDigest(scan, time.Minute)

	requireDigestLines(t, got,
		"最近非自管活动：[", "] recall / external_input",
		"（trigger_source=user）：用户要求复盘上周的决定",
		"谱系未知（无持久 trigger_source）：1 条，不计入新鲜度",
		"观察面外分区：1 条，未计入",
	)
}

func TestMeditation_DigestObservedScan_BoundedPartitions(t *testing.T) {
	overflow := 5
	parts := make([]partitionCounts, digestMaxObservedPartitions+overflow)
	for i := range parts {
		parts[i] = partitionCounts{name: fmt.Sprintf("p%d", i), id: i}
	}
	got := renderObservedScanDigest(observedScanWith(0, len(parts), 0, parts...), time.Minute)

	if lines := strings.Count(got, "- 分区 "); lines != digestMaxObservedPartitions {
		t.Errorf("partition lines = %d, want %d:\n%s", lines, digestMaxObservedPartitions, got)
	}
	requireDigestLines(t, got, fmt.Sprintf("…另有 %d 个观察分区未列出", overflow))
}

func TestMeditation_DigestObservedScan_TruncatesLongSummary(t *testing.T) {
	scan := observedScanWith(0, 1, 1, partitionCounts{name: "recall", id: 7, references: 1, externalHits: 1})
	scan.recent = &recentActivity{partition: "recall", eventType: "external_input", lineage: "user",
		summary: strings.Repeat("嗯", 200), timestampMs: time.Now().UnixMilli()}
	got := renderObservedScanDigest(scan, time.Minute)

	if !strings.Contains(got, "…") {
		t.Errorf("recent activity summary must stay rune-bounded:\n%s", got)
	}
	if strings.Count(got, "嗯") > digestDescMax {
		t.Errorf("summary exceeded the %d-rune bound", digestDescMax)
	}
}

func TestMeditation_DigestObservedScan_EmptySurfaceDegrades(t *testing.T) {
	if got := renderObservedScanDigest(nil, time.Minute); got != "" {
		t.Errorf("nil scan → empty digest, got %q", got)
	}
	if got := renderObservedScanDigest(&observedScan{}, time.Minute); got != "" {
		t.Errorf("an observation surface with no partition → empty digest, got %q", got)
	}
}

// TestMeditation_DigestFromLiveExternalScan 钉住 外部形态的分区概况由判据那一次扫描渲染，不重复扫事实链。
// - 谱系计数只有水合后才分得开，早停之后未及水合的引用照常计入分区引用数。
func TestMeditation_DigestFromLiveExternalScan(t *testing.T) {
	reader := &fakeNoveltyReader{}
	now := time.Now()
	reader.add("planner", now.Add(-3*time.Minute), "user", "另一个分区更早的一条")
	reader.add("recall", now.Add(-2*time.Minute), "user", "本分区的新鲜事")
	reader.add("recall", now.Add(-time.Minute), tagentevent.LineageMeditation, "本分区的自管产出")
	mgr := NewMeditationManager(MeditationConfig{
		PromptText:         "REFLECT_NOW",
		ObservedNamespaces: []string{"recall", "planner"},
	}, &mockMessageInjector{})
	mgr.SetNoveltyReader(reader)

	scan, novel := mgr.scanObservedNovelty()
	if !novel {
		t.Fatalf("the newest non-self-managed event is novelty")
	}
	msg := mgr.buildMeditationMessage(now, time.Minute, scan)

	requireDigestLines(t, msg.Content,
		"观察分区概况",
		"分区 recall（id=", "引用 2；水合样本 非自管=1 自管/未知=1",
		"分区 planner（id=", "引用 1；水合样本 非自管=0 自管/未知=0",
		"最近非自管活动：[", "recall / external_input", "本分区的新鲜事",
		"REFLECT_NOW",
	)
	if _, hydrated := reader.counts(); hydrated != 2 {
		t.Errorf("hydrations must stop at the hit, got %d", hydrated)
	}
	if strings.Index(msg.Content, "观察分区概况") > strings.Index(msg.Content, "REFLECT_NOW") {
		t.Errorf("the digest must precede the prompt:\n%s", msg.Content)
	}
}

// TestMeditation_ExternalDigestWithoutOwnTaskLayer 钉住 外部形态下自身任务层为空只省略任务段，分区概况照常渲染。
func TestMeditation_ExternalDigestWithoutOwnTaskLayer(t *testing.T) {
	mgr := NewMeditationManager(MeditationConfig{PromptText: "REFLECT_NOW"}, &mockMessageInjector{})
	scan := observedScanWith(0, 2, 1, partitionCounts{name: "recall", id: 7, references: 2, externalHits: 1})

	msg := mgr.buildMeditationMessage(time.Now(), time.Hour, scan)

	requireDigestLines(t, msg.Content, "观察分区概况", "REFLECT_NOW")
	if strings.Contains(msg.Content, "自我状态快照") {
		t.Errorf("no task layer must omit the self-task section:\n%s", msg.Content)
	}
}

// TestMeditation_ExternalDigestKeepsOwnTaskSectionWhenPresent 钉住 外部形态自身有任务时两段并存，分区概况在前。
func TestMeditation_ExternalDigestKeepsOwnTaskSectionWhenPresent(t *testing.T) {
	mgr := NewMeditationManager(MeditationConfig{PromptText: "REFLECT_NOW"}, &mockMessageInjector{})
	mgr.SetTaskController(&fakeTaskController{tasks: []*task.Task{
		mkDigestTask("id1", "卡住的任务", task.TaskSuspect, time.Minute),
	}})
	scan := observedScanWith(0, 1, 1, partitionCounts{name: "recall", id: 7, references: 1, externalHits: 1})

	msg := mgr.buildMeditationMessage(time.Now(), time.Hour, scan)

	requireDigestLines(t, msg.Content, "观察分区概况", "自我状态快照", "卡住的任务")
	if strings.Index(msg.Content, "观察分区概况") > strings.Index(msg.Content, "自我状态快照") {
		t.Errorf("the observed surface must lead the digest:\n%s", msg.Content)
	}
}

// TestMeditation_DigestInLoopFormHasNoObservedSection 钉住 in-loop 形态的摘要不出现分区概况，覆盖面与既有完全一致。
func TestMeditation_DigestInLoopFormHasNoObservedSection(t *testing.T) {
	mgr := NewMeditationManager(MeditationConfig{PromptText: "REFLECT_NOW"}, &mockMessageInjector{})
	mgr.SetTaskController(&fakeTaskController{tasks: []*task.Task{
		mkDigestTask("id1", "stuck task", task.TaskSuspect, time.Minute),
	}})

	msg := mgr.buildMeditationMessage(time.Now(), time.Hour, nil)

	if strings.Contains(msg.Content, "观察分区概况") {
		t.Errorf("the in-loop form must not render an observed-partition section:\n%s", msg.Content)
	}
	requireDigestLines(t, msg.Content, "自我状态快照", "stuck task")
}
