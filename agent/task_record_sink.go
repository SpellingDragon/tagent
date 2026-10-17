// 契约: docs/wiki/agent/task-lifecycle.md#restore-rebuild
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/SpellingDragon/tagent/agent/compress"
	"github.com/SpellingDragon/tagent/agent/task"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// TaskManager exposes the org-level resident task registry (R2: rebuilt
// from the fact chain at cold start, shared across executor generations).
func (ta *TagentAgent) TaskManager() *task.TaskManager {
	if ta == nil {
		return nil
	}
	return ta.taskManager
}

// partitionID PartitionID exposes the context manager's snowflake partition (registry
// rebuild queries the same partition that wrote the records).
func (ta *TagentAgent) partitionID() int {
	if ta == nil || ta.contextManager == nil {
		return 0
	}
	return ta.contextManager.partitionID
}

// RebuildTaskRegistryFromWAL（R2 1.10）：冷启动任务 registry 重建入口（build 路径
// 在 R1 投影重建之后调用；rebuildClosures 按承诺表由 tool 侧提供）。
func (ta *TagentAgent) RebuildTaskRegistryFromWAL(store memory.MemoryStore,
	rebuildClosures func(decl task.Declarative) task.TaskSpec) int {
	if ta == nil || ta.taskManager == nil || store == nil {
		return 0
	}
	rebuild := func(decl task.Declarative) task.TaskSpec {
		spec := restoredTaskSpec(decl, rebuildClosures)
		if decl.Kind == "subagent" {
			spec.ResumeFn = subagentResumeClosureWithOverrides(ta.contextManager, decl.AgentName, &subagentRounds{}, decl.Overrides)
			if decl.RoundID > 0 && decl.ProducerRound == 0 {
				spec.Start = restoredSubagentStart(ta.contextManager, decl)
			}
		}
		return spec
	}
	n, pending := rebuildTaskRegistry(store, ta.partitionID(), ta.taskManager, rebuild)
	if ta.taskRecords != nil {
		for _, fact := range pending {
			ta.taskRecords.restorePendingSignal(fact)
		}
	}
	return n
}

// SubagentRedispatcher rebuilds relaunch through the same admission and binding barriers as a fresh delegation.
// Every re-entry resolves the target on its initiating call's generation or the effective resident face.
func SubagentRedispatcher(resolve func(ctx context.Context, agentName string) (*AgentToolWrapper, *ExecLease, error), tm *task.TaskManager) func(ctx context.Context, agentName, body string, overrides *task.Overrides) (task.SpawnResult, error) {
	var redispatch func(ctx context.Context, agentName, body string, overrides *task.Overrides) (task.SpawnResult, error)
	redispatch = func(ctx context.Context, agentName, body string, overrides *task.Overrides) (task.SpawnResult, error) {
		var w *AgentToolWrapper
		var lease *ExecLease
		if resolve != nil {
			target, l, err := resolve(ctx, agentName)
			if err != nil {
				return task.SpawnResult{}, err
			}
			w, lease = target, l
		}
		if w == nil || tm == nil {
			if lease != nil {
				lease.Release()
			}
			return task.SpawnResult{}, fmt.Errorf(
				"subagent %q is not a target of the EFFECTIVE orchestration generation — relaunch refused (a retired binding is not revived, and the task chain context is kept untouched)", agentName)
		}
		defer lease.Release()
		desc := agentName + ": " + body
		if len(desc) > 72 {
			desc = desc[:72]
		}
		opts := agent.RunOptions{}
		if overrides != nil {
			raw, err := json.Marshal(overrides)
			if err != nil {
				return task.SpawnResult{}, err
			}
			opts.RuntimeState = map[string]any{PerCallOverridesKey: json.RawMessage(raw)}
		}
		inv := agent.NewInvocation(agent.WithInvocationMessage(model.NewUserMessage(body)), agent.WithInvocationRunOptions(opts))
		rounds := &subagentRounds{cap: w.effectiveResumeRounds()}
		roundOwner, _ := invocationIDFromContext(ctx)
		spec := task.TaskSpec{
			Kind:       "subagent",
			Desc:       desc,
			Key:        agentName + ":" + body,
			RoundOwner: roundOwner,
			Declarative: &task.Declarative{
				Kind: "subagent", Desc: desc, Key: agentName + ":" + body,
				AgentName: agentName, MessageBody: body,
				Overrides: overrides,
			},
			Relaunch: func(ctx context.Context) (task.SpawnResult, error) {
				return redispatch(ctx, agentName, body, overrides)
			},
			ResumeFn: subagentResumeClosureWithOverrides(w.parentCM, agentName, rounds, overrides),
		}
		spec.Start = w.prepareTaskStart(lease.WithContext(ctx), w.parentCM, rounds, inv)
		var spawner task.TaskSpawner = tm
		if current, ok := task.TaskSpawnerFromContext(ctx); ok {
			spawner = current
		}
		return spawner.Spawn(spec, nil), nil
	}
	return redispatch
}

