// 本文件负责冥想门控：新鲜度（观察面水位后的非自管事件）与节奏（两次执行之间 ≥ MinGap，
// 零水位直通）两道门齐备才注入；水位只在冥想批被消费为回合时推进到注入时刻，让位不烧窗口；
// 执行水位必须跨重启持久，锚文件里的旧键由 Load 自然忽略。
// 契约: docs/wiki/reliability/durable-delivery.md#anchor-persistence
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/reliability"
	"github.com/SpellingDragon/tagent/agent/task"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/kv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	trpcEvent "trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// mockMessageInjector records injected messages for test verification.
type mockMessageInjector struct {
	messages []model.Message
}

func (m *mockMessageInjector) InjectMessageWithSource(source string, msg model.Message) {
	m.messages = append(m.messages, msg)
}

func TestNewMeditationManager(t *testing.T) {
	inj := &mockMessageInjector{}
	cfg := MeditationConfig{
		Enabled:    true,
		Interval:   30 * time.Minute,
		MinGap:     2 * time.Hour,
		PromptText: "meditation prompt",
	}

	mgr := NewMeditationManager(cfg, inj)
	require.NotNil(t, mgr)
	assert.Equal(t, cfg, mgr.cfg)
	assert.Equal(t, inj, mgr.injector)
}

// TestMeditationManager_ReaderWiredAtConstruction 钉住生产装配接线：
//   - 配了冥想即在构造期注入事实链读缝，未接 reader 的门 fail-closed 恒关；
//   - 未声明观察面时解析为 [自身分区]，直接构造点与组合根同一条缺省规则。
func TestMeditationManager_ReaderWiredAtConstruction(t *testing.T) {
	mm := newRecordableMockModel(&model.Response{ID: "r", Done: true,
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "ok"}}}})

	ta, err := NewTagentAgent(&TagentConfig{
		Name: "selfmed", Model: mm, SystemPrompt: "x",
		Meditation: MeditationConfig{Enabled: true, Interval: time.Hour, MinGap: time.Hour},
	})
	require.NoError(t, err)
	defer ta.Close()
	assert.NotNil(t, ta.meditationMgr.noveltyReader,
		"a configured meditation must carry the fact-chain reader from construction; a nil reader keeps the gate closed forever")

	got := ta.meditationMgr.observed
	require.Len(t, got, 1, "an undeclared observation surface resolves to the agent's own partition")
	assert.Equal(t, "selfmed", got[0].name)
	assert.Equal(t, memory.PartitionIDFromName("selfmed"), got[0].id)

	declared, err := NewTagentAgent(&TagentConfig{
		Name: "curator", Model: mm, SystemPrompt: "x",
		Meditation: MeditationConfig{Enabled: true, Interval: time.Hour, MinGap: time.Hour,
			ObservedNamespaces: []string{"someone"}},
	})
	require.NoError(t, err)
	defer declared.Close()
	require.Len(t, declared.meditationMgr.observed, 1, "a declared surface is taken as given")
	assert.Equal(t, "someone", declared.meditationMgr.observed[0].name)
}

// TestMeditationBatchOutcome_ConsumedAdvancesToInjectionMoment 钉住 消费通知才推进水位，注入本身不动水位。
//
// - 推进值取 fire 记下的注入时刻，与所批冥想事件的 Timestamp 同刻（±毫秒），覆盖让位窗口期的事实。
// - 让位窗口里积累的事实正因水位没在注入时推进而保持新鲜，最终一次执行整窗覆盖。
// - 消费之后同一事实自锁：判据读不到水位之外的新鲜度。
//
// 契约: docs/wiki/reliability/durable-delivery.md#anchor-persistence
func TestMeditationBatchOutcome_ConsumedAdvancesToInjectionMoment(t *testing.T) {
	reader := &fakeNoveltyReader{}
	reader.add("recall", time.Now().Add(-time.Minute), "user", "循环窗口里的一条真事")
	mgr, inj := newNoveltyManager(reader, "recall")

	mgr.checkAndMeditate()
	require.Len(t, inj.messages, 1, "novelty ∧ 节奏直通（零水位）就注入")
	assert.Zero(t, mgr.lastMeditation.Load(), "a fire only injects — the watermark moves on consumption")
	require.True(t, mgr.pending, "fire 之后批在途，pending 防重入")
	injectAt := mgr.pendingSince

	mgr.NoteMeditationBatchOutcome(true)
	assert.False(t, mgr.pending)
	assert.Equal(t, injectAt, mgr.lastMeditation.Load(), "consumed advances the watermark to the injection moment")

	mgr.checkAndMeditate()
	assert.Len(t, inj.messages, 1, "the fact chain is spent past the moved watermark — self-locked")
}

// TestMeditationBatchOutcome_DeferredIsPostponement 钉住 让位通知的推迟语义：水位不动、pending 清零，同一事实下个检查点重新评估。
//
// - 让位不烧窗口：deferred 之后事实仍晚于水位，重评必然再次命中。
// - pending 期间的 tick 连事实链都不读（在途批已占住判据）。
//
// 契约: docs/wiki/reliability/durable-delivery.md#anchor-persistence
func TestMeditationBatchOutcome_DeferredIsPostponement(t *testing.T) {
	reader := &fakeNoveltyReader{}
	reader.add("recall", time.Now().Add(-time.Minute), "user", "让位窗口里等着被反思的真事")
	mgr, inj := newNoveltyManager(reader, "recall")

	mgr.checkAndMeditate()
	require.Len(t, inj.messages, 1)
	queriesAtFire, _ := reader.counts()

	mgr.checkAndMeditate()
	assert.Len(t, inj.messages, 1, "pending in flight blocks re-entry before any read")
	_, hydrated := reader.counts()
	assert.Zero(t, hydrated-queriesAtFire, "the blocked tick must not touch the fact chain again")

	mgr.NoteMeditationBatchOutcome(false)
	assert.False(t, mgr.pending)
	assert.Zero(t, mgr.lastMeditation.Load(), "a yield NEVER advances the watermark — the window is not burned")

	mgr.checkAndMeditate()
	assert.Len(t, inj.messages, 2, "the very same facts stay novel, so the next tick retries the injection")
}

// TestDropMeditationFromMixedBatch Mixed-batch defense : meditation yields whenever it shares a batch.
func TestDropMeditationFromMixedBatch(t *testing.T) {
	newMed := func() *AgentEvent {
		return NewExternalInputEvent("meditation", model.Message{Role: model.RoleUser, Content: "[meditation] reflect"})
	}
	newTask := func() *AgentEvent {
		return NewExternalInputEvent(SourceTask, model.Message{Role: model.RoleUser, Content: "[task settled] done"})
	}
	newUser := func() *AgentEvent {
		return NewExternalInputEvent("user", model.Message{Role: model.RoleUser, Content: "hi"})
	}

	t.Run("task first — meditation dropped, turn keeps task lineage", func(t *testing.T) {
		filtered := dropMeditationFromMixedBatch([]*AgentEvent{newTask(), newMed()}, "t")
		require.Len(t, filtered, 1)
		assert.Equal(t, SourceTask, extractTriggerSource(filtered))
	})

	t.Run("meditation first — meditation dropped, turn keeps user lineage", func(t *testing.T) {
		filtered := dropMeditationFromMixedBatch([]*AgentEvent{newMed(), newUser()}, "t")
		require.Len(t, filtered, 1)
		assert.Equal(t, "user", extractTriggerSource(filtered))
	})

	t.Run("pure meditation batch passes through", func(t *testing.T) {
		filtered := dropMeditationFromMixedBatch([]*AgentEvent{newMed()}, "t")
		require.Len(t, filtered, 1)
		assert.Equal(t, "meditation", extractTriggerSource(filtered))
	})

	t.Run("no meditation batch passes through", func(t *testing.T) {
		filtered := dropMeditationFromMixedBatch([]*AgentEvent{newUser(), newTask()}, "t")
		assert.Len(t, filtered, 2)
	})
}

// TestExtractTriggerSource_TaskSettleLineage 钉住 提取触发源必须尊重谱系：冥想回合内产生的回收事件在来源袋里带着冥想标记。
// - 于是回收回合的产出留在冥想投递门之后；
// - 若回落到"最后一次用户闲聊"，内部产出就会泄漏给用户。
func TestExtractTriggerSource_TaskSettleLineage(t *testing.T) {
	newMedSettle := func() *AgentEvent {
		evt := NewExternalInputEvent(SourceTask, model.Message{Role: model.RoleUser, Content: "[task settled] meditation-spawned job"})
		evt.Metadata[tagentevent.MetaKeyTriggerSource] = "meditation"
		return evt
	}
	newUserSettle := func() *AgentEvent {
		evt := NewExternalInputEvent(SourceTask, model.Message{Role: model.RoleUser, Content: "[task settled] user-spawned job"})
		evt.Metadata[tagentevent.MetaKeyTriggerSource] = "user"
		return evt
	}
	newBareTask := func() *AgentEvent {
		return NewExternalInputEvent(SourceTask, model.Message{Role: model.RoleUser, Content: "[task settled] legacy"})
	}
	newUser := func() *AgentEvent {
		return NewExternalInputEvent("user", model.Message{Role: model.RoleUser, Content: "hi"})
	}

	t.Run("meditation lineage wins over mechanical task source", func(t *testing.T) {
		assert.Equal(t, "meditation", extractTriggerSource([]*AgentEvent{newMedSettle()}))
	})

	t.Run("user lineage from user-spawned task resolves to user", func(t *testing.T) {
		assert.Equal(t, "user", extractTriggerSource([]*AgentEvent{newUserSettle()}))
	})

	t.Run("bare task event without lineage keeps mechanical source", func(t *testing.T) {
		assert.Equal(t, SourceTask, extractTriggerSource([]*AgentEvent{newBareTask()}))
	})

	t.Run("real user input outranks meditation lineage in mixed batch", func(t *testing.T) {
		assert.Equal(t, "user", extractTriggerSource([]*AgentEvent{newMedSettle(), newUser()}))
		assert.Equal(t, "user", extractTriggerSource([]*AgentEvent{newUser(), newMedSettle()}))
	})

	t.Run("user lineage outranks foreign mechanical source", func(t *testing.T) {
		assert.Equal(t, "user", extractTriggerSource([]*AgentEvent{newBareTask(), newUserSettle()}))
	})

	t.Run("empty batch defaults to user", func(t *testing.T) {
		assert.Equal(t, "user", extractTriggerSource(nil))
	})

	t.Run("lineage_absent task settles degrade to task-unstamped", func(t *testing.T) {
		newMarkedBareTask := func() *AgentEvent {
			evt := NewExternalInputEvent(SourceTask, model.Message{Role: model.RoleUser, Content: "[task settled] restored-without-origin"})
			evt.Metadata["lineage_absent"] = "true"
			return evt
		}
		assert.Equal(t, "task-unstamped", extractTriggerSource([]*AgentEvent{newMarkedBareTask()}))
		assert.Equal(t, "user", extractTriggerSource([]*AgentEvent{newMarkedBareTask(), newUser()}))
	})
}

