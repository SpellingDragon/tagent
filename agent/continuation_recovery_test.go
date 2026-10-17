// 契约: docs/wiki/agent/task-lifecycle.md#restore-rebuild
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/task"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/kv"
	"github.com/stretchr/testify/require"
	trpcagent "trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/model"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

type continuationCrashStore struct {
	*memory.FileSegmentStore
	marker string
}

type continuationHandoffCrashStore struct {
	*memory.FileSegmentStore
	marker string
}

func (s *continuationHandoffCrashStore) StoreEvent(key int64, fact memory.FullEvent) error {
	if err := s.FileSegmentStore.StoreEvent(key, fact); err != nil {
		return err
	}
	if fact.EventType == tagentevent.TypeContinuationState {
		var state continuationCheckpoint
		if json.Unmarshal([]byte(fact.Content), &state) == nil && state.LastBatchOutcome == "completed" && state.Handoff == nil && state.Outcome == "" && len(state.Routing.Streams) > 0 && state.Routing.Streams[0].Owner != "" {
			if err := os.WriteFile(s.marker, []byte(fact.Content), 0600); err != nil {
				return err
			}
			select {}
		}
	}
	return nil
}

func (s *continuationCrashStore) ReplayEvent(key int64, fact memory.FullEvent) (memory.ReplayResult, memory.FullEvent, error) {
	result, stored, err := s.FileSegmentStore.ReplayEvent(key, fact)
	if err == nil && fact.EventType == tagentevent.TypeTaskSignal && fact.Metadata[tagentevent.MetaKeyAgentName] == "crash-parent" {
		if err := os.WriteFile(s.marker, []byte(fact.Content), 0600); err != nil {
			return result, stored, err
		}
		select {}
	}
	return result, stored, err
}

type continuationEffectTool struct{ path string }

func (s *continuationEffectTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{Name: "action", Description: "record one external effect", InputSchema: &trpctool.Schema{Type: "object"}}
}

func (s *continuationEffectTool) Call(context.Context, []byte) (any, error) {
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if _, err := f.WriteString("effect\n"); err != nil {
		return nil, err
	}
	if err := f.Sync(); err != nil {
		return nil, err
	}
	return "external effect committed", nil
}

