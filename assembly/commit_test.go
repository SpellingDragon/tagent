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
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
)

// livenessDoc is one source document of the configuration liveness fixture:
// two agents, one provider, an optional durable storage root.
type livenessDoc struct {
	Root          string
	UserProvider  string
	UserModel     string
	WorkerModel   string
	UserMaxTokens int
	WorkerMax     int
	Storage       bool
}

// yaml renders the source document.
func (d livenessDoc) yaml() string {
	storage := ""
	if d.Storage {
		storage = "storage: { type: localfile, root: " + filepath.Join(d.Root, "storage") + " }\n"
	}
	maxTokens := d.UserMaxTokens
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
    max_tokens: %d
  worker:
    model: %s
    max_tokens: %d
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

	doc.UserMaxTokens = 512
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

	doc.UserMaxTokens = 640
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
	doc.UserMaxTokens = 512
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

	doc.UserMaxTokens = 768
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

	doc.UserMaxTokens = 512
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