// RecordResidentSession（R3 2.5）：常驻会话生命周期事件的事实链写入入口
// （ActionTool residentSink 经 build 路径接线到这里；记录-only 不发 bus 不进投影）。
func (ta *TagentAgent) RecordResidentSession(sessionID, kind, name, detail string) {
	if ta == nil || ta.contextManager == nil || sessionID == "" {
		return
	}
	cm := ta.contextManager
	md := map[string]string{
		tagentevent.MetaKeyAgentName: cm.name,
		"session_id":                 sessionID,
		"lifecycle":                  kind,
	}
	if cm.sessionID != "" {
		md[tagentevent.MetaKeyRolloutID] = cm.sessionID
	}
	cm.persistTaskRecord(memory.FullEvent{
		EventType:    tagentevent.TypeResidentSession,
		EventSummary: fmt.Sprintf("常驻会话 %s: %s", kind, name),
		Content:      detail,
		Timestamp:    time.Now().UnixMilli(),
		Metadata:     md,
	})
}

// CognitiveAssetChange 是漂移审计事件载荷的最小契约（与 evolution.AssetChange
// 字段对齐；agent 包不反向依赖根包，以本类型解耦）。
type CognitiveAssetChange struct {
	File      string
	OldHash   string
	NewHash   string
	Size      int64
	Timestamp int64
}

// RecordCognitiveAssetChange 是漂移审计批次事件的事实链写入入口——进投影（被看见
// 是审计的最低目标），不发 bus、不打断消息路由。
func (ta *TagentAgent) RecordCognitiveAssetChange(changes []CognitiveAssetChange) {
	if ta == nil || ta.contextManager == nil || len(changes) == 0 {
		return
	}
	cm := ta.contextManager
	var lines []string
	files := make([]string, 0, len(changes))
	for _, c := range changes {
		old8, new8 := shortHash(c.OldHash), shortHash(c.NewHash)
		if c.OldHash == "" {
			old8 = "-"
		}
		if c.NewHash == "" {
			new8 = "(deleted)"
		}
		ts := c.Timestamp
		if ts == 0 {
			ts = time.Now().UnixMilli()
		}
		lines = append(lines, fmt.Sprintf("%s %s->%s size=%d mtime=%s",
			c.File, old8, new8, c.Size, time.UnixMilli(ts).Format(time.RFC3339)))
		files = append(files, c.File)
	}
	md := map[string]string{
		tagentevent.MetaKeyAgentName: cm.name,
		"files":                      strings.Join(files, ","),
	}
	if cm.sessionID != "" {
		md[tagentevent.MetaKeyRolloutID] = cm.sessionID
	}
	content := strings.Join(lines, "\n")
	cm.persistTaskRecord(memory.FullEvent{
		EventType:    tagentevent.TypeCognitiveAssetChanged,
		EventSummary: fmt.Sprintf("认知资产漂移: %d 处变更 [%s]", len(changes), strings.Join(files, ", ")),
		Content:      content,
		Timestamp:    time.Now().UnixMilli(),
		Metadata:     md,
	})
}

