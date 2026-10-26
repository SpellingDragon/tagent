package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureModule names the dependency-free module of the fixture, which keeps
// `go list -export` cheap and offline; the fixture mirrors the real table and the
// real symbol shapes, so the selftest exercises the frozen policy rather than a
// second, hand-written one.
const fixtureModule = "fixture.local/gate"

// TestEventOwnership pins the frozen ownership gate against the mirrored fixture.
// 契约: docs/OPERATION.md#event-gates
func TestEventOwnership(t *testing.T) {
	t.Run("positive_legal_assembly_passes", func(t *testing.T) {
		fixture := newOwnershipFixture(t, baseFixtureFiles(), nil)
		report, code := executeEventOwnership(ownershipOptions{Golden: fixture.golden, RepoRoot: fixture.root})
		if code != 0 || !report.OK {
			t.Fatalf("legal assembly must pass, code=%d violations=%+v notes=%+v", code, report.Violations, report.Notes)
		}
		if report.Counts.Forbidden != 0 || report.Counts.Unowned != 0 || report.Counts.Unregistered != 0 {
			t.Fatalf("legal assembly reported findings: %+v", report.Counts)
		}
		if report.Counts.Allowed == 0 {
			t.Fatal("legal assembly counted no allowed direct call; the gate would be vacuous")
		}
		if report.Counts.Uncovered != 3 {
			t.Fatalf("expected the two wiring callbacks plus the commit port as uncovered, got %d", report.Counts.Uncovered)
		}
	})

	t.Run("negative_forbidden_call_is_rejected", func(t *testing.T) {
		files := baseFixtureFiles()
		files["run.go"] = strings.Replace(files["run.go"], "\treturn r.store.AcceptNotice(ctx, notice)",
			"\tif err := r.store.AcceptNotice(ctx, notice); err != nil {\n\t\treturn err\n\t}\n\treturn r.store.Claim(ctx, notice)", 1)
		fixture := newOwnershipFixture(t, files, nil)
		report, code := executeEventOwnership(ownershipOptions{Golden: fixture.golden, RepoRoot: fixture.root})
		if code == 0 || report.OK {
			t.Fatalf("a host claiming a notification must fail the gate: %+v", report.Counts)
		}
		finding := findViolation(report, "forbidden")
		if finding == nil || finding.Symbol != "mailbox.claim" || finding.Owner != "host" {
			t.Fatalf("expected forbidden mailbox.claim for host, got %+v", report.Violations)
		}
	})

	t.Run("unregistered_edge_is_rejected", func(t *testing.T) {
		files := baseFixtureFiles()
		files["facts.go"] = strings.Replace(files["facts.go"], "\tr.loop.Backflow(notice)",
			"\t_, _, _ = r.feed.NoticesFrom(\"chain\", 0)\n\tr.loop.Backflow(notice)", 1)
		files["run.go"] = strings.Replace(files["run.go"], "\tloop  *agent.Loop",
			"\tloop  *agent.Loop\n\tfeed  agent.NoticeFeed", 1)
		fixture := newOwnershipFixture(t, files, nil)
		report, code := executeEventOwnership(ownershipOptions{Golden: fixture.golden, RepoRoot: fixture.root})
		if code == 0 || report.OK {
			t.Fatalf("an edge the table never ruled on must fail: %+v", report.Counts)
		}
		finding := findViolation(report, "unregistered")
		if finding == nil || finding.Symbol != "consume.feed-notices-from" || finding.Owner != "facts" {
			t.Fatalf("expected unregistered consume.feed-notices-from for facts, got %+v", report.Violations)
		}
	})

	t.Run("unowned_call_site_is_rejected", func(t *testing.T) {
		files := baseFixtureFiles()
		files["ledger.go"] = "package gate\n\nimport (\n\t\"context\"\n\n\t\"" + fixtureModule + "/runtime/task\"\n)\n\n" +
			"func (r *Run) note(ctx context.Context, notice task.Notice) error {\n\treturn r.store.AcceptNotice(ctx, notice)\n}\n"
		fixture := newOwnershipFixture(t, files, nil)
		report, code := executeEventOwnership(ownershipOptions{Golden: fixture.golden, RepoRoot: fixture.root})
		if code == 0 || report.OK {
			t.Fatalf("a call site no owner covers must fail: %+v", report.Counts)
		}
		if findViolation(report, "unowned") == nil {
			t.Fatalf("expected an unowned call-site finding, got %+v", report.Violations)
		}
	})

	t.Run("same_name_non_target_is_not_misjudged", func(t *testing.T) {
		files := baseFixtureFiles()
		files["misc.go"] = "package gate\n\nimport \"context\"\n\n" +
			"type AnchorStore struct{}\n\nfunc (a *AnchorStore) Advance(ctx context.Context, moment int) error { return nil }\n\n" +
			"type toolbox struct{ sink func(string) }\n\n" +
			"func drive(anchor *AnchorStore, box toolbox) error {\n\tbox.sink(\"material\")\n\treturn anchor.Advance(context.Background(), 1)\n}\n"
		fixture := newOwnershipFixture(t, files, nil)
		report, code := executeEventOwnership(ownershipOptions{Golden: fixture.golden, RepoRoot: fixture.root})
		if code != 0 || !report.OK {
			t.Fatalf("same-name calls with another signature are not frozen targets: %+v", report.Violations)
		}
		if report.Counts.SameNameNonTarget < 2 {
			t.Fatalf("expected both same-name calls counted as non-targets, got %+v", report.Counts)
		}
	})

	t.Run("uncovered_is_listed_and_not_counted", func(t *testing.T) {
		base := newOwnershipFixture(t, baseFixtureFiles(), nil)
		baseline, code := executeEventOwnership(ownershipOptions{Golden: base.golden, RepoRoot: base.root})
		if code != 0 {
			t.Fatalf("baseline fixture must pass: %+v", baseline.Violations)
		}
		files := baseFixtureFiles()
		files["runtime/state/indirect.go"] = "package state\n\nimport (\n\t\"context\"\n\t\"reflect\"\n\n\t\"" +
			fixtureModule + "/runtime/task\"\n)\n\n" +
			"type Mailbox interface {\n\tConsume(ctx context.Context, notice task.Notice) error\n}\n\n" +
			"func drain(box Mailbox, notice task.Notice) error {\n\treturn box.Consume(context.Background(), notice)\n}\n\n" +
			"func reflectDrain(store *MemoryStore) reflect.Value {\n\treturn reflect.ValueOf(store).MethodByName(\"Consume\")\n}\n\n" +
			"func consumeCallback(store *MemoryStore) func(context.Context, task.Notice) error {\n\treturn store.Consume\n}\n"
		fixture := newOwnershipFixture(t, files, nil)
		report, code := executeEventOwnership(ownershipOptions{Golden: fixture.golden, RepoRoot: fixture.root})
		if code != 0 || !report.OK {
			t.Fatalf("uncovered call sites are reported, not failures: %+v", report.Violations)
		}
		if report.Counts.Uncovered != baseline.Counts.Uncovered+3 {
			t.Fatalf("expected three more uncovered sites, got %d (baseline %d): %+v",
				report.Counts.Uncovered, baseline.Counts.Uncovered, report.Uncovered)
		}
		if report.Counts.Allowed != baseline.Counts.Allowed {
			t.Fatalf("uncovered sites must not be counted as covered: allowed %d -> %d",
				baseline.Counts.Allowed, report.Counts.Allowed)
		}
		for _, uncovered := range report.Uncovered {
			if strings.TrimSpace(uncovered.Reason) == "" {
				t.Fatalf("every uncovered site needs a reason: %+v", uncovered)
			}
		}
	})

	t.Run("unresolved_frozen_symbol_fails", func(t *testing.T) {
		table, _, err := loadOwnershipTable(filepath.Join("..", "..", "golden", "runtime", "architecture.json"))
		if err != nil {
			t.Fatal(err)
		}
		table.Symbols = append(table.Symbols, ownershipSymbol{ID: "mailbox.retire", Pkg: "runtime/state",
			Receivers: []string{"MemoryStore"}, Method: "Retire", Role: "mailbox-time-point",
			Reason: "fixture: a frozen symbol the tree does not declare"})
		fixture := newOwnershipFixture(t, baseFixtureFiles(), table)
		report, code := executeEventOwnership(ownershipOptions{Golden: fixture.golden, RepoRoot: fixture.root})
		if code == 0 || report.OK {
			t.Fatalf("a frozen symbol the tree cannot resolve must fail the gate: %+v", report.Counts)
		}
		if findViolation(report, "symbol-unresolved") == nil {
			t.Fatalf("expected symbol-unresolved, got %+v", report.Violations)
		}
	})

	t.Run("real_tree_passes", func(t *testing.T) {
		repo, err := filepath.Abs(filepath.Join("..", ".."))
		if err != nil {
			t.Fatal(err)
		}
		report, code := executeEventOwnership(ownershipOptions{
			Golden: filepath.Join(repo, "golden", "runtime", "architecture.json"), RepoRoot: repo})
		if code != 0 || !report.OK {
			t.Fatalf("the real tree must satisfy its frozen table: %+v", report.Violations)
		}
		if report.Counts.Allowed == 0 {
			t.Fatal("the real tree counted no allowed call; the gate would be vacuous")
		}
		for _, uncovered := range report.Uncovered {
			if strings.TrimSpace(uncovered.Reason) == "" {
				t.Fatalf("every uncovered site needs a reason: %+v", uncovered)
			}
		}
	})
}