// TestNewTaskSettledEvent_LineageAbsentMarked 钉住 Origin 缺失的任务在源头事实上打谱系缺失标记，宿主侧对未标注任务与一切未识别值一律扣留。
// - 白名单只有一个来源：源头打标、宿主判断，两处不各写一份。
// 契约: docs/wiki/platform/reincarnation-notice.md#detection
func TestNewTaskSettledEvent_LineageAbsentMarked(t *testing.T) {
	evt := newTaskSettledEvent(&task.Task{ID: "t-no-origin", Spec: task.TaskSpec{Kind: "command", Desc: "svc"}},
		task.SettleSignal{Kind: task.SettleCompleted, Output: "ok"}, 1<<20, "")
	require.Equal(t, "true", evt.Metadata["lineage_absent"], "Origin-less task must be marked at the source")

	evt2 := newTaskSettledEvent(&task.Task{ID: "t-with-origin", Spec: task.TaskSpec{
		Kind: "command", Desc: "svc", Origin: map[string]string{"trigger_source": "user"},
	}}, task.SettleSignal{Kind: task.SettleCompleted, Output: "ok"}, 1<<20, "")
	require.NotEqual(t, "true", evt2.Metadata["lineage_absent"], "lineaged task must not be marked")
}

func TestMeditationManager_StartStop(t *testing.T) {
	reader := &fakeNoveltyReader{}
	reader.add("recall", time.Now().Add(-time.Minute), "user", "循环窗口里的一条真事")
	inj := &mockMessageInjector{}
	cfg := MeditationConfig{
		Interval:           10 * time.Millisecond,
		MinGap:             time.Millisecond,
		PromptText:         "meditation",
		ObservedNamespaces: []string{"recall"},
	}
	mgr := NewMeditationManager(cfg, inj)
	mgr.SetNoveltyReader(reader)
	mgr.Start()

	time.Sleep(50 * time.Millisecond)

	mgr.Stop()

	assert.GreaterOrEqual(t, len(inj.messages), 1)
}

func TestMeditationManager_buildMeditationMessage(t *testing.T) {
	inj := &mockMessageInjector{}
	cfg := MeditationConfig{PromptText: "reflect and summarize"}
	mgr := NewMeditationManager(cfg, inj)

	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	msg := mgr.buildMeditationMessage(now, time.Hour, nil)

	assert.Equal(t, model.RoleUser, msg.Role)
	assert.Contains(t, msg.Content, "[meditation]")
	assert.Contains(t, msg.Content, "reflect and summarize")
	assert.Contains(t, msg.Content, "2026-06-30 12:00:00")
	assert.Contains(t, msg.Content, "首次冥想")
}

func TestMeditationManager_buildMeditationMessage_WithLastMeditation(t *testing.T) {
	inj := &mockMessageInjector{}
	cfg := MeditationConfig{PromptText: "reflect"}
	mgr := NewMeditationManager(cfg, inj)

	lastMed := time.Date(2026, 6, 30, 10, 0, 0, 0, time.UTC)
	mgr.lastMeditation.Store(lastMed.UnixMilli())

	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	msg := mgr.buildMeditationMessage(now, time.Hour, nil)

	assert.Contains(t, msg.Content, lastMed.Format("2006-01-02 15:04:05"))
	assert.NotContains(t, msg.Content, "首次冥想")
}

func TestMeditationManager_MessageContainsRequiredMarkers(t *testing.T) {
	reader := &fakeNoveltyReader{}
	reader.add("recall", time.Now().Add(-time.Minute), "user", "观察分区里的一条真事")
	mgr, inj := newNoveltyManager(reader, "recall")

	mgr.checkAndMeditate()

	require.Len(t, inj.messages, 1)
	content := inj.messages[0].Content

	assert.True(t, strings.HasPrefix(content, "[meditation]"))
	assert.Contains(t, content, "上次有效冥想时间")
	assert.Contains(t, content, "当前时间")
}

// sourceInjector records BOTH the source and the message of every injection.
type sourceInjector struct {
	entries []injEntry
}

type injEntry struct {
	source string
	msg    model.Message
}

func (s *sourceInjector) InjectMessageWithSource(source string, msg model.Message) {
	s.entries = append(s.entries, injEntry{source: source, msg: msg})
}

// TestRunEventLoop_YieldingMeditationEnvelopeConsumedNotZombied 钉住 让出与消费是两件事：冥想产出被筛出本批输入，不改动原有的消费集合。
// - 其持久信封仍须按已受理集回执并确认，绝不因被过滤就停在已领取状态而成为僵尸。
func TestRunEventLoop_YieldingMeditationEnvelopeConsumedNotZombied(t *testing.T) {
	captureModel := &requestCapturingModel{
		resp: &model.Response{ID: "ok", Done: true,
			Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "reply"}}}},
	}
	bus, err := NewReliableEventBus(t.TempDir())
	require.NoError(t, err)
	outputCh := make(chan *trpcEvent.Event, 20)
	ta := newTestTagentAgent("med-zombie", captureModel, nil, outputCh, bus)
	cm := ta.contextManager

	bus.Publish(NewExternalInputEvent("meditation", model.Message{Role: model.RoleUser, Content: "meditate-quietly"}))
	bus.Publish(NewExternalInputEvent("user", model.Message{Role: model.RoleUser, Content: "real-user-input"}))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go ta.runEventLoop(ctx, bus, cm)

	deadline := time.After(3 * time.Second)
	for captureModel.requestCount() < 1 {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for model call")
		case <-time.After(10 * time.Millisecond):
		}
	}

	settled := time.After(3 * time.Second)
	for bus.DurablePending() != 0 {
		select {
		case <-settled:
			t.Fatalf("§4.1 zombie: %d durable envelope(s) left un-consumed after the turn (yielding meditation not receipted)", bus.DurablePending())
		case <-time.After(10 * time.Millisecond):
		}
	}

	refs, err := cm.memStore.QueryEvents(memory.QueryOptions{PartitionIDs: []int{cm.partitionID}, Limit: 100})
	require.NoError(t, err)
	stored := ""
	for _, ref := range refs {
		if evt, gerr := cm.memStore.GetEvent(ref.EventKey); gerr == nil {
			stored += "\n" + evt.Content
		}
	}
	require.Contains(t, stored, "real-user-input", "selected input must be persisted")
	require.NotContains(t, stored, "meditate-quietly", "a yielding meditation is not written as an input fact")
}

