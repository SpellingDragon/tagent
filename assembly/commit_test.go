// The configuration truth chain: the source file is the editing surface, the
// chain is the ledger. These tests drive the chain from the outside the way a
// supervisor does — boot, edit the file, reload, restart — and read what the
// ledger records: one declaration per load, one publication record per
// generation, one result per attempt, and a receipt that names what actually
// took effect instead of what was attempted.
//
// 契约: docs/wiki/platform/org-hot-reload.md#overview
package assembly

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/memory/ledger"
)

// livenessDoc is one source document of the configuration liveness fixture:
// two agents, one provider, an optional durable storage root.
type livenessDoc struct {
	Root             string
	UserProvider     string
	UserModel        string
	WorkerModel      string
	UserReplayWindow int
	WorkerMax        int
	Storage          bool
}

// yaml renders the source document.
func (d livenessDoc) yaml() string {
	storage := ""
	if d.Storage {
		storage = "storage: { type: localfile, root: " + filepath.Join(d.Root, "storage") + " }\n"
	}
	maxTokens := d.UserReplayWindow
	if maxTokens == 0 {
		maxTokens = 256
	}
	workerMax := d.WorkerMax
	if workerMax == 0 {
		workerMax = 128
	}
	userProvider := d.UserProvider
	if userProvider == "" {
		userProvider = "local"
	}
	return fmt.Sprintf(`entry: user
provider: local
model: %s
prompt_dir: %s
request_timeout_seconds: 60
providers:
  local: { provider: openai, api_endpoint: "http://127.0.0.1:1/v1", api_key_env: TAGENT_TEST_API_KEY }
agents:
  user:
    provider: %s
    model: %s
    replay_window: %d
  worker:
    model: %s
    replay_window: %d
%s`, d.UserModel, d.Root, userProvider, d.UserModel, maxTokens, d.WorkerModel, workerMax, storage)
}

// write makes the source document the one the host reads and answers its path.
func (d livenessDoc) write(t *testing.T) string {
	t.Helper()
	path := filepath.Join(d.Root, "tagent.yaml")
	if err := os.WriteFile(path, []byte(d.yaml()), 0o644); err != nil {
		t.Fatalf("write the configuration source: %v", err)
	}
	return path
}

// newLivenessDoc prepares one source directory beside the document it renders.
func newLivenessDoc(t *testing.T) livenessDoc {
	t.Helper()
	return livenessDoc{Root: t.TempDir(), UserModel: "model-a", WorkerModel: "model-w", Storage: true}
}

// bootLiveness boots one host over the source document and registers its close.
func bootLiveness(t *testing.T, path string) *Host {
	t.Helper()
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	host, err := Boot(path)
	if err != nil {
		t.Fatalf("boot the host: %v", err)
	}
	t.Cleanup(func() { closeLiveness(t, host) })
	return host
}

// closeLiveness closes one host and fails the test when the close reports one.
func closeLiveness(t *testing.T, host *Host) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := host.Close(ctx); err != nil {
		t.Fatalf("close the host: %v", err)
	}
}

// latestAttempt reads the newest publication result the chain of one run holds.
func latestAttempt(t *testing.T, run *Run) configFact {
	t.Helper()
	fact, held, err := run.latestPublicationAttempt(context.Background())
	if err != nil {
		t.Fatalf("read the latest publication attempt: %v", err)
	}
	if !held {
		t.Fatal("the configuration chain holds no publication attempt")
	}
	return fact
}

// chainFacts reads every fact of one run's configuration chain.
func chainFacts(t *testing.T, run *Run) []configFact {
	t.Helper()
	facts, err := run.records.chain.snapshot(context.Background())
	if err != nil {
		t.Fatalf("read the configuration chain: %v", err)
	}
	return facts
}

// countFacts counts the facts of one kind.
func countFacts(facts []configFact, kind string) int {
	count := 0
	for _, fact := range facts {
		if fact.Kind == kind {
			count++
		}
	}
	return count
}

// TestDeclarationFactCarriesPayload pins the declaration fact one load commits.
//
// 契约: docs/FACT-VOCABULARY.md#families
func TestDeclarationFactCarriesPayload(t *testing.T) {
	doc := newLivenessDoc(t)
	path := doc.write(t)
	host := bootLiveness(t, path)
	run := host.Run()
	fact, decl, held, err := run.latestDeclaration(context.Background())
	if err != nil {
		t.Fatalf("read the declaration fact: %v", err)
	}
	if !held {
		t.Fatal("the chain holds no declaration fact after boot")
	}
	if fact.Type != (config.Declaration{}).FactType() {
		t.Fatalf("declaration fact type = %q, want the type api/config freezes", fact.Type)
	}
	if fact.Fingerprint != decl.FactKey() {
		t.Fatalf("declaration fact fingerprint = %q, want the canonical declaration key %q", fact.Fingerprint, decl.FactKey())
	}
	if fact.Generation != 1 {
		t.Fatalf("declaration fact generation = %d, want the first generation", fact.Generation)
	}
	var payload map[string]any
	if err := json.Unmarshal(fact.Content, &payload); err != nil {
		t.Fatalf("decode the declaration payload: %v", err)
	}
	if payload["Entry"] != "user" {
		t.Fatalf("declaration payload entry = %v, want the declared entry", payload["Entry"])
	}
	agents, ok := payload["Agents"].([]any)
	if !ok || len(agents) != 2 {
		t.Fatalf("declaration payload agents = %v, want the two declared agents", payload["Agents"])
	}
	for _, agent := range agents {
		spec, ok := agent.(map[string]any)
		if !ok || spec["ID"] == "" {
			t.Fatalf("declaration payload agent = %v, want a named agent", agent)
		}
	}
	if _, held := payload["Raw"]; !held {
		t.Fatal("declaration payload does not render the declaration surface")
	}
	if facts := chainFacts(t, run); countFacts(facts, configDeclarationKind) != 1 {
		t.Fatalf("declaration facts after boot = %d, want the one fact of the one load", countFacts(facts, configDeclarationKind))
	}
}