// shortHash 取指纹前 8 位（证据行展示用；空串原样返回）。
func shortHash(h string) string {
	if len(h) > 8 {
		return h[:8]
	}
	return h
}

// ExecutorConfig returns THIS agent's assembled execution face (model/tools/
// prompt/genConfig — the product of the build that already passed validation).
// The hot-reload path builds a candidate
// shell, reads its face here, constructs the candidate executor on the
// RESIDENT ContextManager and only then publishes it. The face is returned by
// value; the caller owns the copy (mutating Tools must not disturb this agent).
//
// It replaces RebuildExecutorOn (hotswap-fix 5.7), which fused construction and
// swap and therefore could not be abandoned after construction. The incident it
// fixed still holds: the candidate is constructed ON the resident cm, so its
// BeforeModel closures read the resident projection/bus — never the shell's own
// empty one.
func (ta *TagentAgent) ExecutorConfig() ContextManagerConfig {
	if ta == nil || ta.contextManager == nil {
		return ContextManagerConfig{}
	}
	return ta.contextManager.ExecutorConfig()
}

// taskTTLs adapts the owner's hot view to the TaskManager's  TTL pull
// contract: the reaper and the board resolve both manager-level
// lifetimes from the committed record at their NEXT sweep/read, so a rotation
// needs no write into the manager. A cold read yields zeros, which the manager
// guards to its construction values — the same no-op guard the retired
// SetTerminalTTL/SetDefaultTTL pushes carried.
func (ta *TagentAgent) taskTTLs() (terminal, defaultTTL time.Duration) {
	hp, ok := ta.HotSnapshot()
	if !ok {
		return 0, 0
	}
	return hp.TaskTerminalTTL, hp.TaskDefaultTTL
}

// staticHotSource wraps a construction bundle as the owner's hot-param source,
// so an agent is NEVER source-less ( end state, design 「NewTagentAgent
// 恒装源（记录绑定或静态），不存在无源状态」). The composition root replaces it
// with the record-backed source at the first commit; until then (and forever for
// a standalone agent that never joins an org) the construction bundle answers
// every read.
func staticHotSource(p OrgHotParams) func() (OrgHotParams, bool) {
	return func() (OrgHotParams, bool) { return p, true }
}

// SetHotSource installs the owner's RECORD-BACKED hot-param source (S-C/2.3):
// a closure reading the single committed application record (injected once by
// the composition root after the coordinator exists; it always reads the
// LATEST record, so every commit — swap/recordHotApply/recordRollback — is the
// single writer by construction). While installed, HotSnapshot resolves
// through it; the construction-seeded snapshot cache remains only as the
// fallback for standalone/bare-constructed agents with no record to read.
func (ta *TagentAgent) SetHotSource(src func() (OrgHotParams, bool)) {
	if ta == nil {
		return
	}
	ta.hotSource.Store(&src)
	if ta.taskManager != nil {
		ta.taskManager.RecheckDeadlines()
	}
}

// HotSnapshot returns the owner's CURRENT effective hot bundle — resolved solely
// through the installed source ( end state: the committed application record
// after the first commit, the construction bundle before it / for a standalone
// agent; NewTagentAgent always installs one, so ok=false only guards a
// hand-built agent). The second authority this replaces was the reloader-pushed
// hotSnapshot cache, whose rotation had to be kept in step with the record by
// hand at every commit point.
func (ta *TagentAgent) HotSnapshot() (OrgHotParams, bool) {
	if ta == nil {
		return OrgHotParams{}, false
	}
	if src := ta.hotSource.Load(); src != nil && *src != nil {
		return (*src)()
	}
	return OrgHotParams{}, false
}

