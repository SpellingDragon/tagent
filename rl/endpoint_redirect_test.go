package rl

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func redirectReq(t *testing.T, target string) (*http.Request, []*http.Request) {
	t.Helper()
	initial := httptest.NewRequest("POST", "http://proxy.allowed.example/v1/chat/completions", nil)
	req := httptest.NewRequest("GET", target, nil)
	return req, []*http.Request{initial}
}

func TestEndpointRedirectPolicy_HopSemantics(t *testing.T) {
	pol := EndpointRedirectPolicy([]string{"Proxy.Allowed.Example"})

	// In-allowlist hop passes (case-insensitive host match, any port).
	req, via := redirectReq(t, "http://proxy.allowed.example:8443/v1/chat")
	if err := pol(req, via); err != nil {
		t.Errorf("allowlisted hop must pass, got %v", err)
	}

	// Out-of-allowlist hop is rejected with the offending host named.
	req, via = redirectReq(t, "http://internal.metadata.host/latest/meta-data")
	err := pol(req, via)
	if err == nil {
		t.Fatal("out-of-allowlist hop must be rejected")
	}
	if !strings.Contains(err.Error(), "internal.metadata.host") || !strings.Contains(err.Error(), "allowlist") {
		t.Errorf("rejection must name host + allowlist reason: %v", err)
	}

	// Empty allowlist rejects every hop (redirect-disabled semantics).
	off := EndpointRedirectPolicy(nil)
	req, via = redirectReq(t, "http://proxy.allowed.example/next")
	if err := off(req, via); err == nil {
		t.Error("empty allowlist must reject all redirects")
	}
}

func TestGuardedClient_EmptyAllowlistRefusesHopWithoutRequest(t *testing.T) {
	var hops int32
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&hops, 1)
		if n == 1 {
			// Redirect to the canary under a DIFFERENT host name that still
			// resolves nowhere — rejection must happen in CheckRedirect, so
			// the error is the policy error, never a dial/DNS error.
			http.Redirect(w, r, "http://tagent-redirect-canary.invalid/stage", http.StatusFound)
			return
		}
		w.Write([]byte("served"))
	}))
	defer front.Close()

	client := NewEndpointGuardedClient(nil) // redirect disabled
	_, err := client.Get(front.URL)
	if err == nil {
		t.Fatal("expected redirect refusal error")
	}
	if !strings.Contains(err.Error(), "not in endpoint allowlist") {
		t.Errorf("must fail with the policy error, got: %v", err)
	}
	if atomic.LoadInt32(&hops) != 1 {
		t.Errorf("front must be hit exactly once (hop never followed), got %d", hops)
	}
}

func TestGuardedClient_AllowlistedHopChainSucceeds(t *testing.T) {
	var finalHits int32
	final := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&finalHits, 1)
		w.Write([]byte("done"))
	}))
	defer final.Close()
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, final.URL, http.StatusFound)
	}))
	defer front.Close()

	// Both servers live on 127.0.0.1 — the allowlist matches exact host,
	// any port, so the hop is legal and must be followed.
	client := NewEndpointGuardedClient([]string{"127.0.0.1"})
	resp, err := client.Get(front.URL)
	if err != nil {
		t.Fatalf("allowlisted hop must succeed: %v", err)
	}
	defer resp.Body.Close()
	buf := make([]byte, 8)
	n, _ := resp.Body.Read(buf)
	if string(buf[:n]) != "done" || atomic.LoadInt32(&finalHits) != 1 {
		t.Errorf("redirect must land on final server, body=%q hits=%d", buf[:n], finalHits)
	}
}

func TestNormalizeRedirectHost(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"Proxy.Example.COM:8443", "proxy.example.com"},
		{"proxy.example.com", "proxy.example.com"},
		{"[::1]:9000", "::1"},
		{"[::1]", "::1"},
	} {
		if got := normalizeRedirectHost(tc.in); got != tc.want {
			t.Errorf("normalizeRedirectHost(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