// TestPublishConsumesDeclarationFact pins the publication entry that consumes the declaration.
// - the entry consumes the declaration the chain carries
// - an unchanged load commits no second declaration and no new generation
//
// 契约: docs/wiki/platform/org-hot-reload.md#apply-record
func TestPublishConsumesDeclarationFact(t *testing.T) {
	doc := newLivenessDoc(t)
	path := doc.write(t)
	host := bootLiveness(t, path)
	run := host.Run()

	doc.UserReplayWindow = 512
	if err := os.WriteFile(path, []byte(doc.yaml()), 0o644); err != nil {
		t.Fatalf("rewrite the configuration source: %v", err)
	}
	report, err := host.Reload(context.Background())
	if err != nil {
		t.Fatalf("reload the rewritten document: %v", err)
	}
	if report.Publication.Generation != 2 || !report.Publication.Published {
		t.Fatalf("reload publication = %+v, want the second generation published", report.Publication)
	}
	_, decl, held, err := run.latestDeclaration(context.Background())
	if err != nil || !held {
		t.Fatalf("read the newest declaration: held=%v err=%v", held, err)
	}
	record, published, err := run.latestPublishedRecord(context.Background())
	if err != nil || !published {
		t.Fatalf("read the newest publication record: published=%v err=%v", published, err)
	}
	if record.Fingerprint != decl.FactKey() {
		t.Fatalf("publication record fingerprint = %q, want the chain declaration %q it consumed", record.Fingerprint, decl.FactKey())
	}
	if record.Generation != 2 {
		t.Fatalf("publication record generation = %d, want the second generation", record.Generation)
	}
	attempt := latestAttempt(t, run)
	if attempt.Result != publicationResultPublished || attempt.Generation != 2 || attempt.Fingerprint != decl.FactKey() {
		t.Fatalf("publication attempt = %+v, want the second generation published from the chain declaration", attempt)
	}

	again, err := host.Reload(context.Background())
	if err != nil {
		t.Fatalf("reload the unchanged document: %v", err)
	}
	if again.Publication.Generation != 2 || again.Publication.Published {
		t.Fatalf("unchanged reload publication = %+v, want the second generation carried", again.Publication)
	}
	facts := chainFacts(t, run)
	if got := countFacts(facts, configDeclarationKind); got != 2 {
		t.Fatalf("declaration facts after two distinct loads = %d, want 2 (the unchanged load adds none)", got)
	}
	if got := countFacts(facts, configPublicationRecordKind); got != 2 {
		t.Fatalf("publication records after one unchanged reload = %d, want 2 (no record for a carried generation)", got)
	}
	if got := latestAttempt(t, run); got.Result != publicationResultUnchanged {
		t.Fatalf("unchanged reload attempt = %+v, want the unchanged result", got)
	}
}

// TestRevertedReloadAnswersTheChainDeclaration pins the effective-declaration order of a reverted reload.
// - the newest declaration fact answers the declaration the load produced
// - a reversion files its own entry naming the declaration it moves from
//
// 契约: docs/wiki/platform/org-hot-reload.md#apply-record
func TestRevertedReloadAnswersTheChainDeclaration(t *testing.T) {
	doc := newLivenessDoc(t)
	path := doc.write(t)
	host := bootLiveness(t, path)
	run := host.Run()
	ctx := context.Background()
	_, bootDecl, held, err := run.latestDeclaration(ctx)
	if err != nil || !held {
		t.Fatalf("read the boot declaration: held=%v err=%v", held, err)
	}
	previous := bootDecl.FactKey()
	for i, maxTokens := range []int{512, 256, 512, 256} {
		doc.UserReplayWindow = maxTokens
		if err := os.WriteFile(path, []byte(doc.yaml()), 0o644); err != nil {
			t.Fatalf("rewrite the configuration source: %v", err)
		}
		loaded, err := loadHostDocument(path)
		if err != nil {
			t.Fatalf("load the rewritten document: %v", err)
		}
		loadDecl, err := loaded.Declaration()
		if err != nil {
			t.Fatalf("declare the rewritten document: %v", err)
		}
		report, err := host.Reload(ctx)
		if err != nil {
			t.Fatalf("reload attempt %d of the reverted document: %v", i+1, err)
		}
		if !report.Reloaded || !report.Publication.Published {
			t.Fatalf("reverted reload %d publication = %+v, want a published generation", i+1, report.Publication)
		}
		fact, decl, held, err := run.latestDeclaration(ctx)
		if err != nil || !held {
			t.Fatalf("read the declaration of reload %d: held=%v err=%v", i+1, held, err)
		}
		if got := decl.FactKey(); got != loadDecl.FactKey() {
			t.Fatalf("declaration of reload %d on the chain = %q, want the loaded declaration %q", i+1, got, loadDecl.FactKey())
		}
		if fact.Generation != report.Publication.Generation {
			t.Fatalf("declaration fact generation of reload %d = %d, want %d", i+1, fact.Generation, report.Publication.Generation)
		}
		if fact.PreviousFingerprint != previous {
			t.Fatalf("declaration fact of reload %d moves from %q, want %q", i+1, fact.PreviousFingerprint, previous)
		}
		previous = fact.Fingerprint
	}
	if got := countFacts(chainFacts(t, run), configDeclarationKind); got != 5 {
		t.Fatalf("declaration facts after one boot and four loads = %d, want 5", got)
	}
}

