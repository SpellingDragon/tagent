// 契约: docs/wiki/rl/rl-architecture.md#trajectory-capture
//
// Package sample is the training sample loop of one rl capture: it reads a
// fixed request back through its snapshot reference, judges the task's
// artifacts independently of the execution path's self-report, and evaluates
// whether one sample carries all three elements before the capture seals.
//
// The package is the consumer of the rl record vocabulary: it writes no record
// of its own kind, derives no reward, and never reads the execution path's
// self-report as an acceptance verdict.
package sample

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/features/rl"
)

// readback reads one fixed request snapshot back through its reference and
// reconstructs the request it fixed. The snapshot content is rechecked against
// the reference digest at the read entry, and the reconstructed request is
// reconciled a second time: the request shape is rebuilt into a snapshot and
// its digest is compared with the recorded reference digest, so a file that
// merely exists is never accepted as a read-back proof.
//
// A mismatch, a missing snapshot or an unreadable content is returned as the
// rl error that names it; no reconstructed request is returned beside it.
func readback(capture *rl.TrajectoryCapture, ref rl.SnapshotRef) (model.Request, error) {
	if capture == nil {
		return model.Request{}, fmt.Errorf("%w: the read-back has no capture", rl.ErrCaptureSnapshotMissing)
	}
	snapshot, err := capture.SnapshotOf(ref)
	if err != nil {
		return model.Request{}, err
	}
	request, err := requestOf(snapshot)
	if err != nil {
		return model.Request{}, err
	}
	rebuilt, err := snapshotOf(snapshot, request)
	if err != nil {
		return model.Request{}, err
	}
	content, err := rl.MarshalSnapshot(rebuilt)
	if err != nil {
		return model.Request{}, err
	}
	sum := sha256.Sum256(content)
	if got := hex.EncodeToString(sum[:]); got != ref.Digest {
		return model.Request{}, fmt.Errorf("%w: reconstructed request digest %s, reference %s", rl.ErrSnapshotMismatch, got, ref.Digest)
	}
	return request, nil
}

// requestOf rebuilds the model request a safe snapshot fixed: the messages
// verbatim, the generation parameters, the provider extra fields and the
// headers. The tool schemas stay beside the request, because a schema is not a
// tool implementation and rebuilding one would invent a tool the request never
// carried; the structured output declaration is carried in its recorded raw
// form for the same reason, since api/model exposes no second declaration type
// to rebuild it through.
func requestOf(snapshot rl.RequestSnapshot) (model.Request, error) {
	request := model.Request{
		Messages:    snapshot.Messages,
		ExtraFields: snapshot.ExtraFields,
		Headers:     snapshot.Headers,
	}
	if len(snapshot.Parameters) > 0 {
		if err := json.Unmarshal(snapshot.Parameters, &request.GenerationConfig); err != nil {
			return model.Request{}, fmt.Errorf("%w: generation parameters: %v", rl.ErrSnapshotUnreadable, err)
		}
	}
	return request, nil
}

// snapshotOf rebuilds the snapshot of one reconstructed request: the request
// material is re-encoded from the request, while the view members and the tool
// schemas come from the read-back snapshot because neither is reconstructed
// from a request. A rebuilt snapshot is the shape a second digest check reads.
func snapshotOf(readback rl.RequestSnapshot, request model.Request) (rl.RequestSnapshot, error) {
	parameters, err := json.Marshal(request.GenerationConfig)
	if err != nil {
		return rl.RequestSnapshot{}, fmt.Errorf("%w: generation parameters: %v", rl.ErrSnapshotUnreadable, err)
	}
	rebuilt := rl.RequestSnapshot{
		Model:            readback.Model,
		ModelVersion:     readback.ModelVersion,
		PromptVersion:    readback.PromptVersion,
		VisibleTools:     readback.VisibleTools,
		VisibleSkills:    readback.VisibleSkills,
		Parameters:       parameters,
		StructuredOutput: readback.StructuredOutput,
		Messages:         request.Messages,
		ToolSchemas:      readback.ToolSchemas,
		ExtraFields:      request.ExtraFields,
		Headers:          request.Headers,
	}
	return rebuilt, nil
}
