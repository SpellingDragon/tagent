// The retired-state hygiene of the composition root: a publication record is
// committed as one chain fact and encoded once, the trajectory capture runs
// under the mechanism's own budget, and the hot-window pins a restart keeps are
// re-seeded from the chain before the boot trim.
//
// 契约: docs/wiki/platform/org-hot-reload.md#apply-record
package assembly

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/features/rl"
	"github.com/SpellingDragon/tagent/memory/ledger"
)

// TestPublicationNoShadowCopy pins the recorder's one copy of a publication.
//   - the recorder keeps the mutex and the chain alone, no slice beside them
//   - the record a restarted process reads is the chain fact it committed
//
// 契约: docs/wiki/platform/org-hot-reload.md#apply-record
func TestPublicationNoShadowCopy(t *testing.T) {
	recorder := reflect.TypeOf(publicationRecords{})
	for index := 0; index < recorder.NumField(); index++ {
		field := recorder.Field(index)
		switch field.Type.Kind() {
		case reflect.Slice, reflect.Map, reflect.Array:
			t.Fatalf("publicationRecords carries the field %q of kind %s, want the mutex and the chain alone: the chain fact is the one copy of a record",
				field.Name, field.Type.Kind())
		}
	}

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
	committed, held, err := run.latestPublishedRecord(context.Background())
	if err != nil || !held {
		t.Fatalf("read the committed publication record: held=%v err=%v", held, err)
	}
	wantRecords := countFacts(chainFacts(t, run), configPublicationRecordKind)
	closeLiveness(t, host)

	restarted, err := Boot(path)
	if err != nil {
		t.Fatalf("boot beside the durable chain: %v", err)
	}
	defer closeLiveness(t, restarted)
	read, held, err := restarted.Run().latestPublishedRecord(context.Background())
	if err != nil || !held {
		t.Fatalf("read the record beside the restarted process: held=%v err=%v", held, err)
	}
	if read.Generation != committed.Generation || read.Fingerprint != committed.Fingerprint {
		t.Fatalf("record beside the restart = generation %d fingerprint %q, want the committed generation %d fingerprint %q",
			read.Generation, read.Fingerprint, committed.Generation, committed.Fingerprint)
	}
	if got := countFacts(chainFacts(t, restarted.Run()), configPublicationRecordKind); got != wantRecords {
		t.Fatalf("publication record facts beside the restart = %d, want the %d the first process committed and no second copy", got, wantRecords)
	}
}

// TestConfigFactSingleEncoding pins the one encoding of a publication record.
//   - a record fact carries the record in its publication wire alone
//   - the fact-level record fields stay empty, so no data is written twice
//   - the record reads back whole: identity, window, models and dimensions
//
// 契约: docs/FACT-VOCABULARY.md#families
func TestConfigFactSingleEncoding(t *testing.T) {
	doc := newLivenessDoc(t)
	path := doc.write(t)
	host := bootLiveness(t, path)
	run := host.Run()
	doc.WorkerMax = 384
	doc.UserModel = "model-b"
	if err := os.WriteFile(path, []byte(doc.yaml()), 0o644); err != nil {
		t.Fatalf("rewrite the configuration source: %v", err)
	}
	if _, err := host.Reload(context.Background()); err != nil {
		t.Fatalf("reload the cross-dimension move: %v", err)
	}

	records := 0
	for _, fact := range chainFacts(t, run) {
		if fact.Kind != configPublicationRecordKind {
			continue
		}
		records++
		if fact.Publication == nil {
			t.Fatalf("record fact %d carries no publication wire", fact.Seq)
		}
		if fact.Fingerprint != "" || fact.Generation != 0 || fact.PreviousGeneration != 0 ||
			fact.PreviousFingerprint != "" || fact.Result != "" || len(fact.Models) != 0 || len(fact.Dimensions) != 0 {
			t.Fatalf("record fact %d repeats its record beside the wire: fingerprint=%q generation=%d previous=%d result=%q models=%d dimensions=%d",
				fact.Seq, fact.Fingerprint, fact.Generation, fact.PreviousGeneration, fact.Result, len(fact.Models), len(fact.Dimensions))
		}
		if fact.Publication.Fingerprint == "" || fact.Publication.Generation == 0 || fact.Publication.At.IsZero() {
			t.Fatalf("record fact %d carries an empty wire: %+v", fact.Seq, fact.Publication)
		}
	}
	if records == 0 {
		t.Fatal("the chain holds no publication record fact after the boot and the reload")
	}

	record, held, err := run.latestPublishedRecord(context.Background())
	if err != nil || !held {
		t.Fatalf("read the newest publication record: held=%v err=%v", held, err)
	}
	if record.Generation != 2 || record.PreviousGeneration() != 1 {
		t.Fatalf("record window = %d from %d, want 2 from 1", record.Generation, record.PreviousGeneration())
	}
	if got := record.ModelVersions()["user"]; got != "local/model-b" {
		t.Fatalf("record user model = %q, want the version the switch entered", got)
	}
	if got := record.DimensionOutcomes()["worker|"+event.ConfigChangeDimensionBinding]; got != "applied" {
		t.Fatalf("record worker binding dimension = %q, want applied", got)
	}
	if len(record.Checks) == 0 {
		t.Fatal("record carries no check answers, want the judgments the wire serializes")
	}
}

