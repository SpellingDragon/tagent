// 契约: docs/wiki/agent/task-lifecycle.md#restore-rebuild
package tagent

import (
	"context"
	"fmt"
	"sort"

	"github.com/SpellingDragon/tagent/agent"
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
		tm := owner.TaskManager()
		if tm == nil || actionTool == nil {
			continue
		}
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
			detector := actionTool.RecoverTaskDetector(tk)
			if detector == nil {
				log.Warnf("[recovery] command producer %s/%d is unavailable; its continuation will reconcile the missing resource", tk.ID, snapshot.ProducerRound)
				continue
			}
			if err := tm.BindDetector(tk.ID, detector); err != nil {
				detector.Cancel()
				return fmt.Errorf("agent %q command producer %s: %w", name, tk.ID, err)
			}
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