// liveHotNumbers adapts the owner's hot view to the compressor's  pull
// contract: every compression boundary reads the owner's CURRENT effective
// bundle (record source first, construction snapshot fallback). A cold owner
// (no snapshot yet) yields the zero group, which the compressor resolves to its
// own construction values — the no-source boundary stays sane.
func (ta *TagentAgent) liveHotNumbers() compress.HotNumbers {
	hp, ok := ta.HotSnapshot()
	if !ok {
		return compress.HotNumbers{}
	}
	return compress.HotNumbers{
		ThresholdPct: hp.ThresholdPct,
		MaxTokens:    hp.MaxTokens,
		KeepRecent:   hp.KeepRecentTasks,
	}
}

// registerLiveCM adds an in-flight invocation-private CM to this owner's live
// set. Bound to the invocation lifecycle: session.Run defers
// unregisterLiveCM alongside invCM.Close, so the set is bounded by concurrent
// calls, never a history list.
func (ta *TagentAgent) registerLiveCM(cm *ContextManager) {
	if ta == nil || cm == nil {
		return
	}
	ta.liveCMsMu.Lock()
	if ta.liveCMs == nil {
		ta.liveCMs = make(map[*ContextManager]struct{})
	}
	ta.liveCMs[cm] = struct{}{}
	ta.liveCMsMu.Unlock()
}

// unregisterLiveCM removes a finished invocation-private CM from the live set.
func (ta *TagentAgent) unregisterLiveCM(cm *ContextManager) {
	if ta == nil || cm == nil {
		return
	}
	ta.liveCMsMu.Lock()
	delete(ta.liveCMs, cm)
	ta.liveCMsMu.Unlock()
	ta.tryDeferredFinalExit()
	if ta.contextManager != nil {
		ta.contextManager.pokeRetirementDrain()
	}
}

// LiveCMCount reports the number of registered in-flight invocation-private
// CMs.
func (ta *TagentAgent) LiveCMCount() int {
	if ta == nil {
		return 0
	}
	ta.liveCMsMu.Lock()
	defer ta.liveCMsMu.Unlock()
	return len(ta.liveCMs)
}

// snapshotLiveCMs returns this owner's live private execution faces for shutdown and usage accounting.
func (ta *TagentAgent) snapshotLiveCMs() []*ContextManager {
	if ta == nil {
		return nil
	}
	ta.liveCMsMu.Lock()
	defer ta.liveCMsMu.Unlock()
	out := make([]*ContextManager, 0, len(ta.liveCMs))
	for cm := range ta.liveCMs {
		out = append(out, cm)
	}
	return out
}

// SetRollbackFn wires the rollback hook (R4 3.8；tagent 包懒检查闭包注入——按
// ring 2 上一代配置重建并 Swap 回；宿主/运维可调 Rollback())。可安全地从发布
// goroutine 重复调用：字段是 atomic.Pointer，Rollback 读到的是完整闭包指针。
// fn == nil 意为“摘钩”：必须存 **空指针**而不是“指向 nil func 的指针”——否则
// Rollback 的 nil 判定会骗过它并解引用空 func（这一条由 e2e 测的
// “SetRollbackFn(nil) 后 Rollback() 应 no-op” 钉住）。
func (ta *TagentAgent) SetRollbackFn(fn func()) {
	if ta == nil {
		return
	}
	if fn == nil {
		ta.orgRollback.Store(nil)
		return
	}
	ta.orgRollback.Store(&fn)
}

// Rollback triggers the wired rollback hook (R4 3.8；no-op if unset)。
func (ta *TagentAgent) Rollback() {
	if ta == nil {
		return
	}
	fn := ta.orgRollback.Load()
	if fn == nil {
		return
	}
	(*fn)()
}

// SetOrgDiagnostics registers the orchestration-generation diagnostic provider
// 。由装配层（tagent 包）在**启动期一次性**注入：它拥有 payload 形状，
// 本包不知道指纹/序号的含义。与 SetRollbackFn 不同规——后者每次发布重写（故用
// atomic），而本字段运行期只读，所以必须是启动期注入；在 reloader 里调它就错了。
func (ta *TagentAgent) SetOrgDiagnostics(fn func() map[string]any) {
	if ta == nil {
		return
	}
	ta.orgDiags = fn
}

