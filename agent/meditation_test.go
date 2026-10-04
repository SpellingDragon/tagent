// 本文件负责冥想门控：无用户输入/无完成回合/空闲不足时不得触发，门控齐备才触发并更新锚点；
// 锚点必须跨重启持久，缺失按 0 处理。
// 契约: docs/wiki/reliability/durable-delivery.md#anchor-persistence
package agent

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
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

func TestMeditationManager_UpdateAnchors(t *testing.T) {
	inj := &mockMessageInjector{}
	mgr := NewMeditationManager(MeditationConfig{}, inj)

	now := time.Now()
	mgr.UpdateLastUserInput(now)
	mgr.UpdateLastTurnEnd(now)

	assert.WithinDuration(t, now, time.UnixMilli(mgr.lastUserInput.Load()), time.Second)
	assert.WithinDuration(t, now, time.UnixMilli(mgr.lastTurnEnd.Load()), time.Second)
}

func TestMeditationManager_checkAndMeditate_SkipsWhenNoUserInput(t *testing.T) {
	inj := &mockMessageInjector{}
	mgr := NewMeditationManager(MeditationConfig{MinGap: time.Millisecond}, inj)

	mgr.UpdateLastTurnEnd(time.Now().Add(-time.Second))
	mgr.checkAndMeditate()

	assert.Empty(t, inj.messages)
}

func TestMeditationManager_checkAndMeditate_SkipsWhenNoTurnCompleted(t *testing.T) {
	inj := &mockMessageInjector{}
	mgr := NewMeditationManager(MeditationConfig{MinGap: time.Millisecond}, inj)

	mgr.UpdateLastUserInput(time.Now().Add(-time.Second))
	mgr.checkAndMeditate()

	assert.Empty(t, inj.messages)
}

func TestMeditationManager_checkAndMeditate_SkipsWhenIdleTooSmall(t *testing.T) {
	inj := &mockMessageInjector{}
	mgr := NewMeditationManager(MeditationConfig{MinGap: time.Hour}, inj)

	mgr.UpdateLastUserInput(time.Now().Add(-time.Second))
	mgr.UpdateLastTurnEnd(time.Now())
	mgr.checkAndMeditate()

	assert.Empty(t, inj.messages)
}

func TestMeditationManager_checkAndMeditate_FiresWhenGatesMet(t *testing.T) {
	inj := &mockMessageInjector{}
	cfg := MeditationConfig{
		MinGap:     time.Millisecond,
		PromptText: "meditation prompt text",
	}
	mgr := NewMeditationManager(cfg, inj)

	mgr.UpdateLastUserInput(time.Now().Add(-2 * time.Second))
	mgr.UpdateLastTurnEnd(time.Now().Add(-time.Second))
	mgr.checkAndMeditate()

	require.Len(t, inj.messages, 1)
	msg := inj.messages[0]
	assert.Equal(t, model.RoleUser, msg.Role)
	assert.Contains(t, msg.Content, "[meditation]")
	assert.Contains(t, msg.Content, cfg.PromptText)
}

func TestMeditationManager_checkAndMeditate_UpdatesLastMeditation(t *testing.T) {
	inj := &mockMessageInjector{}
	cfg := MeditationConfig{MinGap: time.Millisecond}
	mgr := NewMeditationManager(cfg, inj)

	mgr.UpdateLastUserInput(time.Now().Add(-2 * time.Second))
	mgr.UpdateLastTurnEnd(time.Now().Add(-time.Second))
	mgr.checkAndMeditate()

	assert.Greater(t, mgr.lastMeditation.Load(), int64(0))
}

// TestMeditationManager_checkAndMeditate_SkipsWithoutNewUserInput 钉住 新鲜度门：持续静默期间，自上次以来没有新用户输入就不得再次触发。
func TestMeditationManager_checkAndMeditate_SkipsWithoutNewUserInput(t *testing.T) {
	inj := &mockMessageInjector{}
	mgr := NewMeditationManager(MeditationConfig{MinGap: time.Millisecond}, inj)

	mgr.UpdateLastUserInput(time.Now().Add(-2 * time.Second))
	mgr.UpdateLastTurnEnd(time.Now().Add(-time.Second))
	mgr.checkAndMeditate()
	require.Len(t, inj.messages, 1, "first meditation fires")

	mgr.checkAndMeditate()
	assert.Len(t, inj.messages, 1, "no second meditation without new user input")

	time.Sleep(2 * time.Millisecond)
	mgr.UpdateLastUserInput(time.Now())
	mgr.UpdateLastTurnEnd(time.Now().Add(-10 * time.Millisecond))
	mgr.checkAndMeditate()
	assert.Len(t, inj.messages, 2, "meditation fires again after new user input")
}

