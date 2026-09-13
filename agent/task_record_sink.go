package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/SpellingDragon/tagent/agent/task"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/runner"
)

// TaskManager exposes the org-level resident task registry (R2: rebuilt
// from the fact chain at cold start, shared across executor generations).
func (ta *TagentAgent) TaskManager() *task.TaskManager {
	if ta == nil {
		return nil
	}
	return ta.taskManager
}

// PartitionID exposes the context manager's snowflake partition (registry
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

// SubagentRedispatcher（R2，resident-continuity-r2-r4 D1.2）：跨重启 subagent
// Relaunch 的重投递器——镜像 subagentRelaunch 的 detector 形状（RedispatchAsync
// 同步跑在 detector 的 watch goroutine 内，Spawn 的 sync-wait 窗口语义保持；
// spawnKey=agentName+":"+body 与无 extraName 的原 spawn 键一致→幂等去重覆盖）。
func SubagentRedispatcher(wrappers map[string]*AgentToolWrapper, tm *task.TaskManager) func(agentName, body string) (task.SpawnResult, error) {
	var redispatch func(agentName, body string) (task.SpawnResult, error)
	redispatch = func(agentName, body string) (task.SpawnResult, error) {
		w, ok := wrappers[agentName]
		if !ok || tm == nil {
			return task.SpawnResult{}, fmt.Errorf("subagent %q not available in the current org — cannot relaunch cross-restart", agentName)
		}
		desc := agentName + ": " + body
		if len(desc) > 72 {
			desc = desc[:72]
		}
		detector := task.NewFuncSettleDetector(context.Background(), func(runCtx context.Context) (string, error) {
			out, err := w.RedispatchAsync(runCtx, body)
			if err != nil {
				return "", err
			}
			s, _ := out.(string)
			return s, nil
		}, w.DenseDuration())
		return tm.Spawn(task.TaskSpec{
			Kind: "subagent",
			Desc: desc,
			Key:  agentName + ":" + body,
			Declarative: &task.Declarative{
				Kind: "subagent", Desc: desc, Key: agentName + ":" + body,
				AgentName: agentName, MessageBody: body,
			},
			Relaunch: func() (task.SpawnResult, error) { return redispatch(agentName, body) },
		}, detector), nil
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

// SwapExecutor atomically swaps the executor runner (R4 3.3/3.5；drain-free
// turn 级——进行中 turn 用旧 runner 跑完)。cm/bus/loop/projection/TaskManager
// 等常驻不换（宿主入口零变化）。Runner() 取当前代已在 helpers.go。
// RebuildExecutorOn（hotswap-fix 5.7）：把**本 TA 冷启动时的执行面配置**
// （model/tools/prompt/genConfig——构建验证已通过的产物）应用到**目标 cm**
// 上重建 executor。org 热更换装专用：target=常驻 entry cm，其 projection/
// bus/sessionSvc/回调闭包全部保留——修复旧路径整壳换入导致请求装配被接到
// 新壳空投影的事故（n=1 system-only → provider 400/1214）。newTA 在此仅作
// 执行面配置载体；其自身的 cm/runner 构建产物即弃。
func (ta *TagentAgent) RebuildExecutorOn(target *ContextManager) runner.Runner {
	if ta == nil || target == nil || ta.contextManager == nil {
		return nil
	}
	return target.RebuildExecutor(ta.contextManager.execCfg)
}

func (ta *TagentAgent) SwapExecutor(r runner.Runner) {
	if ta == nil || ta.contextManager == nil {
		return
	}
	ta.contextManager.SwapExecutor(r)
}

// orgRollbackFn（R4 3.8）：回滚钩子（tagent 包懒检查闭包注入——按 ring 2
// 上一代配置重建并 Swap 回）。
type orgRollbackFn func()

// SetRollbackFn wires the rollback hook (R4 3.8；宿主/运维可调 Rollback())。
func (ta *TagentAgent) SetRollbackFn(fn func()) {
	if ta == nil {
		return
	}
	ta.orgRollback = fn
}

// Rollback triggers the wired rollback hook (R4 3.8；no-op if unset)。
func (ta *TagentAgent) Rollback() {
	if ta == nil || ta.orgRollback == nil {
		return
	}
	ta.orgRollback()
}

// taskRecordSink（R2，resident-continuity-r2-r4 1.6）：TaskManager 的
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

// RebuildTaskRegistry（R2 1.9，resident-continuity-r2-r4 D1.3）：冷启动从事实链
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
	//（review 🟠3：单页 Limit 500 会静默截断早期 spawned，对应任务重启丢失）。
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

	// 2) settle 记录（external_input + Metadata[settle_status]；含 inline 标记）。
	// 全量扫 external_input 按结构化 Metadata 归并 task_id→末次 settle_status。
	settled := map[string]string{} // task_id → last settle_status
	for off := 0; ; off += 500 {
		batch, err := store.QueryEvents(memory.QueryOptions{
			PartitionIDs: []int{partitionID},
			EventTypes:   []string{tagentevent.TypeExternalInput},
			Limit:        500,
			Offset:       off,
		})
		if err != nil {
			break // best-effort：已扫部分仍可用
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
					settled[id] = st // 全序扫描下后者覆盖前者（末次胜）
				}
			}
		}
		if len(batch) < 500 {
			break
		}
	}

	// 3) 重建 active：spawned − 终态 settle；状态映射（D1.3）。
	for _, sp := range spawns {
		if sp.id == "" {
			continue
		}
		switch settled[sp.id] {
		case "completed", "failed", "cancelled", "dead":
			continue // 终态不重建（board age-out 语义）
		}
		status := task.TaskSuspect // running/未知 → suspect（探测裁决）
		if settled[sp.id] == "alive-detached" || settled[sp.id] == "alive_detached" {
			status = task.TaskAliveDetached
		}
		spec := task.TaskSpec{Kind: sp.decl.Kind, Desc: sp.decl.Desc, Key: sp.decl.Key, Origin: sp.decl.Origin}
		if rebuildClosures != nil {
			if rebuilt := rebuildClosures(sp.decl); rebuilt.Desc != "" || rebuilt.Kind != "" {
				spec = rebuilt
			}
		}
		if spec.Desc == "" {
			spec.Desc = sp.decl.Desc
		}
		started := time.UnixMilli(sp.decl.StartedAtMilli)
		tm.RestoreTask(sp.id, spec, started, status)
		restored++
	}
	if restored > 0 {
		log.Infof("[rebuild-task-registry] restored %d active task(s) from fact chain", restored)
	}
	return restored
}
