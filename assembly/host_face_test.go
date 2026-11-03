package assembly

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestHealthzContract pins the liveness probe behind the same authentication point every other endpoint of the face passes.
// - positive: a probe carrying the configured credential answers the bare liveness token under the ok status
// - negative anchor (unauthenticated_probe_classified): no credential answers the unauthorized status with the bearer challenge and no liveness token
// - a credential that does not match answers the same refusal, the class an ops probe reports as AUTH_FAIL rather than healthy
// - a face beside no configured credential admits the loopback peer only, so the tokenless deployment keeps the probe it polls
// - the refused class turns healthy beside the credential, so the refusal names a credential failure rather than a dead face
// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
func TestHealthzContract(t *testing.T) {
	const credential = "healthz-contract-credential"

	t.Run("authenticated_probe_answers_liveness", func(t *testing.T) {
		url := healthzTestFaceURL(t, credential)
		status, body, _ := healthzTestProbe(t, url, "Bearer "+credential)
		if status != http.StatusOK || strings.TrimSpace(body) != "ok" {
			t.Fatalf("authenticated probe = (%d, %q), want the liveness answer (200, \"ok\")", status, body)
		}
	})

	t.Run("unauthenticated_probe_classified", func(t *testing.T) {
		url := healthzTestFaceURL(t, credential)
		status, body, header := healthzTestProbe(t, url, "")
		if status != http.StatusUnauthorized {
			t.Fatalf("probe beside no credential = (%d, %q), want the unauthorized status the ops probe classes as auth_fail", status, body)
		}
		if strings.TrimSpace(body) == "ok" {
			t.Fatalf("probe beside no credential answered the liveness token %q, want the named denial instead", body)
		}
		if challenge := header.Get("WWW-Authenticate"); !strings.HasPrefix(challenge, "Bearer") {
			t.Fatalf("challenge of the refused probe = %q, want the bearer challenge a caller reads to recognize the class", challenge)
		}
		status, body, _ = healthzTestProbe(t, url, "Bearer "+credential)
		if status != http.StatusOK || strings.TrimSpace(body) != "ok" {
			t.Fatalf("probe beside the credential = (%d, %q), want the liveness answer (200, \"ok\")", status, body)
		}
	})

	t.Run("mismatched_credential_classified", func(t *testing.T) {
		url := healthzTestFaceURL(t, credential)
		status, body, header := healthzTestProbe(t, url, "Bearer not-the-configured-credential")
		if status != http.StatusUnauthorized {
			t.Fatalf("probe beside a mismatched credential = (%d, %q), want the unauthorized status", status, body)
		}
		if challenge := header.Get("WWW-Authenticate"); !strings.HasPrefix(challenge, "Bearer") {
			t.Fatalf("challenge of the mismatched probe = %q, want the bearer challenge", challenge)
		}
	})

	t.Run("credentialless_face_admits_loopback", func(t *testing.T) {
		url := healthzTestFaceURL(t, "")
		status, body, _ := healthzTestProbe(t, url, "")
		if status != http.StatusOK || strings.TrimSpace(body) != "ok" {
			t.Fatalf("probe of a face beside no credential = (%d, %q), want the loopback peer admitted with (200, \"ok\")", status, body)
		}
	})
}

// healthzTestFaceURL opens one face through the constructor the boot uses, on an
// ephemeral loopback port, and returns the liveness probe URL it serves.
func healthzTestFaceURL(t *testing.T, credential string) string {
	t.Helper()
	t.Setenv(hostFacePortEnv, "0")
	ctx, cancel := context.WithCancel(context.Background())
	face, err := newHostFace(ctx, &Host{}, credential)
	if err != nil {
		cancel()
		t.Fatalf("open the face: %v", err)
	}
	face.start()
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), hostShutdownGrace)
		defer stopCancel()
		if err := face.stop(stopCtx); err != nil {
			t.Errorf("close the face: %v", err)
		}
		cancel()
	})
	return "http://" + face.addr() + "/healthz"
}

// healthzTestProbe polls the probe URL once beside the authorization header the
// caller passes, and reports the status, the body and the headers it answered.
func healthzTestProbe(t *testing.T, url, authorization string) (int, string, http.Header) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("build the probe request: %v", err)
	}
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("poll %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256))
	if err != nil {
		t.Fatalf("read the probe answer: %v", err)
	}
	return resp.StatusCode, string(body), resp.Header
}