// TestConfigFactLegacyReadable pins the record a fact written before the convergence answers.
//   - the build that wrote both encodings is readable, so the single wire changes what is written
//   - the single wire never changes what an old chain means
//
// 契约: docs/FACT-VOCABULARY.md#families
func TestConfigFactLegacyReadable(t *testing.T) {
	legacy := map[string]any{
		"seq":                  1,
		"kind":                 configPublicationRecordKind,
		"type":                 configPublicationFactType,
		"at":                   "2026-11-05T18:27:49.135016Z",
		"fingerprint":          "fp-legacy",
		"generation":           3,
		"previous_generation":  2,
		"previous_fingerprint": "fp-previous",
		"result":               publicationResultPublished,
		"models":               map[string]string{"user": "local/model-legacy"},
		"dimensions":           map[string]string{"user|model": "applied"},
		"publication": map[string]any{
			"generation":           3,
			"fingerprint":          "fp-legacy",
			"result":               1,
			"previous_generation":  2,
			"previous_fingerprint": "fp-previous",
			"models":               map[string]string{"user": "local/model-legacy"},
			"dimensions":           map[string]string{"user|model": "applied"},
			"at":                   "2026-11-05T18:27:49.124942Z",
		},
	}
	body, err := json.Marshal(legacy)
	if err != nil {
		t.Fatalf("render the legacy record fact: %v", err)
	}
	log := newMemoryFactLog()
	if err := log.Put(context.Background(), fmt.Sprintf("%s%020d", configChainPrefix, 1), body); err != nil {
		t.Fatalf("commit the legacy record fact: %v", err)
	}
	run := &Run{}
	run.records.chain.attach(log)

	record, held, err := run.latestPublishedRecord(context.Background())
	if err != nil {
		t.Fatalf("read the legacy publication record: %v", err)
	}
	if !held {
		t.Fatal("the legacy record fact answered no record")
	}
	if record.Generation != 3 || record.Fingerprint != "fp-legacy" || record.PreviousGeneration() != 2 || record.PreviousFingerprint() != "fp-previous" {
		t.Fatalf("legacy record = generation %d fingerprint %q from %d/%q, want 3 fp-legacy from 2/fp-previous",
			record.Generation, record.Fingerprint, record.PreviousGeneration(), record.PreviousFingerprint())
	}
	if got := record.ModelVersions()["user"]; got != "local/model-legacy" {
		t.Fatalf("legacy record user model = %q, want the version the fact carries", got)
	}
	if got := record.DimensionOutcomes()["user|model"]; got != "applied" {
		t.Fatalf("legacy record user model dimension = %q, want applied", got)
	}
	if !record.RecordedAt().Equal(time.Date(2026, 11, 5, 18, 27, 49, 124942000, time.UTC)) {
		t.Fatalf("legacy record time = %s, want the moment the window carries", record.RecordedAt())
	}
}