// TestMeditationManager_MeditationDerivedTurnsDoNotRearm 钉住 由冥想派生的回合（任务结算回收）只推动空闲锚点。
// - 没有新用户输入时，无论经过多少个最小间隔窗口都不得再次触发——否则成永动。
func TestMeditationManager_MeditationDerivedTurnsDoNotRearm(t *testing.T) {
	inj := &mockMessageInjector{}
	mgr := NewMeditationManager(MeditationConfig{MinGap: time.Millisecond}, inj)

	mgr.UpdateLastUserInput(time.Now().Add(-2 * time.Second))
	mgr.UpdateLastTurnEnd(time.Now().Add(-time.Second))
	mgr.checkAndMeditate()
	require.Len(t, inj.messages, 1, "first meditation fires")

	for i := 0; i < 3; i++ {
		mgr.UpdateLastTurnEnd(time.Now().Add(-10 * time.Millisecond))
		mgr.checkAndMeditate()
	}
	assert.Len(t, inj.messages, 1, "derived turn ends must not re-arm meditation")

	time.Sleep(2 * time.Millisecond)
	mgr.UpdateLastUserInput(time.Now())
	mgr.UpdateLastTurnEnd(time.Now().Add(-10 * time.Millisecond))
	mgr.checkAndMeditate()
	assert.Len(t, inj.messages, 2, "meditation fires again after real user input")
}

// TestMeditationManager_SelfLocksAfterFiring 钉住 触发后新鲜度门自行锁住后续检查（上次用户输入不晚于上次冥想），无需在触发时重置锚点。
func TestMeditationManager_SelfLocksAfterFiring(t *testing.T) {
	inj := &mockMessageInjector{}
	mgr := NewMeditationManager(MeditationConfig{MinGap: time.Millisecond}, inj)

	mgr.UpdateLastUserInput(time.Now().Add(-2 * time.Second))
	mgr.UpdateLastTurnEnd(time.Now().Add(-time.Second))
	mgr.checkAndMeditate()
	require.Len(t, inj.messages, 1)

	for i := 0; i < 5; i++ {
		mgr.checkAndMeditate()
	}
	assert.Len(t, inj.messages, 1, "novelty gate self-locks without any reset")
}

// TestOnEventCallback_DoesNotTouchMeditationAnchors 钉住 事件回调一律不写冥想锚点：门控归注入点判新鲜度、归回合结束判空闲。
func TestOnEventCallback_DoesNotTouchMeditationAnchors(t *testing.T) {
	inj := &mockMessageInjector{}
	mgr := NewMeditationManager(MeditationConfig{MinGap: time.Hour}, inj)
	ta := &TagentAgent{name: "t", meditationMgr: mgr}
	callback := ta.makeOnEventCallback()

	final := func(source string) *trpcEvent.Event {
		evt := trpcEvent.New("inv", "t")
		evt.Response = &model.Response{Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "out"}}}}
		evt.StateDelta = map[string][]byte{tagentevent.MetaKeyTriggerSource: []byte(source)}
		return evt
	}

	callback(final("meditation"))
	callback(final("user"))
	callback(final("task"))

	assert.Zero(t, mgr.lastUserInput.Load(), "callback must not arm the novelty gate")
	assert.Zero(t, mgr.lastTurnEnd.Load(), "callback must not move the idle anchor")
}

// TestArmMeditationNoveltyGate_UserSourceOnly Injection-side arming: only source=="user" updates the novelty anchor.
func TestArmMeditationNoveltyGate_UserSourceOnly(t *testing.T) {
	inj := &mockMessageInjector{}
	mgr := NewMeditationManager(MeditationConfig{}, inj)
	ta := &TagentAgent{name: "t", meditationMgr: mgr}

	ta.armMeditationNoveltyGate("meditation")
	ta.armMeditationNoveltyGate(SourceTask)
	assert.Zero(t, mgr.lastUserInput.Load(), "non-user sources must not arm the gate")

	ta.armMeditationNoveltyGate("user")
	assert.Greater(t, mgr.lastUserInput.Load(), int64(0), "user source arms the gate")
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
	inj := &mockMessageInjector{}
	cfg := MeditationConfig{
		Interval:   10 * time.Millisecond,
		MinGap:     time.Millisecond,
		PromptText: "meditation",
	}
	mgr := NewMeditationManager(cfg, inj)

	mgr.UpdateLastUserInput(time.Now().Add(-2 * time.Second))
	mgr.UpdateLastTurnEnd(time.Now().Add(-time.Second))
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
	msg := mgr.buildMeditationMessage(now, time.Hour)

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
	msg := mgr.buildMeditationMessage(now, time.Hour)

	assert.Contains(t, msg.Content, lastMed.Format("2006-01-02 15:04:05"))
	assert.NotContains(t, msg.Content, "首次冥想")
}

