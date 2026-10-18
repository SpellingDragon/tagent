package workspaceexample

import (
	"context"
	"errors"
	"fmt"

	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/workspace"
)

// errReplicaWrite is what the replica writer behind the canonical externalizer reports for a
// write that did not land.
var errReplicaWrite = errors.New("workspace: the replica could not be written")

// keptOriginals is the canonical externalizer: it commits the durable original before the
// replica or the preview exists, and a replica write that fails is reported beside the
// still-effective reference instead of discarding the original.
type keptOriginals struct {
	originals map[string][]byte
	replica   func(memory.OriginalRef, []byte) error
}

// Externalize commits body as the durable original of ref and returns the bounded preview
// with the segment entry for it.
func (e *keptOriginals) Externalize(_ context.Context, ref memory.OriginalRef, body []byte) (workspace.Externalized, error) {
	if ref.Key == "" {
		return workspace.Externalized{}, memory.ErrKeyRequired
	}
	if e.originals == nil {
		e.originals = map[string][]byte{}
	}
	e.originals[string(ref.Key)] = append([]byte(nil), body...)

	preview := body
	if len(preview) > 64 {
		preview = preview[:64]
	}
	out := workspace.Externalized{
		Original: ref,
		Preview:  string(preview),
		Segment:  memory.SegmentRequest{MaxBytes: memory.DefaultSegmentMaxBytes},
	}
	if err := e.replica(ref, body); err != nil {
		return out, fmt.Errorf("%w: %w", workspace.ErrReplicaWriteFailed, err)
	}
	return out, nil
}

var _ workspace.Externalizer = (*keptOriginals)(nil)

// ExampleExternalizer commits one oversized result: the original is kept first, the returned
// preview is bounded while the segment entry still reaches the whole result, and a replica
// write that failed comes back as ErrReplicaWriteFailed with the reference still effective.
//
// 契约: docs/wiki/agent/compression-and-telemetry.md#output-overflow
func ExampleExternalizer() {
	ctx := context.Background()
	externalizer := &keptOriginals{replica: func(memory.OriginalRef, []byte) error { return errReplicaWrite }}
	body := make([]byte, 128)
	ref := memory.OriginalRef{Key: "blob-1", Format: "text/plain"}

	out, err := externalizer.Externalize(ctx, ref, body)
	if !errors.Is(err, workspace.ErrReplicaWriteFailed) {
		return
	}
	if out.Original != ref || len(out.Preview) > 64 || out.Segment.MaxBytes == 0 {
		return
	}
	if len(externalizer.originals["blob-1"]) != len(body) {
		return
	}
}
