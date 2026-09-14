package rl

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// fakeLoop satisfies AgentLoop for auth-path tests (only routing past the
// auth point matters — handlers get a live loop).
type fakeLoop struct{}

func (fakeLoop) InjectMessage(model.Message)                   {}
func (fakeLoop) InjectMessageWithSource(string, model.Message) {}
func (fakeLoop) StartLoop(string, string) (<-chan *event.Event, error) {
	ch := make(chan *event.Event, 1)
	return ch, nil
}
func (fakeLoop) StopLoop()          {}
func (fakeLoop) IsLoopActive() bool { return true }

// Tests for the bearer-token enforcement + loopback fail-closed guard
// (implementation-hardening 3.1/3.2).

func TestHTTPAPI_Auth_401WithoutOrWrongToken(t *testing.T) {
	api := NewHTTPAPI(nil)
	api.SetAuthToken("secret")

	for name, header := range map[string]string{
		"no header":    "",
		"wrong scheme": "Basic secret",
		"wrong token":  "Bearer wrong",
		"prefix only":  "Bearer",
	} {
		req := httptest.NewRequest(http.MethodPost, "/task", strings.NewReader(`{}`))
		if header != "" {
			req.Header.Set("Authorization", header)
		}
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s: status = %d, want 401", name, rec.Code)
		}
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body["error"] != "unauthorized" {
			t.Fatalf("%s: body error = %v, want unauthorized", name, body["error"])
		}
	}
}

func TestHTTPAPI_Auth_CorrectTokenReachesHandler(t *testing.T) {
	api := NewHTTPAPI(fakeLoop{})
	api.SetAuthToken("secret")

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, req)
	if rec.Code == http.StatusUnauthorized {
		t.Fatalf("correct token must not be 401 (got %d)", rec.Code)
	}
}

func TestHTTPAPI_Auth_HealthzNotExempt(t *testing.T) {
	api := NewHTTPAPI(nil)
	api.SetAuthToken("secret")
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("/healthz must NOT be exempt: status = %d, want 401", rec.Code)
	}
}

func TestValidateListenAddr(t *testing.T) {
	cases := []struct {
		name    string
		addr    string
		token   string
		wantErr bool
	}{
		{"token set allows any addr", "0.0.0.0:8089", "tok", false},
		{"loopback ipv4 ok", "127.0.0.1:8089", "", false},
		{"loopback ipv6 ok", "[::1]:8089", "", false},
		{"localhost ok", "localhost:8089", "", false},
		{"all interfaces no token", ":8089", "", true},
		{"explicit wildcard no token", "0.0.0.0:8089", "", true},
		{"lan ip no token", "192.168.1.5:8089", "", true},
	}
	for _, tc := range cases {
		err := ValidateListenAddr(tc.addr, tc.token)
		if (err != nil) != tc.wantErr {
			t.Fatalf("%s: err = %v, wantErr = %v", tc.name, err, tc.wantErr)
		}
		if tc.wantErr && !strings.Contains(err.Error(), "TAGENT_RL_AUTH_TOKEN") {
			t.Fatalf("%s: error must list the fix paths, got: %v", tc.name, err)
		}
	}
}

func TestAuthTokenFromEnv(t *testing.T) {
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "env-tok")
	if got := AuthTokenFromEnv(); got != "env-tok" {
		t.Fatalf("AuthTokenFromEnv = %q", got)
	}
}