// TestRevertedDeclarationSurvivesRestart pins the restart beside a reverted declaration.
// - the restarted run serves the declaration the revert published, at its recorded generation
// - the restart files no second declaration and no second publication
//
// 契约: docs/wiki/platform/org-hot-reload.md#apply-record
func TestRevertedDeclarationSurvivesRestart(t *testing.T) {
	doc := newLivenessDoc(t)
	path := doc.write(t)
	ctx := context.Background()
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	host, err := Boot(path)
	if err != nil {
		t.Fatalf("boot the host: %v", err)
	}
	_, bootDecl, held, err := host.Run().latestDeclaration(ctx)
	if err != nil || !held {
		t.Fatalf("read the boot declaration: held=%v err=%v", held, err)
	}
	doc.UserReplayWindow = 512
	if err := os.WriteFile(path, []byte(doc.yaml()), 0o644); err != nil {
		t.Fatalf("rewrite the configuration source: %v", err)
	}
	if report, err := host.Reload(ctx); err != nil || !report.Publication.Published {
		t.Fatalf("reload the moved document: publication=%+v err=%v", report.Publication, err)
	}
	doc.UserReplayWindow = 256
	if err := os.WriteFile(path, []byte(doc.yaml()), 0o644); err != nil {
		t.Fatalf("revert the configuration source: %v", err)
	}
	reverted, err := host.Reload(ctx)
	if err != nil || !reverted.Publication.Published {
		t.Fatalf("reload the reverted document: publication=%+v err=%v", reverted.Publication, err)
	}
	if err := host.Close(ctx); err != nil {
		t.Fatalf("close the host before the restart: %v", err)
	}
	restarted := bootLiveness(t, path)
	run := restarted.Run()
	_, decl, held, err := run.latestDeclaration(ctx)
	if err != nil || !held {
		t.Fatalf("read the declaration after the restart: held=%v err=%v", held, err)
	}
	if got := decl.FactKey(); got != bootDecl.FactKey() {
		t.Fatalf("declaration after the restart = %q, want the reverted declaration %q", got, bootDecl.FactKey())
	}
	if got := uint64(run.publisher.Current().Generation); got != reverted.Publication.Generation {
		t.Fatalf("generation in effect after the restart = %d, want the reverted generation %d", got, reverted.Publication.Generation)
	}
	facts := chainFacts(t, run)
	if got := countFacts(facts, configDeclarationKind); got != 3 {
		t.Fatalf("declaration facts after the restart = %d, want the 3 the three loads filed", got)
	}
	if got := countFacts(facts, configPublicationRecordKind); got != 3 {
		t.Fatalf("publication records after the restart = %d, want the 3 the three loads filed", got)
	}
}

// TestFailedInstallReceiptIsHonest pins the install-failed receipt of a refused model rebuild.
// - the generation published while the version in effect did not move
// - the chain fact and the receipt name the refusal instead of claiming the switch
//
// 契约: docs/wiki/platform/org-hot-reload.md#candidate-refusal
func TestFailedInstallReceiptIsHonest(t *testing.T) {
	doc := newLivenessDoc(t)
	path := doc.write(t)
	host := bootLiveness(t, path)
	run := host.Run()
	before := run.publisher.Current()
	lease := run.leaseOf("user")
	if lease == nil {
		t.Fatal("the entry agent carries no model lease")
	}
	heldBefore, _ := heldModelVersion(lease.lease)

	doc.UserProvider = "missing"
	doc.UserModel = "model-b"
	if err := os.WriteFile(path, []byte(doc.yaml()), 0o644); err != nil {
		t.Fatalf("rewrite the configuration source: %v", err)
	}
	report, err := host.Reload(context.Background())
	if err == nil {
		t.Fatal("the reload of an unbuildable model answered success")
	}
	if !strings.Contains(err.Error(), publicationResultInstallFailed) {
		t.Fatalf("reload error = %v, want the install-failed receipt", err)
	}
	if report.Publication.Generation != before.Generation+1 || !report.Publication.Published {
		t.Fatalf("reload publication = %+v, want the next generation published", report.Publication)
	}
	heldAfter, _ := heldModelVersion(lease.lease)
	if heldAfter != heldBefore {
		t.Fatalf("model version in effect = %q, want the version %q the refusal kept", heldAfter, heldBefore)
	}
	fact := latestAttempt(t, run)
	if fact.Result != publicationResultInstallFailed {
		t.Fatalf("attempt result = %q, want %q", fact.Result, publicationResultInstallFailed)
	}
	if fact.Generation != uint64(report.Publication.Generation) {
		t.Fatalf("attempt generation = %d, want the published generation %d", fact.Generation, report.Publication.Generation)
	}
	if got := fact.Dimensions["user|"+event.ConfigChangeDimensionModel]; got != "held" {
		t.Fatalf("user model dimension = %q, want held", got)
	}
	if !strings.Contains(fact.Reason, heldBefore) && !strings.Contains(fact.Reason, "missing/model-b") {
		t.Fatalf("attempt reason = %q, want the refused version named", fact.Reason)
	}
}

// TestLoadFailureFactAuditable pins the load-failed fact of a broken source.
// - the running binding stays at the generation still served
// - a repaired file publishes the next generation
//
// 契约: docs/wiki/platform/platform-subsystems.md#strict-decode
func TestLoadFailureFactAuditable(t *testing.T) {
	doc := newLivenessDoc(t)
	path := doc.write(t)
	host := bootLiveness(t, path)
	run := host.Run()
	before := run.publisher.Current()

	if err := os.WriteFile(path, []byte("entry: [unterminated\n"), 0o644); err != nil {
		t.Fatalf("break the configuration source: %v", err)
	}
	if _, err := host.Reload(context.Background()); err == nil {
		t.Fatal("the reload of a broken document answered success")
	}
	fact := latestAttempt(t, run)
	if fact.Result != publicationResultLoadFailed {
		t.Fatalf("attempt result = %q, want load-failed", fact.Result)
	}
	if fact.Generation != before.Generation {
		t.Fatalf("load-failed attempt generation = %d, want the generation %d still in effect", fact.Generation, before.Generation)
	}
	if got := run.publisher.Current().Generation; got != before.Generation {
		t.Fatalf("generation in effect after a load failure = %d, want %d", got, before.Generation)
	}

	doc.UserReplayWindow = 640
	if err := os.WriteFile(path, []byte(doc.yaml()), 0o644); err != nil {
		t.Fatalf("repair the configuration source: %v", err)
	}
	report, err := host.Reload(context.Background())
	if err != nil {
		t.Fatalf("reload the repaired document: %v", err)
	}
	if report.Publication.Generation != before.Generation+1 || !report.Publication.Published {
		t.Fatalf("repaired reload publication = %+v, want the next generation published", report.Publication)
	}
	if got := latestAttempt(t, run); got.Result != publicationResultPublished {
		t.Fatalf("attempt after the repair = %+v, want published", got)
	}
}