// TestOnEventLoop_NotifiesMeditationBatchOutcome 钉住 event_loop 的冥想批结果接线（唯一回调点）。
// - 纯冥想批报 consumed，混合批在让位点报 deferred，不含冥想事件的批永不通知。
//
// - RunFlow 失败仍是 consumed：失败也算执行，防坏模型把重投烧成风暴。
// - deferred 与 consumed 的账都在 manager 侧可见：水位只在 consumed 推进到注入时刻。
// - 用户批不通知任何账：pending 的批继续在途，回合活动碰不到冥想门。
//
// 契约: docs/wiki/agent/event-flow.md#e2e-turn-sequence
func TestOnEventLoop_NotifiesMeditationBatchOutcome(t *testing.T) {
	t.Run("pure meditation batch reports consumed even when RunFlow fails", func(t *testing.T) {
		failing := &failingCaptureModel{}
		bus, err := NewReliableEventBus(t.TempDir())
		require.NoError(t, err)
		ta := newTestTagentAgent("med-consume", failing, nil, make(chan *trpcEvent.Event, 10), bus)

		reader := &fakeNoveltyReader{}
		reader.add("recall", time.Now().Add(-time.Minute), "user", "一条等着被反思的真事")
		mgr, inj := newNoveltyManager(reader, "recall")
		ta.meditationMgr = mgr
		mgr.checkAndMeditate()
		require.Len(t, inj.messages, 1, "precondition: the manager holds one pending injection")
		injectAt := mgr.pendingSince

		medEvt := NewExternalInputEvent("meditation", model.Message{Role: model.RoleUser, Content: "[meditation] reflect"})
		bus.Publish(medEvt)
		received, err := bus.Pull(context.Background())
		require.NoError(t, err)
		ta.processTurn(context.Background(), ta.contextManager, received)

		require.Positive(t, len(failing.snapshot()), "precondition: the failing model did get the batch")
		assert.Equal(t, injectAt, mgr.lastMeditation.Load(),
			"a failed turn is consumed too — the watermark advanced to the injection moment")
		assert.False(t, mgr.pending)
	})

	t.Run("mixed batch reports deferred at the drop point without touching the watermark", func(t *testing.T) {
		captureModel := &requestCapturingModel{
			resp: &model.Response{ID: "ok", Done: true,
				Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "reply"}}}},
		}
		bus, err := NewReliableEventBus(t.TempDir())
		require.NoError(t, err)
		ta := newTestTagentAgent("med-defer", captureModel, nil, make(chan *trpcEvent.Event, 10), bus)

		reader := &fakeNoveltyReader{}
		reader.add("recall", time.Now().Add(-time.Minute), "user", "让位窗口里的事实")
		mgr, inj := newNoveltyManager(reader, "recall")
		ta.meditationMgr = mgr
		mgr.checkAndMeditate()
		require.Len(t, inj.messages, 1)
		require.True(t, mgr.pending)

		bus.Publish(NewExternalInputEvent("user", model.Message{Role: model.RoleUser, Content: "real user input"}))
		bus.Publish(NewExternalInputEvent("meditation", model.Message{Role: model.RoleUser, Content: "[meditation] reflect"}))
		received, err := bus.Pull(context.Background())
		require.NoError(t, err)
		require.Len(t, received, 2, "precondition: one batch carrying meditation AND real input")
		ta.processTurn(context.Background(), ta.contextManager, received)

		assert.Zero(t, mgr.lastMeditation.Load(), "a yield NEVER advances the watermark — the window stays unburned")
		assert.False(t, mgr.pending, "deferred clears the re-entry guard so the next tick can retry")
		assert.Len(t, inj.messages, 1, "the yield itself injects nothing extra")
	})

	t.Run("a turn without meditation reports no outcome at all", func(t *testing.T) {
		captureModel := &requestCapturingModel{
			resp: &model.Response{ID: "ok", Done: true,
				Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "reply"}}}},
		}
		bus, err := NewReliableEventBus(t.TempDir())
		require.NoError(t, err)
		ta := newTestTagentAgent("med-silent", captureModel, nil, make(chan *trpcEvent.Event, 10), bus)

		reader := &fakeNoveltyReader{}
		reader.add("recall", time.Now().Add(-time.Minute), "user", "在途批对应的真事")
		mgr, inj := newNoveltyManager(reader, "recall")
		ta.meditationMgr = mgr
		mgr.checkAndMeditate()
		require.Len(t, inj.messages, 1)
		injectAt := mgr.pendingSince

		bus.Publish(NewExternalInputEvent("user", model.Message{Role: model.RoleUser, Content: "housework turn"}))
		received, err := bus.Pull(context.Background())
		require.NoError(t, err)
		ta.processTurn(context.Background(), ta.contextManager, received)

		assert.Zero(t, mgr.lastMeditation.Load(), "housework turns cannot move the meditation watermark")
		assert.Equal(t, injectAt, mgr.pendingSince, "the in-flight meditation batch keeps its pending bookkeeping")
		assert.True(t, mgr.pending, "an ordinary turn must not answer for the meditation batch")
	})
}

// TestMeditationManager_AnchorStoreRestore 钉住 SetAnchorStore 跨重启恢复执行水位。
// - 历史锚文件里的多余键（旧用户输入锚、旧空闲锚）由 Load 忽略，消费侧只认 lastMeditation。
func TestMeditationManager_AnchorStoreRestore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "anchors.json")
	require.NoError(t, os.WriteFile(path,
		[]byte(`{"last_user_input":111,"last_turn_end":222,"last_meditation":333}`), 0o644))

	m := NewMeditationManager(MeditationConfig{Enabled: true, Interval: time.Hour, MinGap: time.Minute}, &mockMessageInjector{})
	as, err := reliability.NewAnchorStore(path)
	require.NoError(t, err)
	m.SetAnchorStore(as)

	assert.Equal(t, int64(333), m.lastMeditation.Load(), "execution watermark restored across restart")
	assert.False(t, m.pending, "pending is in-flight bookkeeping, never restored")
}

// TestMeditationManager_AnchorStorePersist 钉住 水位推进（consumed）时持久化落盘，重启后可恢复。
// - 落盘快照只剩单锚：旧空闲锚即便字段存在也写零，Load 侧的历史键兼容由忽略机制保证。
func TestMeditationManager_AnchorStorePersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "anchors.json")
	as, err := reliability.NewAnchorStore(path)
	require.NoError(t, err)

	reader := &fakeNoveltyReader{}
	reader.add("recall", time.Now().Add(-time.Minute), "user", "会被消费的一条真事")
	m, inj := newNoveltyManager(reader, "recall")
	m.SetAnchorStore(as)

	m.checkAndMeditate()
	require.Len(t, inj.messages, 1)
	injectAt := m.pendingSince

	reloaded, err := as.Load()
	require.NoError(t, err)
	assert.Zero(t, reloaded.LastMeditation, "a fire alone persists nothing — the watermark advances on consumption")

	m.NoteMeditationBatchOutcome(true)
	reloaded, err = as.Load()
	require.NoError(t, err)
	assert.Equal(t, injectAt, reloaded.LastMeditation, "consumed persists the watermark advance")
	assert.Zero(t, reloaded.LastTurnEnd, "the persisted face carries the meditation watermark only; the idle anchor stays zero")
}

// TestMeditationManager_NoAnchorStoreInMemory 钉住 未注入 AnchorStore 时水位纯内存：消费通知不 panic、门照常判定。
// - 纯内存形态与接了锚的形态只差落盘，判据与账目一致。
// 契约: docs/wiki/reliability/durable-delivery.md#anchor-persistence
func TestMeditationManager_NoAnchorStoreInMemory(t *testing.T) {
	reader := &fakeNoveltyReader{}
	reader.add("recall", time.Now().Add(-time.Minute), "user", "纯内存形态的真事")
	m, inj := newNoveltyManager(reader, "recall")

	m.checkAndMeditate()
	require.Len(t, inj.messages, 1)
	m.NoteMeditationBatchOutcome(true)
	assert.Positive(t, m.lastMeditation.Load(), "in-memory watermark still advances on consumption")
}

func TestRetention_ClosingOneAgentKeepsSharedStoreLease(t *testing.T) {
	kvDir := t.TempDir()
	kvStore, err := kv.NewLocalFileKV(kvDir)
	require.NoError(t, err)
	store, err := memory.NewFileSegmentStore(kvStore, nil, kvDir, 100)
	require.NoError(t, err)
	store.SetRetentionLease(memory.NewRetentionLease())

	now := time.Now().UnixMilli()
	// Two agents, two inboxes, ONE shared store+lease. Each holds an un-acked
	// envelope protecting its own overdue fact original.
	type owner struct {
		bus      *EventBus
		inboxDir string
		factKey  int64
		receipt  int64
		path     string
	}
	newOwner := func(content string, overdue bool) *owner {
		o := &owner{inboxDir: t.TempDir()}
		ts := now
		if overdue {
			ts = now - 10*24*3600*1000
		}
		o.factKey = memory.NewSnowflakeEventKey(1, ts)
		o.receipt = memory.NewSnowflakeEventKey(1, now)
		require.NoError(t, store.StoreEvent(o.factKey, memory.FullEvent{
			EventKey: o.factKey, PartitionID: 1, EventType: "external_input",
			EventSummary: content, Timestamp: ts,
		}))
		bus, berr := NewReliableEventBus(o.inboxDir)
		require.NoError(t, berr)
		bus.SetRetentionGuard(store)
		o.bus = bus
		in := bus.inbox
		_, eerr := in.Enqueue(&reliability.Envelope{
			RequestID: content, State: reliability.InboxStatePending,
			Messages: []reliability.MessageSlot{{Slot: 0, SourceEvent: json.RawMessage(`{"id":"e-` + content + `","type":"external_input","message":{"role":"user","content":"x"}}`)}},
		})
		require.NoError(t, eerr)
		_, o.path, eerr = in.ClaimNext()
		require.NoError(t, eerr)
		require.NoError(t, in.PrepareFacts(o.path, tagentevent.FormatEventKey(o.receipt),
			[]json.RawMessage{json.RawMessage(`{"event_key":` + itoa(o.factKey) + `}`)}))
		require.NoError(t, bus.ArmRetentionFromInbox())
		return o
	}
	a := newOwner("agentA", true)
	b := newOwner("agentB", false)

	require.True(t, store.IsKeyProtected(a.factKey), "agentA's overdue original is leased")
	require.True(t, store.IsKeyProtected(b.factKey), "agentB's original is leased")

	require.NoError(t, a.bus.CloseDurable())
	require.True(t, store.IsKeyProtected(a.factKey), "closing an agent NEVER releases its un-acked material's lease")
	require.True(t, store.IsKeyProtected(b.factKey), "closing an agent NEVER touches another owner's lease")

	in := b.bus.inbox
	cred := reliability.ReceiptCredential{ReceiptKey: tagentevent.FormatEventKey(b.receipt)}
	require.NoError(t, in.RecordCompletion(b.path, json.RawMessage(`{"completion_version":1}`)))
	require.NoError(t, b.bus.ConfirmDurable(b.path, cred))
	require.False(t, store.IsKeyProtected(b.factKey), "agentB's ack released agentB's holder")
	require.False(t, store.IsKeyProtected(b.receipt), "including the receipt original")
	require.True(t, store.IsKeyProtected(a.factKey), "agentA's protection still stands for its next opener")
}

