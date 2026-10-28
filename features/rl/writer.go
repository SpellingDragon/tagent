package rl

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// SealFileName is the file one writer persists its confirmed seal under.
//
// - The durable write point of one capture: the single writer the queue drains into, the seal that writer confirms and the capture state a restarted reader reads back.
// - The writer reports what reached storage and never what was merely handed to it: a file digest covers the bytes on disk, a sync result is the writer's own, and an existing file is never deleted or rotated to make room for new data.
// - A capture without a writer seal is read back partial with its reason, and the counts of such a capture are unknown rather than zero: missing evidence is never reported as an intact account.
// 契约: docs/wiki/rl/rl-architecture.md#trajectory-capture
const SealFileName = "seal.json"

// MaxOpenFilesCap is the hard cap on the files one writer holds open: a bound
// above the cap is clamped to the cap rather than obeyed.
const MaxOpenFilesCap = 16

// DefaultMaxOpenFiles is the open file bound one writer takes when its
// construction names none.
const DefaultMaxOpenFiles = MaxOpenFilesCap

// ErrWriterSealed and the other named errors of the durable write point: each names the way a write or a
// seal was refused, so a caller routes it by name.
var (
	// ErrWriterSealed reports an append offered to a writer that already sealed.
	ErrWriterSealed = errors.New("rl: the capture writer already sealed")
	// ErrWriterUnsealed reports a seal commit offered to a writer that has not
	// sealed yet: the persisted seal states what the writer confirmed, and there
	// is nothing confirmed to persist before the seal.
	ErrWriterUnsealed = errors.New("rl: the capture writer was offered a seal commit before it sealed")
	// ErrWriterFileName reports a file name that is not a plain file name: the
	// writer persists inside its own directory and refuses any name that would
	// reach out of it.
	ErrWriterFileName = errors.New("rl: the capture writer file name is not a plain file name")
	// ErrWriterOpenBound reports a file the writer refuses to open because its
	// open file bound is held. The refusal is a write loss and is never met by
	// closing another file's history.
	ErrWriterOpenBound = errors.New("rl: the capture writer open file bound is reached")
)

// CaptureWriter is the single durable write point of one capture: the queue
// drains its record lines into it and the seal of the capture is what this
// writer confirms.
//
// The redline list (4 rules):
// MUST report the digest entry of every file it actually wrote, in file name order.
// MUST report its own sync result at seal time.
// MUST NOT infer a sync result from record counts or file digests.
// MUST NOT satisfy a bound by removing or rotating what is already stored.
type CaptureWriter interface {
	// Append persists one record line under the named file. A line the writer
	// cannot persist is reported as an error and counted as a write loss by the
	// caller, and a bound reached on the way is refused rather than satisfied by
	// removing or rotating what is already stored.
	Append(name string, line []byte) error
	// Seal closes and syncs every file the writer holds and reports the seal
	// evidence of its own: the per-file digest entries of the bytes actually
	// written, in file name order, and the sync result the writer observed.
	Seal() ([]CaptureFileDigest, WriterSyncResult)
	// Commit persists the confirmed manifest so a restarted reader reads the
	// capture's state back instead of guessing it.
	Commit(manifest CaptureManifest) error
}

// FileCaptureWriter is the real durable writer of one capture: it appends
// record lines to files under its directory, closes and syncs those files at
// seal time and persists the confirmed seal there. Every digest it reports is
// the digest of the bytes it wrote to storage, and no path it opens escapes its
// own directory.
type FileCaptureWriter struct {
	dir     string
	maxOpen int
	files   map[string]*writerFile
	sealed  bool
}

// writerFile is one open file of a FileCaptureWriter: its handle, the digest of
// the bytes written through it and the byte and record counts of those writes.
type writerFile struct {
	file    *os.File
	digest  hash.Hash
	bytes   int64
	records int64
}

// NewFileCaptureWriter returns the real writer rooted at dir. The open file
// bound is clamped to MaxOpenFilesCap: a bound above the cap is clamped rather
// than obeyed, a negative bound is refused rather than read as unlimited, and
// no bound opens a second write point beside this one.
func NewFileCaptureWriter(dir string, maxOpenFiles int) (*FileCaptureWriter, error) {
	if maxOpenFiles < 0 {
		return nil, ErrNegativeBound
	}
	if maxOpenFiles == 0 {
		maxOpenFiles = DefaultMaxOpenFiles
	}
	if maxOpenFiles > MaxOpenFilesCap {
		maxOpenFiles = MaxOpenFilesCap
	}
	return &FileCaptureWriter{dir: dir, maxOpen: maxOpenFiles, files: make(map[string]*writerFile)}, nil
}

