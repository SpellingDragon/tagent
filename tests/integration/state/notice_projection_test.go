package state

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/memory/replay"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// noticeChainKey is the session chain the notice facts of this check are filed
// under: the chain a crash rebuild replays instead of the lanes of the process
// that committed them.
var noticeChainKey = replay.ChainKey{PartitionID: 3, SessionID: event.SessionID("session-notice")}

// noticeTextOf renders the input material one progress notification reads as,
// spelled exactly as the loop spells it into the notice fact.
func noticeTextOf(notice task.Notice) string {
	return fmt.Sprintf("tagent: task %s reports progress in round %d", notice.TaskID, notice.Round)
}

// noticeFactOf builds the chain fact one progress notification commits as: the
// stable identity the loop mints for the notification beside the feedback type,
// the summary the commit stores beside it and the input material it reads as.
func noticeFactOf(invocationID event.InvocationID, notice task.Notice, at time.Time) replay.ChainFact {
	return replay.ChainFact{
		Key:     agent.NoticeFactKey(invocationID, notice),
		Chain:   noticeChainKey,
		Type:    event.TypeFeedback,
		Summary: string(notice.Kind),
		Content: noticeTextOf(notice),
		Time:    at,
	}
}

// TestNoticeFactsSurviveTheChainRebuild pins the crash rebuild of the notice facts a range admitted, through the chain alone.
// - a notice fact committed under the stable identity of its notification rebuilds into the projection exactly once, with its text intact
// - a retried commit of one notification appends no second fact, and another content under that identity is refused by name
// - a rebuild offered to a non-empty projection is skipped and never replaces the notices already replayed
// - the identity prefix of one range selects its own notices alone, so a nested range name joins no neighbour's report
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestNoticeFactsSurviveTheChainRebuild(t *testing.T) {
	ctx := context.Background()
	first := task.Notice{TaskID: "task-notice-1", Round: 2, Kind: task.NoticeProgress, Seq: 0, Event: "fact-notice-1"}
	second := task.Notice{TaskID: "task-notice-2", Round: 1, Kind: task.NoticeProgress, Seq: 0, Event: "fact-notice-2"}

	t.Run("a notice fact rebuilds into the projection exactly once", func(t *testing.T) {
		chain := replay.NewMemoryEventChain()
		facts := []replay.ChainFact{
			noticeFactOf("invocation-1", first, time.Unix(1, 0)),
			{Key: event.EventID("0000-input"), Chain: noticeChainKey, Type: event.TypeExternalInput, Content: "hello", Time: time.Unix(2, 0)},
			noticeFactOf("invocation-1", second, time.Unix(3, 0)),
		}
		for _, fact := range facts {
			if err := chain.Commit(ctx, fact); err != nil {
				t.Fatalf("commit the notice fact %s: %v", fact.Key, err)
			}
		}
		projection := replay.NewReplayProjection()
		report, err := replay.RebuildProjectionFromWAL(ctx, projection, chain, replay.NewMemoryRelations(), noticeChainKey, replay.RebuildOptions{})
		if err != nil || !report.Scanned || report.Entries != len(facts) || report.Dropped != 0 {
			t.Fatalf("rebuild = (%+v, %v), want the three chain facts replayed whole", report, err)
		}
		prefix := agent.NoticeFactPrefix("invocation-1")
		var replayed []replay.ProjectionEntry
		for _, entry := range projection.Entries() {
			if strings.HasPrefix(string(entry.Key), string(prefix)) {
				replayed = append(replayed, entry)
			}
		}
		if len(replayed) != 2 {
			t.Fatalf("replayed notices = %d, want the two committed reports and no duplicate", len(replayed))
		}
		for at, want := range []task.Notice{first, second} {
			key := agent.NoticeFactKey("invocation-1", want)
			if replayed[at].Key != key || replayed[at].Content != noticeTextOf(want) {
				t.Fatalf("replayed notice %d = %+v, want the committed report back as it stood", at, replayed[at])
			}
			if replayed[at].Type != event.TypeFeedback || replayed[at].Summary != string(want.Kind) {
				t.Fatalf("replayed notice %d = %+v, want the feedback type beside the stored summary", at, replayed[at])
			}
		}
		presented, err := projection.AssembleBudget(ctx, 1<<16)
		if err != nil {
			t.Fatalf("assemble the rebuilt projection: %v", err)
		}
		carried := false
		for _, entry := range presented {
			if entry.Key == agent.NoticeFactKey("invocation-1", first) {
				carried = entry.Content == noticeTextOf(first)
			}
		}
		if !carried {
			t.Fatalf("assembled entries = %+v, want the replayed notice carried into the request material", presented)
		}
		role, err := agent.RoleOf(event.TypeFeedback)
		if err != nil || role != "user" {
			t.Fatalf("role of the feedback type = (%q, %v), want the notice read as user input material", role, err)
		}
	})

	t.Run("a retried commit appends no second notice fact", func(t *testing.T) {
		chain := replay.NewMemoryEventChain()
		fact := noticeFactOf("invocation-1", first, time.Unix(1, 0))
		for attempt := 1; attempt <= 2; attempt++ {
			if err := chain.Commit(ctx, fact); err != nil {
				t.Fatalf("commit the same notice fact, attempt %d: %v", attempt, err)
			}
		}
		batch, err := chain.ScanFrom(ctx, "", 10)
		if err != nil || len(batch) != 1 {
			t.Fatalf("chain facts after the retried commit = (%d, %v), want the one notice fact", len(batch), err)
		}
		conflicted := fact
		conflicted.Content = "another report under the same identity"
		if err := chain.Commit(ctx, conflicted); !errors.Is(err, ledger.ErrContentConflict) {
			t.Fatalf("commit another content under the notice identity = %v, want the conflict refused by name", err)
		}
		projection := replay.NewReplayProjection()
		if _, err := replay.RebuildProjectionFromWAL(ctx, projection, chain, replay.NewMemoryRelations(), noticeChainKey, replay.RebuildOptions{}); err != nil {
			t.Fatalf("rebuild the retried notice: %v", err)
		}
		if projection.Len() != 1 || projection.Entries()[0].Key != fact.Key {
			t.Fatalf("rebuilt projection = %+v, want the one notice fact", projection.Entries())
		}
	})

	t.Run("a rebuild never replaces the replayed notices", func(t *testing.T) {
		chain := replay.NewMemoryEventChain()
		fact := noticeFactOf("invocation-1", first, time.Unix(1, 0))
		if err := chain.Commit(ctx, fact); err != nil {
			t.Fatalf("commit the notice fact: %v", err)
		}
		projection := replay.NewReplayProjection()
		if _, err := replay.RebuildProjectionFromWAL(ctx, projection, chain, replay.NewMemoryRelations(), noticeChainKey, replay.RebuildOptions{}); err != nil {
			t.Fatalf("rebuild the notice: %v", err)
		}
		again, err := replay.RebuildProjectionFromWAL(ctx, projection, chain, replay.NewMemoryRelations(), noticeChainKey, replay.RebuildOptions{})
		if err != nil || again.Skipped != replay.RebuildSkipNotEmpty {
			t.Fatalf("second rebuild = (%+v, %v), want the non-empty projection skipped by name", again, err)
		}
		if projection.Len() != 1 || projection.Entries()[0].Key != fact.Key {
			t.Fatalf("projection after the second rebuild = %+v, want the replayed notice untouched", projection.Entries())
		}
	})

	t.Run("the identity prefix of one range selects its own notices alone", func(t *testing.T) {
		nested := event.InvocationID("invocation-10")
		key := agent.NoticeFactKey(nested, first)
		if strings.HasPrefix(string(key), string(agent.NoticeFactPrefix("invocation-1"))) {
			t.Fatalf("notice key %q of range %q carries the prefix of range invocation-1, want the ranges kept apart", key, nested)
		}
		if !strings.HasPrefix(string(key), string(agent.NoticeFactPrefix(nested))) {
			t.Fatalf("notice key %q does not carry the prefix of its own range", key)
		}
		if retry := agent.NoticeFactKey(nested, first); retry != key {
			t.Fatalf("notice identity of a retried delivery = %q, want the stable identity %q", retry, key)
		}
	})
}