// TestGovernanceRejectionReceiptIsHonest pins the governance-rejected receipt.
// - a restart-required move leaves the generation in effect
// - the receipt names the refusal instead of reporting success
//
// 契约: docs/wiki/platform/org-hot-reload.md#restart-required-dimensions
func TestGovernanceRejectionReceiptIsHonest(t *testing.T) {
	doc := newLivenessDoc(t)
	path := doc.write(t)
	host := bootLiveness(t, path)
	run := host.Run()
	before := run.publisher.Current()

	broken := strings.Replace(doc.yaml(), "storage: { type: localfile", "storage: { type: sqlite", 1)
	if err := os.WriteFile(path, []byte(broken), 0o644); err != nil {
		t.Fatalf("rewrite the configuration source: %v", err)
	}
	report, err := host.Reload(context.Background())
	if err == nil {
		t.Fatal("the reload of a restart-required move answered success")
	}
	if report.Publication.Generation != 0 || report.Publication.Published {
		t.Fatalf("refused reload publication = %+v, want no publication", report.Publication)
	}
	if got := run.publisher.Current().Generation; got != before.Generation {
		t.Fatalf("generation in effect after a governance refusal = %d, want %d", got, before.Generation)
	}
	fact := latestAttempt(t, run)
	if fact.Result != publicationResultGovernanceRejected {
		t.Fatalf("attempt result = %q, want governance-rejected", fact.Result)
	}
	if !strings.Contains(fact.Reason, "restart") {
		t.Fatalf("governance-rejected reason = %q, want the named restart-required refusal", fact.Reason)
	}
}

