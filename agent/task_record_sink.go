package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/SpellingDragon/tagent/agent/compress"
	"github.com/SpellingDragon/tagent/agent/task"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"trpc.group/trpc-go/trpc-agent-go/log"
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
	return RebuildTaskRegistry(store, ta.partitionID(), ta.taskManager, rebuildClosures)
}

// SubagentRedispatcher：跨重启
// subagent Relaunch 的重投递器——镜像 subagentRelaunch 的 detector 形状
// （RedispatchAsync 同步跑在 detector 的 watch goroutine 内，Spawn 的 sync-wait
// 窗口语义保持；spawnKey=agentName+":"+body 与无 extraName 的原 spawn 键一致→幂等去重覆盖）。
//
// resolve 是**每次重投时**对目标所属调用绑定的一次解析，而不是启动时冻结的
// wrapper 快照：否则一个被后续代移除的目标仍会在这里被旧代 wrapper 静默复活（跑
// 的是已退役的声明与目标），而当时的拒绝文案又声称“current org”——两者均与
// task-registry-rebuild 的「不复活已退役执行器或静默改投」相逆。调用方应传入
// 「按有效执行面解析」的闭包（见 ContextManager.SubagentWrapper），这就让显式
// 重投与普通委派共用同一个版本真源。
// resolve is the SAME version source ordinary delegation and 's re-entry use
// (see ResolveReentryDelegation): the initiating call's binding when the re-entry
// rides one, the effective face otherwise. Freezing a wrapper snapshot here — or
// resolving against the effective face while an initiator holds an older binding —
// would let a target a later generation removed be silently revived by a stored
// task, which is precisely what task-registry-rebuild「不复活已退役执行器或静默改投」
// and forbid.
func SubagentRedispatcher(resolve func(ctx context.Context, agentName string) (*AgentToolWrapper, *ExecLease, error), tm *task.TaskManager) func(ctx context.Context, agentName, body string) (task.SpawnResult, error) {
	var redispatch func(ctx context.Context, agentName, body string) (task.SpawnResult, error)
	redispatch = func(ctx context.Context, agentName, body string) (task.SpawnResult, error) {
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
		desc := agentName + ": " + body
		if len(desc) > 72 {
			desc = desc[:72]
		}
		detector := task.NewFuncSettleDetector(context.Background(), func(runCtx context.Context) (string, error) {
			defer lease.Release()
			out, err := w.RedispatchAsync(lease.WithContext(runCtx), body)
			if err != nil {
				return "", err
			}
			s, _ := out.(string)
			return s, nil
		}, w.DenseDuration())
		res := tm.Spawn(task.TaskSpec{
			Kind: "subagent",
			Desc: desc,
			Key:  agentName + ":" + body,
			Declarative: &task.Declarative{
				Kind: "subagent", Desc: desc, Key: agentName + ":" + body,
				AgentName: agentName, MessageBody: body,
			},
			Relaunch: func(ctx context.Context) (task.SpawnResult, error) { return redispatch(ctx, agentName, body) },
		}, detector)
		if (res.Blocked != "" || res.Deduped) && hasInitiator(ctx) {
			waitForUnadoptedStop(ctx, detector)
		}
		return res, nil
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

// snapshotLiveCMs returns a snapshot of the registered in-flight invocation-private
// CMs (test-only introspection for  acceptance: seeded values at
// construction, hot-applied values mid-call). No execution path reads it.
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
	cm *ContextManager
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

// RebuildTaskRegistry：冷启动从事实链
// 纯全量回放重建 active 任务集（registry=fold：task_spawned 记录 − 终态 settle）。
// 无 compaction snapshot（任务无折叠语义）。状态映射：running→suspect（进程内
// watch goroutine 不可恢复，交 R3 存活探测裁决）；alive-detached/suspect 原态；
// 终态（completed/failed/cancelled/dead，含 inline settle 记录）不重建。
// rebuildClosures 由 tool/action 提供（承诺表：command 全/subagent Relaunch/generic ❌）。
// best-effort：单条记录损坏跳过 + WARN，不阻断重建。
func RebuildTaskRegistry(store memory.MemoryStore, partitionID int, tm *task.TaskManager,
	rebuildClosures func(decl task.Declarative) task.TaskSpec) (restored int) {
	if store == nil || tm == nil {
		return 0
	}

	// 1) spawned 记录（task_spawned 类型，Content=Declarative JSON）——分页取全
	//。
	type spawnRec struct {
		id   string
		decl task.Declarative
	}
	var spawns []spawnRec
	for off := 0; ; off += 500 {
		batch, err := store.QueryEvents(memory.QueryOptions{
			PartitionIDs: []int{partitionID},
			EventTypes:   []string{tagentevent.TypeTaskSpawned},
			Limit:        500,
			Offset:       off,
		})
		if err != nil {
			log.Errorf("[rebuild-task-registry] query spawned failed: %v", err)
			break
		}
		keys := make([]int64, 0, len(batch))
		for _, r := range batch {
			keys = append(keys, r.EventKey)
		}
		evs, gerr := store.GetEvents(keys)
		if gerr != nil {
			continue
		}
		for _, ev := range evs {
			var decl task.Declarative
			if err := json.Unmarshal([]byte(ev.Content), &decl); err != nil {
				log.Warnf("[rebuild-task-registry] spawned record %d corrupt, skipping: %v", ev.EventKey, err)
				continue
			}
			spawns = append(spawns, spawnRec{id: ev.Metadata["task_id"], decl: decl})
		}
		if len(batch) < 500 {
			break
		}
	}
	if len(spawns) == 0 {
		return 0
	}

	settled := map[string]string{}
	detachedMs := map[string]int64{}
	for off := 0; ; off += 500 {
		batch, err := store.QueryEvents(memory.QueryOptions{
			PartitionIDs: []int{partitionID},
			EventTypes:   []string{tagentevent.TypeExternalInput},
			Limit:        500,
			Offset:       off,
		})
		if err != nil {
			break
		}
		keys := make([]int64, 0, len(batch))
		for _, r := range batch {
			keys = append(keys, r.EventKey)
		}
		evs, err := store.GetEvents(keys)
		if err != nil {
			continue
		}
		for _, ev := range evs {
			if st, ok := ev.Metadata["settle_status"]; ok && st != "" {
				if id := ev.Metadata["task_id"]; id != "" {
					if st == "watch" {
						if _, exists := settled[id]; !exists {
							settled[id] = st
						}
					} else {
						settled[id] = st
					}
					if msStr := ev.Metadata["detached_at_ms"]; msStr != "" {
						if ms, perr := strconv.ParseInt(msStr, 10, 64); perr == nil {
							detachedMs[id] = ms
						}
					}
				}
			}
		}
		if len(batch) < 500 {
			break
		}
	}

	for _, sp := range spawns {
		if sp.id == "" {
			continue
		}
		switch settled[sp.id] {
		case "completed", "failed", "cancelled", "dead":
			continue
		}
		status := task.TaskSuspect
		if settled[sp.id] == "alive-detached" || settled[sp.id] == "alive_detached" {
			status = task.TaskAliveDetached
		}
		spec := task.TaskSpec{Kind: sp.decl.Kind, Desc: sp.decl.Desc, Key: sp.decl.Key, Origin: sp.decl.Origin, Lifetime: sp.decl.Lifetime}
		if rebuildClosures != nil {
			if rebuilt := rebuildClosures(sp.decl); rebuilt.Desc != "" || rebuilt.Kind != "" {
				if spec.Origin != nil && rebuilt.Origin == nil {
					rebuilt.Origin = spec.Origin
				}
				if spec.Key != "" && rebuilt.Key == "" {
					rebuilt.Key = spec.Key
				}
				if spec.Lifetime != "" && rebuilt.Lifetime == "" {
					rebuilt.Lifetime = spec.Lifetime
				}
				spec = rebuilt
			}
		}
		if spec.Desc == "" {
			spec.Desc = sp.decl.Desc
		}
		started := time.UnixMilli(sp.decl.StartedAtMilli)
		tk := tm.RestoreTask(sp.id, spec, started, status)
		if tk != nil && status == task.TaskAliveDetached {
			tk.SetDetachedAtMilli(detachedMs[sp.id])
		}
		restored++
	}
	if restored > 0 {
		log.Infof("[rebuild-task-registry] restored %d active task(s) from fact chain", restored)
	}
	return restored
}
