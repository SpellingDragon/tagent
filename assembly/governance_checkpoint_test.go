// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
package assembly

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/durable"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/features/evolution/governance"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/runtime/task/approval"

	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"
)

// governanceSliceState is the governance state one checkpoint states, as the
// production boot writes it: the layout version, the tail of each governance
// stream, the goals kept in force and the approval book. A tail numbers the
// history its stream keeps, so Records is the field a check reads to hold one cut
// to stating no record of that history.
type governanceSliceState struct {
	Version   int               `json:"version"`
	DenialSeq int64             `json:"denial_seq"`
	GoalSeq   int64             `json:"goal_seq"`
	Records   []json.RawMessage `json:"records"`
	Approvals []struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	} `json:"approvals"`
}

// governanceDoc writes one host document whose governance gate declares the risk
// table this check grades calls under, and reports the source path and the root
// the document keeps its storage under.
func governanceDoc(t *testing.T) (configPath, root, document string) {
	t.Helper()
	root = t.TempDir()
	configPath = filepath.Join(root, "tagent.yaml")
	document = generationDocument(root, "http://127.0.0.1:1/v1", "",
		"governance:\n  risk_rules:\n"+
			"    - { tool: exec, level: high }\n"+
			"    - { tool: read, level: high }\n"+
			"    - { tool: send, level: high }\n")
	return configPath, root, document
}

// bootGovernanceHost writes one document and boots the host over it.
func bootGovernanceHost(t *testing.T, configPath, document string, opts ...HostOption) *Host {
	t.Helper()
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
	if err := os.WriteFile(configPath, []byte(document), 0o644); err != nil {
		t.Fatalf("write the configuration source: %v", err)
	}
	host, err := Boot(configPath, opts...)
	if err != nil {
		t.Fatalf("boot the host: %v", err)
	}
	return host
}

// judgeGradedCall rules one call of the declared tool through the governance face
// the run carries, so the record the ruling leaves is the production ruling's.
func judgeGradedCall(t *testing.T, run *Run, tool string) {
	t.Helper()
	governor := run.governorInEffect()
	if governor == nil {
		t.Fatal("governance face of the booted host = none, want the policy the declared risk table opened")
	}
	if _, err := governor.Judge(plugin.CallPass{Final: trpcmodel.ToolCall{
		ID:       "call-" + tool,
		Function: trpcmodel.FunctionDefinitionParam{Name: tool},
	}}); err != nil {
		t.Fatalf("rule one call of %s: %v", tool, err)
	}
}

// seedGovernanceChain commits one turn of a session chain, so the run carries the
// owner its framework cut names.
func seedGovernanceChain(t *testing.T, run *Run, session string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	commitWindowTurns(t, ctx, run, event.Address{AgentID: "user", SessionID: event.SessionID(session)}, 1)
}

// governanceSliceOf cuts one checkpoint through the run's own entry and reports the
// governance payload it states, so a check reads the production cut and never a
// slice the check assembled.
func governanceSliceOf(t *testing.T, run *Run) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	key, filed, err := run.Checkpoint(ctx)
	if err != nil || !filed {
		t.Fatalf("cut one checkpoint: filed=%v err=%v", filed, err)
	}
	stored, err := run.CommittedEvent(ctx, key)
	if err != nil {
		t.Fatalf("read the checkpoint %s back: %v", key, err)
	}
	checkpoint, stated := event.CheckpointOf(stored)
	if !stated {
		t.Fatalf("the fact %s states no readable checkpoint", key)
	}
	for _, slice := range checkpoint.Slices {
		if slice.Name != "governance" {
			continue
		}
		return []byte(slice.Payload)
	}
	t.Fatalf("the checkpoint %s names the slices %v, want the governance state among them",
		key, checkpoint.CheckpointSliceNames())
	return nil
}

// governanceStateRead decodes the governance state one stated cut payload carries.
func governanceStateRead(t *testing.T, payload []byte) governanceSliceState {
	t.Helper()
	var state governanceSliceState
	if err := json.Unmarshal(payload, &state); err != nil {
		t.Fatalf("the governance slice states no readable state: %v (%s)", err, payload)
	}
	return state
}