// TestRetention_ArmFromInboxAndReleaseOnAck 钉住 持久存储的生命周期扫描自打开起就受保留租约节制。
// - 属主从收件箱里已受理未确认的信封装上该租约，使这些信封的准备事实与回执原件挺过重启竞争；
// - 信封被确认（目录同步）之后释放租约，此后回到按年龄的正常处置。
func TestRetention_ArmFromInboxAndReleaseOnAck(t *testing.T) {
	kvDir := t.TempDir()
	kvStore, err := kv.NewLocalFileKV(kvDir)
	require.NoError(t, err)
	store, err := memory.NewFileSegmentStore(kvStore, nil, kvDir, 100)
	require.NoError(t, err)
	lease := memory.NewRetentionLease()
	store.SetRetentionLease(lease)

	now := time.Now().UnixMilli()
	factKey := memory.NewSnowflakeEventKey(1, now-10*24*3600*1000)
	receiptKey := memory.NewSnowflakeEventKey(1, now)
	require.NoError(t, store.StoreEvent(factKey, memory.FullEvent{
		EventKey: factKey, PartitionID: 1, EventType: "external_input",
		EventSummary: "unacked input", Timestamp: now - 10*24*3600*1000,
	}))

	inboxDir := t.TempDir()
	in, err := reliability.NewInbox(inboxDir, 0)
	require.NoError(t, err)
	_, err = in.Enqueue(&reliability.Envelope{
		RequestID: "r1",
		State:     reliability.InboxStatePending,
		Messages:  []reliability.MessageSlot{{Slot: 0, SourceEvent: json.RawMessage(`{"id":"e1"}`)}},
	})
	require.NoError(t, err)
	env, path, err := in.ClaimNext()
	require.NoError(t, err)
	require.NotNil(t, env)
	require.NoError(t, in.PrepareFacts(path, tagentevent.FormatEventKey(receiptKey),
		[]json.RawMessage{json.RawMessage(`{"event_key":` + itoa(factKey) + `}`)}))

	require.NoError(t, in.Close())
	bus, err := NewReliableEventBus(inboxDir)
	require.NoError(t, err)
	bus.SetRetentionGuard(store)
	require.False(t, store.IsKeyProtected(factKey), "not yet armed → nothing protected")
	select {
	case <-lease.Ready():
		t.Fatal("gate must be closed before ArmRetentionFromInbox")
	default:
	}

	require.NoError(t, bus.ArmRetentionFromInbox())
	require.True(t, store.IsKeyProtected(factKey), "armed lease must protect the prepared fact original")
	require.True(t, store.IsKeyProtected(receiptKey), "armed lease must protect the receipt original")
	select {
	case <-lease.Ready():
	case <-time.After(time.Second):
		t.Fatal("gate must open after arm so the scanner can proceed")
	}

	require.NoError(t, bus.RecordCompletion(path, json.RawMessage(`{"completion_version":1}`)))
	require.NoError(t, bus.ConfirmDurable(path, reliability.ReceiptCredential{ReceiptKey: tagentevent.FormatEventKey(receiptKey)}))
	require.False(t, store.IsKeyProtected(factKey), "ack must release the fact original (§2.8, no leak)")
	require.False(t, store.IsKeyProtected(receiptKey), "ack must release the receipt original")
}

// TestRetention_QuarantineReleasesLease 钉住 隔离与 ack 同为信封的终局处置，必须一并释放被隔离信封的保留持有者。
// - 只搬走文件而不放租约，会留下永久受保护的原件：既淘汰不掉，也数不清还欠着什么。
// 契约: docs/wiki/reliability/durable-delivery.md#reopen-refusal
func TestRetention_QuarantineReleasesLease(t *testing.T) {
	kvDir := t.TempDir()
	kvStore, err := kv.NewLocalFileKV(kvDir)
	require.NoError(t, err)
	store, err := memory.NewFileSegmentStore(kvStore, nil, kvDir, 100)
	require.NoError(t, err)
	store.SetRetentionLease(memory.NewRetentionLease())

	now := time.Now().UnixMilli()
	factKey := memory.NewSnowflakeEventKey(1, now-10*24*3600*1000)
	receiptKey := memory.NewSnowflakeEventKey(1, now)
	require.NoError(t, store.StoreEvent(factKey, memory.FullEvent{
		EventKey: factKey, PartitionID: 1, EventType: "external_input",
		EventSummary: "conflicting input", Timestamp: now - 10*24*3600*1000,
	}))

	inboxDir := t.TempDir()
	in, err := reliability.NewInbox(inboxDir, 0)
	require.NoError(t, err)
	_, err = in.Enqueue(&reliability.Envelope{
		RequestID: "rq", State: reliability.InboxStatePending,
		Messages: []reliability.MessageSlot{{Slot: 0, SourceEvent: json.RawMessage(`{"id":"eq"}`)}},
	})
	require.NoError(t, err)
	_, path, err := in.ClaimNext()
	require.NoError(t, err)
	require.NoError(t, in.PrepareFacts(path, tagentevent.FormatEventKey(receiptKey),
		[]json.RawMessage{json.RawMessage(`{"event_key":` + itoa(factKey) + `}`)}))
	require.NoError(t, in.Close())

	bus, err := NewReliableEventBus(inboxDir)
	require.NoError(t, err)
	bus.SetRetentionGuard(store)
	require.NoError(t, bus.ArmRetentionFromInbox())
	require.True(t, store.IsKeyProtected(factKey), "armed fact original is protected")
	require.True(t, store.IsKeyProtected(receiptKey), "armed receipt original is protected")

	bus.QuarantineEnvelope(path, "deterministic conflict (test)")
	require.False(t, store.IsKeyProtected(factKey), "quarantine MUST release the fact original (§2.8, no lease hang)")
	require.False(t, store.IsKeyProtected(receiptKey), "quarantine MUST release the receipt original")

	require.NoError(t, store.DeleteEvent(factKey), "post-quarantine original must be evictable")
}

// itoa renders an int64 as JSON-number text for hand-building a prepared_fact payload.
func itoa(v int64) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// receiptedCrashEnvelope builds a real durable store+inbox holding ONE envelope that
// is receipted-but-not-acked (the crash window between RecordReceipt and Ack), with a
// prepared fact original + reserved receipt key. It reopens a fresh EventBus over the
// same dir and arms the  lease, returning everything the release tests assert on.
func receiptedCrashEnvelope(t *testing.T) (bus *EventBus, store *memory.FileSegmentStore, factKey, receiptKey int64) {
	t.Helper()
	kvDir := t.TempDir()
	kvStore, err := kv.NewLocalFileKV(kvDir)
	require.NoError(t, err)
	store, err = memory.NewFileSegmentStore(kvStore, nil, kvDir, 100)
	require.NoError(t, err)
	store.SetRetentionLease(memory.NewRetentionLease())

	now := time.Now().UnixMilli()
	factKey = memory.NewSnowflakeEventKey(1, now-10*24*3600*1000)
	receiptKey = memory.NewSnowflakeEventKey(1, now)
	require.NoError(t, store.StoreEvent(factKey, memory.FullEvent{
		EventKey: factKey, PartitionID: 1, EventType: "external_input",
		EventSummary: "unacked input", Timestamp: now - 10*24*3600*1000,
	}))

	inboxDir := t.TempDir()
	in, err := reliability.NewInbox(inboxDir, 0)
	require.NoError(t, err)
	_, err = in.Enqueue(&reliability.Envelope{
		RequestID: "r1", State: reliability.InboxStatePending,
		Messages: []reliability.MessageSlot{{Slot: 0, SourceEvent: json.RawMessage(`{"id":"e1","type":"external_input","message":{"role":"user","content":"x"}}`)}},
	})
	require.NoError(t, err)
	_, path, err := in.ClaimNext()
	require.NoError(t, err)
	require.NoError(t, in.PrepareFacts(path, tagentevent.FormatEventKey(receiptKey),
		[]json.RawMessage{json.RawMessage(`{"event_key":` + itoa(factKey) + `}`)}))
	require.NoError(t, in.RecordCompletion(path, json.RawMessage(`{"completion_version":1}`)))
	require.NoError(t, in.RecordReceipt(path, reliability.ReceiptCredential{ReceiptKey: tagentevent.FormatEventKey(receiptKey)}))
	require.NoError(t, in.Close())

	bus, err = NewReliableEventBus(inboxDir)
	require.NoError(t, err)
	bus.SetRetentionGuard(store)
	require.NoError(t, bus.ArmRetentionFromInbox())
	require.True(t, store.IsKeyProtected(factKey), "precondition: receipted-but-unacked material is armed at open")
	return bus, store, factKey, receiptKey
}

// TestRetention_ReceiptedSweepReleasesLease 钉住 领取扫描清掉"已回执未确认"的信封时必须同时 ack 并释放被保护的原件。
// - 就地删除而不释放保留额，等于把受保护的键永久漏在账上。
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
func TestRetention_ReceiptedSweepReleasesLease(t *testing.T) {
	bus, store, factKey, receiptKey := receiptedCrashEnvelope(t)

	got := bus.TryPull()
	require.Empty(t, got, "a receipted envelope must be Ack-skipped, never re-executed")
	require.False(t, store.IsKeyProtected(factKey), "receipted sweep MUST release the fact original (§2.8, no lease leak)")
	require.False(t, store.IsKeyProtected(receiptKey), "receipted sweep MUST release the receipt original")
}

// fakeNoveltyReader is the external form's fact-chain double. It answers the way the
// stores do: partition filter, inclusive StartTime lower bound, timestamp ordering,
// and a Limit that truncates the page. References never carry metadata, so lineage
// only becomes visible after GetEvent.
type fakeNoveltyReader struct {
	mu sync.Mutex
	// events holds the fact chain in insertion order.
	events []*memory.FullEvent
	// queries records every QueryOptions the gate issued, in order.
	queries []memory.QueryOptions
	// hydrated counts GetEvent calls, the cost the early stop bounds.
	hydrated int
	// looseStartTime drops the StartTime bound, the shape of a store that answers
	// the whole window and leaves the watermark to the decision side.
	looseStartTime bool
	// drift rewires a hydrated event's partition while its reference keeps the
	// declared one, the shape of a store whose two faces disagree.
	drift map[int64]int
	// queryErr fails the read face.
	queryErr error
}

