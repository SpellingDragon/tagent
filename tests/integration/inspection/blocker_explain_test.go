package inspection

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/memory/query"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"
	tagentexec "github.com/SpellingDragon/tagent/tool/exec"

	"trpc.group/trpc-go/trpc-agent-go/skill"

	"github.com/SpellingDragon/tagent/memory/ledger"
)

// bxExecutor is the deterministic execution face of these sub-cases: it owns
// the sessions it starts and reports exactly what it observed, so the retention
// assertions rest on real manager bookkeeping rather than on fabricated facts.
type bxExecutor struct {
	mu        sync.Mutex
	confirmed bool
	probes    []tagentexec.ProbeState
	live      map[tagentexec.ResourceKey]bool
}

// bxNewExecutor returns the fake executor beside a confirmed stop default.
func bxNewExecutor() *bxExecutor {
	return &bxExecutor{confirmed: true, live: map[tagentexec.ResourceKey]bool{}}
}

// Start opens the session the declaration names.
func (e *bxExecutor) Start(_ context.Context, decl tagentexec.ExecutionDecl) (tagentexec.ResourceKey, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.live[decl.Resource] = true
	return decl.Resource, nil
}

// Probe reports the scripted observation where one is queued and the session
// state it holds otherwise.
func (e *bxExecutor) Probe(_ context.Context, key tagentexec.ResourceKey) (tagentexec.ProbeResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.probes) > 0 {
		observed := e.probes[0]
		e.probes = e.probes[1:]
		return tagentexec.ProbeResult{State: observed, Detail: "scripted observation"}, nil
	}
	if e.live[key] {
		return tagentexec.ProbeResult{State: tagentexec.ProbeAlive}, nil
	}
	return tagentexec.ProbeResult{State: tagentexec.ProbeDead, Detail: "no session behind this key"}, nil
}

// Capture returns the frame recorded for the session.
func (e *bxExecutor) Capture(_ context.Context, key tagentexec.ResourceKey) ([]byte, error) {
	return []byte("frame:" + key.Key), nil
}

// Stop ends the session behind the key and reports the evidence observed for
// it, keeping the session where no exit was observed.
func (e *bxExecutor) Stop(_ context.Context, key tagentexec.ResourceKey) (tagentexec.StopEvidence, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.confirmed {
		return tagentexec.StopEvidence{Resource: key, Confirmed: false, Reason: "the execution reported no exit"}, nil
	}
	delete(e.live, key)
	return tagentexec.StopEvidence{Resource: key, Confirmed: true, ExitCode: 0}, nil
}

// bxExecDecl returns the execution declaration one resource of these sub-cases
// runs under.
func bxExecDecl(key tagentexec.ResourceKey) tagentexec.ExecutionDecl {
	return tagentexec.ExecutionDecl{Command: "probe", Args: []string{"one"}, Resource: key}
}

// bxCheckRetention asserts one lease retention reads as the execution ledger
// records it, field by field, beside the reason of an unconfirmed stop.
func bxCheckRetention(t *testing.T, got query.LeaseRetention, want tagentexec.Explanation) {
	t.Helper()
	wantReason := ""
	if want.LastStop != nil && !want.LastStop.Confirmed {
		wantReason = want.LastStop.Reason
	}
	if got.Held != want.Held || got.Deadline != want.Deadline || got.Renewed != want.Renewed ||
		got.Adopted != want.Adopted || got.ConsecutiveUnknowns != want.ConsecutiveUnknowns ||
		got.Escalations != want.Escalations || got.StopReason != wantReason ||
		!reflect.DeepEqual(got.Obligations, append([]string(nil), want.Obligations...)) {
		t.Fatalf("retention %+v disagrees with the ledger explanation %+v (stop reason %q)", got, want, wantReason)
	}
}

// bxRetention locates one lease retention by the resource its registration
// names.
func bxRetention(list []query.LeaseRetention, kind, key string) (query.LeaseRetention, bool) {
	for _, retention := range list {
		if retention.Ref.Kind == kind && retention.Ref.Key == key {
			return retention, true
		}
	}
	return query.LeaseRetention{}, false
}

// bxLeaseBlockers lists the blockers that sit on one lease registration.
func bxLeaseBlockers(list []query.Blocker, ref state.LeaseRef) []query.Blocker {
	var out []query.Blocker
	for _, blocker := range list {
		if blocker.Ref.Kind == query.ObjectLease && blocker.Ref.Lease == ref {
			out = append(out, blocker)
		}
	}
	return out
}

// bxTaskBlockers lists the blockers that sit on one task object.
func bxTaskBlockers(list []query.Blocker, taskID event.TaskID) []query.Blocker {
	var out []query.Blocker
	for _, blocker := range list {
		if blocker.Ref.Kind == query.ObjectTask && blocker.Ref.TaskID == taskID {
			out = append(out, blocker)
		}
	}
	return out
}

