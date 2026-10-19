package event_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/config"
	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/rl"
	"github.com/SpellingDragon/tagent/task"
)

// TestFactCategoryContract pins the fact category contract every registered family answers.
// - each family satisfies event.Fact and answers both questions with a stated value
// - one fact answers the same key every time it is asked
// - facts that differ in identity never share a key
// - a notice pointing at another fact keeps the identity its four-tuple states
//
// 契约: docs/FACT-VOCABULARY.md#fact-category
func TestFactCategoryContract(t *testing.T) {
	notice := task.Notice{TaskID: "task-1", Round: 1, Kind: task.NoticeSettled, Seq: 2}
	facts := []event.Fact{
		event.Event{Key: "evt-1", Type: event.TypeAgentOutput},
		notice,
		rl.AttemptRecord{ID: "attempt-1"},
		rl.FeedbackRecord{FeedbackID: "feedback-1"},
		rl.RewardRecord{InstanceID: "reward-1", Revision: 1},
		config.Declaration{Entry: "main", Baseline: "v1"},
		ledger.OriginalRef{Key: "blob-1", Owner: event.Address{AgentID: "agent-1"}, Format: "text/plain"},
	}

	seen := map[string]string{}
	for _, fact := range facts {
		key, kind := fact.FactKey(), fact.FactType()
		if key == "" || kind == "" {
			t.Fatalf("%T answers key %q and type %q, want both stated", fact, key, kind)
		}
		if again := fact.FactKey(); again != key {
			t.Fatalf("%T answers key %q then %q, want one stable identity", fact, key, again)
		}
		if holder, taken := seen[key]; taken {
			t.Fatalf("%T and %s both answer key %q, want distinct identities", fact, holder, key)
		}
		seen[key] = fmt.Sprintf("%T", fact)
	}

	pointed := notice
	pointed.Event = "evt-2"
	if pointed.FactKey() != notice.FactKey() {
		t.Fatalf("a notice pointing at another fact answers key %q, want the four-tuple key %q",
			pointed.FactKey(), notice.FactKey())
	}
}

// TestFactCategorySeals pins the identity seal of every registered family.
// - a fact restated on a non-identity field answers the same key and type
// - a fact re-identified answers a different key
// - every registered family answers the type word the vocabulary registers
//
// 契约: docs/FACT-VOCABULARY.md#families
func TestFactCategorySeals(t *testing.T) {
	stamp := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	judged := event.CompressRequestFor("memory.curation", "seq-1", "the card sequence exceeds the curation bound")
	restamped := judged
	restamped.At = stamp
	cases := []struct {
		family       string
		fact         event.Fact
		restated     event.Fact
		reidentified event.Fact
		kind         string
	}{
		{
			family:       "event",
			fact:         event.Event{Key: "evt-1", Type: event.TypeAgentOutput, Timestamp: stamp},
			restated:     event.Event{Key: "evt-1", Type: event.TypeAgentOutput, Timestamp: stamp.Add(time.Hour)},
			reidentified: event.Event{Key: "evt-2", Type: event.TypeAgentOutput, Timestamp: stamp},
			kind:         event.TypeAgentOutput,
		},
		{
			family:       "notice",
			fact:         task.Notice{TaskID: "task-1", Round: 1, Kind: task.NoticeSettled, Seq: 2},
			restated:     task.Notice{TaskID: "task-1", Round: 1, Kind: task.NoticeSettled, Seq: 2, Event: "evt-9"},
			reidentified: task.Notice{TaskID: "task-1", Round: 1, Kind: task.NoticeSettled, Seq: 3},
			kind:         string(task.NoticeSettled),
		},
		{
			family:       "attempt",
			fact:         rl.AttemptRecord{ID: "attempt-1", DecisionID: "decision-1"},
			restated:     rl.AttemptRecord{ID: "attempt-1", DecisionID: "decision-2"},
			reidentified: rl.AttemptRecord{ID: "attempt-2", DecisionID: "decision-1"},
			kind:         "rl.attempt",
		},
		{
			family:       "feedback",
			fact:         rl.FeedbackRecord{FeedbackID: "feedback-1", EventKey: "evt-1"},
			restated:     rl.FeedbackRecord{FeedbackID: "feedback-1", EventKey: "evt-2"},
			reidentified: rl.FeedbackRecord{FeedbackID: "feedback-2", EventKey: "evt-1"},
			kind:         "rl.feedback",
		},
		{
			family:       "reward",
			fact:         rl.RewardRecord{InstanceID: "reward-1", Revision: 1},
			restated:     rl.RewardRecord{InstanceID: "reward-1", Revision: 2},
			reidentified: rl.RewardRecord{InstanceID: "reward-2", Revision: 1},
			kind:         "rl.reward",
		},
		{
			family:       "declaration",
			fact:         config.Declaration{Entry: "main", Baseline: "v1"},
			restated:     config.Declaration{Entry: "main", Baseline: "v1", Raw: map[string]any{"source": "restated"}},
			reidentified: config.Declaration{Entry: "main", Baseline: "v2"},
			kind:         "config.declaration",
		},
		{
			family:       "reference",
			fact:         ledger.OriginalRef{Key: "blob-1", Owner: event.Address{AgentID: "agent-1"}, Format: "text/plain"},
			restated:     ledger.OriginalRef{Key: "blob-1", Owner: event.Address{AgentID: "agent-2"}, Format: "text/markdown"},
			reidentified: ledger.OriginalRef{Key: "blob-2", Owner: event.Address{AgentID: "agent-1"}, Format: "text/plain"},
			kind:         "memory.original",
		},
		{
			family:       "compress request",
			fact:         judged,
			restated:     restamped,
			reidentified: event.CompressRequestFor("memory.curation", "seq-2", "the card sequence exceeds the curation bound"),
			kind:         "compress.request",
		},
	}

	for _, c := range cases {
		key := c.fact.FactKey()
		if got := c.fact.FactType(); got != c.kind {
			t.Fatalf("%s answers type %q, want the registered word %q", c.family, got, c.kind)
		}
		if key == "" {
			t.Fatalf("%s answers no identity", c.family)
		}
		if c.restated.FactKey() != key || c.restated.FactType() != c.kind {
			t.Fatalf("%s restated on a non-identity field answers key %q and type %q, want %q and %q",
				c.family, c.restated.FactKey(), c.restated.FactType(), key, c.kind)
		}
		if c.reidentified.FactKey() == key {
			t.Fatalf("%s re-identified still answers key %q", c.family, key)
		}
	}
}
