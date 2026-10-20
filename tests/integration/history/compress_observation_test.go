package history

import (
	"context"
	"strings"
	"testing"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/memory/replica"
)

// TestCompressRequestObservationThroughTheBoundedTool checks the line the bounded tool's filing writes.
//   - an overflow judgment the bounded tool files writes one line beside its producer and reason
//   - the request the line quotes is the one a consumer reads back from the filing log
//   - a failing logger changes no filing: the bypass drops the line and keeps the request
//
// 契约: docs/wiki/agent/compression-and-telemetry.md#output-overflow
func TestCompressRequestObservationThroughTheBoundedTool(t *testing.T) {
	ctx := context.Background()
	owner := event.Address{AgentID: "agent-large", SessionID: "session-large"}
	scope := replica.ResultScope{Owner: owner, Format: replica.FormatToolResult, RetentionPolicy: "keep"}
	oversized := strings.Repeat("x", replica.ThresholdForMaxTokens(replica.DefaultMaxTokens)+100)

	t.Run("an overflow judgment files its request beside its line", func(t *testing.T) {
		observer := &historyObserver{}
		defer watchHistoryLog(t, observer)()
		requests := memory.NewCompressRequests()
		bounded := boundedOverflowTool(t, requests, scope, oversized)
		if _, err := bounded.Call(ctx, nil); err != nil {
			t.Fatalf("call the bounded tool: %v", err)
		}
		pending, err := requests.Pending(ctx)
		if err != nil {
			t.Fatalf("read the filed requests: %v", err)
		}
		if len(pending) != 1 {
			t.Fatalf("pending requests = %d, want the one judgment the overflow filed", len(pending))
		}
		if !observer.mentions("debug", "compress request", "result=filed", "producer="+replica.CompressProducerToolOverflow,
			"reason="+replica.CompressReasonToolOverflow, "id="+string(pending[0].ID)) {
			t.Fatalf("debug lines = %v, want the filed judgment beside its producer, reason and request identity", observer.all())
		}
	})

	t.Run("a failing logger changes no filing", func(t *testing.T) {
		observer := &historyObserver{panicsLeft: 1}
		defer watchHistoryLog(t, observer)()
		requests := memory.NewCompressRequests()
		bounded := boundedOverflowTool(t, requests, scope, oversized)
		if _, err := bounded.Call(ctx, nil); err != nil {
			t.Fatalf("call the bounded tool beside a failing logger: %v", err)
		}
		pending, err := requests.Pending(ctx)
		if err != nil || len(pending) != 1 {
			t.Fatalf("pending requests beside a failing logger = (%v, %v), want the one filed judgment", pending, err)
		}
	})
}

// boundedOverflowTool returns the bounded tool of these checks: it commutes the
// oversized result of one native call under the scope named and files the
// judgment where the filing log of the checks keeps it.
func boundedOverflowTool(t *testing.T, requests *memory.CompressRequests, scope replica.ResultScope, result string) *replica.BoundedTool {
	t.Helper()
	externalizer, err := replica.NewResultExternalizer(ledger.NewMemoryOriginalStore())
	if err != nil {
		t.Fatalf("build the result externalizer: %v", err)
	}
	bounded, err := replica.NewBoundedTool(&largeResultTool{
		declaration: &trpctool.Declaration{Name: "shell", Description: "run one shell command"},
		result:      result,
	}, externalizer, scope, replica.WithCompressRequestFiling(requests.Produce))
	if err != nil {
		t.Fatalf("build the bounded tool: %v", err)
	}
	return bounded
}