// TestRestartReconcilesFileWithChain pins the restart reconciliation against the chain.
// - a restart that agrees with its chain publishes nothing
// - a newer file publishes once
// - a file older than the chain never overwrites it
//
// 契约: docs/wiki/platform/org-hot-reload.md#generations
func TestRestartReconcilesFileWithChain(t *testing.T) {
	doc := newLivenessDoc(t)
	path := doc.write(t)
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")

	first, err := Boot(path)
	if err != nil {
		t.Fatalf("boot the first process: %v", err)
	}
	if generation := first.Run().publisher.Current().Generation; generation != 1 {
		t.Fatalf("first boot generation = %d, want 1", generation)
	}
	doc.UserReplayWindow = 512
	if err := os.WriteFile(path, []byte(doc.yaml()), 0o644); err != nil {
		t.Fatalf("rewrite the configuration source: %v", err)
	}
	if _, err := first.Reload(context.Background()); err != nil {
		t.Fatalf("reload the second generation: %v", err)
	}
	if generation := first.Run().publisher.Current().Generation; generation != 2 {
		t.Fatalf("reload generation = %d, want 2", generation)
	}
	closeLiveness(t, first)

	second, err := Boot(path)
	if err != nil {
		t.Fatalf("boot the consistent restart: %v", err)
	}
	run := second.Run()
	if generation := run.publisher.Current().Generation; generation != 2 {
		t.Fatalf("consistent restart generation = %d, want the chain's 2", generation)
	}
	facts := chainFacts(t, run)
	recordsBefore := countFacts(facts, configPublicationRecordKind)
	if recordsBefore != 2 {
		t.Fatalf("publication records after a consistent restart = %d, want 2", recordsBefore)
	}

	doc.UserReplayWindow = 768
	if err := os.WriteFile(path, []byte(doc.yaml()), 0o644); err != nil {
		t.Fatalf("rewrite the newer configuration source: %v", err)
	}
	closeLiveness(t, second)
	third, err := Boot(path)
	if err != nil {
		t.Fatalf("boot the newer-file restart: %v", err)
	}
	if generation := third.Run().publisher.Current().Generation; generation != 3 {
		t.Fatalf("newer-file restart generation = %d, want 3", generation)
	}
	if got := countFacts(chainFacts(t, third.Run()), configPublicationRecordKind); got != recordsBefore+1 {
		t.Fatalf("publication records after the newer-file restart = %d, want %d", got, recordsBefore+1)
	}
	closeLiveness(t, third)

	doc.UserReplayWindow = 512
	if err := os.WriteFile(path, []byte(doc.yaml()), 0o644); err != nil {
		t.Fatalf("roll the configuration source back: %v", err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatalf("age the rolled-back source: %v", err)
	}
	fourth, err := Boot(path)
	if err != nil {
		t.Fatalf("boot the rolled-back restart: %v", err)
	}
	defer closeLiveness(t, fourth)
	if generation := fourth.Run().publisher.Current().Generation; generation != 3 {
		t.Fatalf("rolled-back restart generation = %d, want the chain's 3", generation)
	}
	fact := latestAttempt(t, fourth.Run())
	if fact.Result != publicationResultUnchanged {
		t.Fatalf("rolled-back restart attempt = %+v, want unchanged", fact)
	}
	if !strings.Contains(fact.Reason, "older than the chain") {
		t.Fatalf("rolled-back restart reason = %q, want the lagging file named", fact.Reason)
	}
	if got := countFacts(chainFacts(t, fourth.Run()), configPublicationRecordKind); got != recordsBefore+1 {
		t.Fatalf("publication records after the rolled-back restart = %d, want no new record", got)
	}
}

// TestPublishRecordLandsOnChain pins the durable publication record and its decision window.
// - one generation switch is read back from the chain by a fresh process
// - the record survives the restart with the same window
//
// 契约: docs/wiki/platform/org-hot-reload.md#apply-record
func TestPublishRecordLandsOnChain(t *testing.T) {
	doc := newLivenessDoc(t)
	path := doc.write(t)
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	host, err := Boot(path)
	if err != nil {
		t.Fatalf("boot the host: %v", err)
	}
	run := host.Run()
	doc.WorkerMax = 384
	if err := os.WriteFile(path, []byte(doc.yaml()), 0o644); err != nil {
		t.Fatalf("rewrite the configuration source: %v", err)
	}
	if _, err := host.Reload(context.Background()); err != nil {
		t.Fatalf("reload the second generation: %v", err)
	}
	record, held, err := run.latestPublishedRecord(context.Background())
	if err != nil || !held {
		t.Fatalf("read the newest publication record: held=%v err=%v", held, err)
	}
	if record.Generation != 2 {
		t.Fatalf("publication record generation = %d, want 2", record.Generation)
	}
	if record.PreviousGeneration() != 1 {
		t.Fatalf("publication record previous generation = %d, want 1", record.PreviousGeneration())
	}
	if got := record.ModelVersions()["worker"]; got != "local/model-w" {
		t.Fatalf("publication record worker model = %q, want the declared version", got)
	}
	if got := record.DimensionOutcomes()["worker|"+event.ConfigChangeDimensionBinding]; got != "applied" {
		t.Fatalf("publication record worker binding dimension = %q, want applied", got)
	}
	closeLiveness(t, host)

	restarted, err := Boot(path)
	if err != nil {
		t.Fatalf("boot beside the durable chain: %v", err)
	}
	defer closeLiveness(t, restarted)
	read, held, err := restarted.Run().latestPublishedRecord(context.Background())
	if err != nil || !held {
		t.Fatalf("read the durable publication record: held=%v err=%v", held, err)
	}
	if read.Generation != record.Generation || read.Fingerprint != record.Fingerprint {
		t.Fatalf("durable record = %+v, want the record of generation %d", read, record.Generation)
	}
	if got := read.ModelVersions()["worker"]; got != "local/model-w" {
		t.Fatalf("durable record worker model = %q, want the declared version", got)
	}
}

// TestCrossDimWindowAuditable pins the decision window of one cross-dimension generation switch.
// - a reader reconstructs both moves from one result fact
// - applied and skipped dimensions are named per agent
//
// 契约: docs/wiki/platform/org-hot-reload.md#apply-record
func TestCrossDimWindowAuditable(t *testing.T) {
	doc := newLivenessDoc(t)
	path := doc.write(t)
	host := bootLiveness(t, path)
	run := host.Run()
	doc.UserModel = "model-b"
	doc.WorkerMax = 384
	if err := os.WriteFile(path, []byte(doc.yaml()), 0o644); err != nil {
		t.Fatalf("rewrite the configuration source: %v", err)
	}
	report, err := host.Reload(context.Background())
	if err != nil {
		t.Fatalf("reload the cross-dimension move: %v", err)
	}
	if len(report.ModelChanges) != 1 || report.ModelChanges[0].Agent != "user" {
		t.Fatalf("judged model changes = %+v, want the one move of the entry agent", report.ModelChanges)
	}
	if len(report.ConfigChanges) != 1 || report.ConfigChanges[0].Agent != "worker" || report.ConfigChanges[0].Dimension != event.ConfigChangeDimensionBinding {
		t.Fatalf("judged configuration changes = %+v, want the one binding move of the worker", report.ConfigChanges)
	}
	fact := latestAttempt(t, run)
	if fact.Result != publicationResultPublished {
		t.Fatalf("cross-dimension attempt = %+v, want published", fact)
	}
	if fact.PreviousGeneration != 1 || fact.Generation != 2 {
		t.Fatalf("cross-dimension window = %d -> %d, want 1 -> 2", fact.PreviousGeneration, fact.Generation)
	}
	if got := fact.Models["user"]; got != "local/model-b" {
		t.Fatalf("window user model = %q, want the version the switch entered", got)
	}
	if got := fact.Models["worker"]; got != "local/model-w" {
		t.Fatalf("window worker model = %q, want the version it kept", got)
	}
	if got := fact.Dimensions["user|"+event.ConfigChangeDimensionModel]; got != "applied" {
		t.Fatalf("window user model dimension = %q, want applied", got)
	}
	if got := fact.Dimensions["worker|"+event.ConfigChangeDimensionBinding]; got != "applied" {
		t.Fatalf("window worker binding dimension = %q, want applied", got)
	}
	if got := fact.Dimensions["user|"+event.ConfigChangeDimensionBinding]; got != "skipped" {
		t.Fatalf("window user binding dimension = %q, want skipped", got)
	}
	if got := fact.Dimensions["worker|"+event.ConfigChangeDimensionModel]; got != "skipped" {
		t.Fatalf("window worker model dimension = %q, want skipped", got)
	}
	if fact.At.IsZero() {
		t.Fatal("the window records no time")
	}
}

// TestCarriedGenerationAddsNoRecord pins the unchanged result beside the record.
// - a reload of equivalent content writes a result fact and no second record
// - the ledger keeps one record per actual generation
//
// 契约: docs/wiki/platform/org-hot-reload.md#generations
func TestCarriedGenerationAddsNoRecord(t *testing.T) {
	doc := newLivenessDoc(t)
	path := doc.write(t)
	host := bootLiveness(t, path)
	run := host.Run()
	before, held, err := run.latestPublishedRecord(context.Background())
	if err != nil || !held {
		t.Fatalf("read the boot publication record: held=%v err=%v", held, err)
	}
	report, err := host.Reload(context.Background())
	if err != nil {
		t.Fatalf("reload the unchanged document: %v", err)
	}
	if report.Publication.Published {
		t.Fatalf("unchanged reload publication = %+v, want the carried generation", report.Publication)
	}
	after, held, err := run.latestPublishedRecord(context.Background())
	if err != nil || !held {
		t.Fatalf("read the publication record after the carried reload: held=%v err=%v", held, err)
	}
	if after.Generation != before.Generation {
		t.Fatalf("record after a carried reload = %d, want the same generation %d", after.Generation, before.Generation)
	}
	if fact := latestAttempt(t, run); fact.Result != publicationResultUnchanged {
		t.Fatalf("carried reload attempt = %+v, want unchanged", fact)
	}
}

// TestConfigChainFindsItsNewestFactByKeyAlone pins the discovery the chain's keys bound.
//   - the newest fact of the chain is found through the commit order's keys, so the statements of other families the walk passes cost no value
//   - the values the load reads are the facts of the chain alone, and the chain it answers is every fact it holds
//
// 契约: docs/wiki/platform/org-hot-reload.md#generations
func TestConfigChainFindsItsNewestFactByKeyAlone(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	tier, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root, WriteRights: true})
	if err != nil {
		t.Fatalf("open the storage tier of the chain fixture: %v", err)
	}
	defer func() {
		if err := tier.Close(); err != nil {
			t.Fatalf("close the storage tier of the chain fixture: %v", err)
		}
	}()
	writer := &configChain{}
	writer.attach(ledger.TierFactLog(tier))
	for at := 1; at <= 2; at++ {
		if _, err := writer.append(ctx, configFact{Kind: configDeclarationKind}); err != nil {
			t.Fatalf("commit configuration fact %d: %v", at, err)
		}
	}
	for at := 1; at <= 6; at++ {
		if err := ledger.TierFactLog(tier).Put(ctx, fmt.Sprintf("1:evt:%d:1", at), []byte("body")); err != nil {
			t.Fatalf("commit event fact %d: %v", at, err)
		}
	}
	readerTier, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root})
	if err != nil {
		t.Fatalf("open the reading view of the chain fixture: %v", err)
	}
	defer func() {
		if err := readerTier.Close(); err != nil {
			t.Fatalf("close the reading view of the chain fixture: %v", err)
		}
	}()
	probe, held := readerTier.Store.(interface{ ValueReads() int64 })
	if !held {
		t.Fatalf("the reading view holds the engine %T, which states no value read measure, so the discovery is not measured", readerTier.Store)
	}
	before := probe.ValueReads()
	reader := &configChain{}
	reader.attach(ledger.TierFactLog(readerTier))
	if err := reader.load(ctx); err != nil {
		t.Fatalf("load the configuration chain: %v", err)
	}
	if got := len(reader.facts); got != 2 {
		t.Fatalf("the load answers %d fact(s), want the 2 the chain holds", got)
	}
	if reads := probe.ValueReads() - before; reads != 2 {
		t.Fatalf("the load read %d value(s) for the 2 fact(s) of the chain, want the chain alone: the discovery read a value of the 6 statement(s) it walked past", reads)
	}
}