// OrgDiagnostics returns the current orchestration-generation diagnostic payload
// (nil when unset). Read-only by design: no execution path may branch on it.
func (ta *TagentAgent) OrgDiagnostics() map[string]any {
	if ta == nil || ta.orgDiags == nil {
		return nil
	}
	return ta.orgDiags()
}

// taskRecordSink：TaskManager 的
// OnSpawn/OnInlineSettle 钩子经此 late-bind 到 cm 的记录-only 持久化
// （cm 在 taskManager 之后构造——与 onEventRef 同款延迟绑定模式；
// 绑定前钩子 nil-safe 静默跳过，best-effort 语义不变）。
type taskRecordSink struct {
	cm       *ContextManager
	owner    *TagentAgent
	delivery taskDeliveryJournal
}

func (s *taskRecordSink) onSpawn(tk *task.Task) {
	if s == nil || s.cm == nil {
		return
	}
	s.cm.EmitTaskSpawnedRecord(tk)
}

func (s *taskRecordSink) onInlineSettle(tk *task.Task, sig task.SettleSignal) {
	if s == nil || s.cm == nil {
		return
	}
	s.cm.EmitTaskInlineSettleRecord(tk, sig)
}

func (s *taskRecordSink) onCancel(tk *task.Task) {
	if s == nil || s.cm == nil {
		return
	}
	s.cm.EmitTaskCancelledRecord(tk)
}

// RebuildTaskRegistry restores task intentions, producer bindings and unaccepted signals in event-key order.
func RebuildTaskRegistry(store memory.MemoryStore, partitionID int, tm *task.TaskManager,
	rebuildClosures func(decl task.Declarative) task.TaskSpec) int {
	n, _ := rebuildTaskRegistry(store, partitionID, tm, rebuildClosures)
	return n
}

type taskRegistryFold struct {
	id         string
	decl       task.Declarative
	status     task.TaskStatus
	statusKey  int64
	settledAt  time.Time
	detachedAt int64
	result     string
	err        error
	bound      bool
	cancelled  bool
	raw        []taskRegistrySignal
}

type taskRegistrySignal struct {
	fact     memory.FullEvent
	record   taskSignalRecord
	accepted bool
}

