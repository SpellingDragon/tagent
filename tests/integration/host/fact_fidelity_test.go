// The fact fidelity anchors of the host:
// one committed fact keeps every field it carries across a close and reassembly
// of the same storage, one identity commits once whatever the commit stamps,
// one identity carrying other content is refused by name and changes nothing,
// one declared stamp survives an unstated repeat and moves never under an
// overwrite, and a turn whose output fact cannot commit fails by name instead
// of answering completed over a fact that never landed.
package host

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/runtime/agent"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// factFidelityAddress is the one agent session the fidelity anchors commit into.
var factFidelityAddress = event.Address{AgentID: "agent-a", SessionID: "session-fact-fidelity"}

// TestFactFidelity drives the fidelity symptom anchors over the formal commit and invoke entries.
//   - a committed fact reads back every field after the storage closed and a new run assembled
//   - a re-sent identity with the same content answers the original identity and keeps one record
//   - a re-sent identity carrying other content is refused by name, writes nothing and leaves the record as it stood
//   - a re-sent identity leaving the declared source and task unstated answers the original identity and keeps the declared stamps
//   - a re-sent identity overwriting a declared source or task is refused by name and changes nothing
//   - a turn whose output fact cannot commit reports the failure and no completed terminal
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestFactFidelity(t *testing.T) {
	t.Run("reassembly reads every field of a committed fact back", factFidelityRestoreKeepsFields)
	t.Run("a re-sent identity with the same content commits once", factFidelityRepeatIsIdempotent)
	t.Run("a re-sent identity with other content is refused by name", factFidelityConflictRefused)
	t.Run("an unstated stamp repeat keeps the declared stamps and commits once", factFidelityUndeclaredStampIsIdempotent)
	t.Run("an overwrite of a declared stamp is refused by name", factFidelityStampOverwriteRefused)
	t.Run("a refused output commit fails the turn", factFidelityCommitFailureFailsTurn)
}

// factFidelityAssemble assembles one runtime over the durable root beside the
// scripted model, exactly as a deployment assembles its run.
func factFidelityAssemble(t *testing.T, root string, provider *entryModel) *assembly.Run {
	t.Helper()
	run, err := assembly.Assemble(context.Background(),
		&config.Declaration{Entry: "agent-a", Agents: []config.AgentSpec{{ID: "agent-a", Model: provider.name}}},
		map[event.AgentID]assembly.NativeAgent{
			"agent-a": {Model: provider, Tools: []trpctool.CallableTool{&entryTool{name: "echo", output: "out-fidelity"}}},
		},
		assembly.WithGovernance(passPolicy{}),
		assembly.WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root}))
	if err != nil {
		t.Fatalf("assemble the fact fidelity runtime over %s: %v", root, err)
	}
	return run
}

// factFidelityClose closes the runtime beside its storage, so the next
// assembly reads the durable root rather than a live ledger.
func factFidelityClose(t *testing.T, run *assembly.Run) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := run.Close(ctx, agent.CloseRequest{Deadline: time.Now().Add(10 * time.Second)}); err != nil {
		t.Fatalf("close the fact fidelity runtime: %v", err)
	}
}

// factFidelityProvider returns the scripted model every anchor assembles over.
func factFidelityProvider() *entryModel {
	return &entryModel{name: "model-a", answer: func(*model.Request) *model.Response {
		return entryFinalTurn("the harbor answered")
	}}
}