// TestConfigChainRefusesAHoleInItsSequence pins the refusal of a chain missing a fact.
//   - a chain whose newest fact states a sequence the log holds no fact of is refused by name
//   - the refusal is the load's, so a boot never serves a chain shorter than the one the ledger states
//
// 契约: docs/wiki/platform/org-hot-reload.md#generations
func TestConfigChainRefusesAHoleInItsSequence(t *testing.T) {
	ctx := context.Background()
	tier, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir(), WriteRights: true})
	if err != nil {
		t.Fatalf("open the storage tier of the chain fixture: %v", err)
	}
	defer func() {
		if err := tier.Close(); err != nil {
			t.Fatalf("close the storage tier of the chain fixture: %v", err)
		}
	}()
	log := ledger.TierFactLog(tier)
	writer := &configChain{}
	writer.attach(log)
	for at := 1; at <= 2; at++ {
		if _, err := writer.append(ctx, configFact{Kind: configDeclarationKind}); err != nil {
			t.Fatalf("commit configuration fact %d: %v", at, err)
		}
	}
	first := fmt.Sprintf("%s%020d", configChainPrefix, 1)
	if err := tier.Store.Commit(ctx, ledger.Batch{Deletes: []string{first}}); err != nil {
		t.Fatalf("delete the first fact of the chain: %v", err)
	}
	reader := &configChain{}
	reader.attach(log)
	err = reader.load(ctx)
	if err == nil {
		t.Fatalf("the load answered %d fact(s) of a chain missing the fact %s, want the hole refused by name", len(reader.facts), first)
	}
	if !strings.Contains(err.Error(), "a hole is a damaged ledger") {
		t.Fatalf("the load refused the chain with %v, want the hole in its own sequence named", err)
	}
}

// chainDiscoveryView is one root's durable engine behind a view that counts what a boot
// asks of the configuration chain's discovery.
//   - A point read and a prefix read under the chain's prefix are counted apart, so a discovery that names its keys is told apart from one that reads the family whole
//   - The key-only commit-order walk counts the keys it names, and the value walk is forwarded uncounted: every boot names its index entries through the value walk, so the key-only walk is what answers whether the discovery walks the history
//   - The engine opens on the first call, so the boot pays the one open the tier pays beside no wrapper
type chainDiscoveryView struct {
	mu       sync.Mutex
	root     string
	engine   ledger.AtomicStore
	points   int
	prefixes int
	walked   int
}