func rebuildTaskRegistry(store memory.MemoryStore, partitionID int, tm *task.TaskManager,
	rebuildClosures func(decl task.Declarative) task.TaskSpec) (int, []memory.FullEvent) {
	if store == nil || tm == nil {
		return 0, nil
	}
	records, err := queryTaskRecords(store, partitionID, []string{
		tagentevent.TypeTaskSpawned, tagentevent.TypeExternalInput,
		tagentevent.TypeTaskSignal, tagentevent.TypeTaskSignalReceipt,
	})
	if err != nil {
		log.Errorf("[rebuild-task-registry] inventory failed: %v", err)
		return 0, nil
	}
	tasks := make(map[string]*taskRegistryFold)
	receipts := make(map[int64]memory.FullEvent)
	for _, ev := range records {
		if ev.EventType == tagentevent.TypeTaskSignalReceipt {
			receipts[ev.EventKey] = ev
			continue
		}
		id := ev.Metadata["task_id"]
		if id == "" {
			continue
		}
		f := tasks[id]
		if f == nil {
			f = &taskRegistryFold{id: id, status: task.TaskSuspect}
			tasks[id] = f
		}
		switch ev.EventType {
		case tagentevent.TypeTaskSpawned:
			var decl task.Declarative
			if err := json.Unmarshal([]byte(ev.Content), &decl); err != nil {
				log.Warnf("[rebuild-task-registry] invalid declaration %d: %v", ev.EventKey, err)
				continue
			}
			foldTaskDeclaration(f, decl, ev)
		case tagentevent.TypeTaskSignal:
			var raw taskSignalRecord
			if err := json.Unmarshal([]byte(ev.Content), &raw); err != nil || raw.TaskID != id || raw.RoundID == 0 || raw.Seq == 0 || raw.Event == nil || raw.ReceiptKey <= 0 || raw.ObservedAt.IsZero() {
				log.Warnf("[rebuild-task-registry] invalid signal %d", ev.EventKey)
				continue
			}
			f.raw = append(f.raw, taskRegistrySignal{fact: ev, record: raw})
			foldTaskSignal(f, raw, ev.EventKey)
		case tagentevent.TypeExternalInput:
			foldLegacyTaskSettle(f, ev)
		}
	}
	var restored int
	var pendingFacts []memory.FullEvent
	for _, f := range tasks {
		if f.decl.Kind == "" {
			continue
		}
		pending := make([]taskRegistrySignal, 0, len(f.raw))
		var latestAcceptedTerminal *taskRegistrySignal
		for i := range f.raw {
			raw := &f.raw[i]
			raw.accepted = taskSignalReceiptMatches(raw.fact, raw.record, receipts[raw.record.ReceiptKey])
			if raw.accepted && raw.record.signal().EndsRound() && raw.record.Observation != "round_aborted" && raw.record.Lineage != task.LineageRetired && raw.record.RoundID == f.decl.ProducerRound {
				latestAcceptedTerminal = raw
			}
			if !raw.accepted && raw.record.ErrorKind != "continuation_interrupted" {
				pending = append(pending, *raw)
			}
		}
		if f.cancelled {
			continue
		}
		terminal := !f.status.Live()
		keepTerminal := len(pending) > 0
		if terminal && f.decl.Kind == "subagent" && latestAcceptedTerminal != nil && time.Since(latestAcceptedTerminal.record.ObservedAt) <= tm.TerminalTTL() {
			keepTerminal = true
		}
		if terminal && !keepTerminal {
			continue
		}
		spec := restoredTaskSpec(f.decl, rebuildClosures)
		started := time.UnixMilli(f.decl.StartedAtMilli)
		if f.decl.StartedAtMilli <= 0 {
			started = time.Time{}
		}
		tk := tm.RestoreTask(f.id, spec, started, f.status)
		if tk == nil {
			continue
		}
		tk.SetDetachedAtMilli(f.detachedAt)
		if terminal {
			tk.RestoreSettlement(f.result, f.err, f.settledAt)
		}
		for _, raw := range pending {
			sig := raw.record.signal()
			if raw.record.Event != nil {
				sig.SourceID = raw.record.Event.ID
			}
			if tm.RestorePendingDeliveryWithMode(f.id, sig, raw.record.Inline) {
				pendingFacts = append(pendingFacts, raw.fact)
			}
		}
		restored++
	}
	return restored, pendingFacts
}

func foldTaskDeclaration(f *taskRegistryFold, decl task.Declarative, ev memory.FullEvent) {
	if f.cancelled {
		return
	}
	if decl.RoundID == 0 {
		decl.RoundID = 1
	}
	phase := ev.Metadata["admission_phase"]
	if phase == "cancelled" {
		if decl.RoundID >= f.decl.RoundID {
			f.decl, f.status, f.statusKey, f.cancelled = decl, task.TaskCancelled, ev.EventKey, true
		}
		return
	}
	intent := strings.Contains(phase, "intent") || phase == "spawn" || phase == "accepted" || phase == "round_accepted" || phase == "round_aborted"
	if decl.StartedAtMilli <= 0 {
		decl.StartedAtMilli = ev.Timestamp
	}
	if decl.RoundID < f.decl.RoundID {
		return
	}
	if intent && f.bound {
		decl.ProducerRound, decl.ProducerOwner, decl.SignalSeq = f.decl.ProducerRound, f.decl.ProducerOwner, f.decl.SignalSeq
		decl.TTLAnchorMilli = f.decl.TTLAnchorMilli
		for _, key := range []string{childInvocationIDParam, localContinuationParam} {
			if value := f.decl.Params[key]; value != "" {
				if decl.Params == nil {
					decl.Params = make(map[string]string)
				}
				decl.Params[key] = value
			}
		}
		f.decl = decl
		return
	}
	if !intent && decl.ProducerRound == 0 {
		decl.ProducerRound = decl.RoundID
	}
	if decl.ProducerOwner == "" {
		decl.ProducerOwner = decl.RoundOwner
	}
	newProducer := !f.bound || decl.ProducerRound > f.decl.ProducerRound
	if decl.ProducerRound < f.decl.ProducerRound {
		return
	}
	f.decl = decl
	if !intent {
		f.bound = true
	}
	if newProducer {
		f.status, f.statusKey, f.cancelled = task.TaskSuspect, ev.EventKey, false
		f.result = ""
		f.err = nil
		f.settledAt = time.Time{}
		f.detachedAt = 0
	}
}

