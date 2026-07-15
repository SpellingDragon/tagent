package agent

import (
	"context"
	"fmt"

	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/log"
)

// RegisterCloser registers a closer to be called on agent shutdown.
func (ta *TagentAgent) RegisterCloser(c Closer) {
	ta.closers = append(ta.closers, c)
}

// SetTrajectoryRecorder sets the trajectory recorder for this agent.
// When set, StartLoop will automatically call SetSessionInfo on it.
// The recorder should also be registered via RegisterCloser for graceful shutdown.
func (ta *TagentAgent) SetTrajectoryRecorder(tr *TrajectoryRecorder) {
	ta.trajectoryRecorder = tr
}

// TrajectoryRecorder returns the trajectory recorder if one is set, or nil.
func (ta *TagentAgent) TrajectoryRecorder() *TrajectoryRecorder {
	return ta.trajectoryRecorder
}

// Close closes all registered resources and the runner.
// Closers (e.g., ActionTool) are stopped first, then the MemoryStore
// (if it supports closing), and finally the runner.
func (ta *TagentAgent) Close() error {
	var errs []error

	// Stop Persistent Event Loop first if active
	if ta.loopActive.Load() {
		ta.StopLoop()
	}

	// Close registered closers first (e.g., ActionTool stops TmuxMonitor)
	for _, c := range ta.closers {
		if err := c.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close resource: %w", err))
		}
	}

	// Close memory store if it supports closing (e.g., FileSegmentStore stops lifecycle components)
	if c, ok := ta.memStore.(interface{ Close() error }); ok {
		if err := c.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close memory store: %w", err))
		}
	}

	// Close ContextManager (closes unified Runner)
	if ta.contextManager != nil {
		if err := ta.contextManager.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close context manager: %w", err))
		}
	}

	// Close TrajectoryRecorder (flush writeLoop + close files)
	// Must be after contextManager.Close() so no new LLM calls are made.
	if ta.trajectoryRecorder != nil {
		if err := ta.trajectoryRecorder.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close trajectory recorder: %w", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("close errors: %v", errs)
	}
	return nil
}

// StartLoop starts the persistent event loop for this agent.
// The loop runs in a dedicated goroutine and processes events until StopLoop is called.
// Returns the output channel that emits events as they are processed.
func (ta *TagentAgent) StartLoop(userID, sessionID string) (<-chan *event.Event, error) {
	// Use sessionMu to prevent concurrent StartLoop calls from racing
	// on the loopActive check + initialization sequence.
	ta.sessionMu.Lock()
	if ta.loopActive.Load() {
		ta.sessionMu.Unlock()
		return ta.outputCh, nil
	}

	ta.loopCtx, ta.loopCancel = context.WithCancel(context.Background())
	ta.loopActive.Store(true)
	ta.sessionMu.Unlock()

	// Cache session context for event injection.
	ta.setSessionContext(userID, sessionID)

	// Create or attach session for the persistent loop.
	sess := ta.getOrCreateSession(sessionID)
	_ = sess // session managed by ContextManager's Runner

	// Update ContextManager with session context.
	ta.contextManager.SetUserIDSessionID(userID, sessionID)

	// Set TrajectoryRecorder session info (if enabled).
	if ta.trajectoryRecorder != nil {
		ta.trajectoryRecorder.SetSessionInfo(userID, sessionID)
	}

	// Launch runEventLoop in a dedicated goroutine.
	ta.loopWg.Add(1)
	go func() {
		defer ta.loopWg.Done()
		defer func() {
			if r := recover(); r != nil {
				log.Errorf("[StartLoop] runEventLoop panic recovered: %v", r)
			}
			close(ta.outputCh)
		}()
		ta.runEventLoop(ta.loopCtx, ta.persistentBus, ta.contextManager)
	}()

	// Start meditation manager (if configured).
	if ta.meditationMgr != nil {
		ta.meditationMgr.Start()
	}

	// Start projection organizer (if configured).
	if ta.organizer != nil {
		ta.organizer.Start()
	}

	log.Infof("[StartLoop] persistent event loop started user=%s session=%s", userID, sessionID)
	return ta.outputCh, nil
}

// StopLoop stops the persistent event loop.
// Cancels the loop context, waits for the AgentLoop goroutine to exit.
func (ta *TagentAgent) StopLoop() {
	if !ta.loopActive.Load() {
		return
	}
	ta.loopActive.Store(false)

	// Stop meditation manager first (stop injecting new meditation events).
	if ta.meditationMgr != nil {
		ta.meditationMgr.Stop()
	}

	// Stop projection organizer (stop background summary refinement).
	if ta.organizer != nil {
		ta.organizer.Stop()
	}

	ta.loopCancel()
	ta.loopWg.Wait()
	log.Infof("[StopLoop] persistent event loop stopped")
}

// IsLoopActive returns true if the persistent event loop is currently running.
func (ta *TagentAgent) IsLoopActive() bool {
	return ta.loopActive.Load()
}