// open answers the engine the view wraps, opening the durable engine of its root on the
// first call.
func (v *chainDiscoveryView) open(ctx context.Context) (ledger.AtomicStore, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.engine != nil {
		return v.engine, nil
	}
	store, err := ledger.OpenAtomicStore(ctx, ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: v.root, WriteRights: true})
	if err != nil {
		return nil, err
	}
	v.engine = store
	return store, nil
}

// Read answers the fetch through the engine the view wraps, counting what the fetch
// named of the chain's own family.
func (v *chainDiscoveryView) Read(ctx context.Context, fetch ledger.Fetch) ([]ledger.Pair, error) {
	engine, err := v.open(ctx)
	if err != nil {
		return nil, err
	}
	v.mu.Lock()
	if fetch.Key != "" && strings.HasPrefix(fetch.Key, configChainPrefix) {
		v.points++
	}
	if fetch.Prefix != "" && strings.HasPrefix(fetch.Prefix, configChainPrefix) {
		v.prefixes++
	}
	v.mu.Unlock()
	return engine.Read(ctx, fetch)
}

// Keys answers the keys the prefix names through the engine the view wraps, counting an
// enumeration of the chain's own family.
func (v *chainDiscoveryView) Keys(ctx context.Context, prefix string) ([]string, error) {
	engine, err := v.open(ctx)
	if err != nil {
		return nil, err
	}
	v.mu.Lock()
	if strings.HasPrefix(prefix, configChainPrefix) {
		v.prefixes++
	}
	v.mu.Unlock()
	return engine.Keys(ctx, prefix)
}

// Namespaces answers the namespaces the store holds through the engine the view wraps.
func (v *chainDiscoveryView) Namespaces(ctx context.Context) ([]string, error) {
	engine, err := v.open(ctx)
	if err != nil {
		return nil, err
	}
	return engine.Namespaces(ctx)
}

// Commit applies one batch through the engine the view wraps.
func (v *chainDiscoveryView) Commit(ctx context.Context, batch ledger.Batch) error {
	engine, err := v.open(ctx)
	if err != nil {
		return err
	}
	return engine.Commit(ctx, batch)
}

// EachCommittedNewestFirst hands the value walk to the order the engine states.
func (v *chainDiscoveryView) EachCommittedNewestFirst(ctx context.Context, visit func(key string, value []byte) (bool, error)) error {
	engine, err := v.open(ctx)
	if err != nil {
		return err
	}
	ordered, held := engine.(ledger.CommitOrder)
	if !held {
		return fmt.Errorf("tagent: the engine %T states no committed order, so the view beside it states none", engine)
	}
	return ordered.EachCommittedNewestFirst(ctx, visit)
}

// EachCommittedKeyNewestFirst hands the key-only walk to the order the engine states,
// counting the keys it names.
func (v *chainDiscoveryView) EachCommittedKeyNewestFirst(ctx context.Context, visit func(key string) (bool, error)) error {
	engine, err := v.open(ctx)
	if err != nil {
		return err
	}
	keys, held := engine.(interface {
		EachCommittedKeyNewestFirst(ctx context.Context, visit func(key string) (bool, error)) error
	})
	if !held {
		return fmt.Errorf("tagent: the engine %T states no key-only committed order, so the view beside it states none", engine)
	}
	return keys.EachCommittedKeyNewestFirst(ctx, func(key string) (bool, error) {
		v.mu.Lock()
		v.walked++
		v.mu.Unlock()
		return visit(key)
	})
}

// counted answers what the view counted so far.
func (v *chainDiscoveryView) counted() chainDiscoveryCounts {
	v.mu.Lock()
	defer v.mu.Unlock()
	return chainDiscoveryCounts{points: v.points, prefixes: v.prefixes, walkKeys: v.walked}
}

// chainDiscoveryCounts is what one boot asked of the chain's discovery.
type chainDiscoveryCounts struct {
	points   int
	prefixes int
	walkKeys int
}

// chainDiscovery is what one boot asked of the chain's discovery beside the chain it
// answered.
type chainDiscovery struct {
	counts      chainDiscoveryCounts
	facts       []configFact
	fingerprint string
	generation  uint64
	recordHeld  bool
}

// chainDiscoveryDeclaration answers the declaration every discovery fixture boots with:
// two agents over one model reference, which is the declaration the assembly fixture
// publishes.
func chainDiscoveryDeclaration() *config.Declaration {
	return &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a", ReplayWindow: 60}, {ID: "agent-b", Model: "model-a", ReplayWindow: 60}},
	}
}