func (f *fakeNoveltyReader) add(namespace string, at time.Time, lineage, summary string) *memory.FullEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	pid := memory.PartitionIDFromName(namespace)
	ts := at.UnixMilli()
	fe := &memory.FullEvent{
		EventKey:     memory.NewSnowflakeEventKey(pid, ts),
		PartitionID:  pid,
		EventType:    tagentevent.TypeExternalInput,
		EventSummary: summary,
		Timestamp:    ts,
		Metadata:     map[string]string{},
	}
	if lineage != "" {
		fe.Metadata[tagentevent.MetaKeyTriggerSource] = lineage
	}
	f.events = append(f.events, fe)
	return fe
}

func (f *fakeNoveltyReader) QueryEvents(q memory.QueryOptions) ([]memory.EventReference, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queries = append(f.queries, q)
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	asked := map[int]bool{}
	for _, id := range q.PartitionIDs {
		asked[id] = true
	}
	var refs []memory.EventReference
	for _, fe := range f.events {
		if len(asked) > 0 && !asked[fe.PartitionID] {
			continue
		}
		if !f.looseStartTime && q.StartTime > 0 && fe.Timestamp < q.StartTime {
			continue
		}
		refs = append(refs, memory.EventReference{
			EventKey: fe.EventKey, PartitionID: fe.PartitionID, EventType: fe.EventType,
			EventSummary: fe.EventSummary, Timestamp: fe.Timestamp,
		})
	}
	newestFirst := q.OrderBy == "timestamp_desc"
	sort.SliceStable(refs, func(i, j int) bool {
		if newestFirst {
			return refs[i].Timestamp > refs[j].Timestamp
		}
		return refs[i].Timestamp < refs[j].Timestamp
	})
	if q.Limit > 0 && len(refs) > q.Limit {
		refs = refs[:q.Limit]
	}
	return refs, nil
}

func (f *fakeNoveltyReader) GetEvent(key int64) (*memory.FullEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hydrated++
	for _, fe := range f.events {
		if fe.EventKey == key {
			if to, ok := f.drift[key]; ok {
				shifted := *fe
				shifted.PartitionID = to
				return &shifted, nil
			}
			return fe, nil
		}
	}
	return nil, nil
}

func (f *fakeNoveltyReader) counts() (queries, hydrated int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.queries), f.hydrated
}

func (f *fakeNoveltyReader) queryAt(i int) (memory.QueryOptions, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if i >= len(f.queries) {
		return memory.QueryOptions{}, false
	}
	return f.queries[i], true
}

// newestTimestamp reports the freshest fact on the fake chain, the coverage bound a
// consumed window must reach or pass.
func (f *fakeNoveltyReader) newestTimestamp() int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	var newest int64
	for _, fe := range f.events {
		if fe.Timestamp > newest {
			newest = fe.Timestamp
		}
	}
	return newest
}

// newNoveltyManager builds a manager over the given observation surface with a zero
// watermark, so the rhythm gate passes straight through and only the novelty gate can
// stop a fire.
func newNoveltyManager(reader NoveltyReader, namespaces ...string) (*MeditationManager, *mockMessageInjector) {
	inj := &mockMessageInjector{}
	mgr := NewMeditationManager(MeditationConfig{
		MinGap:             time.Millisecond,
		PromptText:         "reflect",
		ObservedNamespaces: namespaces,
	}, inj)
	if reader != nil {
		mgr.SetNoveltyReader(reader)
	}
	return mgr, inj
}

// TestMeditationManager_CrossPartitionNoveltyGate 钉住 新鲜度门只读被观察分区的事实链。
// - 观察面外的分区永不参与；引用页无谱系，判定必须等水合。
// - 命中即早停；读取失败或没接事实链则门保持关闭，没有第二条数据面可回落。
func TestMeditationManager_CrossPartitionNoveltyGate(t *testing.T) {
	t.Run("observed partition opens the gate", func(t *testing.T) {
		reader := &fakeNoveltyReader{}
		reader.add("recall", time.Now().Add(-time.Minute), "user", "用户追问了召回结果")
		mgr, inj := newNoveltyManager(reader, "recall")

		mgr.checkAndMeditate()

		require.Len(t, inj.messages, 1, "a non-self-managed event in an observed partition is novelty")
		assert.Contains(t, inj.messages[0].Content, "观察分区概况")
		assert.Contains(t, inj.messages[0].Content, "用户追问了召回结果")

		q, ok := reader.queryAt(0)
		require.True(t, ok, "the gate must read the fact chain")
		assert.Zero(t, q.StartTime, "without a watermark the first pass asks for everything since epoch")
		assert.Equal(t, []int{memory.PartitionIDFromName("recall")}, q.PartitionIDs)
		assert.Equal(t, noveltyScanPageLimit, q.Limit, "an explicit page bound keeps hydration cost bounded")
	})

	t.Run("unobserved partition never opens the gate", func(t *testing.T) {
		reader := &fakeNoveltyReader{}
		reader.add("others", time.Now().Add(-time.Minute), "user", "未被授权的分区")
		mgr, inj := newNoveltyManager(reader, "recall")

		mgr.checkAndMeditate()

		assert.Empty(t, inj.messages, "the query filter is what keeps an unauthorized partition out of the decision")
		queries, hydrated := reader.counts()
		assert.Equal(t, 1, queries)
		assert.Zero(t, hydrated, "nothing outside the surface is ever hydrated")
	})

	t.Run("hydrated partition outside the surface is not counted", func(t *testing.T) {
		reader := &fakeNoveltyReader{drift: map[int64]int{}}
		late := reader.add("recall", time.Now().Add(-time.Minute), "user", "引用与实体分区不一致")
		reader.drift[late.EventKey] = memory.PartitionIDFromName("others")
		mgr, inj := newNoveltyManager(reader, "recall")

		mgr.checkAndMeditate()

		assert.Empty(t, inj.messages, "a record landing off the observation surface never counts as novelty")
	})

	t.Run("first hit stops the pass", func(t *testing.T) {
		reader := &fakeNoveltyReader{}
		now := time.Now()
		reader.add("recall", now.Add(-3*time.Minute), "user", "更早的一条")
		reader.add("recall", now.Add(-2*time.Minute), "user", "再早的一条")
		reader.add("recall", now.Add(-time.Minute), "task", "最新的一条")
		mgr, inj := newNoveltyManager(reader, "recall")

		mgr.checkAndMeditate()

		require.Len(t, inj.messages, 1)
		_, hydrated := reader.counts()
		assert.Equal(t, 1, hydrated, "newest-first means one hydration settles the gate")
		assert.Contains(t, inj.messages[0].Content, "最新的一条")
	})

	t.Run("observation surface without a reader keeps the gate closed", func(t *testing.T) {
		mgr, inj := newNoveltyManager(nil, "recall")

		for i := 0; i < 3; i++ {
			mgr.checkAndMeditate()
		}
		assert.Empty(t, inj.messages, "an unreadable fact chain never falls back to a guess")
	})

	t.Run("read failure keeps the gate closed", func(t *testing.T) {
		reader := &fakeNoveltyReader{queryErr: errors.New("segment unreadable")}
		reader.add("recall", time.Now().Add(-time.Minute), "user", "本来该触发")
		mgr, inj := newNoveltyManager(reader, "recall")

		mgr.checkAndMeditate()

		assert.Empty(t, inj.messages, "a failed read is neither「没有新事」nor「有新事」, so reflection must not act on it")
	})
}

// TestMeditationManager_SelfManagedOutputIsNotNovelty 钉住 自管谱系的产出永不重新武装新鲜度门。
// - 冥想的产出若算新事，静默期里自我供给会烧成永动。
// - 谱系清单只在 event 包一处，判定处零副本。
func TestMeditationManager_SelfManagedOutputIsNotNovelty(t *testing.T) {
	cases := []struct{ name, lineage string }{
		{"meditation output", tagentevent.LineageMeditation},
		{"consolidation hint output", tagentevent.LineageConsolidationHint},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reader := &fakeNoveltyReader{}
			reader.add("recall", time.Now().Add(-time.Minute), tc.lineage, "自管产出")
			mgr, inj := newNoveltyManager(reader, "recall")

			for i := 0; i < 3; i++ {
				mgr.checkAndMeditate()
			}
			require.Empty(t, inj.messages, "%s output must not count as novelty", tc.lineage)

			reader.add("recall", time.Now(), "user", "外部真事")
			mgr.checkAndMeditate()
			assert.Len(t, inj.messages, 1, "the gate opens as soon as a non-self-managed event lands")
		})
	}

	t.Run("non-self-managed lineage in another observed partition counts", func(t *testing.T) {
		reader := &fakeNoveltyReader{}
		reader.add("recall", time.Now().Add(-time.Minute), "task", "另一个分区的工作事件")
		mgr, inj := newNoveltyManager(reader, "recall", "planner")

		mgr.checkAndMeditate()

		assert.Len(t, inj.messages, 1, "the curator reads other agents' activity, not only human turns")
	})
}

