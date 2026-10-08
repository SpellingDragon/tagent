// 本文件负责冥想门控：无用户输入/无完成回合/空闲不足时不得触发，门控齐备才触发并更新锚点；
// 锚点必须跨重启持久，缺失按 0 处理。
// 契约: docs/wiki/reliability/durable-delivery.md#anchor-persistence
package agent

import (
	"context"
	"encoding/json"
	"errors"
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

// TestMeditationManager_ExternalFormReaderWiredAtConstruction 钉住生产装配接线：
//   - 外部形态（观察面非空）构造期注入事实链读缝，否则 novelty 门 fail-closed 恒关；
//   - in-loop 形态（观察面为空）不接 reader，行为与接线引入前一致。
func TestMeditationManager_ExternalFormReaderWiredAtConstruction(t *testing.T) {
	mm := newRecordableMockModel(&model.Response{ID: "r", Done: true,
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "ok"}}}})

	ext, err := NewTagentAgent(&TagentConfig{
		Model: mm, SystemPrompt: "x",
		Meditation: MeditationConfig{Enabled: true, Interval: time.Hour, MinGap: time.Hour,
			ObservedNamespaces: []string{"someone"}},
	})
	require.NoError(t, err)
	defer ext.Close()
	assert.NotNil(t, ext.meditationMgr.noveltyReader,
		"external form must carry the fact-chain reader from construction; a nil reader keeps the gate closed forever")

	loop, err := NewTagentAgent(&TagentConfig{
		Model: mm, SystemPrompt: "x",
		Meditation: MeditationConfig{Enabled: true, Interval: time.Hour, MinGap: time.Hour},
	})
	require.NoError(t, err)
	defer loop.Close()
	assert.Nil(t, loop.meditationMgr.noveltyReader,
		"in-loop form must not touch the fact chain: default-off stays byte-identical")
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

// newNoveltyManager builds a manager over the given observation surface, already idle
// past MinGap so only the novelty gate can stop a fire.
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
	mgr.UpdateLastTurnEnd(time.Now().Add(-time.Second))
	return mgr, inj
}

// TestMeditationManager_CrossPartitionNoveltyGate 钉住 外部形态的新鲜度门只读被观察分区的事实链。
// - 观察面外的分区永不参与；引用页无谱系，判定必须等水合。
// - 命中即早停，读取失败或没接事实链则门保持关闭，绝不回落到注入锚。
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
		assert.Empty(t, inj.messages, "an unreadable fact chain never falls back to the injection anchor")
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
				mgr.UpdateLastTurnEnd(time.Now().Add(-time.Second))
				mgr.checkAndMeditate()
			}
			require.Empty(t, inj.messages, "%s output must not count as novelty", tc.lineage)

			reader.add("recall", time.Now(), "user", "外部真事")
			mgr.UpdateLastTurnEnd(time.Now().Add(-time.Second))
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

// TestMeditationManager_LineageGateIgnoresInjectionAnchor 钉住 外部形态的新鲜度判据只读事实链，注入锚只更新不解锁。
// - 两条数据面互不越界：用户注入解不开外部门，事实链也不需要注入帮忙。
// - 事件回调既不写锚点，也不构成判据路径的一部分。
func TestMeditationManager_LineageGateIgnoresInjectionAnchor(t *testing.T) {
	t.Run("user injection updates the anchor but never unlocks the external gate", func(t *testing.T) {
		reader := &fakeNoveltyReader{}
		mgr, inj := newNoveltyManager(reader, "recall")

		mgr.UpdateLastUserInput(time.Now())
		mgr.UpdateLastTurnEnd(time.Now().Add(-time.Second))
		mgr.checkAndMeditate()

		assert.Greater(t, mgr.lastUserInput.Load(), int64(0), "the injection rule keeps updating the anchor in both forms")
		assert.Empty(t, inj.messages, "without an observed partition's non-self-managed event the external gate stays closed")
	})

	t.Run("the fact chain opens the gate with no injection at all", func(t *testing.T) {
		reader := &fakeNoveltyReader{}
		reader.add("recall", time.Now().Add(-time.Minute), "user", "被观察分区里的人类输入")
		mgr, inj := newNoveltyManager(reader, "recall")

		mgr.checkAndMeditate()

		require.Len(t, inj.messages, 1)
		assert.Zero(t, mgr.lastUserInput.Load(), "the curator's own agent never received a user turn")
	})

	t.Run("event callback writes no anchor and issues no fact-chain read", func(t *testing.T) {
		reader := &fakeNoveltyReader{}
		mgr := NewMeditationManager(MeditationConfig{
			MinGap:             time.Millisecond,
			PromptText:         "reflect",
			ObservedNamespaces: []string{"recall"},
		}, &mockMessageInjector{})
		mgr.SetNoveltyReader(reader)
		callback := (&TagentAgent{name: "t", meditationMgr: mgr}).makeOnEventCallback()

		for _, source := range []string{"user", "task", tagentevent.LineageMeditation} {
			evt := trpcEvent.New("inv", "t")
			evt.Response = &model.Response{Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "out"}}}}
			evt.StateDelta = map[string][]byte{tagentevent.MetaKeyTriggerSource: []byte(source)}
			callback(evt)
		}

		assert.Zero(t, mgr.lastUserInput.Load(), "callback must not arm the novelty gate")
		assert.Zero(t, mgr.lastTurnEnd.Load(), "callback must not move the idle anchor")
		assert.Zero(t, mgr.lastMeditation.Load(), "callback must not move the watermark")
		queries, hydrated := reader.counts()
		assert.Zero(t, queries, "the decision path holds no callback-side read of lineage")
		assert.Zero(t, hydrated)
	})

	t.Run("non-user injection still never arms the anchor in the external form", func(t *testing.T) {
		reader := &fakeNoveltyReader{}
		mgr, inj := newNoveltyManager(reader, "recall")

		mgr.UpdateLastTurnEnd(time.Now().Add(-time.Second))
		for i := 0; i < 3; i++ {
			mgr.checkAndMeditate()
		}

		assert.Empty(t, inj.messages)
		assert.Zero(t, mgr.lastUserInput.Load())
	})
}