// governanceRulingsOf restores one production cut beside the durable face the hosts
// wrote and binds the two, so the rulings the cut numbers are read the way a restart
// reads them: the cut states the tail and the stream behind it states the records.
func governanceRulingsOf(t *testing.T, ctx context.Context, cut []byte, log durable.FactLog) []governance.DenialRecord {
	t.Helper()
	fromCut := governance.NewDenialLedger()
	seat := governance.NewStateSeat(fromCut, nil, nil)
	if err := seat.RestoreState(ctx, cut); err != nil {
		t.Fatalf("restore the governance cut the restart filed: %v", err)
	}
	if err := seat.Bind(ctx, log); err != nil {
		t.Fatalf("bind the stream the cut numbers: %v", err)
	}
	return fromCut.Query(0)
}

// TestGovernanceSliceWiredInProduction pins the governance state the production boot seats.
//   - positive: the real host mounts the governance slice on the run's state seam, and its cut numbers the two rulings the gate recorded beside no record
//   - negative: a restarted host reads that cut instead of scanning the stream, so its streams continue past the keys already written and no key is re-keyed
//   - the restart reads the cut the first process stated: the tail past the two denial events, and the rulings that stream keeps in the order the gate recorded
//   - consumer: the boot entry seats the ledger and the goal registry the governance gate rules through, and one restart restores them
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestGovernanceSliceWiredInProduction(t *testing.T) {
	configPath, root, document := governanceDoc(t)

	first := bootGovernanceHost(t, configPath, document)
	run := first.Run()
	if names := run.StateRegistry().Names(); !slices.Contains(names, "governance") {
		t.Fatalf("state slices of the booted host = %v, want the governance state among them", names)
	}
	seedGovernanceChain(t, run, "session-governance-cut")
	judgeGradedCall(t, run, "exec")
	judgeGradedCall(t, run, "read")
	cut := governanceSliceOf(t, run)
	state := governanceStateRead(t, cut)
	if state.Version == 0 || state.DenialSeq != 2 || len(state.Records) != 0 {
		t.Fatalf("governance state of the first cut = version %d tail %d over %d records, want the layout this code reads and the tail of the two rulings beside no record the cut keeps",
			state.Version, state.DenialSeq, len(state.Records))
	}
	closeLiveness(t, first)

	second := bootGovernanceHost(t, configPath, document)
	restarted := second.Run()
	if names := restarted.StateRegistry().Names(); !slices.Contains(names, "governance") {
		t.Fatalf("state slices of the restarted host = %v, want the governance state among them", names)
	}
	judgeGradedCall(t, restarted, "send")
	restatedCut := governanceSliceOf(t, restarted)
	restated := governanceStateRead(t, restatedCut)
	if restated.DenialSeq != 3 || len(restated.Records) != 0 {
		t.Fatalf("governance state beside the restart = tail %d over %d records, want the tail past the two restored rulings and the one it ruled",
			restated.DenialSeq, len(restated.Records))
	}
	closeLiveness(t, second)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tier, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: filepath.Join(root, "storage")})
	if err != nil {
		t.Fatalf("open the tier the two hosts wrote: %v", err)
	}
	stored, err := ledger.TierFactLog(tier).Scan(ctx, "tagent/governance/v1/denial:")
	if err != nil {
		t.Fatalf("scan the governance stream: %v", err)
	}
	keys := make([]string, 0, len(stored))
	for key := range stored {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	want := []string{
		"tagent/governance/v1/denial:0000000000000000001",
		"tagent/governance/v1/denial:0000000000000000002",
		"tagent/governance/v1/denial:0000000000000000003",
	}
	if strings.Join(keys, " ") != strings.Join(want, " ") {
		t.Fatalf("stored denial events = %v, want the three the two processes wrote under their own keys: the keys the two processes wrote are three in one stream, and a stream that reset its numbering would re-key the third ruling over an event already stored", keys)
	}
	rulings := governanceRulingsOf(t, ctx, restatedCut, ledger.TierFactLog(tier))
	wantRulings := []string{"send", "read", "exec"}
	if len(rulings) != len(wantRulings) {
		t.Fatalf("rulings read beside the cut the restart filed = %d, want the %d the gate recorded", len(rulings), len(wantRulings))
	}
	for index, tool := range wantRulings {
		if rulings[index].ToolName != tool || rulings[index].Subtype != governance.SubtypeAudit {
			t.Fatalf("ruling %d read beside the cut = %+v, want the %s record of %s: the cut numbers the stream and the stream states the records in the order the gate recorded them",
				index, rulings[index], governance.SubtypeAudit, tool)
		}
	}
}