// TestMeditationManager_UnknownLineageNotCounted 钉住 trigger_source 缺失或未知按未知谱系处理，不计入新鲜度。
// - 未知不截断扫描：它之后的候选仍要被水合判定。
func TestMeditationManager_UnknownLineageNotCounted(t *testing.T) {
	t.Run("absent key never counts", func(t *testing.T) {
		reader := &fakeNoveltyReader{}
		reader.add("recall", time.Now().Add(-time.Minute), "", "入库时没盖章的事件")
		mgr, inj := newNoveltyManager(reader, "recall")

		mgr.checkAndMeditate()

		assert.Empty(t, inj.messages, "unattributed events must not drive a cross-domain curator")
	})

	t.Run("empty value never counts", func(t *testing.T) {
		reader := &fakeNoveltyReader{}
		now := time.Now()
		blank := reader.add("recall", now.Add(-time.Minute), "user", "值被写成空串")
		blank.Metadata[tagentevent.MetaKeyTriggerSource] = ""
		mgr, inj := newNoveltyManager(reader, "recall")

		mgr.checkAndMeditate()

		assert.Empty(t, inj.messages, `trigger_source="" is unknown lineage, not "user"`)
	})

	t.Run("unknown does not truncate the scan", func(t *testing.T) {
		reader := &fakeNoveltyReader{}
		now := time.Now()
		reader.add("recall", now.Add(-2*time.Minute), "user", "较早的真事")
		reader.add("recall", now.Add(-time.Minute), "totally-unknown-source", "最新的未识字谱")
		mgr, inj := newNoveltyManager(reader, "recall")

		mgr.checkAndMeditate()

		require.Len(t, inj.messages, 1, "the pass keeps walking past unknown lineage")
		assert.Contains(t, inj.messages[0].Content, "较早的真事", "the hit is the older genuine one")
		_, hydrated := reader.counts()
		assert.Equal(t, 2, hydrated, "both candidates were hydrated")
	})
}

// TestMeditationManager_WatermarkAdvancesOnConsumed 钉住 水位是执行水位：触发只推进在途记号，批被消费才推到注入时刻。
// - 触发时刻水位仍为零：没跑起来的反思烧不掉任何事实。
// - 同一事件不会被计两次；水位之后的新事件重新开门。
// - 存储侧下界是包含式，判据侧仍自己再核一遍严格大于。
func TestMeditationManager_WatermarkAdvancesOnConsumed(t *testing.T) {
	reader := &fakeNoveltyReader{}
	reader.add("recall", time.Now().Add(-time.Minute), "user", "水位之前的真事")
	mgr, inj := newNoveltyManager(reader, "recall")

	mgr.checkAndMeditate()
	require.Len(t, inj.messages, 1, "first pass reads from epoch and fires")
	assert.Zero(t, mgr.lastMeditation.Load(), "a fire only injects: the execution watermark stays put")
	require.True(t, mgr.pending, "the batch is in flight")
	injectAt := mgr.pendingSince
	assert.Contains(t, inj.messages[0].Content, "首次冥想", "the first fire had no execution on record")

	mgr.NoteMeditationBatchOutcome(true)
	assert.Equal(t, injectAt, mgr.lastMeditation.Load(),
		"consumption advances the watermark to the injection moment")

	time.Sleep(2 * time.Millisecond)
	mgr.checkAndMeditate()
	assert.Len(t, inj.messages, 1, "a spent event must not stay novelty after the watermark moved")

	second, ok := reader.queryAt(1)
	require.True(t, ok)
	assert.Equal(t, injectAt+1, second.StartTime, "the strictly-greater watermark is encoded on an inclusive lower bound")

	time.Sleep(2 * time.Millisecond)
	reader.add("recall", time.Now(), "user", "水位之后的新事")
	mgr.checkAndMeditate()
	require.Len(t, inj.messages, 2, "the gate re-opens on activity past the watermark")

	third, ok := reader.queryAt(2)
	require.True(t, ok)
	assert.Equal(t, injectAt+1, third.StartTime, "a check reads the window the last execution left behind")
	assert.Contains(t, inj.messages[1].Content, "水位之后的新事")

	secondInject := mgr.pendingSince
	require.Greater(t, secondInject, injectAt)
	mgr.NoteMeditationBatchOutcome(true)
	advanced := mgr.lastMeditation.Load()
	require.Equal(t, secondInject, advanced, "the second execution moved the watermark to its own injection moment")

	time.Sleep(2 * time.Millisecond)
	mgr.checkAndMeditate()
	assert.Len(t, inj.messages, 2, "nothing is new past the moved watermark")

	fourth, ok := reader.queryAt(3)
	require.True(t, ok)
	assert.Equal(t, advanced+1, fourth.StartTime, "the next pass starts right after the new watermark")

	t.Run("a store that ignores the bound cannot resurrect a spent event", func(t *testing.T) {
		stale := &fakeNoveltyReader{}
		stale.add("recall", time.Now().Add(-time.Hour), "user", "很久以前的一条")
		m, in := newNoveltyManager(stale, "recall")
		m.checkAndMeditate()
		require.Len(t, in.messages, 1)
		m.NoteMeditationBatchOutcome(true)

		stale.looseStartTime = true
		before, _ := stale.counts()
		time.Sleep(2 * time.Millisecond)
		m.checkAndMeditate()

		assert.Len(t, in.messages, 1, "the watermark check is the decision's own, not the store's favor")
		_, hydrated := stale.counts()
		assert.Equal(t, before+1, hydrated, "the stale reference was re-read and rejected on its timestamp")
	})
}

// TestMeditationManager_ObservationSurfaceResolution 钉住 观察面按分区身份去重、按声明顺序保留，空串与重复不改变机制。
// - DeliverTo 在本层只承载不消费，投递门的语义属装配层。
func TestMeditationManager_ObservationSurfaceResolution(t *testing.T) {
	cases := []struct {
		name      string
		observed  []string
		deliverTo []string
		wantNames []string
	}{
		{"unset resolves to nothing at this layer", nil, nil, nil},
		{"empty entries resolve to nothing", []string{"", ""}, nil, nil},
		{"duplicate namespace collapses", []string{"recall", "recall"}, nil, []string{"recall"}},
		{"declared order survives", []string{"recall", "planner"}, []string{"worker"}, []string{"recall", "planner"}},
		{"deliver list alone is not an observation surface", nil, []string{"worker"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mgr := NewMeditationManager(MeditationConfig{
				ObservedNamespaces: tc.observed,
				DeliverTo:          tc.deliverTo,
			}, &mockMessageInjector{})

			var got []string
			for _, p := range mgr.observed {
				assert.Equal(t, memory.PartitionIDFromName(p.name), p.id, "identity maps to partition through the single derivation axis")
				got = append(got, p.name)
			}
			assert.Equal(t, tc.wantNames, got)
			assert.Equal(t, tc.deliverTo, mgr.cfg.DeliverTo, "the manager carries the delivery whitelist as configured")
		})
	}
}

// TestMeditationManager_EmptySurfaceIssuesNoRead 钉住 观察面为空时门保持关闭，且绝不发起一次事实链读取。
// - 空集合下发起查询等于全分区扫描，越出声明的观察面即越权。
func TestMeditationManager_EmptySurfaceIssuesNoRead(t *testing.T) {
	reader := &fakeNoveltyReader{}
	reader.add("recall", time.Now().Add(-time.Minute), "user", "未声明分区里的事")
	mgr, inj := newNoveltyManager(reader)

	mgr.checkAndMeditate()

	assert.Empty(t, inj.messages, "no declared surface means no evidence face, so the gate stays closed")
	queries, hydrated := reader.counts()
	assert.Zero(t, queries, "an empty surface must never degrade into an all-partition scan")
	assert.Zero(t, hydrated)
}

// TestMeditationManager_ScanConcurrentWithOutcomeNotes 钉住 判据读取与批结果通知可并发，-race 下无撕裂。
// - 水位只能等于某次注入时刻或保持零：并发通知烧不出杂值。
// - 锚锁仍序列化「pending 迁移 + 水位更新 + 持久化快照」。
func TestMeditationManager_ScanConcurrentWithOutcomeNotes(t *testing.T) {
	reader := &fakeNoveltyReader{}
	reader.add("recall", time.Now().Add(-time.Minute), "user", "并发窗口里的一条真事")
	mgr, inj := newNoveltyManager(reader, "recall")
	mgr.SetTaskController(&fakeTaskController{})
	started := time.Now().UnixMilli()

	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				mgr.NoteMeditationBatchOutcome(j%2 == 0)
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 20; j++ {
			mgr.checkAndMeditate()
		}
	}()
	wg.Wait()

	queries, _ := reader.counts()
	assert.Greater(t, queries, 0, "the gate kept reading the fact chain throughout")
	for _, msg := range inj.messages {
		assert.True(t, strings.HasPrefix(msg.Content, "[meditation]"), "every fire stayed a well-formed meditation")
	}
	watermark := mgr.lastMeditation.Load()
	assert.True(t, watermark == 0 || watermark >= started,
		"the watermark is either untouched or equal to some injection moment, never a torn value")
}

// TestMeditationGate_ColdStartZeroWatermarkPassesThrough 钉住 冷启动（远端积压案翻转）：零水位直通节奏门，首 tick 即完成存量通读。
// - 直通只认零水位这一条判据：回合锚与启动时刻都不参与。
// - 存量比 min_gap 还老，等的必须是新鲜度而不是间隔：首 tick 即触发。
// - 触发之后靠执行水位自锁，新事实仍要等 min_gap 的执行间下限。
func TestMeditationGate_ColdStartZeroWatermarkPassesThrough(t *testing.T) {
	reader := &fakeNoveltyReader{}
	reader.add("recall", time.Now().Add(-18*time.Hour), "user", "远端积压了十八小时的存量事实")
	inj := &mockMessageInjector{}
	mgr := NewMeditationManager(MeditationConfig{
		Interval:           10 * time.Millisecond,
		MinGap:             time.Hour,
		PromptText:         "reflect",
		ObservedNamespaces: []string{"recall"},
	}, inj)
	mgr.SetNoveltyReader(reader)

	mgr.checkAndMeditate()

	require.Len(t, inj.messages, 1, "零水位没有区间可量，直通节奏门才算迈出第一步")
	assert.Contains(t, inj.messages[0].Content, "远端积压了十八小时的存量事实")
	assert.Zero(t, mgr.lastMeditation.Load(), "触发不是执行")

	mgr.NoteMeditationBatchOutcome(true)
	require.Positive(t, mgr.lastMeditation.Load())

	mgr.checkAndMeditate()
	assert.Len(t, inj.messages, 1, "水位之后没有新事，执行后自锁")

	time.Sleep(2 * time.Millisecond)
	reader.add("recall", time.Now(), "user", "存量通读之后落地的新事实")
	mgr.checkAndMeditate()
	assert.Len(t, inj.messages, 1, "新鲜度已点亮，第二次触发仍受执行间 min_gap 下限约束")
}

