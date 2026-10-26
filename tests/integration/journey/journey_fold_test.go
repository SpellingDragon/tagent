// J5 is the compression journey: a session declared with a small model window
// folds its projection once the replayed volume passes the window's threshold,
// and the material the fold consumed still reaches the model in readable form.
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
package journey

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory/replay"
)

// TestJourneyFoldKeepsContextReadable drives a long session under a few-dozen-byte fold threshold:
// - three turns stay inside the kept window and the chain carries no product
// - the fourth turn folds the default window and mints a history summary under a synthetic key
// - that fold reads the standing summary again, and the early turn still reads in the new one
// - the fourth turn's request presents the summary, so the early context stays readable
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestJourneyFoldKeepsContextReadable(t *testing.T) {
	const (
		session = "session-journey-fold"
		early   = "EARLY-MARKER: the first user turn of the long session"
	)
	provider := newJourneyProvider(t)
	provider.scriptAnswers(journeyEntryModel,
		"the first answer of the long session",
		"the second answer of the long session",
		"the third answer of the long session",
		"the fourth answer of the long session",
	)
	host := bootJourney(t, provider, journeySpec{
		MaxTokens: 64,
		EntryExtra: []string{
			"    compress_threshold: 0.2",
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	host.invokeOK(t, ctx, session, early)
	host.invokeOK(t, ctx, session, "the second user turn")
	if entries, carried := host.host.Run().ReplayEntries(event.SessionID(session)); carried {
		for _, entry := range entries {
			if entry.Type == replay.ProjectionTypeHistorySummary {
				t.Fatalf("replay entries = %+v before the middle band opened, want no product yet", entries)
			}
		}
	}

	host.invokeOK(t, ctx, session, "the third user turn")
	first := waitForFoldSummary(t, host, session, early, "")
	if !replay.SyntheticKey(first.Key) {
		t.Fatalf("history summary key = %q, want a synthetic key", first.Key)
	}
	if !strings.HasPrefix(string(first.Key), "-hist-") {
		t.Fatalf("history summary key = %q, want the -hist- product identity", first.Key)
	}
	if !strings.Contains(first.Content, early) {
		t.Fatalf("history summary content = %q, want the early turn it folded", first.Content)
	}

	host.invokeOK(t, ctx, session, "the fourth user turn")

	summary := waitForFoldSummary(t, host, session, "external_input: "+early, first.Key)
	if summary.Key == first.Key {
		t.Fatalf("history summary key = %q, want the second fold replacing the first", summary.Key)
	}
	if !strings.Contains(summary.Content, early) {
		t.Fatalf("history summary content = %q, want the early turn surviving the second fold", summary.Content)
	}

	requests := provider.requestsFor(journeyEntryModel)
	if len(requests) < 4 {
		t.Fatalf("entry requests = %d, want the four turns", len(requests))
	}
	text := requests[3].text()
	if !strings.Contains(text, "history summary") {
		t.Fatalf("fourth request text = %q, want the history summary the fold minted", text)
	}
	if !strings.Contains(text, early) {
		t.Fatalf("fourth request text = %q, want the early turn still readable", text)
	}
}

// waitForFoldSummary waits until the session projection carries a history
// summary whose content holds want and whose key is not the product named by
// notKey, and reports it. The fold runs beside the commit that grew the
// projection, so the wait observes the product instead of assuming when the
// commit's observation ran.
func waitForFoldSummary(t *testing.T, host *journeyHost, session, want string, notKey event.EventID) replay.ProjectionEntry {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		entries, carried := host.host.Run().ReplayEntries(event.SessionID(session))
		if carried {
			for _, entry := range entries {
				if entry.Type == replay.ProjectionTypeHistorySummary && entry.Key != notKey && strings.Contains(entry.Content, want) {
					return entry
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("replay entries = %+v after 30s, want a history summary carrying %q", entries, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