func continuationOpenCrashStore(t *testing.T, dir string) *memory.FileSegmentStore {
	t.Helper()
	k, err := kv.NewLocalFileKV(dir)
	require.NoError(t, err)
	store, err := memory.NewFileSegmentStore(k, nil, dir, 100)
	require.NoError(t, err)
	store.SetRetentionLease(memory.NewRetentionLease())
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func continuationCrashAgents(t *testing.T, store memory.MemoryStore, effectPath string, recover bool) (*TagentAgent, *TagentAgent, *requestCapturingModel, *requestCapturingModel) {
	t.Helper()
	owners := make(map[string]*TagentAgent)
	resolve := func(name string) *TagentAgent { return owners[name] }
	parentRecovered := &requestCapturingModel{resp: finalTextResponse("recover-parent", "reconciled original result")}
	childRecovered := &requestCapturingModel{resp: finalTextResponse("recover-child", "must never execute")}
	var childModel model.Model = childRecovered
	var parentModel model.Model = parentRecovered
	if !recover {
		childCount, parentCount := 0, 0
		childModel = &sequenceMockModel{callCount: &childCount, responses: []*model.Response{toolCallResponse("external-effect", "perform once"), finalTextResponse("child-result", "durable child result")}}
		call := toolCallResponse("delegate-once", "")
		call.Choices[0].Message.ToolCalls[0].Function = model.FunctionDefinitionParam{Name: "crash-child", Arguments: []byte(`{"request":"perform external effect once"}`)}
		parentModel = &sequenceMockModel{callCount: &parentCount, responses: []*model.Response{call, finalTextResponse("parent-result", "normal result")}}
	}
	child, err := NewTagentAgent(&TagentConfig{Name: "crash-child", Model: childModel, Tools: []trpctool.Tool{&continuationEffectTool{path: effectPath}}, MemoryStore: store, MemStoreBorrowed: true, ContinuationOwnerResolver: resolve})
	require.NoError(t, err)
	owners[child.name] = child
	w := NewAgentToolWrapper(child, "perform the action", nil, store)
	w.SetAsyncDenseDuration(5 * time.Second)
	parent, err := NewTagentAgent(&TagentConfig{Name: "crash-parent", Model: parentModel, Tools: []trpctool.Tool{w}, MemoryStore: store, MemStoreBorrowed: true, ContinuationOwnerResolver: resolve})
	require.NoError(t, err)
	owners[parent.name] = parent
	parent.SetDelegationOwnerCM(parent.contextManager)
	parent.SetToolParentProjection()
	t.Cleanup(func() { _ = parent.Close(); _ = child.Close() })
	return parent, child, parentRecovered, childRecovered
}

// TestContinuationSubprocessRawResultCrash preserves the real Run/TaskManager/WAL boundary across SIGKILL.
func TestContinuationSubprocessRawResultCrash(t *testing.T) {
	if root := os.Getenv("TAGENT_CONTINUATION_CRASH_DIR"); root != "" {
		store := &continuationCrashStore{FileSegmentStore: continuationOpenCrashStore(t, filepath.Join(root, "store")), marker: filepath.Join(root, "raw-ready")}
		parent, _, _, _ := continuationCrashAgents(t, store, filepath.Join(root, "effects"), false)
		inv := trpcagent.NewInvocation(trpcagent.WithInvocationID("crashed-parent-scope"), trpcagent.WithInvocationMessage(model.NewUserMessage("perform the external action exactly once")))
		ch, err := parent.Run(context.Background(), inv)
		require.NoError(t, err)
		for range ch {
		}
		t.Fatal("crash boundary was not reached")
	}
	root := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestContinuationSubprocessRawResultCrash$", "-test.timeout=15s")
	cmd.Env = append(os.Environ(), "TAGENT_CONTINUATION_CRASH_DIR="+root)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	require.NoError(t, cmd.Start())
	defer cmd.Process.Kill()
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	marker := filepath.Join(root, "raw-ready")
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	var rawBytes []byte
	for len(rawBytes) == 0 {
		select {
		case err := <-waited:
			t.Fatalf("subprocess exited before raw result barrier: %v\n%s", err, output.String())
		case <-ctx.Done():
			t.Fatal("subprocess did not reach the raw result barrier")
		case <-ticker.C:
			rawBytes, _ = os.ReadFile(marker)
		}
	}
	require.NoError(t, cmd.Process.Kill())
	require.Error(t, <-waited)
	var original taskSignalRecord
	require.NoError(t, json.Unmarshal(rawBytes, &original))
	require.True(t, original.Inline)
	require.Equal(t, task.SettleCompleted, original.Kind)
	require.Equal(t, "durable child result", original.Output)
	store := continuationOpenCrashStore(t, filepath.Join(root, "store"))
	parent, child, parentModel, childModel := continuationCrashAgents(t, store, filepath.Join(root, "effects"), true)
	facts, err := continuationFacts(store, parent.partitionID())
	require.NoError(t, err)
	for _, fact := range facts {
		require.NotEqual(t, "delegate-once", fact.ToolID, "SIGKILL must precede the native tool response")
	}
	parent.RebuildTaskRegistryFromWAL(store, nil)
	_, err = child.RebuildContinuationsFromWAL()
	require.NoError(t, err)
	_, err = parent.RebuildContinuationsFromWAL()
	require.NoError(t, err)
	ch, found, err := parent.ResumeContinuationByID(ctx, "crashed-parent-scope", "", 0)
	require.NoError(t, err)
	require.True(t, found)
	var logical int
	for evt := range ch {
		if len(evt.StateDelta["delegation_outcome"]) > 0 {
			logical++
			require.Equal(t, "completed", string(evt.StateDelta["delegation_outcome"]))
		}
	}
	require.Equal(t, 1, logical)
	require.Equal(t, 1, parentModel.requestCount())
	require.Zero(t, childModel.requestCount())
	require.NoError(t, parent.RecoverTaskOutbox())
	tk, exists := parent.taskManager.Get(original.TaskID)
	require.True(t, exists)
	require.Equal(t, original.RoundID, tk.RoundID())
	require.Equal(t, original.ChildInvocationID, tk.SpecSnapshot().Declarative.Params[childInvocationIDParam])
	effects, err := os.ReadFile(filepath.Join(root, "effects"))
	require.NoError(t, err)
	require.Equal(t, "effect\n", string(effects))
	facts, err = continuationFacts(store, parent.partitionID())
	require.NoError(t, err)
	var originalInputs, recoveredResults, interrupted int
	for _, fact := range facts {
		if fact.EventType == tagentevent.TypeExternalInput && fact.Content == "perform the external action exactly once" {
			originalInputs++
		}
		if fact.EventType == tagentevent.TypeExternalInput && fact.Metadata[tagentevent.MetaKeySourceEventID] == original.Event.ID {
			recoveredResults++
		}
		if fact.ToolID == "delegate-once" && strings.Contains(fact.Content, "interrupted") {
			interrupted++
		}
	}
	require.Equal(t, 1, originalInputs)
	require.Equal(t, 1, recoveredResults)
	require.Equal(t, 1, interrupted)
}

// TestContinuationSubprocessReadyBeforeHandoffCrash resumes the committed turn's routing phase without another model turn.
func TestContinuationSubprocessReadyBeforeHandoffCrash(t *testing.T) {
	if root := os.Getenv("TAGENT_HANDOFF_CRASH_DIR"); root != "" {
		store := &continuationHandoffCrashStore{FileSegmentStore: continuationOpenCrashStore(t, filepath.Join(root, "store")), marker: filepath.Join(root, "ready")}
		count := 0
		child, err := NewTagentAgent(&TagentConfig{Name: "handoff-crash-child", Model: &sequenceMockModel{callCount: &count, responses: []*model.Response{toolCallResponse("start-service", ""), finalTextResponse("ready", "service ready")}}, Tools: []trpctool.Tool{&continuationServiceTool{started: make(chan *task.ManualDetector, 1)}}, MemoryStore: store, MemStoreBorrowed: true})
		require.NoError(t, err)
		address := ContinuationContext{ParentAgent: "handoff-crash-entry", ParentTaskID: "original-parent-task", ParentTaskRound: 1}
		inv := trpcagent.NewInvocation(trpcagent.WithInvocationID("crashed-service-scope"), trpcagent.WithInvocationMessage(model.NewUserMessage("start the service once")), trpcagent.WithInvocationRunOptions(trpcagent.RunOptions{RuntimeState: map[string]any{ContinuationContextKey: address}}))
		ch, err := child.Run(context.Background(), inv)
		require.NoError(t, err)
		for range ch {
		}
		t.Fatal("handoff crash barrier was not reached")
	}
	root := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestContinuationSubprocessReadyBeforeHandoffCrash$", "-test.timeout=15s")
	cmd.Env = append(os.Environ(), "TAGENT_HANDOFF_CRASH_DIR="+root)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	require.NoError(t, cmd.Start())
	defer cmd.Process.Kill()
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	var checkpoint []byte
	for len(checkpoint) == 0 {
		select {
		case err := <-waited:
			t.Fatalf("subprocess exited before the handoff barrier: %v\n%s", err, output.String())
		case <-ctx.Done():
			t.Fatal("subprocess did not reach the handoff barrier")
		case <-ticker.C:
			checkpoint, _ = os.ReadFile(filepath.Join(root, "ready"))
		}
	}
	require.NoError(t, cmd.Process.Kill())
	require.Error(t, <-waited)
	var original continuationCheckpoint
	require.NoError(t, json.Unmarshal(checkpoint, &original))
	require.Empty(t, original.Routing.Waits)
	require.Len(t, original.Routing.Streams, 1)
	require.Nil(t, original.Handoff)
	require.Empty(t, original.Outcome)
	store := continuationOpenCrashStore(t, filepath.Join(root, "store"))
	owners := make(map[string]*TagentAgent)
	resolve := func(name string) *TagentAgent { return owners[name] }
	childModel := &requestCapturingModel{resp: finalTextResponse("unused", "must not restart service")}
	child, err := NewTagentAgent(&TagentConfig{Name: "handoff-crash-child", Model: childModel, MemoryStore: store, MemStoreBorrowed: true, ContinuationOwnerResolver: resolve})
	require.NoError(t, err)
	owners[child.name] = child
	entryModel := &requestCapturingModel{resp: finalTextResponse("late", "service terminal delivered")}
	entry, err := NewTagentAgent(&TagentConfig{Name: "handoff-crash-entry", Model: entryModel, MemoryStore: store, MemStoreBorrowed: true, ContinuationOwnerResolver: resolve})
	require.NoError(t, err)
	owners[entry.name] = entry
	defer child.Close()
	defer entry.Close()
	require.Equal(t, 1, child.RebuildTaskRegistryFromWAL(store, nil))
	stream := original.Routing.Streams[0]
	detector := task.NewManualDetector()
	defer detector.Cancel()
	require.NoError(t, child.taskManager.BindDetector(stream.TaskID, detector))
	_, err = child.RebuildContinuationsFromWAL()
	require.NoError(t, err)
	c := child.continuationRegistry().snapshot()[0]
	require.Empty(t, c.state.Outcome, "a live owned stream still needs a committed handoff")
	out, err := entry.StartLoop("handoff-recover", "handoff-recover")
	require.NoError(t, err)
	ch, found, err := child.ResumeContinuationByID(ctx, original.InvocationID, original.Address.ParentTaskID, original.Address.ParentTaskRound)
	require.NoError(t, err)
	require.True(t, found)
	for evt := range ch {
		if len(evt.StateDelta["delegation_outcome"]) > 0 {
			require.Equal(t, "completed", string(evt.StateDelta["delegation_outcome"]))
		}
	}
	require.Zero(t, childModel.requestCount())
	require.Zero(t, entryModel.requestCount())
	c.mu.Lock()
	require.NotNil(t, c.state.Handoff)
	require.Equal(t, entry.name, c.state.Handoff.ParentAgent)
	c.mu.Unlock()
	child.taskManager.ResumePendingDeliveries()
	detector.Emit(task.SettleSignal{Kind: task.SettleCompleted, Output: "original service terminal"})
	detector.Done()
	detector.FireStop()
	require.Equal(t, "service terminal delivered", continuationAwaitFinal(t, out).Response.Choices[0].Message.Content)
	entry.StopLoop()
	tk, exists := child.taskManager.Get(stream.TaskID)
	require.True(t, exists)
	require.Equal(t, stream.RoundID, tk.ProducerRound())
	require.Zero(t, childModel.requestCount())
	require.Equal(t, 1, entryModel.requestCount())
}