// TestMeditationGate_RhythmGateMeasuresExecutions 钉住 节奏门量在两次执行之间：不足 min_gap 只推迟，下限过后同一份新鲜度仍然有效。
// - 家务回合、让位、触发都不参与该测量：只有 consumed 移动它。
// - 水位回到过去（重启恢复老锚的同构形态）即等于达标，机制里没有第二条时钟。
func TestMeditationGate_RhythmGateMeasuresExecutions(t *testing.T) {
	reader := &fakeNoveltyReader{}
	reader.add("recall", time.Now().Add(-time.Minute), "user", "第一条真事")
	inj := &mockMessageInjector{}
	mgr := NewMeditationManager(MeditationConfig{
		MinGap:             80 * time.Millisecond,
		PromptText:         "reflect",
		ObservedNamespaces: []string{"recall"},
	}, inj)
	mgr.SetNoveltyReader(reader)

	mgr.checkAndMeditate()
	require.Len(t, inj.messages, 1)
	mgr.NoteMeditationBatchOutcome(true)

	time.Sleep(2 * time.Millisecond)
	reader.add("recall", time.Now(), "user", "执行刚结束就到的第二条真事")
	mgr.checkAndMeditate()
	assert.Len(t, inj.messages, 1, "两次执行之间不足 min_gap：本轮不触发")

	time.Sleep(90 * time.Millisecond)
	mgr.checkAndMeditate()
	require.Len(t, inj.messages, 2, "min_gap 过去后，同一份新鲜度重新触发")
	assert.Contains(t, inj.messages[1].Content, "第二条真事")
}

// TestMeditationGate_PendingReentryGuardBlocksSecondFire 钉住 批在途（pending）期间的重入闸门。
// - 在途期永不二投，且连事实链都不读：一次在途注入就是本轮全部结论。
// - 消费把水位推到注入时刻：在途期新增的事实晚于该时刻，照常在下一次开门。
func TestMeditationGate_PendingReentryGuardBlocksSecondFire(t *testing.T) {
	reader := &fakeNoveltyReader{}
	reader.add("recall", time.Now().Add(-time.Minute), "user", "在途批对应的真事")
	mgr, inj := newNoveltyManager(reader, "recall")

	mgr.checkAndMeditate()
	require.Len(t, inj.messages, 1)
	injectAt := mgr.pendingSince
	queries, _ := reader.counts()

	for i := 0; i < 5; i++ {
		time.Sleep(2 * time.Millisecond)
		reader.add("recall", time.Now(), "user", itoa(int64(i)))
		mgr.checkAndMeditate()
	}
	require.Greater(t, reader.newestTimestamp(), injectAt, "在途期新增的事实确实晚于注入时刻")

	assert.Len(t, inj.messages, 1, "pending 期间永不二投")
	afterQueries, _ := reader.counts()
	assert.Equal(t, queries, afterQueries, "在途批的 tick 不发起新的事实链读取")

	time.Sleep(2 * time.Millisecond)
	mgr.NoteMeditationBatchOutcome(true)
	assert.Equal(t, injectAt, mgr.lastMeditation.Load(), "结账仍用注入时刻")

	mgr.checkAndMeditate()
	require.Len(t, inj.messages, 2, "在途期新增的事实晚于注入时刻，批消费后仍要重开一次门")
	second, ok := reader.queryAt(1)
	require.True(t, ok)
	assert.Equal(t, injectAt+1, second.StartTime, "水位覆盖到注入时刻为止")
}

// TestMeditationGate_ContinuousYieldKeepsWindow 钉住 连续让位的窗口无损：让位=推迟，不是放弃。
// - 每一轮 deferred 都不动水位、不清空新鲜度，下一 tick 重投同一份事实。
// - 最终执行的注入时刻晚于整窗事实，一次消费即覆盖全程，此后自锁。
func TestMeditationGate_ContinuousYieldKeepsWindow(t *testing.T) {
	reader := &fakeNoveltyReader{}
	reader.add("recall", time.Now().Add(-time.Minute), "user", "让位窗口第一条")
	mgr, inj := newNoveltyManager(reader, "recall")
	windowFacts := []string{"让位窗口第二条", "让位窗口第三条", "让位窗口第四条"}

	for round := 0; round < len(windowFacts); round++ {
		mgr.checkAndMeditate()
		require.Len(t, inj.messages, round+1, "第 %d 轮让位后下个 tick 重投同一份新鲜度", round)
		assert.Zero(t, mgr.lastMeditation.Load(), "让位永不推进水位")
		require.True(t, mgr.pending)

		time.Sleep(2 * time.Millisecond)
		reader.add("recall", time.Now(), "user", windowFacts[round])
		mgr.NoteMeditationBatchOutcome(false)
		assert.False(t, mgr.pending, "deferred 清零重入闸门")
		assert.Zero(t, mgr.lastMeditation.Load(), "deferred 之后水位仍然为零")
	}

	windowBound := reader.newestTimestamp()
	mgr.checkAndMeditate()
	require.Len(t, inj.messages, 4)
	injectAt := mgr.pendingSince
	assert.GreaterOrEqual(t, injectAt, windowBound, "最终注入时刻晚于整窗事实")

	mgr.NoteMeditationBatchOutcome(true)
	assert.Equal(t, injectAt, mgr.lastMeditation.Load(), "一次消费即把整窗水位推到注入时刻")
	assert.Contains(t, inj.messages[3].Content, "让位窗口第四条", "被执行的批读到的就是窗口里最新的一条")

	time.Sleep(2 * time.Millisecond)
	mgr.checkAndMeditate()
	assert.Len(t, inj.messages, 4, "窗口已被这次执行覆盖，自锁")
}

// TestMeditationGate_PendingLongHoldKeepsSingleton 钉住在途批长期未决只出观察线、绝不重投：长回合执行期注入恒单例。
// - 真实模型回合时长无上界：按时间阈值的复位会把还在跑的长回合误判成注入丢失，酿成重投踩踏（实测 71 张卡堆一批）。
// - 通知缺位的兜底取向是 fail-safe 停摆而非风暴；恢复只依赖批的真实结果通知。
func TestMeditationGate_PendingLongHoldKeepsSingleton(t *testing.T) {
	reader := &fakeNoveltyReader{}
	reader.add("recall", time.Now().Add(-time.Minute), "user", "一条等着被反思的真事")
	inj := &mockMessageInjector{}
	mgr := NewMeditationManager(MeditationConfig{
		Interval:           10 * time.Millisecond,
		MinGap:             time.Hour,
		PromptText:         "reflect",
		ObservedNamespaces: []string{"recall"},
	}, inj)
	mgr.SetNoveltyReader(reader)

	mgr.checkAndMeditate()
	require.Len(t, inj.messages, 1)
	firstInject := mgr.pendingSince
	require.Positive(t, firstInject)

	time.Sleep(15 * time.Millisecond)
	mgr.checkAndMeditate()
	assert.Len(t, inj.messages, 1, "未到 3×interval 的 tick 只防重入")
	assert.True(t, mgr.pending, "在途批仍受保护")

	time.Sleep(60 * time.Millisecond)
	mgr.checkAndMeditate()
	mgr.checkAndMeditate()
	assert.Len(t, inj.messages, 1, "超过 3×interval 也不重投：长回合的在途批必须保持单例")
	assert.True(t, mgr.pending, "未决批仍受保护")
	assert.Zero(t, mgr.lastMeditation.Load(), "长期未决不烧水位，窗口仍然无损")

	mgr.NoteMeditationBatchOutcome(true)
	assert.Equal(t, firstInject, mgr.lastMeditation.Load(), "结账号只认最初的注入时刻")
}

// TestMeditationGate_HouseworkStreamLeavesGatesTransparent 钉住 家务流对冥想门彻底透明。
// - 自管谱系的产出（冥想、巩固提示、未标注）永不点亮新鲜度门。
// - 家务回合从不通知批结果：节奏门量的是冥想执行，与家务无关。
// - 每 tick 的代价就是一次只读扫描，与事件同量级。
func TestMeditationGate_HouseworkStreamLeavesGatesTransparent(t *testing.T) {
	reader := &fakeNoveltyReader{}
	now := time.Now()
	for i := 0; i < 5; i++ {
		at := now.Add(time.Duration(i) * time.Millisecond)
		reader.add("selfmed", at, tagentevent.LineageMeditation, "冥想回合的产出")
		reader.add("selfmed", at, tagentevent.LineageConsolidationHint, "巩固提示的产出")
		reader.add("selfmed", at, "", "没盖章的自管产出")
	}
	mgr, inj := newNoveltyManager(reader, "selfmed")

	for i := 0; i < 4; i++ {
		mgr.checkAndMeditate()
	}
	assert.Empty(t, inj.messages, "家务再多也不构成新鲜度")
	assert.Zero(t, mgr.lastMeditation.Load(), "没有执行过，水位就该是零")
	queries, _ := reader.counts()
	assert.Equal(t, 4, queries, "每个 tick 一次只读扫描")

	reader.add("selfmed", time.Now(), "user", "用户事实终于落地")
	mgr.checkAndMeditate()
	require.Len(t, inj.messages, 1, "家务堆之上的一条真事照常开门")
	assert.Contains(t, inj.messages[0].Content, "用户事实终于落地")
}