// bxOpenOf lists the open obligations of one task object.
func bxOpenOf(list []query.OpenObligation, taskID event.TaskID) []query.OpenObligation {
	var out []query.OpenObligation
	for _, open := range list {
		if open.Ref.Kind == query.ObjectTask && open.Ref.TaskID == taskID {
			out = append(out, open)
		}
	}
	return out
}

// bxUnreadableTasks is the task reading face that refuses every lookup, so a
// storage failure beside the task dimension is observable by name.
type bxUnreadableTasks struct{}

// Lookup refuses the read by name.
func (bxUnreadableTasks) Lookup(context.Context, event.TaskID) (task.Spec, task.Status, error) {
	return task.Spec{}, task.StatusUnknown, errors.New("the task store refused the read")
}

// bxOwners is the binding resolution of one cold start: it resolves the target
// a chain names to itself, so the restore rebuilds every obligation beside the
// target its declaration froze.
type bxOwners struct{}

// OwnerOf resolves the target under the current binding.
func (bxOwners) OwnerOf(_ context.Context, _ event.AgentID, target event.AgentID) (event.AgentID, error) {
	return target, nil
}

// bxModel is the deterministic model provider of the root-wiring sub-case: it
// answers one scripted turn and records the request it was driven with.
type bxModel struct {
	mu   sync.Mutex
	name string
}

// GenerateContent answers the scripted terminal response of this provider.
func (m *bxModel) GenerateContent(context.Context, *model.Request) (<-chan *model.Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ch := make(chan *model.Response, 1)
	ch <- &model.Response{
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "done"}}},
		Done:    true,
	}
	close(ch)
	return ch, nil
}

// Info reports the fake provider's descriptor.
func (m *bxModel) Info() model.Info {
	return model.Info{Name: m.name}
}

// bxPolicy is the deterministic governance of the root-wiring sub-case: the
// fixed passing dispositions are mechanism inputs carrying no quality
// conclusion.
type bxPolicy struct{}

// Check implements state.Governance with one passing disposition per required
// check.
func (bxPolicy) Check(context.Context, state.StatePlan) (map[state.CheckName]state.CheckAnswer, error) {
	return map[state.CheckName]state.CheckAnswer{
		"eval":       bxPassingCheck("eval"),
		"benchmark":  bxPassingCheck("benchmark"),
		"apply-gate": bxPassingCheck("apply-gate"),
	}, nil
}

// bxPassingCheck reports the passing disposition of one named mechanism check.
func bxPassingCheck(name state.CheckName) state.CheckAnswer {
	return state.CheckAnswer{Name: name, Verdict: state.CheckPass, Reason: "the stand-in passes the mechanism check", Scope: "assembly", Evidence: []string{"deterministic stand-in"}}
}

// bxDeclaration declares one agent assembled from the prompt and skill files
// under the given root.
func bxDeclaration(root string) *config.Declaration {
	return &config.Declaration{
		Entry: "agent-a",
		Agents: []config.AgentSpec{{
			ID:    "agent-a",
			Model: "model-a",
			Files: []config.FileRef{
				{Path: filepath.Join(root, "prompts", "system.md"), Kind: config.FileKindPrompt},
				{Path: filepath.Join(root, "skills", "greet", skill.SkillFile), Kind: config.FileKindSkill},
			},
			Params: map[string]float64{},
		}},
	}
}

// bxWritePrompt writes one prompt file under the given root.
func bxWritePrompt(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create the prompt directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write the prompt file: %v", err)
	}
}

// bxWriteSkill writes one skill folder with its SKILL.md under the given root,
// in the shape the framework skill repository scans and parses.
func bxWriteSkill(t *testing.T, root, name, body string) {
	t.Helper()
	dir := filepath.Join(root, "skills", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create the skill directory: %v", err)
	}
	content := "---\nname: " + name + "\ndescription: \"greet the user\"\n---\n\n" + body
	if err := os.WriteFile(filepath.Join(dir, skill.SkillFile), []byte(content), 0o644); err != nil {
		t.Fatalf("write the skill file: %v", err)
	}
}

// bxSourceDimensions collects every named dimension one joined source refusal
// reports, so a failure is checked against the dimensions it affects.
func bxSourceDimensions(err error) []query.ObjectKind {
	var dims []query.ObjectKind
	var walk func(error)
	walk = func(e error) {
		if e == nil {
			return
		}
		var sourceErr *query.SourceError
		if errors.As(e, &sourceErr) {
			dims = append(dims, sourceErr.Dimension)
		}
		if joined, ok := e.(interface{ Unwrap() []error }); ok {
			for _, sub := range joined.Unwrap() {
				walk(sub)
			}
		}
	}
	walk(err)
	return dims
}

