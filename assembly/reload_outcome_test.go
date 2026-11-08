// One reload attempt files one outcome: a step that fails after the generation entered service reaches the caller beside the receipt instead of being filed as a second, refusing outcome of the same attempt.
// - a result the attempt did not take into service never carries the generation this attempt published
// 契约: docs/wiki/reliability/durable-delivery.md#lineage-visibility
package assembly

import (
	"context"
	"os"
	"strings"
	"testing"
)

// TestReloadAttemptSingleOutcome pins the one-outcome rule of one attempt.
//   - the live MCP registry is shut down behind the running binding, so a document that declares a server cannot be bound after its generation published
//   - the attempt states the published generation it took into service, and the bind failure reaches the caller beside that receipt
//   - the chain holds exactly one result fact for the attempt, and no result of the published generation contradicts it
//   - the document in effect moved with the generation: the same file is carried by the next attempt instead of published again
//
// 契约: docs/wiki/platform/org-hot-reload.md#apply-record
func TestReloadAttemptSingleOutcome(t *testing.T) {
	doc := newLivenessDoc(t)
	path := doc.write(t)
	host := bootLiveness(t, path)
	run := host.Run()
	if run.mcpServers == nil {
		t.Fatal("the booted run carries no live MCP registry")
	}
	if err := run.mcpServers.Close(); err != nil {
		t.Fatalf("close the live MCP registry: %v", err)
	}
	generation := run.publisher.Current().Generation
	resultsBefore := countFacts(chainFacts(t, run), configPublicationResultKind)

	declared := doc.yaml() + "mcp_servers:\n  probe: { transport: streamable-http, url: \"http://127.0.0.1:1/mcp\" }\n"
	if err := os.WriteFile(path, []byte(declared), 0o644); err != nil {
		t.Fatalf("rewrite the configuration source: %v", err)
	}

	report, err := host.Reload(context.Background())
	if err == nil {
		t.Fatal("the reload of a document the closed registry cannot bind answered success")
	}
	if !strings.Contains(err.Error(), "mcp") {
		t.Fatalf("reload error = %v, want the failed MCP binding named", err)
	}
	if got := run.publisher.Current().Generation; got != generation+1 {
		t.Fatalf("generation in service = %d, want %d", got, generation+1)
	}

	facts := chainFacts(t, run)
	if got := countFacts(facts, configPublicationResultKind); got != resultsBefore+1 {
		t.Fatalf("result facts after one attempt = %d, want the one fact of the attempt beside the %d before it", got, resultsBefore)
	}
	attempt := latestAttempt(t, run)
	if attempt.Result != publicationResultPublished {
		t.Fatalf("attempt result = %q, want %q: the generation of this attempt is in service", attempt.Result, publicationResultPublished)
	}
	if attempt.Generation != generation+1 {
		t.Fatalf("attempt generation = %d, want the generation %d it published", attempt.Generation, generation+1)
	}
	for _, fact := range facts {
		if fact.Kind != configPublicationResultKind || fact.Generation != generation+1 {
			continue
		}
		if fact.Result != publicationResultPublished {
			t.Fatalf("result %q of the published generation = %+v, want no outcome contradicting the publication", fact.Result, fact)
		}
	}
	if !report.Publication.Published || report.Publication.Generation != generation+1 {
		t.Fatalf("reload publication = %+v, want generation %d published", report.Publication, generation+1)
	}

	again, againErr := host.Reload(context.Background())
	if againErr == nil {
		t.Fatal("the reload of the same document answered success beside the closed registry")
	}
	if again.Publication.Published || again.Publication.Generation != generation+1 {
		t.Fatalf("carried reload publication = %+v, want the generation %d carried", again.Publication, generation+1)
	}
	if got := latestAttempt(t, run); got.Result != publicationResultUnchanged {
		t.Fatalf("carried reload attempt = %+v, want the unchanged result", got)
	}
}
