package rlexample

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/SpellingDragon/tagent/rl"
)

// errEmptyLine is reported for a record line the writer cannot persist.
var errEmptyLine = errors.New("rlexample: the record line is empty")

// digestWriter is the canonical durable write point of one capture: it drains record lines,
// seals every file it holds and confirms the sync it observed.
type digestWriter struct {
	names []string
	files map[string][]byte
	seals []rl.CaptureManifest
}

// Append persists one record line under the named file.
func (w *digestWriter) Append(name string, line []byte) error {
	if len(line) == 0 {
		return errEmptyLine
	}
	if w.files == nil {
		w.files = map[string][]byte{}
	}
	if _, ok := w.files[name]; !ok {
		w.names = append(w.names, name)
	}
	w.files[name] = append(w.files[name], line...)
	return nil
}

// Seal closes and syncs every file and reports the per-file digest entries in file name
// order beside the sync result the writer observed.
func (w *digestWriter) Seal() ([]rl.CaptureFileDigest, rl.WriterSyncResult) {
	digests := make([]rl.CaptureFileDigest, 0, len(w.names))
	for _, name := range w.names {
		body := w.files[name]
		sum := sha256.Sum256(body)
		digests = append(digests, rl.CaptureFileDigest{Name: name, SHA256: hex.EncodeToString(sum[:]), Bytes: int64(len(body)), Records: 1})
	}
	return digests, rl.WriterSyncResult{Synced: true, At: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
}

// Commit persists the confirmed manifest so a restarted reader reads the capture's state
// back.
func (w *digestWriter) Commit(manifest rl.CaptureManifest) error {
	w.seals = append(w.seals, manifest)
	return nil
}

var _ rl.CaptureWriter = (*digestWriter)(nil)

// ExampleCaptureWriter persists two record lines, seals the files it wrote and commits the
// confirmed manifest.
//
// 契约: docs/wiki/rl/rl-architecture.md#trajectory-capture
func ExampleCaptureWriter() {
	writer := &digestWriter{}
	if err := writer.Append("attempts.jsonl", []byte("{}\n")); err != nil {
		return
	}
	if err := writer.Append("attempts.jsonl", []byte("{}\n")); err != nil {
		return
	}
	if err := writer.Append("attempts.jsonl", nil); !errors.Is(err, errEmptyLine) {
		return
	}
	digests, sync := writer.Seal()
	if !sync.Synced || len(digests) != 1 || digests[0].Records != 1 {
		return
	}
	if err := writer.Commit(rl.CaptureManifest{CaptureID: "capture-1", Sealed: true}); err != nil {
		return
	}
}