// Append persists one record line under the named file inside the writer's
// directory. A sealed writer refuses the line, a name that would reach out of
// the directory is refused, and a file beyond the open file bound is refused as
// a write loss with ErrWriterOpenBound.
func (w *FileCaptureWriter) Append(name string, line []byte) error {
	if w.sealed {
		return ErrWriterSealed
	}
	if name == "" || name == "." || name == ".." || name != filepath.Base(name) {
		return fmt.Errorf("%w: %q", ErrWriterFileName, name)
	}
	entry := w.files[name]
	if entry == nil {
		if len(w.files) >= w.maxOpen {
			return fmt.Errorf("%w: %d open files, refusing %q", ErrWriterOpenBound, w.maxOpen, name)
		}
		file, err := os.OpenFile(filepath.Join(w.dir, name), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return err
		}
		entry = &writerFile{file: file, digest: sha256.New()}
		w.files[name] = entry
	}
	payload := append(append([]byte(nil), line...), '\n')
	written, err := entry.file.Write(payload)
	entry.digest.Write(payload[:written])
	entry.bytes += int64(written)
	if err != nil {
		return err
	}
	entry.records++
	return nil
}

// Seal closes and syncs every file the writer holds and reports the seal
// evidence of its own: the per-file digest entries of the bytes actually
// written, in file name order so the same capture seals to the same account,
// and the sync result the writer observed. A sync that did not complete is
// reported with its reason and is never presented as a completed sync.
func (w *FileCaptureWriter) Seal() ([]CaptureFileDigest, WriterSyncResult) {
	w.sealed = true
	names := make([]string, 0, len(w.files))
	for name := range w.files {
		names = append(names, name)
	}
	sort.Strings(names)
	files := make([]CaptureFileDigest, 0, len(names))
	sync := WriterSyncResult{At: time.Now()}
	for _, name := range names {
		entry := w.files[name]
		if err := entry.file.Sync(); err != nil && sync.Reason == "" {
			sync.Reason = fmt.Sprintf("%s: %v", name, err)
		}
		if err := entry.file.Close(); err != nil && sync.Reason == "" {
			sync.Reason = fmt.Sprintf("%s: %v", name, err)
		}
		files = append(files, CaptureFileDigest{
			Name:    name,
			SHA256:  hex.EncodeToString(entry.digest.Sum(nil)),
			Bytes:   entry.bytes,
			Records: entry.records,
		})
	}
	sync.Synced = sync.Reason == ""
	return files, sync
}

// Commit persists the confirmed manifest under SealFileName in the writer's
// directory and syncs it to storage. A seal that is not persisted leaves a
// restarted reader with the partial state of a capture whose writer never
// sealed, so the commit is part of the seal rather than an afterthought.
func (w *FileCaptureWriter) Commit(manifest CaptureManifest) error {
	if !w.sealed {
		return ErrWriterUnsealed
	}
	content, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(w.dir, SealFileName), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(content); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

// ReadCaptureSeal reads the state of one capture back from the writer's
// directory: the persisted writer seal when there is one, and the explicit
// partial state of a capture whose writer never sealed when there is not. A
// capture read back without a seal is partial with its reason and its counts
// reported unknown, never complete and never zero-loss.
func ReadCaptureSeal(dir string) (CaptureManifest, error) {
	content, err := os.ReadFile(filepath.Join(dir, SealFileName))
	if errors.Is(err, os.ErrNotExist) {
		return CaptureManifest{
			Sealed:  false,
			Partial: true,
			Reasons: []string{"no writer seal: the capture was read back without a writer-confirmed seal, and its record counts are unknown"},
		}, nil
	}
	if err != nil {
		return CaptureManifest{}, err
	}
	var manifest CaptureManifest
	if err := json.Unmarshal(content, &manifest); err != nil {
		return CaptureManifest{}, fmt.Errorf("rl: read back the capture seal under %s: %w", dir, err)
	}
	return manifest, nil
}