// ownershipFixture is one temporary module plus the golden table that judges it.
type ownershipFixture struct {
	root   string
	golden string
}

func newOwnershipFixture(t *testing.T, files map[string]string, table *ownershipTable) ownershipFixture {
	t.Helper()
	if table == nil {
		loaded, _, err := loadOwnershipTable(filepath.Join("..", "..", "golden", "runtime", "architecture.json"))
		if err != nil {
			t.Fatal(err)
		}
		table = loaded
	}
	root := t.TempDir()
	for name, content := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	document := map[string]interface{}{
		"module_path": fixtureModule,
		"scope": map[string]interface{}{
			"exclude_dir_names": []string{"scripts", "tests", "examples", "vendor", "node_modules",
				"docs", "resources", "testdata", ".git", ".github", ".agents", ".tagent-workspace",
				".pre-isolation-backup"},
		},
		"event_ownership": table,
	}
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	golden := filepath.Join(root, "architecture.json")
	if err := os.WriteFile(golden, encoded, 0o644); err != nil {
		t.Fatal(err)
	}
	return ownershipFixture{root: root, golden: golden}
}

func findViolation(report *ownershipReport, rule string) *ownershipFinding {
	for index := range report.Violations {
		if report.Violations[index].Rule == rule {
			return &report.Violations[index]
		}
	}
	return nil
}

