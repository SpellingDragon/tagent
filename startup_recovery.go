// 契约: docs/wiki/agent/task-lifecycle.md#restore-rebuild
package tagent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/agent/task"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/prompt"
	"trpc.group/trpc-go/trpc-agent-go/log"
)

func recoveryOwnerNames(cache map[string]*agent.TagentAgent) []string {
	names := make([]string, 0, len(cache))
	for name := range cache {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func buildRecoveryOwners(cfg Config, rc *runtimeConfig, loader *prompt.Loader, cache map[string]*agent.TagentAgent) error {
	for {
		needed := make(map[string]bool)
		for _, name := range recoveryOwnerNames(cache) {
			owner := cache[name]
			if owner == nil {
				continue
			}
			if tm := owner.TaskManager(); tm != nil {
				for _, tk := range tm.TasksSnapshot() {
					if tk == nil {
						continue
					}
					spec := tk.SpecSnapshot()
					if spec.Kind == "subagent" && spec.Declarative != nil && spec.Declarative.AgentName != "" {
						needed[spec.Declarative.AgentName] = true
					}
				}
			}
			bindings, err := owner.RecoveredContinuationBindingsFromWAL()
			if err != nil {
				return fmt.Errorf("agent %q recovery owner inventory: %w", name, err)
			}
			for _, binding := range bindings {
				if binding.ParentAgent != "" {
					needed[binding.ParentAgent] = true
				}
			}
			targets, err := persistedOutboxTargets(owner)
			if err != nil {
				return fmt.Errorf("agent %q outbox owner inventory: %w", name, err)
			}
			for _, target := range targets {
				needed[target] = true
			}
		}
		var missing []string
		for name := range needed {
			if cache[name] == nil {
				if _, declared := cfg.Agents[name]; declared {
					missing = append(missing, name)
				}
			}
		}
		if len(missing) == 0 {
			return nil
		}
		sort.Strings(missing)
		for _, name := range missing {
			if cache[name] != nil {
				continue
			}
			if _, err := buildAgentDFS(name, cfg.Agents[name], cfg, rc, loader, cache, buildModeResident, map[string]bool{}); err != nil {
				return fmt.Errorf("rebuild persisted owner %q: %w", name, err)
			}
		}
	}
}

func persistedOutboxTargets(owner *agent.TagentAgent) ([]string, error) {
	store := owner.MemStore()
	if store == nil {
		return nil, nil
	}
	targets := make(map[string]bool)
	for offset := 0; ; offset += 500 {
		refs, err := store.QueryEvents(memory.QueryOptions{PartitionIDs: []int{memory.PartitionIDFromName(owner.Info().Name)}, EventTypes: []string{tagentevent.TypeTaskSignal}, Limit: 500, Offset: offset})
		if err != nil {
			return nil, err
		}
		for _, ref := range refs {
			fact, err := store.GetEvent(ref.EventKey)
			if err != nil {
				return nil, err
			}
			if fact == nil {
				return nil, fmt.Errorf("missing raw task signal %d", ref.EventKey)
			}
			var signal struct {
				TaskID            string            `json:"task_id"`
				RoundID           uint64            `json:"task_round_id"`
				Seq               uint64            `json:"seq"`
				ReceiptKey        int64             `json:"receipt_key"`
				ObservedAt        time.Time         `json:"observed_at"`
				Event             *agent.AgentEvent `json:"event"`
				ChildAgentName    string            `json:"child_agent_name"`
				ChildInvocationID string            `json:"child_invocation_id"`
			}
			if json.Unmarshal([]byte(fact.Content), &signal) == nil && signal.TaskID != "" && signal.RoundID > 0 && signal.Seq > 0 && signal.Event != nil && signal.ReceiptKey > 0 && !signal.ObservedAt.IsZero() && signal.ChildAgentName != "" && signal.ChildInvocationID != "" {
				targets[signal.ChildAgentName] = true
			}
		}
		if len(refs) < 500 {
			break
		}
	}
	names := make([]string, 0, len(targets))
	for name := range targets {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func prepareResidentRecovery(ctx context.Context, rc *runtimeConfig) error {
	if rc == nil || rc.resident == nil {
		return nil
	}
	owners := rc.resident.Snapshot()
	names := recoveryOwnerNames(owners)
	for _, name := range names {
		if _, err := owners[name].RebuildContinuationsFromWAL(); err != nil {
			return fmt.Errorf("agent %q continuations: %w", name, err)
		}
	}
	for _, name := range names {
		if err := owners[name].RestoreServiceHandoffs(ctx); err != nil {
			return fmt.Errorf("agent %q service receivers: %w", name, err)
		}
	}
	for _, name := range names {
		owner, actionTool := owners[name], rc.actionTools[name]
		if actionTool == nil {
			continue
		}
		if err := recoverCommandTasks(name, owner, actionTool.RecoverTaskDetector); err != nil {
			return err
		}
	}
	return nil
}

func recoverCommandTasks(name string, owner *agent.TagentAgent, recoverDetector func(*task.Task) task.SettleDetector) error {
	if owner == nil || owner.TaskManager() == nil || recoverDetector == nil {
		return nil
	}
	tm := owner.TaskManager()
	for _, tk := range tm.TasksSnapshot() {
		if tk == nil || tk.HasProducer() {
			continue
		}
		snapshot := tk.RoutingSnapshot()
		if snapshot.Spec.Kind != "command" || snapshot.Spec.Declarative == nil {
			continue
		}
		if snapshot.ProducerRound == 0 && snapshot.Status.Live() && snapshot.Spec.Start != nil {
			if err := tm.ResumeRestoredStart(tk.ID); err != nil {
				log.Warnf("[recovery] command intent %s/%d could not resume: %v", tk.ID, snapshot.RoundID, err)
			}
			continue
		}
		if !snapshot.Status.Live() && len(snapshot.PendingSignals) == 0 {
			continue
		}
		detector := recoverDetector(tk)
		if detector == nil {
			log.Warnf("[recovery] command producer %s/%d is unavailable; its continuation will reconcile the missing resource", tk.ID, snapshot.ProducerRound)
			continue
		}
		if err := tm.BindDetector(tk.ID, detector); err != nil {
			detector.Cancel()
			return fmt.Errorf("agent %q command producer %s: %w", name, tk.ID, err)
		}
	}
	return nil
}

func recheckResidentDeadlines(rc *runtimeConfig) {
	if rc == nil || rc.resident == nil {
		return
	}
	for _, owner := range rc.resident.Snapshot() {
		if owner != nil && owner.TaskManager() != nil {
			owner.TaskManager().RecheckDeadlines()
		}
	}
}