// TestCaptureNoBudgetParam pins the capture opened from the sink's own budget.
//   - the opener takes the storage root alone: no deployment override travels beside it
//   - the capture lands under <root>/trajectories, clears a stale swap store and seals
//
// 契约: docs/wiki/rl/rl-architecture.md#trajectory-capture
func TestCaptureNoBudgetParam(t *testing.T) {
	var _ func(string) (*rl.TrajectoryCapture, error) = openHostCapture
	root := t.TempDir()
	dir := filepath.Join(root, hostTrajectoriesDir)
	stale := filepath.Join(dir, hostCaptureSwapDir, "stale.snapshot")
	if err := os.MkdirAll(filepath.Dir(stale), 0o755); err != nil {
		t.Fatalf("prepare the swap store: %v", err)
	}
	if err := os.WriteFile(stale, []byte("stale"), 0o644); err != nil {
		t.Fatalf("write the stale snapshot: %v", err)
	}

	capture, err := openHostCapture(root)
	if err != nil {
		t.Fatalf("open the trajectory capture: %v", err)
	}
	if err := sealHostCaptureErr(capture); err != nil {
		t.Fatalf("seal the trajectory capture: %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("the trajectory directory %s is missing beside the opened capture: %v", dir, err)
	}
	if _, err := os.Stat(stale); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the stale swap store survived the open (stat: %v), want the swap directory cleared", err)
	}
}

// sealHostCaptureErr seals one capture and answers what the writer reported.
func sealHostCaptureErr(capture *rl.TrajectoryCapture) error {
	ctx, cancel := context.WithTimeout(context.Background(), hostShutdownGrace)
	defer cancel()
	manifest, err := capture.Seal(ctx)
	if err != nil {
		return err
	}
	if manifest.CaptureID != hostCaptureID || !manifest.Sealed {
		return fmt.Errorf("seal = capture %q sealed %v, want the boot capture confirmed by the writer", manifest.CaptureID, manifest.Sealed)
	}
	return nil
}

// TestPinnedFactsReseeded pins the boot order of the hot-window pins.
//   - the delivery account restored from the chain pins the settlement fact of every obligation that still owes something
//   - the pins land before the seeded chains are trimmed, so a restart keeps what the previous process kept
//   - a seed trimmed before any pin is restored drops the settlement fact
//   - the restarted run holds the pin the live process held
//   - the pinned settlement fact survives the boot trim
//
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
func TestPinnedFactsReseeded(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	doc := newLivenessDoc(t)
	path := doc.write(t)
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	host, err := Boot(path)
	if err != nil {
		t.Fatalf("boot the host: %v", err)
	}
	run := host.Run()
	addr := event.Address{AgentID: "user", SessionID: "session-pin-reseed"}
	identity := "delivery-pin-reseed"
	delivery := plugin.Delivery{Content: "held across the restart", Lineage: "user", Target: addr}
	if err := host.writeDeliverySettlement(ctx, addr, identity, heldStateOf(deliveryReasonApproval), delivery, time.Now(), nil); err != nil {
		t.Fatalf("write the unsettled settlement fact: %v", err)
	}
	key := deliverySettlementKey(identity, heldStateOf(deliveryReasonApproval))
	for index := 0; index < noWindowResidentFacts+32; index++ {
		if _, err := run.commitFactChecked(ctx, event.Event{
			Type:      event.TypeFeedback,
			Timestamp: time.Now(),
			Owner:     addr,
			Source:    addr,
			Target:    addr,
			Meta:      map[string]string{event.MetaKeyEventSummary: fmt.Sprintf("filler %d", index)},
		}); err != nil {
			t.Fatalf("commit filler %d: %v", index, err)
		}
	}
	if _, err := run.commitFactChecked(ctx, event.Event{
		Type:      event.TypeAgentOutput,
		Timestamp: time.Now(),
		Message:   model.Message{Role: model.RoleAssistant, Content: "close the seeded segment"},
		Owner:     addr,
		Source:    addr,
		Target:    addr,
	}); err != nil {
		t.Fatalf("close the seeded segment: %v", err)
	}
	if !run.pinnedFact(key) {
		t.Fatal("the live process lost the pin of the unsettled settlement fact")
	}
	closeLiveness(t, host)

	early := snapshotTestRunWithTrim(t, ctx, true, WithStorage(ledger.StorageConfig{
		Type: ledger.StorageLocalFile,
		Root: filepath.Join(doc.Root, "storage"),
	}))
	if _, held := early.factOf(key); held {
		t.Fatal("the early-trim control kept the settlement fact, so it does not exercise the pin the boot restores")
	}

	restarted, err := Boot(path)
	if err != nil {
		t.Fatalf("boot beside the committed chain: %v", err)
	}
	defer closeLiveness(t, restarted)
	if !restarted.Run().pinnedFact(key) {
		t.Fatal("the restarted run holds no pin on the unsettled settlement fact, want the pin re-seeded from the chain before the boot trim")
	}
	if _, held := restarted.Run().factOf(key); !held {
		t.Fatal("the unsettled settlement fact was trimmed during the boot, want it resident under its re-seeded pin")
	}
}
