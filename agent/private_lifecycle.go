// 契约: docs/wiki/agent/execution-generations.md#lifecycle-convergence
package agent

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"
)

// releaseInvocationCM retains a private execution face until its producers drain.
func (ta *TagentAgent) releaseInvocationCM(cm *ContextManager) {
	if ta == nil || cm == nil {
		return
	}
	cm.retireActiveBinding()
	if cm.OutstandingRefs() == 0 && len(cm.UnconvergedRefs()) == 0 {
		ta.unregisterLiveCM(cm)
		return
	}
	cm.armFullyDrained(func() { ta.unregisterLiveCM(cm); ta.tryDeferredFinalExit() })
}

// resolveContinuationOwner uses the composition root or already declared local targets.
func (ta *TagentAgent) resolveContinuationOwner(name string) *TagentAgent {
	if ta == nil {
		return nil
	}
	if ta.name == name {
		return ta
	}
	if ta.config != nil && ta.config.ContinuationOwnerResolver != nil {
		if owner := ta.config.ContinuationOwnerResolver(name); owner != nil {
			return owner
		}
	}
	if ta.contextManager != nil {
		if w := ta.contextManager.SubagentWrapper(name); w != nil {
			if child, ok := w.agent.(*TagentAgent); ok {
				return child
			}
		}
	}
	if ta.config != nil {
		for _, w := range collectAgentToolWrappers(ta.config.Tools) {
			if child, ok := w.agent.(*TagentAgent); ok && child.name == name {
				return child
			}
		}
	}
	return nil
}

func (ta *TagentAgent) tryDeferredFinalExit() {
	if ta == nil || !ta.finalExitReady() {
		return
	}
	ta.closeMu.Lock()
	tail := ta.closeTail
	ta.closeMu.Unlock()
	if tail != nil {
		go tail()
	}
}

func (ta *TagentAgent) finalExitReady() bool {
	if !ta.producersStopped() || ta.LiveCMCount() != 0 {
		return false
	}
	if cm := ta.contextManager; cm != nil {
		for _, b := range cm.allBindings() {
			if !b.fullyReclaimed() {
				return false
			}
		}
	}
	for _, c := range ta.closers {
		if _, staged := c.(interface{ BeginClose() }); !staged {
			continue
		}
		if completed, ok := c.(interface{ CloseDone() <-chan struct{} }); ok {
			select {
			case <-completed.CloseDone():
			default:
				return false
			}
		}
	}
	return true
}

func (ta *TagentAgent) producersStopped() bool {
	ta.closeMu.Lock()
	done := ta.closeProducersDone
	ta.closeMu.Unlock()
	if done == nil {
		return false
	}
	select {
	case <-done:
		return true
	default:
		return false
	}
}

func (ta *TagentAgent) executionWorkQuiet() bool {
	if ta.LiveCMCount() != 0 {
		return false
	}
	if cm := ta.contextManager; cm != nil {
		for _, b := range cm.allBindings() {
			if !b.shutdownQuiet() {
				return false
			}
		}
	}
	return true
}

func (ta *TagentAgent) beginOwnedShutdown() {
	ta.closeMu.Lock()
	if ta.closeProducersDone != nil {
		ta.closeMu.Unlock()
		return
	}
	ta.closeProducersDone = make(chan struct{})
	done := ta.closeProducersDone
	ta.closeMu.Unlock()
	ta.sessionMu.Lock()
	state := ta.loopState.Load()
	if state == loopIdle {
		ta.loopState.Store(loopClosed)
		if ta.loopDone == nil {
			ta.loopDone = make(chan struct{})
		}
	} else if state == loopRunning {
		ta.loopState.Store(loopStopping)
	}
	if ta.loopCancel != nil {
		ta.loopCancel()
	}
	if ta.meditationMgr != nil && ta.meditationMgr.cancel != nil {
		ta.meditationMgr.cancel()
	}
	loopDone := ta.loopDone
	waitLoop := state == loopRunning || state == loopStopping
	ta.sessionMu.Unlock()
	if ta.cleanupCancel != nil {
		ta.cleanupCancel()
	}
	request, cancelRequest := context.WithCancel(context.Background())
	cancelRequest()
	_ = ta.stopContinuations(request)
	if ta.taskManager != nil {
		_ = ta.taskManager.Close(request)
	}
	for _, cm := range ta.snapshotLiveCMs() {
		go ta.releaseInvocationCM(cm)
	}
	go func() {
		_ = ta.stopContinuations(context.Background())
		if ta.taskManager != nil {
			<-ta.taskManager.CloseDone()
		}
		if waitLoop && loopDone != nil {
			<-loopDone
			ta.loopWg.Wait()
			ta.loopState.Store(loopClosed)
		}
		if ta.cleanupDone != nil {
			<-ta.cleanupDone
		}
		if ta.meditationMgr != nil {
			ta.meditationMgr.Stop()
		}
		close(done)
		ta.tryDeferredFinalExit()
	}()
}