func foldTaskSignal(f *taskRegistryFold, raw taskSignalRecord, key int64) {
	producer := raw.ProducerRound
	if producer == 0 && raw.Observation != "round_aborted" {
		producer = raw.RoundID
	}
	if raw.RoundID > f.decl.RoundID {
		f.decl.RoundID = raw.RoundID
		f.decl.RoundOwner = raw.RoundOwner
	}
	if raw.Observation == "round_aborted" {
		if producer >= f.decl.ProducerRound {
			f.decl.ProducerRound = producer
			if raw.ProducerOwner != "" {
				f.decl.ProducerOwner = raw.ProducerOwner
			}
			if raw.ProducerLastSeq > f.decl.SignalSeq {
				f.decl.SignalSeq = raw.ProducerLastSeq
			}
			if raw.ProducerStatus != "" {
				f.status = raw.ProducerStatus
			}
		}
		return
	}
	if raw.RoundID < f.decl.ProducerRound || f.cancelled {
		return
	}
	if raw.RoundID == f.decl.ProducerRound {
		if owner := f.decl.ProducerOwner; owner != "" && raw.RoundOwner != "" && owner != raw.RoundOwner {
			return
		}
		if invocation := f.decl.Params[childInvocationIDParam]; invocation != "" && raw.ChildInvocationID != "" && invocation != raw.ChildInvocationID {
			return
		}
	}
	if raw.RoundID > f.decl.ProducerRound {
		f.decl.ProducerRound = raw.RoundID
		f.decl.ProducerOwner = raw.RoundOwner
		f.decl.SignalSeq = 0
	}
	if raw.Seq < f.decl.SignalSeq {
		return
	}
	f.decl.SignalSeq = raw.Seq
	if raw.LifetimeAnchorMilli > 0 {
		f.decl.TTLAnchorMilli = raw.LifetimeAnchorMilli
	}
	if raw.ChildInvocationID != "" {
		if f.decl.Params == nil {
			f.decl.Params = make(map[string]string)
		}
		f.decl.Params[childInvocationIDParam] = raw.ChildInvocationID
		f.decl.Params[localContinuationParam] = strconv.FormatBool(raw.ChildContinuation)
	}
	if raw.ErrorKind == "continuation_interrupted" {
		f.status = task.TaskSuspect
		f.statusKey = key
		return
	}
	sig := raw.signal()
	f.statusKey = key
	f.result, f.err, f.settledAt = raw.Output, sig.Err, raw.ObservedAt
	switch raw.Kind {
	case task.SettleCompleted:
		f.status = task.TaskCompleted
		if sig.Err != nil {
			f.status = task.TaskFailed
		}
	case task.SettleFailed:
		f.status = task.TaskFailed
	case task.SettleStable, task.SettleReady:
		f.status = task.TaskAliveDetached
	case task.SettleSuspect:
		f.status = task.TaskSuspect
	}
}