// TestMeditationManager_WatermarkAdvancesOnFire 钉住 外部形态以 lastMeditation 为 novelty 水位，触发即推进，不另立新锚。
// - 同一事件不会被计两次；水位之后的新事件重新开门。
// - 存储侧的下界是包含式，判据侧仍自己再核一遍严格大于。
func TestMeditationManager_WatermarkAdvancesOnFire(t *testing.T) {
	reader := &fakeNoveltyReader{}
	reader.add("recall", time.Now().Add(-time.Minute), "user", "水位之前的真事")
	mgr, inj := newNoveltyManager(reader, "recall")

	mgr.checkAndMeditate()
	require.Len(t, inj.messages, 1, "first pass reads from epoch and fires")
	watermark := mgr.lastMeditation.Load()
	require.Greater(t, watermark, int64(0))

	mgr.UpdateLastTurnEnd(time.Now().Add(-time.Second))
	mgr.checkAndMeditate()
	assert.Len(t, inj.messages, 1, "a spent event must not stay novelty after the watermark moved")

	second, ok := reader.queryAt(1)
	require.True(t, ok)
	assert.Equal(t, watermark+1, second.StartTime, "the strictly-greater watermark is encoded on an inclusive lower bound")
	assert.Contains(t, inj.messages[0].Content, "首次冥想", "the first fire had no watermark")

	time.Sleep(2 * time.Millisecond)
	reader.add("recall", time.Now(), "user", "水位之后的新事")
	mgr.UpdateLastTurnEnd(time.Now().Add(-time.Second))
	mgr.checkAndMeditate()
	require.Len(t, inj.messages, 2, "the gate re-opens on activity past the watermark")

	third, ok := reader.queryAt(2)
	require.True(t, ok)
	assert.Equal(t, watermark+1, third.StartTime, "a check reads the window the last fire left behind")
	assert.Contains(t, inj.messages[1].Content, "水位之后的新事")

	advanced := mgr.lastMeditation.Load()
	require.Greater(t, advanced, watermark, "the fire moved the watermark")
	time.Sleep(2 * time.Millisecond)
	mgr.UpdateLastTurnEnd(time.Now().Add(-time.Second))
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

		stale.looseStartTime = true
		before, _ := stale.counts()
		m.UpdateLastTurnEnd(time.Now().Add(-time.Second))
		m.checkAndMeditate()

		assert.Len(t, in.messages, 1, "the watermark check is the decision's own, not the store's favor")
		_, hydrated := stale.counts()
		assert.Equal(t, before+1, hydrated, "the stale reference was re-read and rejected on its timestamp")
	})
}