// factFidelityEvents returns the two facts the fidelity anchor commits: one
// assistant output carrying pure tool calls beside every identity the fact
// scopes itself by, and one execution audit fact carrying its meta alone.
func factFidelityEvents(addr event.Address) []event.Event {
	stamp := time.UnixMilli(1760000000123)
	return []event.Event{
		{
			Key:       "fact-fidelity-output",
			Type:      event.TypeAgentOutput,
			Timestamp: stamp,
			Meta: map[string]string{
				event.MetaKeyEventSummary:      "the output of the drive",
				event.MetaPrefix + "resource":  "exec-7",
				event.MetaPrefix + "phase":     "stopped",
				event.MetaPrefix + "exit_code": "0",
			},
			Message: model.Message{
				Role: model.RoleAssistant,
				ToolCalls: []model.ToolCall{{
					ID:   "call-fidelity",
					Type: "function",
					Function: model.FunctionDefinitionParam{
						Name:      "echo",
						Arguments: []byte(`{"x":1}`),
					},
				}},
			},
			Owner:        addr,
			Source:       addr,
			Target:       addr,
			InvocationID: "inv-fidelity",
			TaskID:       "task-fidelity",
			EpisodeID:    "episode-fidelity",
			DecisionID:   "decision-fidelity",
			AttemptID:    "attempt-fidelity",
			CallID:       "call-fidelity",
			ApprovalID:   "approval-fidelity",
		},
		{
			Key:       "fact-fidelity-exec-meta",
			Type:      event.TypeFeedback,
			Timestamp: stamp,
			Meta: map[string]string{
				event.MetaKeyTriggerSource:     "host",
				event.MetaKeyEventSummary:      "exec-state: stopped",
				event.MetaPrefix + "resource":  "exec-7",
				event.MetaPrefix + "phase":     "stopped",
				event.MetaPrefix + "exit_code": "0",
			},
			Owner:        addr,
			Source:       addr,
			Target:       addr,
			InvocationID: "inv-fidelity",
			TaskID:       "task-fidelity",
			EpisodeID:    "episode-fidelity",
			DecisionID:   "decision-fidelity",
		},
	}
}

// factFidelityRestoreKeepsFields commits every field-carrying fact, closes the
// storage, assembles a second runtime over the same root and reads the facts
// back: the restored fact equals the committed one field for field, so a
// restarted process holds the invocation, task, decision and episode lineage,
// the execution audit meta and the pure tool calls the fact committed with.
func factFidelityRestoreKeepsFields(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	run := factFidelityAssemble(t, root, factFidelityProvider())

	events := factFidelityEvents(factFidelityAddress)
	committed := make(map[event.EventID]event.Event, len(events))
	for _, evt := range events {
		key, err := run.CommitToSession(ctx, factFidelityAddress, evt)
		if err != nil {
			t.Fatalf("commit the fact %s: %v", evt.Key, err)
		}
		if key != evt.Key {
			t.Fatalf("commit of %s answered %s, want the identity the caller stated", evt.Key, key)
		}
		live, err := run.CommittedEvent(ctx, key)
		if err != nil {
			t.Fatalf("read the live fact %s: %v", key, err)
		}
		committed[key] = live
	}

	factFidelityClose(t, run)
	restored := factFidelityAssemble(t, root, factFidelityProvider())
	defer factFidelityClose(t, restored)

	for _, evt := range events {
		got, err := restored.CommittedEvent(ctx, evt.Key)
		if err != nil {
			t.Fatalf("read the restored fact %s: %v", evt.Key, err)
		}
		factFidelityRequireSameEvent(t, committed[evt.Key], got)
	}
}

// factFidelityRepeatIsIdempotent commits one fact and re-sends its identity
// twice — once byte identical and once carrying fresh commit stamps — and
// requires the original identity back beside a single record keeping the
// original content: one identity stands for one fact, whatever the commit
// path stamps beside it.
func factFidelityRepeatIsIdempotent(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	run := factFidelityAssemble(t, root, factFidelityProvider())
	defer factFidelityClose(t, run)

	original := event.Event{
		Type:      event.TypeExternalInput,
		Timestamp: time.UnixMilli(1760000000123),
		Meta:      map[string]string{event.MetaKeyEventSummary: "the repeated input"},
		Message:   model.Message{Role: model.RoleUser, Content: "the repeated input"},
	}
	key, err := run.CommitToSession(ctx, factFidelityAddress, original)
	if err != nil {
		t.Fatalf("commit the fact: %v", err)
	}
	if key == "" {
		t.Fatalf("the commit minted no identity")
	}

	exact := original
	exact.Key = key
	if again, err := run.CommitToSession(ctx, factFidelityAddress, exact); err != nil || again != key {
		t.Fatalf("the exact repeat of %s answered (%s, %v), want the original identity and no error", key, again, err)
	}

	resent := original
	resent.Key = key
	resent.Timestamp = time.UnixMilli(1760000000456)
	if again, err := run.CommitToSession(ctx, factFidelityAddress, resent); err != nil || again != key {
		t.Fatalf("the re-sent identity %s answered (%s, %v), want the original identity and no error", key, again, err)
	}

	if held := factFidelityCountKey(run, key); held != 1 {
		t.Fatalf("the chain holds %d records of %s, want one record for one identity", held, key)
	}
	stored, err := run.CommittedEvent(ctx, key)
	if err != nil {
		t.Fatalf("read the committed fact %s: %v", key, err)
	}
	if got := stored.Timestamp.UnixMilli(); got != original.Timestamp.UnixMilli() {
		t.Fatalf("the record of %s kept the stamp %d, want the original record time %d", key, got, original.Timestamp.UnixMilli())
	}
	if stored.Message.Content != original.Message.Content {
		t.Fatalf("the record of %s kept %q, want the original content %q", key, stored.Message.Content, original.Message.Content)
	}
}

