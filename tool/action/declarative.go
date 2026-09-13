package action

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/SpellingDragon/tagent/agent/task"
	"trpc.group/trpc-go/trpc-agent-go/log"
)

// declarative.go（R2，resident-continuity-r2-r4 1.2/1.3）：TaskSpec 的声明式投影
// 与跨重启闭包工厂（承诺表，design D1.2）。进程内 spawn 站点直传闭包不变；
// Declarative 是 task_spawned 记录的载荷与 RebuildTaskRegistry 的重建输入。

// declarativeParams keys = ActionArgs 的 spawn 字段全集（session-op 字段
// Op/Keys/Enter/Tail/Ansi/GraceSec/SessionID 非 spawn 参数，排除；Command 单列）。
const (
	pTimeout    = "timeout"
	pWorkDir    = "work_dir"
	pIsTUI      = "is_tui"
	pQuiet      = "quiet_timeout"
	pMode       = "mode"
	pName       = "name"
	pWatch      = "watch"
	pProbe      = "probe"
	pProbeEvery = "probe_interval_sec"
	pProbeFails = "probe_failures"
)

// DeclarativeFromArgs builds the serializable projection of a command spawn.
// sessionID is the tmux session backing the task (TaskID bridge; empty for
// unnamed oneshots — those die with the round and are not restorable).
func DeclarativeFromArgs(args ActionArgs, sessionID string) *task.Declarative {
	p := map[string]string{
		pTimeout: strconv.Itoa(args.Timeout),
	}
	if args.WorkDir != "" {
		p[pWorkDir] = args.WorkDir
	}
	if args.IsTUI {
		p[pIsTUI] = "true"
	}
	if args.QuietTimeout > 0 {
		p[pQuiet] = strconv.Itoa(args.QuietTimeout)
	}
	if args.Mode != "" {
		p[pMode] = string(args.Mode)
	}
	if args.Name != "" {
		p[pName] = args.Name
	}
	if args.Watch != "" {
		p[pWatch] = args.Watch
	}
	if args.Probe != "" {
		p[pProbe] = args.Probe
	}
	if args.ProbeIntervalSec > 0 {
		p[pProbeEvery] = strconv.Itoa(args.ProbeIntervalSec)
	}
	if args.ProbeFailures > 0 {
		p[pProbeFails] = strconv.Itoa(args.ProbeFailures)
	}
	return &task.Declarative{
		Kind:    "command",
		Desc:    args.Command,
		Key:     args.Command,
		Command: args.Command,
		TaskID:  sessionID,
		Params:  p,
	}
}

// argsFromDeclarative reconstructs ActionArgs from the Params projection.
// Unknown keys are rejected (🟡14: a half-rebuilt spec silently changing
// behavior is exactly what the strict decode prevents).
func argsFromDeclarative(decl task.Declarative) (ActionArgs, error) {
	var args ActionArgs
	args.Command = decl.Command
	for k, v := range decl.Params {
		switch k {
		case pTimeout:
			n, err := strconv.Atoi(v)
			if err != nil {
				return args, fmt.Errorf("param %s=%q: %w", k, v, err)
			}
			args.Timeout = n
		case pWorkDir:
			args.WorkDir = v
		case pIsTUI:
			args.IsTUI = v == "true"
		case pQuiet:
			n, err := strconv.Atoi(v)
			if err != nil {
				return args, fmt.Errorf("param %s=%q: %w", k, v, err)
			}
			args.QuietTimeout = n
		case pMode:
			args.Mode = v
		case pName:
			args.Name = v
		case pWatch:
			args.Watch = v
		case pProbe:
			args.Probe = v
		case pProbeEvery:
			n, err := strconv.Atoi(v)
			if err != nil {
				return args, fmt.Errorf("param %s=%q: %w", k, v, err)
			}
			args.ProbeIntervalSec = n
		case pProbeFails:
			n, err := strconv.Atoi(v)
			if err != nil {
				return args, fmt.Errorf("param %s=%q: %w", k, v, err)
			}
			args.ProbeFailures = n
		default:
			return args, fmt.Errorf("unknown declarative param %q (strict decode)", k)
		}
	}
	return args, nil
}

