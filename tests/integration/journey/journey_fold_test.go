// J5 is the compression journey: a session declared with a small model window
// folds its projection once the replayed volume passes the window's threshold,
// and the material the fold consumed stays reachable — the newest band verbatim,
// the middle band as one product, and the oldest band as one bounded summary
// whose covered keys the chain still answers.
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

// compressCoveredMeta and compressKeyMeta are the metadata keys a compress
// product fact states its coverage and its product key under, as the durable
// chain spells them.
const (
	compressCoveredMeta = "compress_covered"
	compressKeyMeta     = "compress_key"
)

// TestJourneyFoldKeepsContextReadable drives a long session under a few-dozen-byte fold threshold:
// - three turns stay inside the kept window and the chain carries no product
// - the third turn folds the default window and mints a history summary under a synthetic key, and that summary stands for the early turn
// - the fourth turn folds the standing summary again: the new summary names the product covering it instead of re-carrying its text
// - every key the new summary covers answers on the chain
// - the fourth turn's request presents that summary, so the compressed early context reaches the model
// - the provider window is wider than the 64-token replay window, so the gate admits every turn
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
		MaxTokens:             64,
		ProviderContextWindow: 256,
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

	summary := waitForFoldSummary(t, host, session, string(first.Key), first.Key)
	if summary.Key == first.Key {
		t.Fatalf("history summary key = %q, want the second fold replacing the first", summary.Key)
	}
	if strings.Contains(summary.Content, early) {
		t.Fatalf("history summary content = %q, want the early turn referenced by key rather than re-carried: a summary carries no copy of the text it stands for", summary.Content)
	}
	covered := journeyCoveredKeysOf(t, host, session, summary.Key)
	if !containsEventID(covered, first.Key) {
		t.Fatalf("the summary %s covers %v, want the standing summary %s it replaced", summary.Key, covered, first.Key)
	}
	for _, key := range append(covered, journeyCoveredKeysOf(t, host, session, first.Key)...) {
		if !journeyAnswersKey(ctx, host, session, key) {
			t.Fatalf("the covered key %s answers no fact of the chain and no product that carries its text", key)
		}
	}

	requests := journeyDecisionRequests(provider, journeyEntryModel)
	if len(requests) < 4 {
		t.Fatalf("entry decision requests = %d, want the four turns", len(requests))
	}
	text := requests[3].text()
	if !strings.Contains(text, "history summary") {
		t.Fatalf("fourth request text = %q, want the history summary the fold minted", text)
	}
	if !strings.Contains(text, summary.Content) {
		t.Fatalf("fourth request text = %q, want the summary %q the chain records", text, summary.Content)
	}
}

// journeyDecisionRequests reports the requests one model name was asked as
// decisions: the band summaries the fold asks that same model for are not
// decisions, so a journey counts the turns it declared.
func journeyDecisionRequests(provider *journeyProvider, modelName string) []journeyRequest {
	out := make([]journeyRequest, 0, 4)
	for _, request := range provider.requestsFor(modelName) {
		if _, summary := bandSummaryGist(request); summary {
			continue
		}
		out = append(out, request)
	}
	return out
}

// journeyCoveredKeysOf reports the entry keys one recorded summary product
// covers, in the order the chain states them.
func journeyCoveredKeysOf(t *testing.T, host *journeyHost, session string, key event.EventID) []event.EventID {
	t.Helper()
	for _, fact := range host.sessionFacts(session) {
		if fact.Type != event.CompressProductType || fact.Meta[compressKeyMeta] != string(key) {
			continue
		}
		raw := fact.Meta[compressCoveredMeta]
		if raw == "" {
			return nil
		}
		parts := strings.Split(raw, "\n")
		out := make([]event.EventID, 0, len(parts))
		for _, part := range parts {
			if part == "" {
				continue
			}
			out = append(out, event.EventID(part))
		}
		return out
	}
	t.Fatalf("the chain carries no compress product fact for %s", key)
	return nil
}

// journeyAnswersKey reports whether one covered key is still answerable on the
// chain: an entry the chain committed answers its own fact — from the resident
// window or from the durable tier the window released it to — and a product of
// an earlier fold answers the compress product fact that states its text.
func journeyAnswersKey(ctx context.Context, host *journeyHost, session string, key event.EventID) bool {
	if _, err := host.host.Run().CommittedEvent(ctx, key); err == nil {
		return true
	}
	for _, fact := range host.sessionFacts(session) {
		if fact.Type == event.CompressProductType && fact.Meta[compressKeyMeta] == string(key) {
			return true
		}
	}
	return false
}

// containsEventID reports whether one key sequence carries the given key.
func containsEventID(keys []event.EventID, want event.EventID) bool {
	for _, key := range keys {
		if key == want {
			return true
		}
	}
	return false
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