func TestMeditationManager_MessageContainsRequiredMarkers(t *testing.T) {
	inj := &mockMessageInjector{}
	cfg := MeditationConfig{PromptText: "meditation prompt"}
	mgr := NewMeditationManager(cfg, inj)

	mgr.UpdateLastUserInput(time.Now().Add(-2 * time.Second))
	mgr.UpdateLastTurnEnd(time.Now().Add(-time.Second))
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

func TestSwitchCombo_MeditationDoesNotSelfFeed(t *testing.T) {
	inj := &sourceInjector{}
	mgr := NewMeditationManager(MeditationConfig{MinGap: time.Millisecond, PromptText: "reflect"}, inj)

	mgr.UpdateLastUserInput(time.Now().Add(-2 * time.Second))
	mgr.UpdateLastTurnEnd(time.Now().Add(-time.Second))
	mgr.checkAndMeditate()

	if len(inj.entries) != 1 {
		t.Fatalf("expected exactly one meditation injection, got %d", len(inj.entries))
	}
	if inj.entries[0].source == "user" {
		t.Fatalf("meditation self-injection must not use the user source (would re-arm novelty)")
	}
	if inj.entries[0].source != "meditation" {
		t.Fatalf("expected source 'meditation', got %q", inj.entries[0].source)
	}

	mgr.UpdateLastTurnEnd(time.Now().Add(-time.Second))
	mgr.checkAndMeditate()
	if len(inj.entries) != 1 {
		t.Fatalf("meditation self-fed a second round without user input: %d injections", len(inj.entries))
	}

	time.Sleep(2 * time.Millisecond)
	mgr.UpdateLastUserInput(time.Now())
	mgr.UpdateLastTurnEnd(time.Now().Add(-time.Second))
	mgr.checkAndMeditate()
	if len(inj.entries) != 2 {
		t.Fatalf("after real user input the gate should re-arm and fire, got %d injections", len(inj.entries))
	}
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

// TestMeditationManager_AnchorStoreRestore 钉住 SetAnchorStore 从持久化恢复三锚点——跨重启 冥想门控连续性（重启后不立即误触发冥想、正确计算 novelty）。
func TestMeditationManager_AnchorStoreRestore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "anchors.json")
	pre, err := reliability.NewAnchorStore(path)
	if err != nil {
		t.Fatalf("NewAnchorStore: %v", err)
	}
	if err := pre.Save(reliability.MeditationAnchors{LastUserInput: 111, LastTurnEnd: 222, LastMeditation: 333}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	m := NewMeditationManager(MeditationConfig{Enabled: true, Interval: time.Hour, MinGap: time.Minute}, &mockMessageInjector{})
	as, _ := reliability.NewAnchorStore(path)
	m.SetAnchorStore(as)

	if m.lastUserInput.Load() != 111 {
		t.Fatalf("应恢复 lastUserInput=111, got %d", m.lastUserInput.Load())
	}
	if m.lastTurnEnd.Load() != 222 {
		t.Fatalf("应恢复 lastTurnEnd=222, got %d", m.lastTurnEnd.Load())
	}
	if m.lastMeditation.Load() != 333 {
		t.Fatalf("应恢复 lastMeditation=333, got %d", m.lastMeditation.Load())
	}
}

// TestMeditationManager_AnchorStorePersist 钉住 锚点更新（UpdateLastTurnEnd）持久化落盘—— 重启后可恢复（persistAnchors 在锚点更新时触发）。
func TestMeditationManager_AnchorStorePersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "anchors.json")
	as, _ := reliability.NewAnchorStore(path)
	m := NewMeditationManager(MeditationConfig{Enabled: true, Interval: time.Hour, MinGap: time.Minute}, &mockMessageInjector{})
	m.SetAnchorStore(as)

	m.UpdateLastTurnEnd(time.UnixMilli(999))
	m.UpdateLastUserInput(time.UnixMilli(888))

	reloaded, err := as.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if reloaded.LastTurnEnd != 999 {
		t.Fatalf("UpdateLastTurnEnd 应 persist, got %d", reloaded.LastTurnEnd)
	}
	if reloaded.LastUserInput != 888 {
		t.Fatalf("UpdateLastUserInput 应 persist, got %d", reloaded.LastUserInput)
	}
}

// TestMeditationManager_NoAnchorStoreInMemory 钉住 向后兼容：未注入 AnchorStore 时锚点纯内存 （现状），Update 不 panic、不落盘。
func TestMeditationManager_NoAnchorStoreInMemory(t *testing.T) {
	m := NewMeditationManager(MeditationConfig{Enabled: true, Interval: time.Hour, MinGap: time.Minute}, &mockMessageInjector{})
	m.UpdateLastTurnEnd(time.UnixMilli(500))
	if m.lastTurnEnd.Load() != 500 {
		t.Fatalf("内存锚点应更新, got %d", m.lastTurnEnd.Load())
	}
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
