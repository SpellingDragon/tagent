// 契约: docs/wiki/tool/tool-architecture.md#resident-continuity
package action

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/SpellingDragon/tagent/agent/task"
)

// pTimeout et al. are the spawn-parameter keys for the declarative
// round-trip — the full ActionArgs spawn field set (session-op fields
// Op/Keys/Enter/Tail/Ansi/GraceSec/SessionID are not spawn parameters and are
// excluded; Command is handled separately).
const (
	pTimeout      = "timeout"
	pTTL          = "ttl"
	pWorkDir      = "work_dir"
	pIsTUI        = "is_tui"
	pQuiet        = "quiet_timeout"
	pMode         = "mode"
	pName         = "name"
	pWatch        = "watch"
	pProbe        = "probe"
	pProbeEvery   = "probe_interval_sec"
	pProbeFails   = "probe_failures"
	pReadyPattern = "ready_pattern"
	pInputPattern = "input_pattern"
	pResourceID   = "resource_id"
	pPhase        = "phase"
	pPlaceholder  = "placeholder"
	pBaseline     = "baseline"
	pEnv          = "env"
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
	if args.TTL > 0 {
		p[pTTL] = strconv.Itoa(args.TTL)
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
	if len(args.Env) > 0 {
		if b, err := json.Marshal(args.Env); err == nil {
			p[pEnv] = string(b)
		}
	}
	if args.ReadyPattern != "" {
		p[pReadyPattern] = args.ReadyPattern
	}
	if args.InputPattern != "" {
		p[pInputPattern] = args.InputPattern
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
		case pTTL:
			n, err := strconv.Atoi(v)
			if err != nil {
				return args, fmt.Errorf("param %s=%q: %w", k, v, err)
			}
			args.TTL = n
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
		case pResourceID, pPhase, pPlaceholder, pBaseline:
		case pEnv:
			if err := json.Unmarshal([]byte(v), &args.Env); err != nil {
				return args, fmt.Errorf("param env: %w", err)
			}
		case pReadyPattern:
			args.ReadyPattern = v
		case pInputPattern:
			args.InputPattern = v
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
		return task.TaskSpec{Kind: decl.Kind, Desc: decl.Desc, Key: decl.Key, TTL: defaultTaskTTL, Declarative: &decl}
	}
	if args.Mode == "" {
		args.Mode = string(ModeOneshot)
	}
	spec := ct.commandSpec(spawner, args)
	spec.Declarative = &decl
	if decl.TaskID != "" {
		spec.Start = nil
		spec.ResumeFn = ct.rebuiltResumeResourceClosure(decl.TaskID, decl.Params[pResourceID], args.IsTUI)
		spec.Alive = ct.resourceAliveClosure(decl.TaskID, decl.Params[pResourceID])
	}
	return spec
}

func (ct *ActionTool) rebuiltResumeClosure(sessionID string, isTUI bool) func(context.Context, string) (task.SettleDetector, error) {
	return ct.rebuiltResumeResourceClosure(sessionID, "", isTUI)
}

func (ct *ActionTool) rebuiltResumeResourceClosure(sessionID, resourceID string, isTUI bool) func(context.Context, string) (task.SettleDetector, error) {
	return func(ctx context.Context, input string) (task.SettleDetector, error) {
		ct.detectorMu.Lock()
		var detector *TmuxSettleDetector
		for _, candidate := range ct.detectors {
			if candidate.sessionID == sessionID && (resourceID == "" || resourceID == candidate.resourceID) {
				detector = candidate
				break
			}
		}
		ct.detectorMu.Unlock()
		if detector == nil {
			return nil, fmt.Errorf("session %s is no longer monitored; use relaunch_task", sessionID)
		}
		return ct.resumeResourceClosure(sessionID, detector.resourceID, isTUI, detector)(ctx, input)
	}
}

// SubagentSpecFromDeclarative rebuilds a subagent TaskSpec (promise table:
// Relaunch✅ via redispatch through the resident agents map; Resume❌ — the
// rounds chain has no event source, cross-restart resume returns guidance).
// The overrides frozen at spawn ride the re-dispatch, so the rebuilt task
// runs on the same view the original call assembled.
func SubagentSpecFromDeclarative(redispatch func(ctx context.Context, agentName, body string, overrides *task.Overrides) (task.SpawnResult, error), decl task.Declarative) task.TaskSpec {
	spec := task.TaskSpec{
		Kind:        "subagent",
		Desc:        decl.Desc,
		Key:         decl.Key,
		Declarative: &decl,
	}
	if s := decl.Params["ttl"]; s != "" {
		if secs, err := strconv.ParseInt(s, 10, 64); err == nil && secs > 0 {
			spec.TTL = time.Duration(secs) * time.Second
		}
	}
	if redispatch != nil && decl.AgentName != "" {
		body := decl.MessageBody
		spec.Relaunch = func(ctx context.Context) (task.SpawnResult, error) {
			return redispatch(ctx, decl.AgentName, body, decl.Overrides)
		}
	}
	spec.ResumeFn = func(_ context.Context, _ string) (task.SettleDetector, error) {
		return nil, fmt.Errorf("跨重启的子 agent 会话上下文（多轮链）无事件源、不可恢复——请 relaunch_task 重新发起（rounds 事件化另立变更）")
	}
	return spec
}