// TestGovernanceApprovalBootReadsStateSlice pins the approval book of a boot that states a governance cut.
//   - positive: the restarted host reports the hold and the ruling the stated book carries
//   - negative: the boot reads the stated book, so a process whose request files are gone still reports them
//   - the request files are the per-request durable write beside the stated book, so the files are gone by the time the boot reads
//   - consumer: the approval channel a real boot wires answers its presence from the restored state
//
// 契约: docs/wiki/agent/governance-enforcement.md#approval-channel
func TestGovernanceApprovalBootReadsStateSlice(t *testing.T) {
	configPath, root, document := governanceDoc(t)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	book := approval.NewApprovalManager(filepath.Join(root, "approvals"), 0, approval.WithApprovalSeat())
	first := bootGovernanceHost(t, configPath, document, WithApprovals(book))
	held, err := book.Request(ctx, "approval-production-1", "exec", []byte(`{"cmd":"deploy"}`), plugin.RiskCritical, "no-deploy", "critical risk awaits approval")
	if err != nil {
		t.Fatalf("park one critical call through the approval channel: %v", err)
	}
	seedGovernanceChain(t, first.Run(), "session-governance-approval")
	state := governanceStateRead(t, governanceSliceOf(t, first.Run()))
	if len(state.Approvals) != 1 || state.Approvals[0].ID != held.ID || state.Approvals[0].Status != string(approval.ApprovalPending) {
		t.Fatalf("stated approvals = %+v, want the hold the channel parked", state.Approvals)
	}
	closeLiveness(t, first)

	if err := os.RemoveAll(filepath.Join(root, "approvals")); err != nil {
		t.Fatalf("withdraw the request files under %s: %v", root, err)
	}
	restartedBook := approval.NewApprovalManager(filepath.Join(root, "approvals"), 0, approval.WithApprovalSeat())
	second := bootGovernanceHost(t, configPath, document, WithApprovals(restartedBook))
	defer closeLiveness(t, second)
	presence := restartedBook.Pending()
	if len(presence) != 1 || presence[0].ID != held.ID {
		t.Fatalf("pending approvals beside the restart = %+v, want the hold the stated book carries", presence)
	}
	if names := second.Run().StateRegistry().Names(); !slices.Contains(names, "governance") {
		t.Fatalf("state slices of the restarted host = %v, want the governance state among them", names)
	}
}

// TestGovernanceBootRefusesUnreadableStateCut pins the refusal of a governance cut this code cannot read.
//   - positive: the cut a boot restored before the governance state was mounted lands on the mount, and the state seam names the slice it could not restore
//   - negative: an unreadable cut stops the boot instead of restoring an empty state the streams would then re-key over
//   - consumer: the production seating entry answers that refusal, so no runtime starts beside a governance state it could not read
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestGovernanceBootRefusesUnreadableStateCut(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	run := windowChainRun(t, ctx)
	unreadable := event.CheckpointSlice{Name: "governance", Payload: json.RawMessage(`{"version":99}`)}
	if err := run.StateRegistry().RestoreState(ctx, []event.CheckpointSlice{unreadable}); err != nil {
		t.Fatalf("state the cut before the governance state is mounted: %v", err)
	}
	seat := governance.NewStateSeat(governance.NewDenialLedger(), governance.NewGoalRegistry(), nil)
	err := (&Host{run: run}).seatGovernance(ctx, seat)
	if err == nil {
		t.Fatal("seat the governance state beside an unreadable cut = nil error, want the refusal by name")
	}
	for _, named := range []string{"governance", "layout"} {
		if !strings.Contains(err.Error(), named) {
			t.Fatalf("the refusal beside an unreadable cut = %v, want %q named", err, named)
		}
	}
	if names := run.StateRegistry().Names(); slices.Contains(names, "governance") {
		t.Fatalf("state slices after the refused mount = %v, want the governance state unmounted", names)
	}
}
