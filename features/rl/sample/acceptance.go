package sample

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/bench"
	"github.com/SpellingDragon/tagent/features/rl"
)

// 契约: docs/wiki/rl/rl-architecture.md#trajectory-capture
//
// Accept is the independent acceptance of one training sample: the verdict is
// reached by running the bench task's verifier over the task's artifacts, never
// from the execution path's self-report. The acceptance annex it returns carries
// the verdict, the acceptance rule version and the artifact references the
// verdict read, bound to the attempt and the bench task together.
//
// A task the verifier rejects is not an error here: the sample is a valid
// negative sample and its verdict is recorded as fail. The execution path's
// claim of success or failure is not an input to this function at all, so a
// self-report that contradicts the artifacts cannot change the verdict.
func Accept(attemptID event.AttemptID, task bench.Task, workspace, output string) rl.AnnexRecord {
	verdict := VerdictPass
	if err := task.Verify(workspace, output); err != nil {
		verdict = VerdictFail
	}
	return rl.AnnexRecord{
		AttemptID:   attemptID,
		TaskID:      task.ID,
		Verdict:     verdict,
		RuleVersion: RuleVersionV1,
		Artifacts:   artifactRefs(task.ID, workspace, output),
	}
}

// artifactRefs names what the verdict read: every file in the task workspace
// relative to that workspace, and the final response by its digest rather than
// its bytes. A workspace that cannot be listed still yields the task's
// workspace reference; the verifier already judged the missing evidence, and
// the annex never hides the task it judged.
func artifactRefs(taskID, workspace, output string) []string {
	refs := []string{}
	if workspace != "" {
		_ = filepath.Walk(workspace, func(path string, info os.FileInfo, err error) error {
			if err != nil || info == nil || info.IsDir() {
				return nil
			}
			rel, relErr := filepath.Rel(workspace, path)
			if relErr != nil {
				return nil
			}
			refs = append(refs, fmt.Sprintf("artifact:%s/workspace/%s", taskID, filepath.ToSlash(rel)))
			return nil
		})
		if len(refs) == 0 {
			refs = append(refs, fmt.Sprintf("artifact:%s/workspace", taskID))
		}
	}
	if output != "" {
		sum := sha256.Sum256([]byte(output))
		refs = append(refs, fmt.Sprintf("artifact:%s/response/sha256:%s", taskID, hex.EncodeToString(sum[:])))
	}
	sort.Strings(refs)
	return refs
}
