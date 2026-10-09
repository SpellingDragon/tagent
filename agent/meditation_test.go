// 本文件负责冥想门控：新鲜度（观察面水位后的非自管事件）与空闲（距上次回合结束）两道门齐备
// 才触发并推进水位；锚点必须跨重启持久，缺失按 0 处理。
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

func TestMeditationManager_UpdateAnchors(t *testing.T) {
	inj := &mockMessageInjector{}
	mgr := NewMeditationManager(MeditationConfig{}, inj)

	now := time.Now()
	mgr.UpdateLastTurnEnd(now)

	assert.WithinDuration(t, now, time.UnixMilli(mgr.lastTurnEnd.Load()), time.Second)
}

// TestOnEventCallback_DoesNotTouchMeditationAnchors 钉住 事件回调一律不写冥想锚点、不读事实链。
// - 空闲归回合结束判、新鲜度归观察面水位判，回调不在判据路径上。
func TestOnEventCallback_DoesNotTouchMeditationAnchors(t *testing.T) {
	reader := &fakeNoveltyReader{}
	mgr := NewMeditationManager(MeditationConfig{
		MinGap:             time.Millisecond,
		PromptText:         "reflect",
		ObservedNamespaces: []string{"recall"},
	}, &mockMessageInjector{})
	mgr.SetNoveltyReader(reader)
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

	assert.Zero(t, mgr.lastTurnEnd.Load(), "callback must not move the idle anchor")
	assert.Zero(t, mgr.lastMeditation.Load(), "callback must not move the watermark")
	queries, hydrated := reader.counts()
	assert.Zero(t, queries, "the decision path holds no callback-side read of lineage")
	assert.Zero(t, hydrated)
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

// TestMeditationManager_AnchorStoreRestore 钉住 SetAnchorStore 从持久化恢复两锚点——跨重启冥想门控连续性。
// - 历史文件里带着多余键照常恢复：未知键在 Load 时忽略即可，无需迁移。
func TestMeditationManager_AnchorStoreRestore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "anchors.json")
	require.NoError(t, os.WriteFile(path,
		[]byte(`{"last_user_input":111,"last_turn_end":222,"last_meditation":333}`), 0o644))

	m := NewMeditationManager(MeditationConfig{Enabled: true, Interval: time.Hour, MinGap: time.Minute}, &mockMessageInjector{})
	as, err := reliability.NewAnchorStore(path)
	require.NoError(t, err)
	m.SetAnchorStore(as)

	assert.Equal(t, int64(222), m.lastTurnEnd.Load(), "idle anchor restored across restart")
	assert.Equal(t, int64(333), m.lastMeditation.Load(), "novelty watermark restored across restart")
}

// TestMeditationManager_AnchorStorePersist 钉住 锚点更新持久化落盘，重启后可恢复。
// - persistAnchors 在 UpdateLastTurnEnd 时触发。
func TestMeditationManager_AnchorStorePersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "anchors.json")
	as, err := reliability.NewAnchorStore(path)
	require.NoError(t, err)
	m := NewMeditationManager(MeditationConfig{Enabled: true, Interval: time.Hour, MinGap: time.Minute}, &mockMessageInjector{})
	m.SetAnchorStore(as)

	m.UpdateLastTurnEnd(time.UnixMilli(999))

	reloaded, err := as.Load()
	require.NoError(t, err)
	assert.Equal(t, int64(999), reloaded.LastTurnEnd, "UpdateLastTurnEnd must persist")
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

// TestMeditationManager_WatermarkAdvancesOnFire 钉住 判据以 lastMeditation 为 novelty 水位，触发即推进，不另立新锚。
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

// TestMeditationManager_ScanConcurrentWithAnchorUpdates 钉住 判据的读取与锚点更新可并发，-race 下无撕裂。
// - 锚点锁仍序列化「更新 + 持久化快照」。
func TestMeditationManager_ScanConcurrentWithAnchorUpdates(t *testing.T) {
	reader := &fakeNoveltyReader{}
	reader.add("recall", time.Now().Add(-time.Minute), "user", "并发窗口里的一条真事")
	mgr, inj := newNoveltyManager(reader, "recall")
	mgr.SetTaskController(&fakeTaskController{})

	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				mgr.UpdateLastTurnEnd(time.Now())
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

	assert.Greater(t, mgr.lastTurnEnd.Load(), int64(0))
	queries, _ := reader.counts()
	assert.Greater(t, queries, 0, "the gate kept reading the fact chain throughout")
	for _, msg := range inj.messages {
		assert.True(t, strings.HasPrefix(msg.Content, "[meditation]"), "every fire stayed a well-formed meditation")
	}
}

