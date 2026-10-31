// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
package execution

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/features/rl"
)

const (
	// debugPprofCredential is the request credential the credentialed boot arms
	// the face with.
	debugPprofCredential = "debug-pprof-credential"
	// debugPprofWindow is the span one profile request may take.
	debugPprofWindow = 30 * time.Second
	// debugPprofSnapshots is the number of snapshots the resident sample fixes.
	debugPprofSnapshots = 8
	// debugPprofBudget is the resident budget of that capture's fixed snapshots.
	debugPprofBudget = 1 << 20
	// debugPprofBodyKB is the request body one fixed snapshot carries: the view
	// a long-context request of about 134K tokens asks the capture to keep.
	debugPprofBodyKB = 500
)

// debugPprofNames are the three profile endpoints the debug face publishes.
var debugPprofNames = []string{"heap", "goroutine", "allocs"}

// TestDebugPprofServesHeap pins the debug face of one booted host:
// - the listener is the loopback address, and each of the three profile endpoints answers a pprof payload
// - a face beside a configured credential refuses the same three requests until the bearer token arrives
// - the resident cost one fixed snapshot adds is logged beside the budget the capture holds
// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
func TestDebugPprofServesHeap(t *testing.T) {
	host := debugPprofBoot(t, newIntakeProvider(t), "")
	if addr := host.FaceAddr(); !strings.HasPrefix(addr, "127.0.0.1:") {
		t.Fatalf("debug face address = %q, want the loopback interface", addr)
	}
	base := "http://" + host.FaceAddr()
	for _, name := range debugPprofNames {
		debugPprofAssertProfile(t, name, debugPprofGet(t, base+"/debug/pprof/"+name, ""))
	}
	debugPprofSampleCapturePeak(t)

	t.Run("the credential gates the debug face", func(t *testing.T) {
		guarded := debugPprofBoot(t, newIntakeProvider(t), debugPprofCredential)
		guardedBase := "http://" + guarded.FaceAddr()
		for _, name := range debugPprofNames {
			refused := debugPprofGet(t, guardedBase+"/debug/pprof/"+name, "")
			if refused.status != http.StatusUnauthorized {
				t.Fatalf("GET /debug/pprof/%s without the credential status = %d, want the 401 refusal: %s",
					name, refused.status, debugPprofHead(refused.body))
			}
			debugPprofAssertProfile(t, name, debugPprofGet(t, guardedBase+"/debug/pprof/"+name, "Bearer "+debugPprofCredential))
		}
	})
}

// TestGCTraceSwitchStatesItsBoundary pins the GC trace switch a boot carries:
// - TAGENT_GCTRACE=1 beside a GODEBUG that declares no gctrace merges the setting into GODEBUG
// - TAGENT_GCTRACE=1 beside a GODEBUG that already declares gctrace leaves that declaration standing
// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
func TestGCTraceSwitchStatesItsBoundary(t *testing.T) {
	t.Run("the switch merges into a GODEBUG that declares none", func(t *testing.T) {
		t.Setenv("TAGENT_GCTRACE", "1")
		t.Setenv("GODEBUG", "gcstoptheworld=0")
		debugPprofBoot(t, newIntakeProvider(t), "")
		if debug := os.Getenv("GODEBUG"); debug != "gcstoptheworld=0,gctrace=1" {
			t.Fatalf("GODEBUG after the boot = %q, want the declared setting beside the merged gctrace", debug)
		}
	})

	t.Run("a declared gctrace stands", func(t *testing.T) {
		t.Setenv("TAGENT_GCTRACE", "1")
		t.Setenv("GODEBUG", "gctrace=1,gcstoptheworld=0")
		debugPprofBoot(t, newIntakeProvider(t), "")
		if debug := os.Getenv("GODEBUG"); debug != "gctrace=1,gcstoptheworld=0" {
			t.Fatalf("GODEBUG after the boot = %q, want the declaration left standing", debug)
		}
	})
}

// debugPprofBoot boots the formal host of the anchor's document beside the
// request credential it names: an empty credential leaves the loopback peer as
// the only admitted subject, and the listener takes an ephemeral port so two
// boots of one run never contend for the same one.
func debugPprofBoot(t *testing.T, provider *intakeProvider, credential string) *assembly.Host {
	t.Helper()
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", credential)
	root := t.TempDir()
	configPath := filepath.Join(root, "tagent.yaml")
	if err := os.WriteFile(configPath, []byte(intakeHostDocument(root, provider.endpoint())), 0o644); err != nil {
		t.Fatalf("write the host document: %v", err)
	}
	host, err := assembly.Boot(configPath, assembly.WithSender(newIntakeSender()))
	if err != nil {
		t.Fatalf("boot the host: %v", err)
	}
	t.Cleanup(func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := host.Close(closeCtx); err != nil {
			t.Logf("the host ended its close beside: %v", err)
		}
	})
	return host
}

// debugPprofOutcome is one answer of the debug face: the status it declared,
// the content type it named and the body the client read.
type debugPprofOutcome struct {
	status      int
	contentType string
	body        []byte
}