func foldLegacyTaskSettle(f *taskRegistryFold, ev memory.FullEvent) {
	if source := ev.Metadata[tagentevent.MetaKeySourceEventID]; source != "" {
		for _, raw := range f.raw {
			if raw.record.Event != nil && raw.record.Event.ID == source {
				return
			}
		}
	}
	status := ev.Metadata["settle_status"]
	if status == "" || status == "watch" || f.cancelled {
		return
	}
	round, _ := strconv.ParseUint(ev.Metadata[tagentevent.MetaKeyTaskRoundID], 10, 64)
	if round == 0 {
		round = 1
		if status == "cancelled" && f.decl.ProducerRound > 0 {
			round = f.decl.ProducerRound
		}
	}
	if round < f.decl.ProducerRound || ev.EventKey < f.statusKey {
		return
	}
	if ms, err := strconv.ParseInt(ev.Metadata["detached_at_ms"], 10, 64); err == nil && ms > 0 {
		f.detachedAt = ms
	}
	f.statusKey = ev.EventKey
	f.settledAt = time.UnixMilli(ev.Timestamp)
	switch status {
	case "completed":
		f.status = task.TaskCompleted
	case "failed":
		f.status = task.TaskFailed
	case "cancelled":
		f.status = task.TaskCancelled
		f.cancelled = true
	case "dead":
		f.status = task.TaskDead
	case "alive-detached", "alive_detached", "ready":
		f.status = task.TaskAliveDetached
	case "suspect":
		f.status = task.TaskSuspect
	}
}

func taskSignalReceiptMatches(fact memory.FullEvent, raw taskSignalRecord, receipt memory.FullEvent) bool {
	if receipt.EventType != tagentevent.TypeTaskSignalReceipt || receipt.EventKey == 0 || receipt.EventKey != raw.ReceiptKey || receipt.Content != tagentevent.FormatEventKey(fact.EventKey) {
		return false
	}
	if receipt.PartitionID != fact.PartitionID || receipt.Timestamp != fact.Timestamp || receipt.Timestamp != raw.ObservedAt.UnixMilli() {
		return false
	}
	if memory.PartitionIDFromEventKey(fact.EventKey) != fact.PartitionID || memory.PartitionIDFromEventKey(receipt.EventKey) != fact.PartitionID {
		return false
	}
	if receipt.Metadata["task_id"] != raw.TaskID || receipt.Metadata[tagentevent.MetaKeyTaskRoundID] != strconv.FormatUint(raw.RoundID, 10) || receipt.Metadata[tagentevent.MetaKeyTaskSignalSeq] != strconv.FormatUint(raw.Seq, 10) {
		return false
	}
	if raw.Event != nil && receipt.Metadata[tagentevent.MetaKeySourceEventID] != raw.Event.ID {
		return false
	}
	return receipt.Metadata[childInvocationIDParam] == raw.ChildInvocationID && receipt.Metadata[tagentevent.MetaKeyTaskRoundOwner] == fact.Metadata[tagentevent.MetaKeyTaskRoundOwner]
}

func restoredTaskSpec(decl task.Declarative, rebuild func(task.Declarative) task.TaskSpec) task.TaskSpec {
	spec := task.TaskSpec{Kind: decl.Kind, Desc: decl.Desc, Key: decl.Key, Origin: decl.Origin, Lifetime: decl.Lifetime, Mode: decl.Mode, WaitFor: decl.WaitFor, RoundOwner: decl.RoundOwner, Declarative: &decl}
	if rebuild != nil {
		built := rebuild(decl)
		if built.Kind != "" || built.Desc != "" {
			if built.Kind == "" {
				built.Kind = spec.Kind
			}
			if built.Desc == "" {
				built.Desc = spec.Desc
			}
			if built.Key == "" {
				built.Key = spec.Key
			}
			built.Origin, built.Lifetime, built.Mode, built.WaitFor, built.RoundOwner, built.Declarative = decl.Origin, decl.Lifetime, decl.Mode, decl.WaitFor, decl.RoundOwner, &decl
			spec = built
		}
	}
	if spec.TTL <= 0 {
		if seconds, err := strconv.ParseInt(decl.Params[subagentTTLParam], 10, 64); err == nil && seconds > 0 {
			spec.TTL = time.Duration(seconds) * time.Second
		}
	}
	return spec
}
