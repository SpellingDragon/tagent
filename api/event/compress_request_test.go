package event_test

import (
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
)

// TestCompressRequestIsAFact pins the compress request family's identity: who judged, why and what.
// - a request answers its content-addressed identity as its fact key and the frozen compress.request type
// - the identity is derived from producer, subject and reason, so a different judgment answers a different key
// - one judgment answers one key however often it is re-judged
//
// 契约: docs/FACT-VOCABULARY.md#families
func TestCompressRequestIsAFact(t *testing.T) {
	t.Run("a request answers its identity and its frozen type", func(t *testing.T) {
		request := event.CompressRequestFor("memory.curation", "seq-1", "the card sequence exceeds the curation bound")
		var fact event.Fact = request
		if got := fact.FactType(); got != "compress.request" {
			t.Fatalf("FactType() = %q, want the frozen compress.request type", got)
		}
		if got, want := fact.FactKey(), string(request.ID); got != want {
			t.Fatalf("FactKey() = %q, want the request identity %q", got, want)
		}
		if request.ID == "" {
			t.Fatal("the judgment carries no identity, want a content-addressed key")
		}
		if request.Producer != "memory.curation" || request.Reason != "the card sequence exceeds the curation bound" || request.Subject != "seq-1" {
			t.Fatalf("request = %+v, want the producer, the reason and the judged subject", request)
		}
	})

	t.Run("the identity follows the judgment it names", func(t *testing.T) {
		base := event.CompressRequestFor("memory.curation", "seq-1", "the card sequence exceeds the curation bound")
		if repeat := event.CompressRequestFor("memory.curation", "seq-1", "the card sequence exceeds the curation bound"); repeat.ID != base.ID {
			t.Fatalf("identities of one judgment = (%q, %q), want one key", base.ID, repeat.ID)
		}
		for name, other := range map[string]event.CompressRequest{
			"another producer": event.CompressRequestFor("workspace.tool-overflow", "seq-1", "the card sequence exceeds the curation bound"),
			"another subject":  event.CompressRequestFor("memory.curation", "seq-2", "the card sequence exceeds the curation bound"),
			"another reason":   event.CompressRequestFor("memory.curation", "seq-1", "another ground"),
		} {
			if other.ID == base.ID {
				t.Fatalf("%s answers the identity of the base judgment %q, want its own key", name, base.ID)
			}
		}
	})
}