// TestMeditationGate_ObservedSurfaceMatrix 钉住 判据只有一条，观察面三组走同一条路径。
// - 三组 = {自身分区}、{他人分区}、{自身＋他人}。
// - 三组的触发与不触发条件完全同构：观察面上的非自管事件 + 执行间下限达标才注入；水位只在批被消费时推进到注入时刻，推进后自锁。
// - 组间差别只有分区集合本身，没有任何一组留着第二条数据面。
func TestMeditationGate_ObservedSurfaceMatrix(t *testing.T) {
	const own = "selfmed"
	const other = "recall"
	const off = "unobserved"

	surfaces := []struct {
		name     string
		observed []string
		onFace   string
	}{
		{"观察面＝自身分区", []string{own}, own},
		{"观察面＝他人分区", []string{other}, other},
		{"观察面＝自身＋他人", []string{own, other}, other},
	}

	for _, s := range surfaces {
		t.Run(s.name, func(t *testing.T) {
			t.Run("触发只注入，消费才推进水位并自锁", func(t *testing.T) {
				reader := &fakeNoveltyReader{}
				reader.add(s.onFace, time.Now().Add(-time.Minute), "user", "观察面上的一条真事")
				mgr, inj := newNoveltyManager(reader, s.observed...)

				mgr.checkAndMeditate()

				require.Len(t, inj.messages, 1, "非自管事件遇上零水位就是冥想")
				assert.Equal(t, model.RoleUser, inj.messages[0].Role)
				assert.Contains(t, inj.messages[0].Content, "[meditation]")
				assert.Contains(t, inj.messages[0].Content, "reflect")
				assert.Zero(t, mgr.lastMeditation.Load(), "触发只注入：执行水位仍为零")
				require.True(t, mgr.pending, "批在途")

				mgr.NoteMeditationBatchOutcome(true)
				assert.Positive(t, mgr.lastMeditation.Load(), "消费把水位推到注入时刻")

				time.Sleep(2 * time.Millisecond)
				mgr.checkAndMeditate()
				assert.Len(t, inj.messages, 1, "水位之后没有新事，执行后自锁")
			})

			t.Run("观察面上无非自管事件则不开", func(t *testing.T) {
				reader := &fakeNoveltyReader{}
				reader.add(s.onFace, time.Now().Add(-time.Minute), tagentevent.LineageMeditation, "自管产出")
				mgr, inj := newNoveltyManager(reader, s.observed...)

				for i := 0; i < 3; i++ {
					mgr.checkAndMeditate()
				}

				assert.Empty(t, inj.messages, "自管产出永不重新武装新鲜度门")
			})

			t.Run("节奏不足只推迟不否决", func(t *testing.T) {
				reader := &fakeNoveltyReader{}
				reader.add(s.onFace, time.Now().Add(-time.Minute), "user", "观察面上的一条真事")
				inj := &mockMessageInjector{}
				mgr := NewMeditationManager(MeditationConfig{
					MinGap:             time.Hour,
					PromptText:         "reflect",
					ObservedNamespaces: s.observed,
				}, inj)
				mgr.SetNoveltyReader(reader)

				mgr.checkAndMeditate()
				require.Len(t, inj.messages, 1)
				mgr.NoteMeditationBatchOutcome(true)

				time.Sleep(2 * time.Millisecond)
				reader.add(s.onFace, time.Now(), "user", "执行刚结束就到的新事")
				mgr.checkAndMeditate()
				assert.Len(t, inj.messages, 1, "两次执行之间不足 min_gap：本轮不触发")

				mgr.lastMeditation.Store(time.Now().Add(-2 * time.Hour).UnixMilli())
				mgr.checkAndMeditate()
				assert.Len(t, inj.messages, 2, "下限过去后，同一份新鲜度仍然有效")
			})

			t.Run("零水位直通：从未执行过也开门", func(t *testing.T) {
				reader := &fakeNoveltyReader{}
				reader.add(s.onFace, time.Now().Add(-time.Minute), "user", "观察面上的一条真事")
				inj := &mockMessageInjector{}
				mgr := NewMeditationManager(MeditationConfig{
					MinGap:             time.Hour,
					PromptText:         "reflect",
					ObservedNamespaces: s.observed,
				}, inj)
				mgr.SetNoveltyReader(reader)

				mgr.checkAndMeditate()

				assert.Len(t, inj.messages, 1, "没有前一次执行就没有区间可量，冷启动直通")
			})

			t.Run("未接事实链则门关且不猜", func(t *testing.T) {
				mgr, inj := newNoveltyManager(nil, s.observed...)

				for i := 0; i < 3; i++ {
					mgr.checkAndMeditate()
				}

				assert.Empty(t, inj.messages, "unreadable evidence never degrades into a guess")
			})

			t.Run("读取失败则门关", func(t *testing.T) {
				reader := &fakeNoveltyReader{queryErr: errors.New("segment unreadable")}
				reader.add(s.onFace, time.Now().Add(-time.Minute), "user", "本来该触发")
				mgr, inj := newNoveltyManager(reader, s.observed...)

				mgr.checkAndMeditate()

				assert.Empty(t, inj.messages, "读失败既不是「没有新事」也不是「有新事」，不得据以反思")
			})

			t.Run("观察面外的事件不计入", func(t *testing.T) {
				reader := &fakeNoveltyReader{}
				reader.add(off, time.Now().Add(-time.Minute), "user", "面外分区的事")
				mgr, inj := newNoveltyManager(reader, s.observed...)

				mgr.checkAndMeditate()

				assert.Empty(t, inj.messages, "查询过滤器就是那条授权边界")
				queries, hydrated := reader.counts()
				assert.Equal(t, 1, queries)
				assert.Zero(t, hydrated, "面外分区从不水合")
			})

			t.Run("查询只带本组声明的分区", func(t *testing.T) {
				reader := &fakeNoveltyReader{}
				mgr, _ := newNoveltyManager(reader, s.observed...)

				mgr.checkAndMeditate()

				q, ok := reader.queryAt(0)
				require.True(t, ok, "事实链是每一组观察面唯一的数据面")
				want := make([]int, 0, len(s.observed))
				for _, ns := range s.observed {
					want = append(want, memory.PartitionIDFromName(ns))
				}
				assert.Equal(t, want, q.PartitionIDs)
			})
		})
	}
}

// TestMeditationGate_InjectsUnderMeditationSource 钉住 反思动作恒为向本 agent 的循环注入一条 source=meditation 的输入事件。
// - 观察面配置不改变动作本身。
// - 用 user 源注入会重新武装新鲜度门，自我供给就此烧成永动。
// - 在途批不二投；批消费后冥想自己的产出仍不构成下一条新鲜度。
func TestMeditationGate_InjectsUnderMeditationSource(t *testing.T) {
	reader := &fakeNoveltyReader{}
	reader.add("recall", time.Now().Add(-time.Minute), "user", "被观察分区的一条真事")
	inj := &sourceInjector{}
	mgr := NewMeditationManager(MeditationConfig{
		MinGap:             time.Millisecond,
		PromptText:         "reflect",
		ObservedNamespaces: []string{"recall"},
	}, inj)
	mgr.SetNoveltyReader(reader)

	mgr.checkAndMeditate()

	require.Len(t, inj.entries, 1)
	assert.Equal(t, tagentevent.LineageMeditation, inj.entries[0].source,
		"meditation self-injection must not use the user source (would re-arm novelty)")

	for i := 0; i < 3; i++ {
		mgr.checkAndMeditate()
	}
	assert.Len(t, inj.entries, 1, "批在途期间永不二投")

	mgr.NoteMeditationBatchOutcome(true)
	reader.add("recall", time.Now(), tagentevent.LineageMeditation, "这一轮冥想自己的产出")
	time.Sleep(2 * time.Millisecond)
	for i := 0; i < 3; i++ {
		mgr.checkAndMeditate()
	}
	assert.Len(t, inj.entries, 1, "冥想派生的产出永不构成下一条新鲜度")
}

// TestMeditationDefault_ObservesOwnPartition 钉住 只开冥想、不声明观察面的装配在统一判据下自维护：
//   - 用户输入落到自身分区的事实链即构成新鲜度，零水位直通后冥想触发；
//   - 反思进入本 agent 自己的循环 session（模型请求里出现冥想头），无需任何观察面声明。
//
// 契约: docs/wiki/reliability/durable-delivery.md#lineage-visibility
func TestMeditationDefault_ObservesOwnPartition(t *testing.T) {
	mm := newRecordableMockModel(&model.Response{ID: "r", Done: true,
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "ok"}}}})

	ta, err := NewTagentAgent(&TagentConfig{
		Name: "selfmed", Model: mm, SystemPrompt: "x",
		Meditation: MeditationConfig{Enabled: true, Interval: 50 * time.Millisecond, MinGap: time.Millisecond},
	})
	require.NoError(t, err)
	defer ta.Close()

	require.Len(t, ta.meditationMgr.observed, 1, "缺省观察面就是自身分区")
	assert.Equal(t, memory.PartitionIDFromName("selfmed"), ta.meditationMgr.observed[0].id)

	out, err := ta.StartLoop("u", "selfmed-session")
	require.NoError(t, err)
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		for range out {
		}
	}()

	servedMeditation := func() bool {
		req := mm.GetLastRequest()
		if req == nil {
			return false
		}
		for _, msg := range req.Messages {
			if strings.Contains(msg.Content, "[meditation] 这是一次定时冥想事件") {
				return true
			}
		}
		return false
	}

	_, err = ta.InjectMessageContext(context.Background(), "user", model.NewUserMessage("self-observe-this"))
	require.NoError(t, err)

	deadline := time.After(5 * time.Second)
	for !servedMeditation() {
		select {
		case <-deadline:
			t.Fatal("a user turn landing on the agent's own partition must arm the default self-observation gate")
		case <-time.After(10 * time.Millisecond):
		}
	}

	ta.StopLoop()
	<-drained
}