// SpecFromDeclarative rebuilds a command TaskSpec from its declarative
// projection (promise table: Relaunch✅ via fresh startSession; Alive✅ via
// the TaskID session check; Resume rebuilt as a monitored-check stub — the
// live detector binding is re-armed by the R3 reattach; until then the stub
// returns the same relaunch guidance as an unmonitored session).
func (ct *ActionTool) SpecFromDeclarative(spawner task.TaskSpawner, decl task.Declarative) task.TaskSpec {
	args, err := argsFromDeclarative(decl)
	if err != nil {
		// Unrecoverable projection: display-only task (no closures).
		return task.TaskSpec{Kind: decl.Kind, Desc: decl.Desc, Key: decl.Key, Declarative: &decl}
	}
	spec := task.TaskSpec{
		Kind:        "command",
		Desc:        args.Command,
		Key:         args.Command,
		Relaunch:    ct.relaunchClosure(spawner, args),
		ResumeFn:    ct.rebuiltResumeClosure(decl.TaskID, args.IsTUI),
		Alive:       ct.sessionAliveClosure(decl.TaskID),
		Declarative: &decl,
	}
	return spec
}

// rebuiltResumeClosure（R3 2.7④，跨重启 resume 真供能）：镜像 resumeClosure 主体
// （TouchSession 校验→baseline→SendKeys→Rearm），但 detector 为新建——重挂场景
// 下 reattachOne 的原 detector 归属 monitor 回调链，此处新 detector 服务本轮
// resume 的 settle 检测（watch 兕底仍由 reattach detector 承担）。R3 重挂前
// （未跟踪）返回与旧路径同款的 relaunch 引导。
func (ct *ActionTool) rebuiltResumeClosure(sessionID string, isTUI bool) func(string) (task.SettleDetector, error) {
	return func(input string) (task.SettleDetector, error) {
		if isTUI {
			return nil, fmt.Errorf("session %s is a TUI — resume (send-keys) would corrupt the screen; use cancel + a fresh call instead", sessionID)
		}
		if !ct.tmuxMonitor.TouchSession(sessionID) {
			return nil, fmt.Errorf("session %s is no longer monitored — use relaunch_task instead", sessionID)
		}
		detector := NewTmuxSettleDetector(sessionID, func() {
			if err := ct.tmuxExecutor.KillSession(sessionID); err != nil {
				log.Warnf("[rebuiltResume] kill %s: %v", sessionID, err)
			}
			ct.tmuxMonitor.RemoveSession(sessionID)
			ct.removeResidentMeta(sessionID)
		})
		baseline := 0
		if out, err := ct.tmuxExecutor.GetSessionOutput(sessionID); err == nil {
			baseline = strings.Count(out, "\n")
		}
		detector.Rearm(baseline)
		if err := ct.tmuxExecutor.SendKeys(sessionID, input+"\n"); err != nil {
			return nil, fmt.Errorf("send to session %s failed (session may be gone): %w", sessionID, err)
		}
		return detector, nil
	}
}

// SubagentSpecFromDeclarative rebuilds a subagent TaskSpec (promise table:
// Relaunch✅ via redispatch through the resident agents map; Resume❌ — the
// rounds chain has no event source, cross-restart resume returns guidance).
func SubagentSpecFromDeclarative(redispatch func(agentName, body string) (task.SpawnResult, error), decl task.Declarative) task.TaskSpec {
	spec := task.TaskSpec{
		Kind:        "subagent",
		Desc:        decl.Desc,
		Key:         decl.Key,
		Declarative: &decl,
	}
	if redispatch != nil && decl.AgentName != "" {
		body := decl.MessageBody
		spec.Relaunch = func() (task.SpawnResult, error) {
			return redispatch(decl.AgentName, body)
		}
	}
	spec.ResumeFn = func(string) (task.SettleDetector, error) {
		return nil, fmt.Errorf("跨重启的子 agent 会话上下文（多轮链）无事件源、不可恢复——请 relaunch_task 重新发起（rounds 事件化另立变更）")
	}
	return spec
}