func (ta *TagentAgent) waitOwnedShutdown(ctx context.Context) error {
	ta.closeMu.Lock()
	done := ta.closeProducersDone
	ta.closeMu.Unlock()
	var cleanerDeadline <-chan time.Time
	if ta.cleanupDone != nil {
		timer := time.NewTimer(cleanerStopGrace)
		defer timer.Stop()
		cleanerDeadline = timer.C
	}
	waiting := true
	for waiting {
		select {
		case <-done:
			waiting = false
		case <-cleanerDeadline:
			if !ta.CleanerStopped() {
				return fmt.Errorf("%w: workspace cleaner has not stopped", ErrExecUnconverged)
			}
			cleanerDeadline = nil
		case <-ctx.Done():
			return fmt.Errorf("%w: producer or transport shutdown: %v", ErrExecUnconverged, ctx.Err())
		}
	}

	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for !ta.executionWorkQuiet() {
		select {
		case <-tick.C:
		case <-ctx.Done():
			return fmt.Errorf("%w: execution references remain held: %v", ErrExecUnconverged, ctx.Err())
		}
	}
	return nil
}

func (cm *ContextManager) retireForOwnerClose() {
	b := cm.activeBinding()
	b.retire()
	cm.retiredMu.Lock()
	if cm.retiredBindings == nil {
		cm.retiredBindings = make(map[*execBinding]struct{})
	}
	b.mu.Lock()
	closed := b.closed
	if !closed {
		cm.retiredBindings[b] = struct{}{}
	}
	b.mu.Unlock()
	cm.retiredMu.Unlock()
	if !closed {
		go b.release(leaseKindNoop)
	}
}

func (ta *TagentAgent) waitFinalExitReady(ctx context.Context) bool {
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for !ta.finalExitReady() {
		select {
		case <-tick.C:
		case <-ctx.Done():
			return false
		}
	}
	return true
}

func (ta *TagentAgent) closeRegularResources(beforeOwned bool) error {
	var errs []error
	before := true
	for _, c := range ta.closers {
		if _, staged := c.(interface{ BeginClose() }); staged {
			if _, owned := c.(interface{ CloseDone() <-chan struct{} }); owned {
				before = false
			}
			continue
		}
		if before == beforeOwned {
			if err := c.Close(); err != nil {
				errs = append(errs, fmt.Errorf("close resource: %w", err))
			}
		}
	}
	return errors.Join(errs...)
}

func (ta *TagentAgent) finishOwnedExit(carryClosers bool) error {
	var errs []error
	if carryClosers {
		errs = append(errs, ta.closeRegularResources(true))
	}
	errs = append(errs, ta.closeRegularResources(false))
	if ta.persistentBus != nil {
		errs = append(errs, ta.persistentBus.CloseDurable())
	}
	errs = append(errs, ta.CloseContinuationInboxes())
	ta.settleOutput()
	if ta.trajectoryRecorder != nil {
		errs = append(errs, ta.trajectoryRecorder.Close())
	}
	errs = append(errs, ta.exitMemoryStore())
	return errors.Join(errs...)
}

func (ta *TagentAgent) signalFinalExit() {
	ta.closeFinalOnce.Do(func() {
		ta.closeMu.Lock()
		if ta.closeFinalDone == nil {
			ta.closeFinalDone = make(chan struct{})
		}
		close(ta.closeFinalDone)
		ta.closeMu.Unlock()
	})
}

// CloseDone closes after owned producers, transports, execution faces and final exit have finished.
func (ta *TagentAgent) CloseDone() <-chan struct{} {
	ta.closeMu.Lock()
	defer ta.closeMu.Unlock()
	if ta.closeFinalDone == nil {
		ta.closeFinalDone = make(chan struct{})
	}
	return ta.closeFinalDone
}

func (ta *TagentAgent) startRecoveredOwners(ctx context.Context) error {
	owners := map[string]*TagentAgent{ta.name: ta}
	if ta.resident != nil {
		for name, owner := range ta.resident.Snapshot() {
			owners[name] = owner
		}
	}
	names := make([]string, 0, len(owners))
	for name, owner := range owners {
		if owner != nil {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		if _, err := owners[name].ResumePendingDelegations(); err != nil {
			return err
		}
	}
	for _, name := range names {
		if err := owners[name].StartRecoveredContinuations(ctx); err != nil {
			return err
		}
	}
	for _, name := range names {
		if err := owners[name].RecoverTaskOutbox(); err != nil {
			return err
		}
	}
	for _, owner := range owners {
		if owner != nil && owner.taskManager != nil {
			owner.taskManager.ResumePendingDeliveries()
		}
	}
	return nil
}