// baseFixtureFiles is a legal assembly of the same owners the real tree has: the
// task package mints and commits, the mailbox implements its time points, the
// facts consumer books one notification, the host only accepts delivery, and the
// composition root only forwards the task commit face.
func baseFixtureFiles() map[string]string {
	return map[string]string{
		"go.mod": "module " + fixtureModule + "\n\ngo 1.21\n",
		"runtime/task/task.go": `package task

import "context"

type Notice struct {
	TaskID string
	Round  int64
	Kind   string
	Seq    int64
}

type Envelope struct {
	Notice Notice
}

type MemoryRegistry struct{}

func (r *MemoryRegistry) IssueNotice(ctx context.Context, taskID string, kind string) (Envelope, error) {
	return Envelope{}, nil
}

func (r *MemoryRegistry) Advance(ctx context.Context, taskID string, envelope Envelope) error {
	return nil
}

type Wrapper struct {
	registry *MemoryRegistry
}

func (w *Wrapper) execute(ctx context.Context, taskID string) {
	_, _ = w.registry.IssueNotice(ctx, taskID, "settled")
	_ = w.registry.Advance(ctx, taskID, Envelope{})
}
`,
		"runtime/state/state.go": `package state

import (
	"context"

	"` + fixtureModule + `/runtime/task"
)

type Store interface {
	AcceptNotice(ctx context.Context, notice task.Notice) error
}

type MemoryStore struct{}

func (m *MemoryStore) TrackDelivery(ctx context.Context, notice task.Notice) error { return nil }

func (m *MemoryStore) AcceptNotice(ctx context.Context, notice task.Notice) error { return nil }

func (m *MemoryStore) Claim(ctx context.Context, notice task.Notice) error { return nil }

func (m *MemoryStore) Consume(ctx context.Context, notice task.Notice) error { return nil }

func (m *MemoryStore) Reclaim(ctx context.Context, notice task.Notice) error { return nil }
`,
		"runtime/agent/agent.go": `package agent

import (
	"context"

	"` + fixtureModule + `/runtime/task"
)

type NoticeView struct {
	Key string
}

type NoticeFeed interface {
	NoticeBoundary(chain string) (int, bool)
	NoticesFrom(chain string, cursor int) ([]NoticeView, int, bool)
}

type Loop struct{}

func (l *Loop) Backflow(notice task.Notice) {}

type DelegationTasks interface {
	Advance(ctx context.Context, taskID string, envelope task.Envelope) error
}

type Executor struct {
	tasks DelegationTasks
}

func (e *Executor) settle(ctx context.Context, taskID string) {
	_ = e.tasks.Advance(ctx, taskID, task.Envelope{})
}

func (l *Loop) read(feed NoticeFeed) {
	_, _ = feed.NoticeBoundary("chain")
	_, _, _ = feed.NoticesFrom("chain", 0)
}
`,
		"run.go": `package gate

import (
	"context"

	"` + fixtureModule + `/runtime/agent"
	"` + fixtureModule + `/runtime/state"
	"` + fixtureModule + `/runtime/task"
)

type Run struct {
	store *state.MemoryStore
	loop  *agent.Loop
}

func (r *Run) deliver(ctx context.Context, notice task.Notice) error {
	if err := r.store.TrackDelivery(ctx, notice); err != nil {
		return err
	}
	return r.store.AcceptNotice(ctx, notice)
}

func assemble(r *Run, register func(func(context.Context, task.Notice) error),
	envelopeDeliverer func(context.Context, task.Notice) error) {
	register(r.sink)
	_ = envelopeDeliverer
	deliver := r.deliver
	_ = deliver
}
`,
		"facts.go": `package gate

import (
	"context"

	"` + fixtureModule + `/runtime/task"
)

func (r *Run) sink(ctx context.Context, notice task.Notice) error {
	return r.bookNotice(ctx, notice)
}

func (r *Run) bookNotice(ctx context.Context, notice task.Notice) error {
	if err := r.store.TrackDelivery(ctx, notice); err != nil {
		return err
	}
	if err := r.store.AcceptNotice(ctx, notice); err != nil {
		return err
	}
	if err := r.store.Claim(ctx, notice); err != nil {
		return err
	}
	if err := r.store.Consume(ctx, notice); err != nil {
		return err
	}
	r.loop.Backflow(notice)
	return nil
}
`,
		"commit.go": `package gate

import (
	"context"

	"` + fixtureModule + `/runtime/task"
)

type closeFace struct {
	registry *task.MemoryRegistry
}

func (f closeFace) Advance(ctx context.Context, taskID string, envelope task.Envelope) error {
	return f.registry.Advance(ctx, taskID, envelope)
}
`,
	}
}