// measureChainDiscovery boots one root holding the named number of configuration facts
// beside the named number of unrelated commits, and answers what the boot asked of the
// chain's discovery.
//   - The newest configuration fact is the publication record of the declaration the boot carries, so the boot adopts the generation its chain records and files no fact of its own: the facts the boot answers are the fixture's
//   - The writer view closes before the boot opens, so the boot reads a log no other writer handle holds
func measureChainDiscovery(t *testing.T, ctx context.Context, facts, unrelated int) chainDiscovery {
	t.Helper()
	decl := chainDiscoveryDeclaration()
	fingerprint := decl.FactKey()
	if fingerprint == "" {
		t.Fatalf("the declaration of the discovery fixture answers no canonical fingerprint")
	}
	root := t.TempDir()
	tier, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root, WriteRights: true})
	if err != nil {
		t.Fatalf("open the writer view of the discovery fixture: %v", err)
	}
	writer := &configChain{}
	writer.attach(ledger.TierFactLog(tier))
	if _, err := writer.append(ctx, configFact{
		Kind:        configPublicationRecordKind,
		Type:        configPublicationFactType,
		Fingerprint: fingerprint,
		Generation:  1,
		Publication: &configPublicationWire{Generation: 1, Fingerprint: fingerprint, At: time.Now().UTC()},
	}); err != nil {
		t.Fatalf("commit the publication record of the discovery fixture: %v", err)
	}
	for at := 1; at < facts; at++ {
		if _, err := writer.append(ctx, configFact{
			Kind:        configPublicationResultKind,
			Type:        configPublicationFactType,
			Fingerprint: fingerprint,
			Generation:  1,
		}); err != nil {
			t.Fatalf("commit the configuration fact %d of the discovery fixture: %v", at+1, err)
		}
	}
	log := ledger.TierFactLog(tier)
	for at := 1; at <= unrelated; at++ {
		if err := log.Put(ctx, fmt.Sprintf("1:evt:%d:1", at), []byte("body")); err != nil {
			t.Fatalf("commit the unrelated fact %d of the discovery fixture: %v", at, err)
		}
	}
	if err := tier.Close(); err != nil {
		t.Fatalf("close the writer view of the discovery fixture: %v", err)
	}
	view := &chainDiscoveryView{root: root}
	provider := &snapshotTestModel{answer: func(*model.Request) *model.Response {
		return &model.Response{Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: factWindowAnswer}}}, Done: true}
	}}
	run, err := assemble(ctx, decl, map[event.AgentID]NativeAgent{"agent-a": {Model: provider}, "agent-b": {Model: provider}}, false,
		WithGovernance(snapshotTestGovernance{}),
		WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root, Engine: view}))
	if err != nil {
		t.Fatalf("boot the discovery fixture: %v", err)
	}
	measured := chainDiscovery{counts: view.counted(), facts: chainFacts(t, run)}
	record, held, err := run.latestPublishedRecord(ctx)
	if err != nil {
		t.Fatalf("read the publication record the discovery fixture booted from: %v", err)
	}
	measured.fingerprint = record.Fingerprint
	measured.generation = uint64(record.Generation)
	measured.recordHeld = held
	run.stopCheckpointFiler()
	endRun(t, run)
	return measured
}

// TestConfigChainDiscoveryCostsTheChainNotTheHistory pins the cost of the one discovery a boot pays before it reads the chain.
//   - The newest fact is found by naming the keys the chain can hold, so no commit-order walk runs
//   - The discovery costs the chain and never the history committed after it
//   - No enumeration of the configuration family runs: the discovery pays point reads of the names it asks for
//   - The point reads of the discovery are one count after 16 and after 512 unrelated commits, so the cost is a function of the chain
//   - At 1 and at 64 configuration facts the point reads stay inside the chain plus the binary digits of its length
//   - The bound is the chain's own values plus two probes per binary digit of its length
//   - So the discovery follows the chain and not the history
//   - Neither boot answers a shorter chain: both hold the same single fact and serve the same publication record
//
// 契约: docs/wiki/platform/org-hot-reload.md#generations
func TestConfigChainDiscoveryCostsTheChainNotTheHistory(t *testing.T) {
	ctx := context.Background()
	short := measureChainDiscovery(t, ctx, 1, 16)
	long := measureChainDiscovery(t, ctx, 1, 512)
	for _, measured := range []struct {
		name string
		got  chainDiscovery
	}{{"16 unrelated commits", short}, {"512 unrelated commits", long}} {
		if measured.got.counts.walkKeys != 0 {
			t.Fatalf("the discovery walked %d commit key(s) after %s, want none: the newest fact is found by the names the chain can hold, so the history beside it is never walked", measured.got.counts.walkKeys, measured.name)
		}
		if measured.got.counts.prefixes != 0 {
			t.Fatalf("the discovery enumerated the configuration family %d time(s) after %s, want none: the discovery names its keys instead of reading the family whole", measured.got.counts.prefixes, measured.name)
		}
	}
	if short.counts.walkKeys != long.counts.walkKeys {
		t.Fatalf("the discovery walked %d key(s) after 16 unrelated commits and %d after 512, want one count: the cost is a function of the chain", short.counts.walkKeys, long.counts.walkKeys)
	}
	if short.counts.points != long.counts.points {
		t.Fatalf("the discovery read %d point key(s) after 16 unrelated commits and %d after 512, want one count: the cost is a function of the chain", short.counts.points, long.counts.points)
	}
	want := chainDiscoveryDeclaration().FactKey()
	for _, measured := range []struct {
		name string
		got  chainDiscovery
	}{{"16 unrelated commits", short}, {"512 unrelated commits", long}} {
		if len(measured.got.facts) != 1 {
			t.Fatalf("the boot after %s answered %d configuration fact(s), want the 1 the fixture committed: the discovery skipped a fact of the chain", measured.name, len(measured.got.facts))
		}
		if !measured.got.recordHeld || measured.got.generation != 1 || measured.got.fingerprint != want {
			t.Fatalf("the boot after %s served record %q of generation %d (held=%t), want the record of generation 1 the fixture committed", measured.name, measured.got.fingerprint, measured.got.generation, measured.got.recordHeld)
		}
	}
	one := measureChainDiscovery(t, ctx, 1, 16)
	deep := measureChainDiscovery(t, ctx, 64, 16)
	if bound := 1 + 2*0 + 2; one.counts.points > bound {
		t.Fatalf("the discovery of a 1-fact chain read %d point key(s), want at most %d: the discovery is bounded by the chain", one.counts.points, bound)
	}
	if bound := 64 + 2*6 + 2; deep.counts.points > bound {
		t.Fatalf("the discovery of a 64-fact chain read %d point key(s), want at most %d: the discovery is bounded by the chain and its binary digits", deep.counts.points, bound)
	}
	if len(deep.facts) != 64 {
		t.Fatalf("the boot of a 64-fact chain answered %d fact(s), want the 64 the fixture committed", len(deep.facts))
	}
	t.Logf("discovery over 16 and 512 unrelated commits: point reads %d/%d, prefix reads %d/%d, walked keys %d/%d",
		short.counts.points, long.counts.points, short.counts.prefixes, long.counts.prefixes, short.counts.walkKeys, long.counts.walkKeys)
	t.Logf("discovery over 1 and 64 configuration facts: point reads %d/%d", one.counts.points, deep.counts.points)
}
