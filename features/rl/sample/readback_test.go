package sample

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/features/rl"
)

// TestSnapshotReadbackReconstructsRequest pins that a sealed snapshot reads back into the request it fixed and reconciles against its digest.
// - the snapshot leaves the heap for the swap store before the seal
// - the reconstructed messages and generation parameters match the original
// - redacted fields stay redacted through the read-back
// 契约: docs/wiki/rl/rl-architecture.md#trajectory-capture
func TestSnapshotReadbackReconstructsRequest(t *testing.T) {
	capture, swapDir := readbackCapture(t)
	ctx := context.Background()
	maxTokens := 2048
	temperature := 0.2
	topP := 0.9
	original := model.Request{
		Messages: []model.Message{
			{Role: model.RoleSystem, Content: "answer from the workspace"},
			{Role: model.RoleUser, Content: "read the task file and report its contents"},
		},
		ExtraFields: map[string]any{"api_key": "secret-value", "region": "us-east"},
		Headers:     map[string]string{"Authorization": "Bearer secret-value", "X-Trace": "trace-1"},
	}
	original.MaxTokens = &maxTokens
	original.Temperature = &temperature
	original.TopP = &topP
	original.Stop = []string{"END"}
	view := rl.ModelViewRef{
		Model:         "scripted",
		ModelVersion:  "v1",
		PromptVersion: "prompt-v1",
		VisibleTools:  []rl.VersionedName{{Name: "file_read", Version: "v2"}},
		VisibleSkills: []rl.VersionedName{{Name: "read-task", Version: "v1"}},
	}
	scope := capture.Scope(view, "decision:readback")
	fixed, err := scope.Snapshot(ctx, original)
	if err != nil {
		t.Fatalf("fix the request snapshot: %v", err)
	}
	if fixed.Snapshot.SnapshotID == "" || fixed.Snapshot.Digest == "" {
		t.Fatalf("fixed view snapshot = %+v, want a readable reference", fixed.Snapshot)
	}
	manifest, err := capture.Seal(ctx)
	if err != nil {
		t.Fatalf("seal the capture: %v", err)
	}
	if !manifest.Sealed {
		t.Fatalf("manifest = %+v, want a writer-confirmed seal", manifest)
	}
	swapped, err := os.ReadDir(swapDir)
	if err != nil || len(swapped) != 1 {
		t.Fatalf("swap store entries = %v, %v, want the released snapshot on disk", swapped, err)
	}
	snapshot, err := capture.SnapshotOf(fixed.Snapshot)
	if err != nil {
		t.Fatalf("read the snapshot back by reference: %v", err)
	}
	content, err := rl.MarshalSnapshot(snapshot)
	if err != nil {
		t.Fatalf("marshal the read-back snapshot: %v", err)
	}
	sum := sha256.Sum256(content)
	if got := hex.EncodeToString(sum[:]); got != fixed.Snapshot.Digest {
		t.Fatalf("read-back content digest = %s, recorded summary = %s", got, fixed.Snapshot.Digest)
	}
	reconstructed, err := readback(capture, fixed.Snapshot)
	if err != nil {
		t.Fatalf("read back and reconstruct the request: %v", err)
	}
	if !reflect.DeepEqual(reconstructed.Messages, original.Messages) {
		t.Fatalf("reconstructed messages = %+v, want %+v", reconstructed.Messages, original.Messages)
	}
	if !reflect.DeepEqual(reconstructed.GenerationConfig, original.GenerationConfig) {
		t.Fatalf("reconstructed parameters = %+v, want %+v", reconstructed.GenerationConfig, original.GenerationConfig)
	}
	wantExtra := map[string]any{"api_key": rl.RedactedValue, "region": "us-east"}
	if !reflect.DeepEqual(reconstructed.ExtraFields, wantExtra) {
		t.Fatalf("reconstructed extra fields = %+v, want the projected %+v", reconstructed.ExtraFields, wantExtra)
	}
	wantHeaders := map[string]string{"Authorization": rl.RedactedValue, "X-Trace": rl.RedactedValue}
	if !reflect.DeepEqual(reconstructed.Headers, wantHeaders) {
		t.Fatalf("reconstructed headers = %+v, want the projected %+v", reconstructed.Headers, wantHeaders)
	}
}

// TestReadbackMismatchFailsClosed pins that content drifting from the recorded digest is refused and returns no reconstructed request.
// - the read-back returns ErrSnapshotMismatch and no request beside it
// 契约: docs/wiki/rl/rl-architecture.md#trajectory-capture
func TestReadbackMismatchFailsClosed(t *testing.T) {
	capture, swapDir := readbackCapture(t)
	ctx := context.Background()
	scope := capture.Scope(rl.ModelViewRef{Model: "scripted", PromptVersion: "prompt-v1"}, "decision:drift")
	fixed, err := scope.Snapshot(ctx, model.Request{
		Messages: []model.Message{{Role: model.RoleUser, Content: "the request that was fixed"}},
	})
	if err != nil {
		t.Fatalf("fix the request snapshot: %v", err)
	}
	if _, err := capture.Seal(ctx); err != nil {
		t.Fatalf("seal the capture: %v", err)
	}
	swapped, err := os.ReadDir(swapDir)
	if err != nil || len(swapped) != 1 {
		t.Fatalf("swap store entries = %v, %v, want the released snapshot on disk", swapped, err)
	}
	tampered := []byte(`{"model":"scripted","messages":[{"role":"user","content":"a different request"}]}`)
	if err := os.WriteFile(filepath.Join(swapDir, swapped[0].Name()), tampered, 0o600); err != nil {
		t.Fatalf("tamper the stored snapshot: %v", err)
	}
	reconstructed, err := readback(capture, fixed.Snapshot)
	if !errors.Is(err, rl.ErrSnapshotMismatch) {
		t.Fatalf("read-back of drifted content = %v, want ErrSnapshotMismatch", err)
	}
	if !reflect.DeepEqual(reconstructed, model.Request{}) {
		t.Fatalf("reconstructed request = %+v, want none beside the refusal", reconstructed)
	}
}

// readbackCapture opens one capture whose snapshots leave the heap for a swap
// store, so the read-back tests read storage rather than a kept copy.
func readbackCapture(t *testing.T) (*rl.TrajectoryCapture, string) {
	t.Helper()
	root := t.TempDir()
	swapDir := filepath.Join(root, "swap")
	captureDir := filepath.Join(root, "capture")
	if err := os.MkdirAll(captureDir, 0o755); err != nil {
		t.Fatalf("create the capture directory: %v", err)
	}
	writer, err := rl.NewFileCaptureWriter(captureDir, 4)
	if err != nil {
		t.Fatalf("open the capture writer: %v", err)
	}
	sink, err := rl.NewCaptureSink("cap:readback", rl.SinkConfig{}, writer)
	if err != nil {
		t.Fatalf("open the capture sink: %v", err)
	}
	capture, err := rl.NewTrajectoryCaptureWithBudget("cap:readback", sink, rl.SnapshotPolicy{}, rl.CaptureBudget{SwapDir: swapDir})
	if err != nil {
		t.Fatalf("open the capture: %v", err)
	}
	return capture, swapDir
}
