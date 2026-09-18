package rl

import (
	"fmt"
	"net"
	"net/http"
	"strings"
)

// Endpoint redirect policy (resident-remaining-hardening 1.4 / design D3,
// closing cold-eyes Major 5): the HTTPAPI endpoint allowlist only bounds the
// INITIAL llm_base_url — without a CheckRedirect hook an allowlisted endpoint
// could 30x the LLM client to any host (SSRF bridge to metadata services).
// These helpers give the host the per-hop guard to install on the LLM HTTP
// client, keeping the allowlist decision (rl package) free of any provider
// SDK dependency (the host wires it via openai.WithOpenAIOptions).

// EndpointRedirectPolicy builds a http.Client CheckRedirect func enforcing
// the endpoint allowlist on EVERY hop of a 30x chain: the target host must be
// on the allowlist (exact host match, any port — same semantics as
// HTTPAPI.validateEndpointURL). An empty allowlist rejects every redirect,
// which is the disabled-redirect deployment semantics: dynamic llm_base_url
// redirect is off, so no hop may leave the initial URL.
func EndpointRedirectPolicy(allowedHosts []string) func(*http.Request, []*http.Request) error {
	allowlist := make(map[string]bool, len(allowedHosts))
	for _, hst := range allowedHosts {
		if hst = strings.ToLower(strings.TrimSpace(hst)); hst != "" {
			allowlist[hst] = true
		}
	}
	return func(req *http.Request, via []*http.Request) error {
		host := normalizeRedirectHost(req.URL.Host)
		if host == "" {
			return fmt.Errorf("endpoint redirect policy: unparseable redirect target %q", req.URL.Host)
		}
		if !allowlist[host] {
			return fmt.Errorf("endpoint redirect policy: hop %d target host %q not in endpoint allowlist (initial URL %q)",
				len(via), host, via[0].URL.String())
		}
		return nil
	}
}

// normalizeRedirectHost lowercases the host and strips the port (allowlist
// matches exact host, any port). Bracketed IPv6 literals keep their brackets.
func normalizeRedirectHost(hostPort string) string {
	if host, _, err := net.SplitHostPort(hostPort); err == nil {
		return strings.ToLower(host)
	}
	return strings.ToLower(strings.Trim(hostPort, "[]"))
}

// NewEndpointGuardedClient returns an http.Client suitable as an LLM transport
// for openai-style SDKs: default proxy transport plus the per-hop allowlist
// guard from EndpointRedirectPolicy. A nil/empty allowlist yields a client
// that refuses all redirects.
func NewEndpointGuardedClient(allowedHosts []string) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	return &http.Client{
		Transport:     transport,
		CheckRedirect: EndpointRedirectPolicy(allowedHosts),
	}
}