// factFidelityConflictRefused commits one fact and re-sends its identity
// carrying other content: the second commit is refused by name, writes nothing
// and leaves the record and the live ledger exactly as the first commit left
// them, so two contents never both succeed under one identity.
func factFidelityConflictRefused(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	run := factFidelityAssemble(t, root, factFidelityProvider())

	original := event.Event{
		Key:       "fact-fidelity-conflict",
		Type:      event.TypeExternalInput,
		Timestamp: time.UnixMilli(1760000000123),
		Meta:      map[string]string{event.MetaKeyEventSummary: "the original input"},
		Message:   model.Message{Role: model.RoleUser, Content: "the original input"},
	}
	if _, err := run.CommitToSession(ctx, factFidelityAddress, original); err != nil {
		t.Fatalf("commit the fact %s: %v", original.Key, err)
	}

	changed := original
	changed.Message = model.Message{Role: model.RoleUser, Content: "the changed input"}
	refused, err := run.CommitToSession(ctx, factFidelityAddress, changed)
	if err == nil {
		t.Fatalf("the second content of %s answered success beside %s, want a named conflict", original.Key, refused)
	}
	if !errors.Is(err, memory.ErrFactKeyConflict) {
		t.Fatalf("the second content of %s failed beside %v, want the named key conflict", original.Key, err)
	}
	if refused != "" {
		t.Fatalf("the refused commit of %s answered the identity %s, want no identity beside a refusal", original.Key, refused)
	}

	if held := factFidelityCountKey(run, original.Key); held != 1 {
		t.Fatalf("the chain holds %d records of %s after the refused commit, want one", held, original.Key)
	}
	live, err := run.CommittedEvent(ctx, original.Key)
	if err != nil {
		t.Fatalf("read the live fact %s: %v", original.Key, err)
	}
	if live.Message.Content != original.Message.Content {
		t.Fatalf("the refused commit moved the live fact %s to %q, want the original %q", original.Key, live.Message.Content, original.Message.Content)
	}

	factFidelityClose(t, run)
	restored := factFidelityAssemble(t, root, factFidelityProvider())
	defer factFidelityClose(t, restored)
	stored, err := restored.CommittedEvent(ctx, original.Key)
	if err != nil {
		t.Fatalf("read the restored fact %s: %v", original.Key, err)
	}
	if stored.Message.Content != original.Message.Content {
		t.Fatalf("the refused commit moved the stored fact %s to %q, want the original %q", original.Key, stored.Message.Content, original.Message.Content)
	}
}

