// 契约: docs/wiki/agent/execution-generations.md#lifecycle-convergence
package agent

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
	if ta.LiveCMCount() != 0 {
		return false
	}
	if ta.contextManager != nil && (ta.contextManager.OutstandingRefs() != 0 || len(ta.contextManager.UnconvergedRefs()) != 0) {
		return false
	}
	if ta.taskManager != nil {
		select {
		case <-ta.taskManager.CloseDone():
		default:
			return false
		}
	}
	return true
}