// TestMeditationManager_SwitchbackToInLoopKeepsAnchorSemantics 钉住 清空观察面即回切 in-loop 判据，锚点历史无需迁移。
// - 回切后判据读注入锚：水位之前的用户输入不构成新颖性。
// - in-loop 判据从不读事实链。
func TestMeditationManager_SwitchbackToInLoopKeepsAnchorSemantics(t *testing.T) {
	path := filepath.Join(t.TempDir(), "anchors.json")
	externalStore, err := reliability.NewAnchorStore(path)
	require.NoError(t, err)

	reader := &fakeNoveltyReader{}
	reader.add("recall", time.Now().Add(-time.Minute), "user", "外部事实上的一刻")
	ext, extInj := newNoveltyManager(reader, "recall")
	ext.SetAnchorStore(externalStore)
	ext.UpdateLastUserInput(time.Now().Add(-30 * time.Second))
	ext.checkAndMeditate()

	require.Len(t, extInj.messages, 1, "the external form fired on the fact chain")
	require.Greater(t, ext.lastUserInput.Load(), int64(0), "the injection rule kept running under the external form")
	externalQueries, _ := reader.counts()
	require.Greater(t, externalQueries, 0)

	restoredStore, err := reliability.NewAnchorStore(path)
	require.NoError(t, err)
	inj := &sourceInjector{}
	loop := NewMeditationManager(MeditationConfig{MinGap: time.Millisecond, PromptText: "reflect"}, inj)
	loop.SetAnchorStore(restoredStore)
	loop.SetNoveltyReader(reader)

	loop.UpdateLastTurnEnd(time.Now().Add(-time.Second))
	loop.checkAndMeditate()
	assert.Empty(t, inj.entries, "back in-loop, the pre-watermark user input read by the anchor gate must not re-fire")

	time.Sleep(2 * time.Millisecond)
	loop.UpdateLastUserInput(time.Now())
	loop.UpdateLastTurnEnd(time.Now().Add(-time.Second))
	loop.checkAndMeditate()

	require.Len(t, inj.entries, 1, "a user turn newer than the restored watermark arms the in-loop gate again")
	assert.Equal(t, "meditation", inj.entries[0].source)
	loopQueries, _ := reader.counts()
	assert.Equal(t, externalQueries, loopQueries, "the in-loop form never touches the fact chain")
	assert.NotContains(t, inj.entries[0].msg.Content, "观察分区概况")
}

// TestMeditationManager_ObservationSurfaceResolution 钉住 观察面按分区身份去重、按声明顺序保留，空串与重复不构成形态切换。
// - DeliverTo 在本层只承载不消费，投递门的语义属装配层。
func TestMeditationManager_ObservationSurfaceResolution(t *testing.T) {
	cases := []struct {
		name       string
		observed   []string
		deliverTo  []string
		wantNames  []string
		wantObserv bool
	}{
		{"unset stays in-loop", nil, nil, nil, false},
		{"empty entries resolve to nothing", []string{"", ""}, nil, nil, false},
		{"duplicate namespace collapses", []string{"recall", "recall"}, nil, []string{"recall"}, true},
		{"declared order survives", []string{"recall", "planner"}, []string{"worker"}, []string{"recall", "planner"}, true},
		{"deliver list alone does not switch form", nil, []string{"worker"}, nil, false},
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
			assert.Equal(t, tc.wantObserv, mgr.observing())
			assert.Equal(t, tc.deliverTo, mgr.cfg.DeliverTo, "the manager carries the delivery whitelist as configured")
		})
	}
}

// TestMeditationManager_InLoopFormNeverReadsTheFactChain 钉住 观察面为空时 in-loop 判据与摘要逐字保持既有形态。
// - 事实链双件已接入也不被查询、不被水合。
func TestMeditationManager_InLoopFormNeverReadsTheFactChain(t *testing.T) {
	reader := &fakeNoveltyReader{}
	reader.add("recall", time.Now().Add(-time.Minute), "user", "隔壁分区的事")
	mgr, inj := newNoveltyManager(reader)

	mgr.UpdateLastUserInput(time.Now().Add(-2 * time.Second))
	mgr.UpdateLastTurnEnd(time.Now().Add(-time.Second))
	mgr.checkAndMeditate()

	require.Len(t, inj.messages, 1)
	queries, hydrated := reader.counts()
	assert.Zero(t, queries, "an empty observation surface selects the anchor face, not both")
	assert.Zero(t, hydrated)
	assert.NotContains(t, inj.messages[0].Content, "观察分区概况")
}

// TestMeditationManager_ExternalScanConcurrentWithAnchorUpdates 钉住 外部判据的读取与三锚点更新可并发，-race 下无撕裂。
// - 锚点锁仍序列化「更新 + 持久化快照」。
func TestMeditationManager_ExternalScanConcurrentWithAnchorUpdates(t *testing.T) {
	reader := &fakeNoveltyReader{}
	reader.add("recall", time.Now().Add(-time.Minute), "user", "并发窗口里的一条真事")
	mgr, inj := newNoveltyManager(reader, "recall")
	mgr.SetTaskController(&fakeTaskController{})

	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				if i%2 == 0 {
					mgr.UpdateLastUserInput(time.Now())
				} else {
					mgr.UpdateLastTurnEnd(time.Now())
				}
			}
		}(i)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 20; j++ {
			mgr.checkAndMeditate()
		}
	}()
	wg.Wait()

	assert.Greater(t, mgr.lastUserInput.Load(), int64(0))
	assert.Greater(t, mgr.lastTurnEnd.Load(), int64(0))
	queries, hydrated := reader.counts()
	assert.Greater(t, queries, 0, "the external form kept reading the fact chain throughout")
	assert.GreaterOrEqual(t, hydrated, 0)
	for _, msg := range inj.messages {
		assert.True(t, strings.HasPrefix(msg.Content, "[meditation]"), "every fire stayed a well-formed meditation")
	}
}