// factFidelityUndeclaredStampIsIdempotent commits one fact declaring a source
// and a task of its own, then re-sends its identity with both stamps left
// unstated: the unstated stamps backfill to the commit default and judge as the
// same fact beside the declared ones, so the repeat answers the original
// identity beside one record keeping the stamps the first commit declared.
func factFidelityUndeclaredStampIsIdempotent(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	run := factFidelityAssemble(t, root, factFidelityProvider())
	defer factFidelityClose(t, run)

	declared := event.Address{AgentID: "agent-declared", SessionID: "session-declared-stamp"}
	original := event.Event{
		Key:       "fact-fidelity-declared-stamp",
		Type:      event.TypeExternalInput,
		Timestamp: time.UnixMilli(1760000000123),
		Meta:      map[string]string{event.MetaKeyEventSummary: "the declared-stamp input"},
		Message:   model.Message{Role: model.RoleUser, Content: "the declared-stamp input"},
		Source:    declared,
		TaskID:    "task-declared",
	}
	key, err := run.CommitToSession(ctx, factFidelityAddress, original)
	if err != nil {
		t.Fatalf("commit the fact declaring its own source: %v", err)
	}
	live, err := run.CommittedEvent(ctx, key)
	if err != nil {
		t.Fatalf("read the live fact %s: %v", key, err)
	}
	if live.Source != declared {
		t.Fatalf("the commit stamped the source %v over the caller's %v, want the declared stamp kept", live.Source, declared)
	}
	if live.TaskID != "task-declared" {
		t.Fatalf("the commit stamped the task %q over the caller's task-declared, want the declared stamp kept", live.TaskID)
	}

	repeat := original
	repeat.Key = key
	repeat.Source = event.Address{}
	repeat.TaskID = ""
	again, err := run.CommitToSession(ctx, factFidelityAddress, repeat)
	if err != nil || again != key {
		t.Fatalf("the unstated-stamp repeat of %s answered (%s, %v), want the original identity and no error", key, again, err)
	}
	if held := factFidelityCountKey(run, key); held != 1 {
		t.Fatalf("the chain holds %d records of %s, want one record for one identity", held, key)
	}
	stored, err := run.CommittedEvent(ctx, key)
	if err != nil {
		t.Fatalf("read the committed fact %s: %v", key, err)
	}
	if stored.Source != declared || stored.TaskID != "task-declared" {
		t.Fatalf("the unstated repeat moved the stamps of %s to (%v, %q), want the declared (%v, task-declared)", key, stored.Source, stored.TaskID, declared)
	}
}

// factFidelityStampOverwriteRefused commits one fact declaring a source and a
// task of its own, then re-sends its identity overwriting that source with
// another range, and again overwriting the task: each overwrite is refused
// beside the named key conflict and changes neither the record nor the live
// ledger, so a stamp the first commit declared is never overwritten under the
// same identity.
func factFidelityStampOverwriteRefused(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	run := factFidelityAssemble(t, root, factFidelityProvider())
	defer factFidelityClose(t, run)

	declared := event.Address{AgentID: "agent-declared", SessionID: "session-stamp-overwrite"}
	original := event.Event{
		Key:       "fact-fidelity-stamp-overwrite",
		Type:      event.TypeExternalInput,
		Timestamp: time.UnixMilli(1760000000123),
		Meta:      map[string]string{event.MetaKeyEventSummary: "the stamp-overwrite input"},
		Message:   model.Message{Role: model.RoleUser, Content: "the stamp-overwrite input"},
		Source:    declared,
		TaskID:    "task-declared",
	}
	if _, err := run.CommitToSession(ctx, factFidelityAddress, original); err != nil {
		t.Fatalf("commit the fact declaring its own source: %v", err)
	}

	overwritten := original
	overwritten.Source = event.Address{AgentID: "agent-other", SessionID: "session-stamp-overwrite"}
	if refused, err := run.CommitToSession(ctx, factFidelityAddress, overwritten); err == nil {
		t.Fatalf("the source overwrite of %s answered success beside %s, want a named conflict", original.Key, refused)
	} else if !errors.Is(err, memory.ErrFactKeyConflict) {
		t.Fatalf("the source overwrite of %s failed beside %v, want the named key conflict", original.Key, err)
	}

	retasked := original
	retasked.TaskID = "task-other"
	if refused, err := run.CommitToSession(ctx, factFidelityAddress, retasked); err == nil {
		t.Fatalf("the task overwrite of %s answered success beside %s, want a named conflict", original.Key, refused)
	} else if !errors.Is(err, memory.ErrFactKeyConflict) {
		t.Fatalf("the task overwrite of %s failed beside %v, want the named key conflict", original.Key, err)
	}

	if held := factFidelityCountKey(run, original.Key); held != 1 {
		t.Fatalf("the chain holds %d records of %s after the refused overwrites, want one", held, original.Key)
	}
	stored, err := run.CommittedEvent(ctx, original.Key)
	if err != nil {
		t.Fatalf("read the committed fact %s: %v", original.Key, err)
	}
	if stored.Source != declared || stored.TaskID != "task-declared" {
		t.Fatalf("the refused overwrites moved the stamps of %s to (%v, %q), want the declared (%v, task-declared)", original.Key, stored.Source, stored.TaskID, declared)
	}
}

