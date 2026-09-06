package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/SpellingDragon/tagent/agent/reliability"
)

// TestMCPCall_CircuitBreak (5.4, design-report-closeout): while DepMCP is
// degraded and probeEvery=N>0, every call but the Nth short-circuits with a
// readable result (failure permeates as result, never error). Default off
// (probeEvery=0 → real calls).
func TestMCPCall_CircuitBreak(t *testing.T) {
	reg := NewRegistry() // empty: server lookups fail anyway — the breaker
	// must fire BEFORE the lookup, so an empty registry still proves the path.
	ct := NewCallTool(reg)
	mgr := reliability.NewDegradationManager(nil)
	ct.SetDegradation(mgr)
	ct.SetMCPProbeEvery(2)

	// Force DepMCP degraded via report failures.
	for i := 0; i < 6; i++ {
		mgr.ReportFailure(reliability.DepMCP, context.DeadlineExceeded)
	}
	if !mgr.IsDegraded(reliability.DepMCP) {
		t.Fatal("setup: DepMCP should be degraded")
	}

	call := func() string {
		res, err := ct.Call(context.Background(), []byte(`{"server":"x","tool":"y","args":{}}`))
		if err != nil {
			t.Fatalf("breaker must not error: %v", err)
		}
		// callErrorResult carries Error + AvailableServers; the breaker's message
		// is identifiable by the probe interval wording.
		r, _ := res.(callErrorResult)
		return r.Error
	}
	first := call()  // probeCount=1, 1%2 != 0 → breaker
	second := call() // probeCount=2 → probe allowed (real lookup fails with
	// the ordinary "server not found" style error, NOT the breaker message)
	if !strings.Contains(first, "熔断") {
		t.Fatalf("first call should be circuit-broken, got %q", first)
	}
	if strings.Contains(second, "熔断") {
		t.Fatalf("second call (probe) must pass through, got %q", second)
	}

	// Default off: probeEvery=0 → no breaker message even while degraded.
	ct2 := NewCallTool(reg)
	ct2.SetDegradation(mgr)
	for i := 0; i < 6; i++ {
		mgr.ReportFailure(reliability.DepMCP, context.DeadlineExceeded)
	}
	res, _ := ct2.Call(context.Background(), []byte(`{"server":"x","tool":"y","args":{}}`))
	if strings.Contains(res.(callErrorResult).Error, "熔断") {
		t.Fatal("probeEvery=0 must disable the breaker (zero behavior change)")
	}
}