// TestExplainRuntimeBlockers proves the runtime blocker explanation read face over a real runtime assembly.
// - 未止义务与阻塞具名可解释: every blocker names its object, its owner-recorded reason and the party that advances it
// - 租约保留与计数面可解释: every registered lease reads beside the retention facts and the accounting the execution ledger holds
// - 连续 unknown 不作判死: an undecidable probe run escalates diagnosis beside a retained resource and never decides its death
// - 经授权读面: the authorization filter runs before anything is listed and before anything is counted
// - 不建第二真源: the explanation re-reads the owners at read time and records nothing of its own
// - 重启与存储失败可辨: a restored chain lists its owed obligations beside their advancer and an unreadable source refuses by name
// - 根接线消费租约账: the assembled run explains its leases through the production ledger face
// 契约: docs/wiki/agent/agent-architecture.md#core-components
func TestExplainRuntimeBlockers(t *testing.T) {
	t.Run("S1 未止义务与阻塞具名可解释: every blocker names its object, its reason and its advancer", func(t *testing.T) {
		f := newRuntimeFixture(t)
		explainer := query.NewExplainer(f.inventory, tagent.ExecLeaseExplainer(tagentexec.NewManager(bxNewExecutor())))
		report, err := explainer.Explain(f.ctx, filterAll)
		if err != nil {
			t.Fatalf("Explain: %v", err)
		}
		if len(report.Blockers) != 1 {
			t.Fatalf("blockers = %+v, want the one refused delivery the owner records", report.Blockers)
		}
		blocker := report.Blockers[0]
		if !reflect.DeepEqual(blocker.Ref, taskRef(f)) || blocker.Notice != f.notices[3] {
			t.Fatalf("blocker = %+v, want named on task %s beside notification seq 3", blocker, f.task1)
		}
		if blocker.Reason != f.refusal.Error() || blocker.Advancer != query.AdvancerNoticeTransport || blocker.State != query.AdvanceWaiting {
			t.Fatalf("blocker = %+v, want the owner-recorded reason beside the notice transport as next advancer", blocker)
		}
		owner1, err := f.manager.Obligations(f.ctx, f.task1)
		if err != nil {
			t.Fatalf("Obligations(%s): %v", f.task1, err)
		}
		owner2, err := f.manager.Obligations(f.ctx, f.task2)
		if err != nil {
			t.Fatalf("Obligations(%s): %v", f.task2, err)
		}
		open1 := bxOpenOf(report.Open, f.task1)
		if len(open1) != 1 || open1[0].Obligation != owner1.Open[0] {
			t.Fatalf("task-1 open obligations = %+v, want the owner reading %+v", open1, owner1.Open)
		}
		if open1[0].Obligation.Kind != task.ObligationAwaitingDelivery || open1[0].Obligation.Notice != f.notices[3] || open1[0].Advancer != query.AdvancerNoticeTransport {
			t.Fatalf("task-1 open obligation = %+v, want the owed delivery beside the notice transport as next advancer", open1[0])
		}
		open2 := bxOpenOf(report.Open, f.task2)
		if len(open2) != 1 || open2[0].Obligation != owner2.Open[0] || open2[0].Obligation.Kind != task.ObligationAwaitingTool {
			t.Fatalf("task-2 open obligations = %+v, want the owner admission obligation %+v", open2, owner2.Open)
		}
		if open2[0].Advancer != query.AdvancerExecutor || open2[0].State != query.AdvanceWaiting {
			t.Fatalf("task-2 open obligation = %+v, want the executor named as next advancer", open2[0])
		}
		if report.Counts != (query.BlockerCounts{Blockers: 1, Open: 2, Leases: 2}) {
			t.Fatalf("counts = %+v, want derived from the listed entries", report.Counts)
		}
		if len(report.Leases) != 2 {
			t.Fatalf("lease retentions = %+v, want both registrations explained", report.Leases)
		}
		for _, retention := range report.Leases {
			if retention.Held {
				t.Fatalf("retention %+v, want a registration beside no held resource read as not held", retention)
			}
		}
	})

	t.Run("S2 租约保留与计数面可解释: the retention and the accounting read as the execution ledger records them", func(t *testing.T) {
		f := newRuntimeFixture(t)
		executor := bxNewExecutor()
		manager := tagentexec.NewManager(executor)
		keyA := tagentexec.ResourceKey{Kind: "model", Key: "quota-a"}
		keyB := tagentexec.ResourceKey{Kind: "storage", Key: "slot-b"}
		if _, err := manager.Acquire(f.ctx, bxExecDecl(keyA)); err != nil {
			t.Fatalf("Acquire: %v", err)
		}
		if err := manager.Renew(f.ctx, keyA, time.Minute); err != nil {
			t.Fatalf("Renew: %v", err)
		}
		executor.probes = []tagentexec.ProbeState{tagentexec.ProbeUnknown, tagentexec.ProbeUnknown, tagentexec.ProbeUnknown}
		for i := 0; i < 3; i++ {
			if observed, err := manager.Observe(f.ctx, keyA); err != nil || observed.State != tagentexec.ProbeUnknown {
				t.Fatalf("Observe %d = (%+v, %v), want the undecidable state reported as a state", i, observed, err)
			}
		}
		executor.confirmed = false
		if evidence, err := manager.Release(f.ctx, keyA); err != nil || evidence.Released {
			t.Fatalf("Release %v = (%+v, %v), want the retained resource beside its unconfirmed stop", keyA, evidence, err)
		}
		if _, err := manager.Acquire(f.ctx, bxExecDecl(keyB)); err != nil {
			t.Fatalf("Acquire %v: %v", keyB, err)
		}
		executor.confirmed = true
		if evidence, err := manager.Release(f.ctx, keyB); err != nil || !evidence.Released {
			t.Fatalf("Release %v = (%+v, %v), want the resource freed beside a confirmed stop", keyB, evidence, err)
		}

		explainer := query.NewExplainer(f.inventory, tagent.ExecLeaseExplainer(manager))
		report, err := explainer.Explain(f.ctx, filterAll)
		if err != nil {
			t.Fatalf("Explain: %v", err)
		}
		retentionA, ok := bxRetention(report.Leases, "model", "quota-a")
		if !ok {
			t.Fatalf("retentions = %+v, want the model/quota-a registration listed", report.Leases)
		}
		heldA, err := manager.Explain(keyA)
		if err != nil {
			t.Fatalf("Manager.Explain: %v", err)
		}
		bxCheckRetention(t, retentionA, heldA)
		if !retentionA.Held || retentionA.Renewed != 1 || retentionA.ConsecutiveUnknowns != 3 ||
			retentionA.Escalations != 3 || retentionA.StopReason != "the execution reported no exit" {
			t.Fatalf("retention = %+v, want the held resource beside one renewal, the unknown run and the unconfirmed stop", retentionA)
		}
		ownerRefs := f.leases.Refs()
		if !reflect.DeepEqual(retentionA.Ref, ownerRefs[0]) {
			t.Fatalf("retention registration = %+v, want the owner registration %+v", retentionA.Ref, ownerRefs[0])
		}
		retentionB, ok := bxRetention(report.Leases, "storage", "slot-b")
		if !ok || retentionB.Held {
			t.Fatalf("retention = %+v, want the released registration explained as not held", retentionB)
		}
		bxCheckRetention(t, retentionB, tagentexec.Explanation{Key: keyB, Held: false})

		leaseBlockers := bxLeaseBlockers(report.Blockers, retentionA.Ref)
		if len(leaseBlockers) != 3 {
			t.Fatalf("lease blockers = %+v, want one per open obligation, one per unconfirmed stop and one per unknown run", leaseBlockers)
		}
		diagnosis, unconfirmed, unsettled := false, false, false
		for _, blocker := range leaseBlockers {
			switch {
			case blocker.Advancer == query.AdvancerDiagnosis && strings.Contains(blocker.Reason, "3"):
				diagnosis = true
			case blocker.Advancer == query.AdvancerExplicitStop && blocker.Reason == "the execution reported no exit":
				unconfirmed = true
			case blocker.Advancer == query.AdvancerExplicitStop && blocker.Reason == "the execution outcome is unsettled":
				unsettled = true
			}
			if blocker.Ref.Lease != retentionA.Ref || !reflect.DeepEqual(blocker.Retention, retentionA) {
				t.Fatalf("lease blocker = %+v, want named on the registration beside its retention", blocker)
			}
		}
		if !diagnosis || !unconfirmed || !unsettled {
			t.Fatalf("lease blockers = %+v, want the diagnosis escalation, the unconfirmed stop and the unsettled outcome each named beside its advancer", leaseBlockers)
		}
		if len(bxLeaseBlockers(report.Blockers, retentionB.Ref)) != 0 {
			t.Fatalf("released registration carries blockers = %+v, want none beside a freed resource", report.Blockers)
		}
		if report.Ledger != (query.LeaseLedgerCounts{Held: 1, Acquired: 2, Released: 1, Escalated: 3}) {
			t.Fatalf("ledger accounting = %+v, want one held resource, two acquisitions, one release and three escalations", report.Ledger)
		}
		if report.Ledger.Held != manager.Held() || report.Ledger.Acquired != manager.Acquired() ||
			report.Ledger.Adopted != manager.Adopted() || report.Ledger.Reused != manager.Reused() ||
			report.Ledger.Released != manager.Released() || report.Ledger.Escalated != manager.Escalated() {
			t.Fatalf("ledger accounting = %+v, disagrees with the manager counters", report.Ledger)
		}
		if report.Counts.Blockers != len(report.Blockers) || report.Counts.Open != len(report.Open) || report.Counts.Leases != len(report.Leases) {
			t.Fatalf("counts = %+v, want derived from the listed entries", report.Counts)
		}

		if heldA.Held != true || manager.Released() != 1 {
			t.Fatalf("after the unknown run = (held %v, released %d), want the lease kept beside the diagnosis escalations and nothing freed", heldA.Held, manager.Released())
		}

		executor.confirmed = true
		if evidence, err := manager.Release(f.ctx, keyA); err != nil || !evidence.Released {
			t.Fatalf("Release %v = (%+v, %v), want the resource freed beside the confirmed stop", keyA, evidence, err)
		}
		after, err := explainer.Explain(f.ctx, filterAll)
		if err != nil {
			t.Fatalf("Explain after the release: %v", err)
		}
		retired, ok := bxRetention(after.Leases, "model", "quota-a")
		if !ok || retired.Held {
			t.Fatalf("retention = %+v, want the freed registration still listed and read as not held", retired)
		}
		if after.Ledger.Released != 2 || len(bxLeaseBlockers(after.Blockers, retentionA.Ref)) != 0 {
			t.Fatalf("after the confirmed release = (ledger %+v, blockers %+v), want two releases and no blocker beside the freed resource", after.Ledger, after.Blockers)
		}
	})

	t.Run("S3 经授权读面: the authorization filter runs before anything is listed or counted", func(t *testing.T) {
		f := newRuntimeFixture(t)
		explainer := query.NewExplainer(f.inventory, tagent.ExecLeaseExplainer(tagentexec.NewManager(bxNewExecutor())))
		narrow, err := explainer.Explain(f.ctx, filterA)
		if err != nil {
			t.Fatalf("Explain under the narrowed scope: %v", err)
		}
		for _, open := range narrow.Open {
			if open.Ref.TaskID == f.task2 {
				t.Fatalf("narrowed reading lists %+v, want the agent-c task excluded before anything is listed", open)
			}
		}
		for _, blocker := range narrow.Blockers {
			if blocker.Ref.Address.AgentID == "agent-c" {
				t.Fatalf("narrowed reading lists %+v, want no entry outside the presented scope", blocker)
			}
		}
		if len(bxOpenOf(narrow.Open, f.task2)) != 0 || len(bxOpenOf(narrow.Open, f.task1)) != 1 {
			t.Fatalf("narrowed open obligations = %+v, want the agent-a task's obligation alone", narrow.Open)
		}
		if narrow.Counts != (query.BlockerCounts{Blockers: 1, Open: 1, Leases: 2}) {
			t.Fatalf("narrowed counts = %+v, want counted off the filtered entries alone", narrow.Counts)
		}
		if narrow.Counts.Open != len(narrow.Open) || narrow.Counts.Blockers != len(narrow.Blockers) || narrow.Counts.Leases != len(narrow.Leases) {
			t.Fatalf("narrowed counts = %+v, disagree with the listed entries of the same reading", narrow.Counts)
		}
		if len(narrow.Leases) != 2 {
			t.Fatalf("narrowed retentions = %+v, want both registrations disclosed under a non-empty scope", narrow.Leases)
		}
		full, err := explainer.Explain(f.ctx, filterAll)
		if err != nil {
			t.Fatalf("Explain under the full scope: %v", err)
		}
		if full.Counts.Open != 2 || narrow.Counts.Open >= full.Counts.Open {
			t.Fatalf("full counts = %+v beside narrowed %+v, want the widened scope listing strictly more", full.Counts, narrow.Counts)
		}
		empty, err := explainer.Explain(f.ctx, query.ListFilter{})
		if err != nil {
			t.Fatalf("Explain under an empty scope: %v", err)
		}
		if empty.Counts != (query.BlockerCounts{}) || len(empty.Blockers)+len(empty.Open)+len(empty.Leases) != 0 {
			t.Fatalf("empty-scope reading = %+v, want nothing listed and nothing counted beside no scope", empty)
		}
	})

	t.Run("S4 不建第二真源: the explanation re-reads the owners and records nothing of its own", func(t *testing.T) {
		f := newRuntimeFixture(t)
		explainer := query.NewExplainer(f.inventory, tagent.ExecLeaseExplainer(tagentexec.NewManager(bxNewExecutor())))
		before, err := f.manager.Obligations(f.ctx, f.task1)
		if err != nil {
			t.Fatalf("Obligations(%s): %v", f.task1, err)
		}
		first, err := explainer.Explain(f.ctx, filterAll)
		if err != nil {
			t.Fatalf("Explain: %v", err)
		}
		second, err := explainer.Explain(f.ctx, filterAll)
		if err != nil {
			t.Fatalf("Explain repeated: %v", err)
		}
		if !reflect.DeepEqual(first, second) {
			t.Fatalf("repeated readings differ: %+v versus %+v", first, second)
		}
		if first.Blockers[0].Reason != before.Blocked[0].Reason || first.Blockers[0].Reason != f.refusal.Error() {
			t.Fatalf("blocker reason = %q, want the owner-recorded reason verbatim", first.Blockers[0].Reason)
		}
		after, err := f.manager.Obligations(f.ctx, f.task1)
		if err != nil {
			t.Fatalf("Obligations(%s) after the reads: %v", f.task1, err)
		}
		if !reflect.DeepEqual(before, after) {
			t.Fatalf("owner obligations changed through the reads: %+v versus %+v", before, after)
		}
		if err := f.authority.CheckRead(f.ctx, f.inspector, f.addrB); err != nil {
			t.Fatalf("CheckRead after the reads: %v, want the recorded grant intact", err)
		}
		first.Blockers[0].Reason = "tampered"
		first.Open[0].Advancer = "tampered"
		third, err := explainer.Explain(f.ctx, filterAll)
		if err != nil {
			t.Fatalf("Explain after the mutation: %v", err)
		}
		if third.Blockers[0].Reason == "tampered" || third.Open[0].Advancer == "tampered" {
			t.Fatalf("later reading changed through a returned report: %+v", third)
		}
		var taskBlockers int
		for _, blocker := range third.Blockers {
			if blocker.Ref.Kind == query.ObjectTask {
				taskBlockers++
			}
		}
		if taskBlockers != len(before.Blocked) {
			t.Fatalf("task blockers = %d, want exactly the %d records their owner holds", taskBlockers, len(before.Blocked))
		}

		spec3 := task.Spec{
			CallID:       "call-3",
			DedupKey:     task.DedupKey("inst-1\x00inv-d1\x00dec-d1\x00call-3"),
			InvocationID: event.InvocationID("inv-d1"),
			InstanceID:   f.instance,
			Description:  "agent-b runs the follow-up work",
			Source:       f.addrA,
			Target:       f.addrB,
			Execution:    "serialized",
		}
		receipt3, err := f.manager.Admit(f.ctx, spec3)
		if err != nil {
			t.Fatalf("Admit task-3: %v", err)
		}
		task3 := receipt3.TaskID
		if err := f.durable.Declare(f.ctx, task3, spec3); err != nil {
			t.Fatalf("Declare task-3: %v", err)
		}
		beforeFlush, err := explainer.Explain(f.ctx, filterAll)
		if err != nil {
			t.Fatalf("Explain beside the fresh admission: %v", err)
		}
		if len(bxTaskBlockers(beforeFlush.Blockers, task3)) != 0 {
			t.Fatalf("blockers before the refused delivery = %+v, want none recorded before their owner records them", beforeFlush.Blockers)
		}
		notice3 := task.Notice{TaskID: task3, Round: 1, Kind: task.NoticeSettled, Seq: 3, Event: event.EventID("evt-task3")}
		envelope := task.Envelope{Notice: notice3, Target: f.addrB, Source: f.addrA, TargetInstance: f.instance}
		if result, err := f.registry.Advance(f.ctx, task3, task.StatusSettled, task.MutationID("settle:"+string(task3)), []task.Envelope{envelope}); err != nil || result != task.CommitCommitted {
			t.Fatalf("Advance settle = %v, %v, want committed", result, err)
		}
		framed := task.WithCallFrame(f.ctx, task.CallFrame{
			Identity: task.CallIdentity{
				InstanceID:   f.instance,
				InvocationID: event.InvocationID("inv-d1"),
				DecisionID:   event.DecisionID("dec-d1"),
				CallID:       "call-3",
			},
			Address:   f.addrA,
			Execution: "serialized",
		})
		if err := f.manager.Flush(framed, task3); err != nil {
			t.Fatalf("Flush task-3: %v", err)
		}
		afterFlush, err := explainer.Explain(f.ctx, filterAll)
		if err != nil {
			t.Fatalf("Explain beside the refused delivery: %v", err)
		}
		recorded := bxTaskBlockers(afterFlush.Blockers, task3)
		if len(recorded) != 1 || recorded[0].Reason != f.refusal.Error() || recorded[0].Notice != notice3 {
			t.Fatalf("blockers after the refused delivery = %+v, want the owner record named beside its reason", recorded)
		}
	})

	t.Run("S5 重启与存储失败可辨: a restored chain lists its owed obligations and an unreadable source refuses by name", func(t *testing.T) {
		ctx := context.Background()
		tier, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()})
		if err != nil {
			t.Fatalf("OpenTier: %v", err)
		}
		durable, err := state.NewDurableCommit(ledger.TierFactLog(tier), nil)
		if err != nil {
			t.Fatalf("NewDurableCommit: %v", err)
		}
		registry := task.NewMemoryRegistry(durable, task.NewMemoryAdmission())
		addrA := event.Address{AgentID: event.AgentID("agent-a"), SessionID: event.SessionID("s1")}
		addrB := event.Address{AgentID: event.AgentID("agent-b"), SessionID: event.SessionID("s1")}
		instance := event.InstanceID("inst-1")
		spec := task.Spec{
			CallID:       "call-r1",
			DedupKey:     task.DedupKey("inst-1\x00inv-r1\x00dec-r1\x00call-r1"),
			InvocationID: event.InvocationID("inv-r1"),
			InstanceID:   instance,
			Description:  "agent-b runs the requested work",
			Source:       addrA,
			Target:       addrB,
			Execution:    "serialized",
		}
		receipt, err := registry.Register(ctx, spec)
		if err != nil {
			t.Fatalf("Register: %v", err)
		}
		taskID := receipt.TaskID
		if err := durable.Declare(ctx, taskID, spec); err != nil {
			t.Fatalf("Declare: %v", err)
		}
		notices := make([]task.Notice, 0, 4)
		envelopes := make([]task.Envelope, 0, 4)
		for seq := int64(0); seq < 4; seq++ {
			notice := task.Notice{
				TaskID: taskID,
				Round:  1,
				Kind:   task.NoticeSettled,
				Seq:    seq,
				Event:  event.EventID(fmt.Sprintf("evt-r%d", seq)),
			}
			notices = append(notices, notice)
			envelopes = append(envelopes, task.Envelope{Notice: notice, Target: addrB, Source: addrA, TargetInstance: instance})
		}
		if result, err := registry.Advance(ctx, taskID, task.StatusSettled, task.MutationID("settle:"+string(taskID)), envelopes); err != nil || result != task.CommitCommitted {
			t.Fatalf("Advance settle = %v, %v, want committed", result, err)
		}
		deliver := func(ctx context.Context, notice task.Notice) (task.DeliveryAcceptance, error) {
			if _, err := durable.Accept(ctx, notice); err != nil {
				return task.DeliveryPending, err
			}
			return task.DeliveryAccepted, nil
		}
		manager := task.NewMemoryManager(registry, deliver)
		framed := task.WithCallFrame(ctx, task.CallFrame{
			Identity:  task.CallIdentity{InstanceID: instance, InvocationID: event.InvocationID("inv-r1"), DecisionID: event.DecisionID("dec-r1"), CallID: "call-r1"},
			Address:   addrA,
			Execution: "serialized",
		})
		if err := manager.Flush(framed, taskID); err != nil {
			t.Fatalf("Flush: %v", err)
		}
		if obligations, err := manager.Obligations(ctx, taskID); err != nil || obligations.Count != 0 {
			t.Fatalf("live obligations after the drained queue = (%+v, %v), want every accepted delivery settled", obligations, err)
		}

		durable2, err := state.NewDurableCommit(ledger.TierFactLog(tier), nil)
		if err != nil {
			t.Fatalf("NewDurableCommit over the same log: %v", err)
		}
		registry2 := task.NewMemoryRegistry(durable2, task.NewMemoryAdmission())
		manager2 := task.NewMemoryManager(registry2, deliver)
		chain, err := durable2.Records(ctx, taskID)
		if err != nil {
			t.Fatalf("Records: %v", err)
		}
		restored, err := registry2.Restore(ctx, chain, bxOwners{})
		if err != nil || len(restored.Tasks) != 1 || !restored.Tasks[0].Recovered {
			t.Fatalf("Restore = (%+v, %v), want the task rebuilt from its chain", restored, err)
		}
		inventory := query.NewInventory(fixtureReading{
			agents:  planAgents{plan: state.StatePlan{}},
			sites:   storeSites{store: state.NewMemoryStore(state.NewAuthority()), subject: state.Subject("inspector")},
			slots:   state.NewOccupancy(),
			leases:  state.NewLeases(),
			durable: durable2,
			lookup:  registry2,
			manager: manager2,
		})
		report, err := query.NewExplainer(inventory, nil).Explain(ctx, filterAll)
		if err != nil {
			t.Fatalf("Explain after the restart: %v", err)
		}
		if len(report.Open) != 4 || report.Counts.Open != 4 {
			t.Fatalf("restored obligations = %+v (counts %+v), want the four owed deliveries listed again", report.Open, report.Counts)
		}
		seen := map[int64]bool{}
		for _, open := range report.Open {
			if open.Ref.TaskID != taskID || open.Obligation.Kind != task.ObligationAwaitingDelivery {
				t.Fatalf("restored obligation = %+v, want the owed delivery of the rebuilt task", open)
			}
			if open.Advancer != query.AdvancerNoticeTransport || open.State != query.AdvanceWaiting {
				t.Fatalf("restored obligation = %+v, want the notice transport named as next advancer", open)
			}
			seen[open.Obligation.Notice.Seq] = true
		}
		for seq := int64(0); seq < 4; seq++ {
			if !seen[seq] {
				t.Fatalf("restored obligations = %+v, want notification seq %d named beside its delivery obligation", report.Open, seq)
			}
		}
		if _, status, err := registry2.Lookup(ctx, taskID); err != nil || status != task.StatusSettled {
			t.Fatalf("restored status = (%v, %v), want the settled reading beside the obligations that still stand", status, err)
		}

		unreadable := query.NewInventory(fixtureReading{
			agents:  planAgents{plan: state.StatePlan{}},
			sites:   storeSites{store: state.NewMemoryStore(state.NewAuthority()), subject: state.Subject("inspector")},
			slots:   state.NewOccupancy(),
			leases:  state.NewLeases(),
			durable: durable2,
			lookup:  bxUnreadableTasks{},
			manager: manager2,
		})
		refused, err := query.NewExplainer(unreadable, nil).Explain(ctx, filterAll)
		if err == nil {
			t.Fatalf("Explain beside an unreadable task store = %+v, want a named refusal instead of an empty report posing as normal", refused)
		}
		if !errors.Is(err, query.ErrSourceUnreadable) {
			t.Fatalf("refusal = %v, want the unreadable-source class", err)
		}
		dims := bxSourceDimensions(err)
		named := false
		for _, dim := range dims {
			if dim == query.ObjectTask {
				named = true
			}
		}
		if !named {
			t.Fatalf("refusal = %v, want the affected task dimension named among %v", err, dims)
		}
		if refused.Counts != (query.BlockerCounts{}) || len(refused.Blockers)+len(refused.Open)+len(refused.Leases) != 0 {
			t.Fatalf("refused reading = %+v, want no detail beside the named refusal", refused)
		}
	})

	t.Run("S6 根接线消费租约账: the assembled run explains its leases through the production ledger face", func(t *testing.T) {
		ctx := context.Background()
		root := t.TempDir()
		bxWritePrompt(t, root, filepath.Join("prompts", "system.md"), "system prompt\n")
		bxWriteSkill(t, root, "greet", "body v1\n")
		executor := bxNewExecutor()
		manager := tagentexec.NewManager(executor)
		key := tagentexec.ResourceKey{Kind: "model", Key: "model-a"}
		if _, err := manager.Acquire(ctx, bxExecDecl(key)); err != nil {
			t.Fatalf("Acquire: %v", err)
		}
		if err := manager.Renew(ctx, key, time.Minute); err != nil {
			t.Fatalf("Renew: %v", err)
		}
		run, err := tagent.Assemble(ctx, bxDeclaration(root), map[event.AgentID]tagent.NativeAgent{
			"agent-a": {Model: &bxModel{name: "model-a"}},
		}, tagent.WithGovernance(bxPolicy{}), tagent.WithExecManager(manager))
		if err != nil {
			t.Fatalf("Assemble: %v", err)
		}
		filter := query.ListFilter{Scope: query.AuthorizationScope{Agents: []event.AgentID{"agent-a"}}}
		report, err := run.Explain(ctx, filter)
		if err != nil {
			t.Fatalf("Run.Explain: %v", err)
		}
		held, err := manager.Explain(key)
		if err != nil {
			t.Fatalf("Manager.Explain: %v", err)
		}
		retention, ok := bxRetention(report.Leases, "model", "model-a")
		if !ok {
			t.Fatalf("retentions = %+v, want the model registration listed", report.Leases)
		}
		bxCheckRetention(t, retention, held)
		if !retention.Held || retention.Renewed != 1 {
			t.Fatalf("retention = %+v, want the held resource beside its renewal", retention)
		}
		if len(report.Leases) != 4 {
			t.Fatalf("retentions = %+v, want the four registrations of one agent explained", report.Leases)
		}
		for _, registration := range [][2]string{{"toolset", "agent-a"}, {"filesource", "agent-a"}, {"plugins", "agent-a"}} {
			other, ok := bxRetention(report.Leases, registration[0], registration[1])
			if !ok || other.Held {
				t.Fatalf("retention %v = (%+v, %v), want the registration explained as not held", registration, other, ok)
			}
		}
		if report.Ledger.Held != manager.Held() || report.Ledger.Acquired != manager.Acquired() ||
			report.Ledger.Adopted != manager.Adopted() || report.Ledger.Reused != manager.Reused() ||
			report.Ledger.Released != manager.Released() || report.Ledger.Escalated != manager.Escalated() {
			t.Fatalf("ledger accounting = %+v, disagrees with the manager counters of the attached ledger", report.Ledger)
		}
		if report.Ledger != (query.LeaseLedgerCounts{Held: 1, Acquired: 1}) {
			t.Fatalf("ledger accounting = %+v, want one held resource acquired once", report.Ledger)
		}

		executor.confirmed = false
		if evidence, err := manager.Release(ctx, key); err != nil || evidence.Released {
			t.Fatalf("Release = (%+v, %v), want the retained resource beside its unconfirmed stop", evidence, err)
		}
		retained, err := run.Explain(ctx, filter)
		if err != nil {
			t.Fatalf("Run.Explain beside the retained resource: %v", err)
		}
		blockers := bxLeaseBlockers(retained.Blockers, retention.Ref)
		if len(blockers) != 2 {
			t.Fatalf("retained blockers = %+v, want the unconfirmed stop and the unsettled outcome each named", blockers)
		}
		unconfirmed := false
		for _, blocker := range blockers {
			if blocker.Advancer == query.AdvancerExplicitStop && blocker.Reason == "the execution reported no exit" && reflect.DeepEqual(blocker.Ref.Lease, retention.Ref) {
				unconfirmed = true
			}
		}
		if !unconfirmed {
			t.Fatalf("retained blockers = %+v, want the unconfirmed stop named beside the explicit stop handling as next advancer", blockers)
		}
		heldRetention, ok := bxRetention(retained.Leases, "model", "model-a")
		if !ok || !heldRetention.Held || heldRetention.StopReason != "the execution reported no exit" {
			t.Fatalf("retention = %+v, want the resource still held beside the unconfirmed stop", heldRetention)
		}

		empty, err := run.Explain(ctx, query.ListFilter{})
		if err != nil {
			t.Fatalf("Run.Explain under an empty scope: %v", err)
		}
		if empty.Counts != (query.BlockerCounts{}) || len(empty.Blockers)+len(empty.Open)+len(empty.Leases) != 0 {
			t.Fatalf("empty-scope reading = %+v, want the authorization filter excluding everything before it is counted", empty)
		}
	})
}