// factFidelityCommitFailureFailsTurn holds the agent's shard at the atomic
// rename barrier from inside the second model call — the input fact of that
// turn already committed, so only its output commit meets the barrier — and
// drives one turn against it: the output fact cannot commit, so the turn
// reports the failure beside a terminal that is not completed and leaves no
// assistant fact behind. The barrier lifts before the retry, so the normal
// path before and after the hold still completes.
func factFidelityCommitFailureFailsTurn(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	var mu sync.Mutex
	calls := 0
	var release func()
	defer func() {
		mu.Lock()
		held := release
		release = nil
		mu.Unlock()
		if held != nil {
			held()
		}
	}()
	provider := &entryModel{name: "model-a", answer: func(*model.Request) *model.Response {
		mu.Lock()
		calls++
		round := calls
		if round == 2 && release == nil {
			release = factFidelityHoldShard(t, root)
		}
		mu.Unlock()
		return entryFinalTurn("answer-of-round-" + strconv.Itoa(round))
	}}
	run := factFidelityAssemble(t, root, provider)
	defer factFidelityClose(t, run)

	warmup, err := run.Invoke(ctx, assembly.InvokeRequest{
		InvocationID: "inv-fidelity-warmup",
		Address:      factFidelityAddress,
		Input:        []model.Message{{Role: model.RoleUser, Content: "the warmup turn lands"}},
	})
	if err != nil {
		t.Fatalf("the warmup turn beside an open shard failed: %v", err)
	}
	if warmup.Terminal != model.KindCompleted {
		t.Fatalf("the warmup turn reached %s, want the completed terminal of the normal path", warmup.Terminal)
	}

	refused, err := run.Invoke(ctx, assembly.InvokeRequest{
		InvocationID: "inv-fidelity-refused",
		Address:      factFidelityAddress,
		Input:        []model.Message{{Role: model.RoleUser, Content: "the turn whose output cannot land"}},
	})
	if err == nil {
		t.Fatalf("the turn beside a held segment barrier returned no failure beside the terminal %s", refused.Terminal)
	}
	if !errors.Is(err, ledger.ErrSegmentBarrier) {
		t.Fatalf("the turn failed beside %v, want the named segment barrier", err)
	}
	if refused.Terminal == model.KindCompleted {
		t.Fatalf("the turn whose output fact never landed reported %s, want a terminal that is not completed", refused.Terminal)
	}
	for _, fact := range run.SessionFacts(factFidelityAddress) {
		if fact.Message.Content == "answer-of-round-2" {
			t.Fatalf("the refused turn left the assistant fact %s behind, want no output beside a failed commit", fact.Key)
		}
	}

	mu.Lock()
	held := release
	release = nil
	mu.Unlock()
	if held != nil {
		held()
	}
	retry, err := run.Invoke(ctx, assembly.InvokeRequest{
		InvocationID: "inv-fidelity-retry",
		Address:      factFidelityAddress,
		Input:        []model.Message{{Role: model.RoleUser, Content: "the retry lands"}},
	})
	if err != nil {
		t.Fatalf("the retry after the release failed: %v", err)
	}
	if retry.Terminal != model.KindCompleted {
		t.Fatalf("the retry reached %s, want the completed terminal of the normal path", retry.Terminal)
	}
}

// factFidelityCountKey reports how many records of the session chain carry the
// identity, so one identity is observed as exactly one record.
func factFidelityCountKey(run *assembly.Run, key event.EventID) int {
	held := 0
	for _, fact := range run.SessionFacts(factFidelityAddress) {
		if fact.Key == key {
			held++
		}
	}
	return held
}