// debugPprofGet issues one GET over the listener the host opened, under the
// authorization header the caller arms it with, and reports the answer as it
// stands.
func debugPprofGet(t *testing.T, url, authorization string) debugPprofOutcome {
	t.Helper()
	client := &http.Client{Timeout: debugPprofWindow}
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("build the request for %s: %v", url, err)
	}
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 64<<20))
	if err != nil {
		t.Fatalf("read %s: %v", url, err)
	}
	return debugPprofOutcome{status: response.StatusCode, contentType: response.Header.Get("Content-Type"), body: body}
}

// debugPprofAssertProfile asserts one endpoint answered the named profile: the
// 200 a profile read takes, the binary content type the handler declares, and a
// profile message that names its own sample type.
func debugPprofAssertProfile(t *testing.T, name string, got debugPprofOutcome) {
	t.Helper()
	if got.status != http.StatusOK {
		t.Fatalf("GET /debug/pprof/%s status = %d, want 200: %s", name, got.status, debugPprofHead(got.body))
	}
	if !strings.HasPrefix(got.contentType, "application/octet-stream") {
		t.Fatalf("GET /debug/pprof/%s content type = %q, want the binary profile type", name, got.contentType)
	}
	raw := got.body
	if len(raw) > 2 && raw[0] == 0x1f && raw[1] == 0x8b {
		reader, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			t.Fatalf("%s profile answered a gzip head the reader refused: %v", name, err)
		}
		defer reader.Close()
		if raw, err = io.ReadAll(io.LimitReader(reader, 64<<20)); err != nil {
			t.Fatalf("%s profile could not be unwrapped: %v", name, err)
		}
	}
	if len(raw) < 2 {
		t.Fatalf("%s profile carried %d bytes, want a profile message", name, len(raw))
	}
	if tag := raw[0]; tag&0x07 != 0x02 || tag>>3 < 1 || tag>>3 > 14 {
		t.Fatalf("%s profile head = %q, want a pprof profile field", name, debugPprofHead(raw))
	}
	if sample := debugPprofSampleType(name); !bytes.Contains(raw, []byte(sample)) {
		t.Fatalf("%s profile names no %q sample type: %q", name, sample, debugPprofHead(raw))
	}
}

// debugPprofSampleType reports the sample type the named profile carries.
func debugPprofSampleType(name string) string {
	switch name {
	case "heap":
		return "inuse_space"
	case "allocs":
		return "alloc_space"
	default:
		return "goroutine"
	}
}

// debugPprofHead shortens a body for a failure message: the profile payload is
// binary, so a bare %s would be unreadable.
func debugPprofHead(body []byte) string {
	const head = 80
	if len(body) <= head {
		return fmt.Sprintf("%q", body)
	}
	return fmt.Sprintf("%q…(%d bytes)", body[:head], len(body))
}

// debugPprofSampleCapturePeak fixes snapshots one at a time beside a capture
// held to a byte budget and a swap store, and logs the resident peak one
// snapshot added, the total the capture grew and the heap profile taken at that
// point. The numbers are the observation, so no bound is asserted here.
func debugPprofSampleCapturePeak(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	writer, err := rl.NewFileCaptureWriter(filepath.Join(dir, "records"), 0)
	if err != nil {
		t.Fatalf("open the capture writer: %v", err)
	}
	sink, err := rl.NewCaptureSink("cap:debug-pprof", rl.SinkConfig{}, writer)
	if err != nil {
		t.Fatalf("open the capture sink: %v", err)
	}
	capture, err := rl.NewTrajectoryCaptureWithBudget("debug-pprof", sink, rl.SnapshotPolicy{}, rl.CaptureBudget{
		Bytes:   debugPprofBudget,
		SwapDir: filepath.Join(dir, "swap"),
	})
	if err != nil {
		t.Fatalf("open the bounded capture: %v", err)
	}
	body := strings.Repeat("m", debugPprofBodyKB<<10)
	ctx := context.Background()
	baseline := debugPprofHeap()
	previous := baseline
	var peak int64
	for at := 0; at < debugPprofSnapshots; at++ {
		decision := event.DecisionID(fmt.Sprintf("decision-debug-pprof-%d", at))
		_, err := capture.Scope(rl.ModelViewRef{Model: "model-debug"}, decision).Snapshot(ctx, model.Request{
			Messages: []model.Message{{Role: model.RoleUser, Content: body}},
		})
		if err != nil {
			t.Fatalf("fix snapshot %d: %v", at, err)
		}
		current := debugPprofHeap()
		if grew := int64(current) - int64(previous); grew > peak {
			peak = grew
		}
		previous = current
	}
	var profile bytes.Buffer
	if err := pprof.Lookup("heap").WriteTo(&profile, 0); err != nil {
		t.Fatalf("write the heap profile: %v", err)
	}
	t.Logf("resident cost of one fixed snapshot: peak %d bytes over %d snapshots of %dKB, capture total %d bytes, budget %d bytes, pprof heap profile %d bytes",
		peak, debugPprofSnapshots, debugPprofBodyKB, int64(previous)-int64(baseline), debugPprofBudget, profile.Len())
}

// debugPprofHeap reports the allocated heap bytes after one full collection.
func debugPprofHeap() uint64 {
	runtime.GC()
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	return stats.HeapAlloc
}
