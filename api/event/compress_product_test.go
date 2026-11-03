package event_test

import (
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
)

// TestCompressProductIsAFact pins the product family's identity: which request it answers and the key it carries (its prefix states the kind).
// - a product answers its content-addressed identity as its fact key and the frozen compress.product type
// - the identity is derived from the request and the key, so one request answers one product
// - the covered entries and the text travel with the fact
//
// 契约: docs/FACT-VOCABULARY.md#families
func TestCompressProductIsAFact(t *testing.T) {
	t.Run("a product answers its identity and its frozen type", func(t *testing.T) {
		covered := []event.EventID{"evt-a", "evt-b"}
		product := event.CompressProductFor("cmp-1", "assembly.age-fold", "-hist-1", covered, "history summary: a; b")
		var fact event.Fact = product
		if got := fact.FactType(); got != "compress.product" {
			t.Fatalf("FactType() = %q, want the frozen compress.product type", got)
		}
		if got, want := fact.FactKey(), string(product.ID); got != want {
			t.Fatalf("FactKey() = %q, want the product identity %q", got, want)
		}
		if product.Answers.Request != "cmp-1" || product.Answers.Producer != "assembly.age-fold" || product.Key != "-hist-1" {
			t.Fatalf("product = %+v, want the answered request, the key and the kind", product)
		}
		if len(product.Covered) != 2 || product.Covered[0] != "evt-a" || product.Covered[1] != "evt-b" {
			t.Fatalf("covered = %v, want the covered entries in their original order", product.Covered)
		}
		if product.Text != "history summary: a; b" {
			t.Fatalf("text = %q, want the product text", product.Text)
		}
	})

	t.Run("one request answers one product identity", func(t *testing.T) {
		base := event.CompressProductFor("cmp-1", "assembly.age-fold", "-hist-1", nil, "t")
		if repeat := event.CompressProductFor("cmp-1", "assembly.age-fold", "-hist-1", nil, "t"); repeat.ID != base.ID {
			t.Fatalf("identities of one execution = (%q, %q), want one key", base.ID, repeat.ID)
		}
		for name, other := range map[string]event.CompressProduct{
			"another request":  event.CompressProductFor("cmp-2", "assembly.age-fold", "-hist-1", nil, "t"),
			"another key":      event.CompressProductFor("cmp-1", "assembly.age-fold", "-hist-2", nil, "t"),
			"another producer": event.CompressProductFor("cmp-1", "workspace.tool-overflow", "-hist-1", nil, "t"),
		} {
			if other.ID == base.ID {
				t.Fatalf("%s answers the same identity as the base product, want a distinct key", name)
			}
		}
	})
}