// TestMeditationGate_ObservedSurfaceMatrix 钉住 判据只有一条，观察面三组走同一条路径。
// - 三组 = {自身分区}、{他人分区}、{自身＋他人}。
// - 三组的触发与不触发条件完全同构：观察面上的非自管事件 + 空闲达标才触发，触发即推进水位并自锁。
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
			t.Run("门齐则触发并推进水位", func(t *testing.T) {
				reader := &fakeNoveltyReader{}
				reader.add(s.onFace, time.Now().Add(-time.Minute), "user", "观察面上的一条真事")
				mgr, inj := newNoveltyManager(reader, s.observed...)

				mgr.checkAndMeditate()

				require.Len(t, inj.messages, 1, "非自管事件遇上空闲达标就是冥想")
				assert.Equal(t, model.RoleUser, inj.messages[0].Role)
				assert.Contains(t, inj.messages[0].Content, "[meditation]")
				assert.Contains(t, inj.messages[0].Content, "reflect")
				require.Greater(t, mgr.lastMeditation.Load(), int64(0), "触发即推进水位")

				mgr.UpdateLastTurnEnd(time.Now().Add(-time.Second))
				mgr.checkAndMeditate()
				assert.Len(t, inj.messages, 1, "水位之后没有新事，触发后自锁")
			})

			t.Run("观察面上无非自管事件则不开", func(t *testing.T) {
				reader := &fakeNoveltyReader{}
				reader.add(s.onFace, time.Now().Add(-time.Minute), tagentevent.LineageMeditation, "自管产出")
				mgr, inj := newNoveltyManager(reader, s.observed...)

				for i := 0; i < 3; i++ {
					mgr.UpdateLastTurnEnd(time.Now().Add(-time.Second))
					mgr.checkAndMeditate()
				}

				assert.Empty(t, inj.messages, "自管产出永不重新武装新鲜度门")
			})

			t.Run("空闲不足只推迟不否决", func(t *testing.T) {
				reader := &fakeNoveltyReader{}
				reader.add(s.onFace, time.Now().Add(-time.Minute), "user", "观察面上的一条真事")
				inj := &mockMessageInjector{}
				mgr := NewMeditationManager(MeditationConfig{
					MinGap:             time.Hour,
					PromptText:         "reflect",
					ObservedNamespaces: s.observed,
				}, inj)
				mgr.SetNoveltyReader(reader)
				mgr.UpdateLastTurnEnd(time.Now())

				mgr.checkAndMeditate()
				assert.Empty(t, inj.messages, "回合刚结束，本轮不触发")

				mgr.UpdateLastTurnEnd(time.Now().Add(-2 * time.Hour))
				mgr.checkAndMeditate()
				assert.Len(t, inj.messages, 1, "空闲达标后，同一份新鲜度仍然有效")
			})

			t.Run("从未有过回合结束则不开", func(t *testing.T) {
				reader := &fakeNoveltyReader{}
				reader.add(s.onFace, time.Now().Add(-time.Minute), "user", "观察面上的一条真事")
				inj := &mockMessageInjector{}
				mgr := NewMeditationManager(MeditationConfig{
					MinGap:             time.Millisecond,
					PromptText:         "reflect",
					ObservedNamespaces: s.observed,
				}, inj)
				mgr.SetNoveltyReader(reader)

				mgr.checkAndMeditate()
				assert.Empty(t, inj.messages, "本 agent 还没跑过回合，无空闲可反思")
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
// - 冥想派生的回合只会推迟下一次，永不构成下一条新鲜度。
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
	mgr.UpdateLastTurnEnd(time.Now().Add(-time.Second))

	mgr.checkAndMeditate()

	require.Len(t, inj.entries, 1)
	assert.Equal(t, tagentevent.LineageMeditation, inj.entries[0].source,
		"meditation self-injection must not use the user source (would re-arm novelty)")

	for i := 0; i < 3; i++ {
		mgr.UpdateLastTurnEnd(time.Now().Add(-time.Second))
		mgr.checkAndMeditate()
	}
	assert.Len(t, inj.entries, 1, "derived turn ends must not re-arm meditation")
}

// TestMeditationDefault_ObservesOwnPartition 钉住 只开冥想、不声明观察面的装配在统一判据下自维护：
//   - 用户输入落到自身分区的事实链即构成新鲜度，空闲达标后冥想触发；
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