// factFidelityRequireSameEvent requires the restored fact to equal the
// committed one field for field, naming every field that drifted.
func factFidelityRequireSameEvent(t *testing.T, want, got event.Event) {
	t.Helper()
	same := func(what string, diff bool) {
		t.Helper()
		if diff {
			t.Fatalf("the restored fact %s carries %s = %v, want the committed %v", want.Key, what, got, want)
		}
	}
	same("key", got.Key != want.Key)
	same("type", got.Type != want.Type)
	same("timestamp", !got.Timestamp.Equal(want.Timestamp))
	same("meta", !factFidelitySameMeta(want.Meta, got.Meta))
	same("message", !factFidelitySameMessage(want.Message, got.Message))
	same("owner", got.Owner != want.Owner)
	same("source", got.Source != want.Source)
	same("target", got.Target != want.Target)
	same("invocation", got.InvocationID != want.InvocationID)
	same("task", got.TaskID != want.TaskID)
	same("instance", got.InstanceID != want.InstanceID)
	same("episode", got.EpisodeID != want.EpisodeID)
	same("decision", got.DecisionID != want.DecisionID)
	same("attempt", got.AttemptID != want.AttemptID)
	same("call", got.CallID != want.CallID)
	same("approval", got.ApprovalID != want.ApprovalID)
}

// factFidelitySameMeta reports whether the two metadata maps carry the same
// entries.
func factFidelitySameMeta(want, got map[string]string) bool {
	if len(want) != len(got) {
		return false
	}
	for key, value := range want {
		if got[key] != value {
			return false
		}
	}
	return true
}

// factFidelitySameMessage reports whether the two native messages carry the
// same payload, byte for byte where the payload is bytes.
func factFidelitySameMessage(want, got model.Message) bool {
	if want.Role != got.Role || want.Content != got.Content ||
		want.ToolID != got.ToolID || want.ToolName != got.ToolName ||
		want.ReasoningContent != got.ReasoningContent || want.ReasoningSignature != got.ReasoningSignature {
		return false
	}
	if len(want.ContentParts) != len(got.ContentParts) {
		return false
	}
	for at := range want.ContentParts {
		if want.ContentParts[at].Type != got.ContentParts[at].Type {
			return false
		}
		if (want.ContentParts[at].Text == nil) != (got.ContentParts[at].Text == nil) {
			return false
		}
		if want.ContentParts[at].Text != nil && *want.ContentParts[at].Text != *got.ContentParts[at].Text {
			return false
		}
	}
	if len(want.ToolCalls) != len(got.ToolCalls) {
		return false
	}
	for at := range want.ToolCalls {
		call, restored := want.ToolCalls[at], got.ToolCalls[at]
		if call.ID != restored.ID || call.Type != restored.Type ||
			call.Function.Name != restored.Function.Name || string(call.Function.Arguments) != string(restored.Function.Arguments) {
			return false
		}
	}
	return true
}

// factFidelityShardPath reports the shard file the agent's events are committed
// into: the partition its identity resolves to.
func factFidelityShardPath(root string) string {
	partition := memory.PartitionIDFromName(string(factFidelityAddress.AgentID))
	return filepath.Join(root, "kv", url.PathEscape(strconv.Itoa(partition)), "snapshot.json")
}

// factFidelityHoldShard makes every commit into the agent's shard fail at the
// atomic-rename barrier: the snapshot path becomes a directory, so the rename
// cannot land whatever user the process runs as, and the failure is the storage
// barrier the commit path must report instead of answering success.
func factFidelityHoldShard(t *testing.T, root string) func() {
	t.Helper()
	shard := factFidelityShardPath(root)
	held := shard + ".held"
	if _, err := os.Stat(shard); err != nil {
		t.Fatalf("the durable shard %s is missing: the earlier turn committed nothing into the storage tier: %v", shard, err)
	}
	if err := os.Rename(shard, held); err != nil {
		t.Fatalf("hold the shard snapshot %s: %v", shard, err)
	}
	if err := os.MkdirAll(shard, 0o755); err != nil {
		t.Fatalf("hold the shard snapshot %s: %v", shard, err)
	}
	return func() {
		t.Helper()
		if err := os.RemoveAll(shard); err != nil {
			t.Fatalf("release the shard snapshot %s: %v", shard, err)
		}
		if err := os.Rename(held, shard); err != nil {
			t.Fatalf("release the shard snapshot %s: %v", shard, err)
		}
	}
}
