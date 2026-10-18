// The 08.1 acceptance tests of the host HTTP authentication gate: the request
// subject of every diagnostic request authenticates at one point ahead of the
// route resolution, an unauthenticated request is refused by name instead of
// being served, and a shell beside no configured credential keeps its
// diagnostic face closed.
package host

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/model"
	"github.com/SpellingDragon/tagent/query"
)

// diagCredential is the request credential the test shells authenticate with.
const diagCredential = "diag-test-credential"

// diagResponse is the outcome of one raw diagnostic request: the status code
// beside the response body.
type diagResponse struct {
	status int
	body   string
}

// diagGet issues one raw GET over the host HTTP face with the authorization
// header the test arms it with, and reports the outcome as it stands.
func diagGet(t *testing.T, url, authorization string) diagResponse {
	t.Helper()
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		t.Fatalf("build the request for %s: %v", url, err)
	}
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		t.Fatalf("read %s: %v", url, err)
	}
	return diagResponse{status: resp.StatusCode, body: buf.String()}
}

// diagRefusal asserts the response is the 401 named refusal carrying the
// named denial text, never a served reading.
func diagRefusal(t *testing.T, got diagResponse, wantText string) {
	t.Helper()
	if got.status != http.StatusUnauthorized {
		t.Fatalf("status = %d (%s), want the 401 named refusal", got.status, got.body)
	}
	if !strings.Contains(got.body, wantText) {
		t.Fatalf("refusal = %q, want the named denial %q", got.body, wantText)
	}
}

// newDiagService starts one host HTTP service over a fresh assembled run and
// reports its base URL beside the run, under the credential the caller names:
// an empty credential configures no request authentication at all.
func newDiagService(t *testing.T, ctx context.Context, credential string) (*tagent.Run, string, func()) {
	t.Helper()
	run, _, _ := newEntryRun(t, ctx)
	addr := event.Address{AgentID: "agent-a", SessionID: "session-diag"}
	if _, err := run.Invoke(ctx, tagent.InvokeRequest{
		InvocationID: "inv-diag",
		Address:      addr,
		Input:        []model.Message{{Role: model.RoleUser, Content: "hello"}},
	}); err != nil {
		t.Fatalf("invoke the assembled run: %v", err)
	}
	scope := query.AuthorizationScope{
		Agents:   []event.AgentID{"agent-a"},
		Sessions: []event.Address{addr},
	}
	opts := []tagent.CLIOption{}
	if credential != "" {
		opts = append(opts, tagent.WithRequestCredential(credential))
	}
	cli := tagent.NewCLI(run, scope, opts...)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	serveCtx, stopServe := context.WithCancel(ctx)
	serving := make(chan error, 1)
	go func() { serving <- cli.Serve(serveCtx, listener) }()
	stop := func() {
		stopServe()
		if err := <-serving; err != nil {
			t.Fatalf("serve: %v", err)
		}
	}
	return run, "http://" + listener.Addr().String(), stop
}

// TestDiagnosticEndpointRequiresAuthentication proves the authentication of the host diagnostic endpoints over the CLI shell:
// - the request subject authenticates at one gate ahead of the route resolution
// - the readings behind the gate are the same ones the SDK serves
// - 无认证具名拒而非裸奔: a request without a credential gets the 401 named refusal and no reading
// - 错凭据具名拒: an unmatched credential gets the same named refusal
// - 带认证可达: an authenticated request reads the same counts the SDK reading carries
// - 未配认证不开诊断口: a shell beside no configured credential refuses every diagnostic request by name
// - 无端点旁路: the liveness probe answers through the same single authentication point
// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
func TestDiagnosticEndpointRequiresAuthentication(t *testing.T) {
	t.Run("无认证具名拒而非裸奔: a request without a credential gets the 401 named refusal", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, base, stop := newDiagService(t, ctx, diagCredential)
		defer stop()
		diagRefusal(t, diagGet(t, base+"/query", ""), "the request subject is not authenticated")
	})

	t.Run("错凭据具名拒: an unmatched credential gets the same named refusal", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, base, stop := newDiagService(t, ctx, diagCredential)
		defer stop()
		diagRefusal(t, diagGet(t, base+"/query", "Bearer not-the-credential"), "the request subject is not authenticated")
		diagRefusal(t, diagGet(t, base+"/query", "Basic "+diagCredential), "the request subject is not authenticated")
	})

	t.Run("带认证可达: an authenticated request reads the same counts the SDK reading carries", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		run, base, stop := newDiagService(t, ctx, diagCredential)
		defer stop()
		got := diagGet(t, base+"/query", "Bearer "+diagCredential)
		if got.status != http.StatusOK {
			t.Fatalf("authenticated request status = %d (%s), want the served reading", got.status, got.body)
		}
		var overHTTP query.Snapshot
		if err := json.Unmarshal([]byte(got.body), &overHTTP); err != nil {
			t.Fatalf("decode the http reading: %v", err)
		}
		sdk, err := run.Query(ctx, query.ListFilter{Scope: query.AuthorizationScope{
			Agents:   []event.AgentID{"agent-a"},
			Sessions: []event.Address{{AgentID: "agent-a", SessionID: "session-diag"}},
		}})
		if err != nil {
			t.Fatalf("query through the sdk: %v", err)
		}
		if a, b := kindCounts(overHTTP), kindCounts(sdk); !reflect.DeepEqual(a, b) {
			t.Fatalf("http reading = %v, want the same derived counts as the sdk reading %v", a, b)
		}
		lower := diagGet(t, base+"/query", "bearer "+diagCredential)
		if lower.status != http.StatusOK {
			t.Fatalf("lower-case bearer status = %d (%s), want the scheme matched case independently", lower.status, lower.body)
		}
	})

	t.Run("未配认证不开诊断口: a shell beside no configured credential refuses by name", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, base, stop := newDiagService(t, ctx, "")
		defer stop()
		diagRefusal(t, diagGet(t, base+"/query", "Bearer "+diagCredential), "the diagnostic endpoints are not open")
		diagRefusal(t, diagGet(t, base+"/query", ""), "the diagnostic endpoints are not open")
	})

	t.Run("无端点旁路: the liveness probe answers through the same authentication point", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, base, stop := newDiagService(t, ctx, diagCredential)
		defer stop()
		diagRefusal(t, diagGet(t, base+"/healthz", ""), "the request subject is not authenticated")
		probe := diagGet(t, base+"/healthz", "Bearer "+diagCredential)
		if probe.status != http.StatusOK || probe.body != "ok" {
			t.Fatalf("authenticated probe = (%d, %q), want the liveness answer behind the gate", probe.status, probe.body)
		}
	})
}
