package rl

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/model"
)

// TestReplacementRefusalMatrix pins the replacement lease: publication needs both rebuild and publish, and a refusal keeps the version in effect.
// - a lease built beside no model is refused by name
// - a replacement naming no version is refused by name
// - a replacement naming the version in effect rebuilds nothing and publishes nothing
// - a failed rebuild and an empty rebuild publish no binding and close nothing they never built
// - a replacement carrying no rebuild is refused by name and records the refusal
// - a replacement carrying no publication is refused by name and releases the rebuilt model once
// - a publication that refuses releases the rebuilt model once and leaves the version in effect untouched
// - a closed lease installs no further version, however the replacement is built
// - every refusal is recorded as refused, naming the version asked for under the model name in effect
// 契约: docs/wiki/rl/rl-architecture.md#swappable-model
func TestReplacementRefusalMatrix(t *testing.T) {
	ctx := context.Background()

	newLease := func(t *testing.T, version string) (*ModelLease, *leaseModel) {
		t.Helper()
		initial := &leaseModel{name: "m"}
		lease, err := NewModelLease(initial, version)
		if err != nil {
			t.Fatalf("open the lease at version %q: %v", version, err)
		}
		return lease, initial
	}

	t.Run("a lease built beside no model is refused", func(t *testing.T) {
		if _, err := NewModelLease(nil, "v1"); !errors.Is(err, ErrLeaseModelMissing) {
			t.Fatalf("lease beside no model = %v, want ErrLeaseModelMissing", err)
		}
	})

	t.Run("a replacement naming no version is refused", func(t *testing.T) {
		lease, _ := newLease(t, "v1")
		if _, err := lease.Replace(ctx, "", func() (model.Model, error) {
			t.Fatal("a replacement naming no version ran its rebuild")
			return nil, nil
		}, nil); !errors.Is(err, ErrLeaseVersionMissing) {
			t.Fatalf("replacement naming no version = %v, want ErrLeaseVersionMissing", err)
		}
		if len(lease.Replacements()) != 0 {
			t.Fatalf("history = %v, want no record for a replacement that named no version", lease.Replacements())
		}
	})

	t.Run("a replacement naming the version in effect rebuilds nothing", func(t *testing.T) {
		lease, _ := newLease(t, "v1")
		rebuilds, publishes := 0, 0
		record, err := lease.Replace(ctx, "v1",
			func() (model.Model, error) { rebuilds++; return &leaseModel{name: "m"}, nil },
			func(ModelIdentity, model.Model) error { publishes++; return nil })
		if !errors.Is(err, ErrLeaseVersionHeld) {
			t.Fatalf("replacement naming the version in effect = %v, want ErrLeaseVersionHeld", err)
		}
		if rebuilds != 0 || publishes != 0 {
			t.Fatalf("runs = (rebuilds %d, publishes %d), want neither step reached", rebuilds, publishes)
		}
		if record != (ReplacementRecord{}) {
			t.Fatalf("record = %+v, want no record for a version already in effect", record)
		}
		if len(lease.Replacements()) != 0 || lease.CurrentVersion() != "m@v1" {
			t.Fatalf("lease = (history %v, current %q), want the version in effect untouched", lease.Replacements(), lease.CurrentVersion())
		}
	})

	t.Run("a failed rebuild publishes no binding", func(t *testing.T) {
		lease, _ := newLease(t, "v1")
		publishes := 0
		record, err := lease.Replace(ctx, "v2",
			func() (model.Model, error) { return nil, errors.New("provider refused the rebuild") },
			func(ModelIdentity, model.Model) error { publishes++; return nil })
		if !errors.Is(err, ErrLeaseRebuildFailed) {
			t.Fatalf("failed rebuild = %v, want ErrLeaseRebuildFailed", err)
		}
		if publishes != 0 {
			t.Fatalf("publishes = %d, want a failed rebuild to publish no binding", publishes)
		}
		assertRefused(t, record, "m@v2")
		if lease.CurrentVersion() != "m@v1" {
			t.Fatalf("current version = %q, want the version in effect kept", lease.CurrentVersion())
		}
	})

	t.Run("a rebuild yielding no model publishes no binding", func(t *testing.T) {
		lease, _ := newLease(t, "v1")
		record, err := lease.Replace(ctx, "v2",
			func() (model.Model, error) { return nil, nil },
			func(ModelIdentity, model.Model) error {
				t.Fatal("an empty rebuild reached the publication entry")
				return nil
			})
		if !errors.Is(err, ErrLeaseRebuildFailed) {
			t.Fatalf("empty rebuild = %v, want ErrLeaseRebuildFailed", err)
		}
		assertRefused(t, record, "m@v2")
	})

	t.Run("a replacement carrying no rebuild is refused", func(t *testing.T) {
		lease, _ := newLease(t, "v1")
		record, err := lease.Replace(ctx, "v2", nil, nil)
		if !errors.Is(err, ErrLeaseRebuildMissing) {
			t.Fatalf("replacement carrying no rebuild = %v, want ErrLeaseRebuildMissing", err)
		}
		assertRefused(t, record, "m@v2")
	})

	t.Run("a replacement carrying no publication releases the rebuilt model", func(t *testing.T) {
		lease, _ := newLease(t, "v1")
		rebuilt := &leaseModel{name: "m"}
		record, err := lease.Replace(ctx, "v2", func() (model.Model, error) { return rebuilt, nil }, nil)
		if !errors.Is(err, ErrLeasePublishMissing) {
			t.Fatalf("replacement carrying no publication = %v, want ErrLeasePublishMissing", err)
		}
		if closed := rebuilt.closed.Load(); closed != 1 {
			t.Fatalf("rebuilt model closed %d times, want the unpublished model released exactly once", closed)
		}
		assertRefused(t, record, "m@v2")
		if lease.CurrentVersion() != "m@v1" {
			t.Fatalf("current version = %q, want the version in effect kept", lease.CurrentVersion())
		}
	})

	t.Run("a publication that refuses leaves the version in effect untouched", func(t *testing.T) {
		lease, _ := newLease(t, "v1")
		rebuilt := &leaseModel{name: "m"}
		publishes := 0
		record, err := lease.Replace(ctx, "v2",
			func() (model.Model, error) { return rebuilt, nil },
			func(identity ModelIdentity, _ model.Model) error {
				publishes++
				if identity.Name() != "m@v2" {
					t.Fatalf("published identity = %q, want the requested version", identity.Name())
				}
				return errors.New("the binding refused the rebuild")
			})
		if !errors.Is(err, ErrLeasePublishFailed) {
			t.Fatalf("refusing publication = %v, want ErrLeasePublishFailed", err)
		}
		if publishes != 1 {
			t.Fatalf("publishes = %d, want the refused publication attempted once", publishes)
		}
		if closed := rebuilt.closed.Load(); closed != 1 {
			t.Fatalf("rebuilt model closed %d times, want the refused model released exactly once", closed)
		}
		assertRefused(t, record, "m@v2")
		if lease.CurrentVersion() != "m@v1" {
			t.Fatalf("current version = %q, want the version in effect kept", lease.CurrentVersion())
		}
	})

	t.Run("a closed lease installs no further version", func(t *testing.T) {
		lease, _ := newLease(t, "v1")
		if err := lease.Close(); err != nil {
			t.Fatalf("close the lease: %v", err)
		}
		record, err := lease.Replace(ctx, "v2", func() (model.Model, error) {
			t.Fatal("a closed lease ran a rebuild")
			return nil, nil
		}, nil)
		if !errors.Is(err, ErrLeaseClosed) {
			t.Fatalf("replacement on a closed lease = %v, want ErrLeaseClosed", err)
		}
		if record != (ReplacementRecord{}) {
			t.Fatalf("record = %+v, want no record for a replacement a closed lease never attempted", record)
		}
	})
}

// TestReplacementPinsTheServingGeneration pins the single-generation rule: an open stream keeps its version to the end, and the retired model closes once.
// - a replacement retires the previous version without closing it while a stream is open
// - a response read after the replacement still declares the retired version
// - the published handle serves the new version and the lease reports it as current
// - a request on the retired handle is refused by name once the version is closed
// - the retired model is closed exactly once, after its last stream ended
// 契约: docs/wiki/rl/rl-architecture.md#swappable-model
func TestReplacementPinsTheServingGeneration(t *testing.T) {
	ctx := context.Background()
	release := make(chan struct{})
	previous := &leaseModel{name: "m", hold: release}
	lease, err := NewModelLease(previous, "v1")
	if err != nil {
		t.Fatalf("open the lease: %v", err)
	}
	held := lease.Current()

	stream, err := held.GenerateContent(ctx, &model.Request{})
	if err != nil {
		t.Fatalf("serve the first request: %v", err)
	}
	if inFlight := lease.InFlight(); inFlight != 1 {
		t.Fatalf("in flight after the first stream opened = %d, want the one stream", inFlight)
	}

	next := &leaseModel{name: "m"}
	var published *VersionedModel
	record, err := lease.Replace(ctx, "v2",
		func() (model.Model, error) { return next, nil },
		func(identity ModelIdentity, rebuilt model.Model) error {
			handle, ok := rebuilt.(*VersionedModel)
			if !ok {
				t.Fatalf("published model = %T, want the versioned handle", rebuilt)
			}
			if got := handle.Info().Name; got != identity.Model {
				t.Fatalf("published handle serves %q, want the identity the publication accepted", got)
			}
			published = handle
			return nil
		})
	if err != nil {
		t.Fatalf("replace the model: %v", err)
	}
	if record.State != ReplacementApplied || record.Reason != "" || record.From != "m@v1" || record.To != "m@v2" {
		t.Fatalf("record = %+v, want an applied replacement from the old version to the new one", record)
	}
	if closed := previous.closed.Load(); closed != 0 {
		t.Fatalf("retired model closed %d times while its stream is open, want it kept serving", closed)
	}
	if got := lease.CurrentVersion(); got != "m@v2" {
		t.Fatalf("current version = %q, want the published version", got)
	}

	response := <-stream
	if response == nil || response.Model != "m@v1" {
		t.Fatalf("response read after the replacement = %+v, want it declaring the retired version", response)
	}

	_, err = lease.Current().GenerateContent(ctx, &model.Request{})
	if err != nil {
		t.Fatalf("serve a request that began after the replacement: %v", err)
	}
	if published == nil {
		t.Fatal("the publication entry was never reached")
	}

	close(release)
	models := []string{}
	for rest := range stream {
		if rest != nil {
			models = append(models, rest.Model)
		}
	}
	for _, served := range models {
		if served != "m@v1" {
			t.Fatalf("stream responses = %v, want every response of the pinned stream under the retired version", models)
		}
	}
	waitFor(t, "the retired model to close once", func() bool { return previous.closed.Load() == 1 })
	waitFor(t, "the lease to account every stream", func() bool { return lease.InFlight() == 0 })

	if _, err := held.GenerateContent(ctx, &model.Request{}); !errors.Is(err, ErrLeaseVersionClosed) {
		t.Fatalf("request on the closed version = %v, want ErrLeaseVersionClosed", err)
	}
	if closed := previous.closed.Load(); closed != 1 {
		t.Fatalf("retired model closed %d times, want exactly one close", closed)
	}
}

// TestReplacementRestoreKeepsOneGeneration pins restore: the history must end at the held version, and a restored account never moves the version in effect.
// - a history ending at another version is refused by name and appends nothing
// - a record naming no version is refused by name
// - a history ending at the held version is accepted and its records stamped with the schema
// - a refusal recorded after the applied version is an account, not a switch
// 契约: docs/wiki/rl/rl-architecture.md#swappable-model
func TestReplacementRestoreKeepsOneGeneration(t *testing.T) {
	lease, err := NewModelLease(&leaseModel{name: "m"}, "v2")
	if err != nil {
		t.Fatalf("open the lease: %v", err)
	}
	mismatch := []ReplacementRecord{{To: "m@v1", State: ReplacementApplied}}
	if err := lease.Restore(mismatch); !errors.Is(err, ErrLeaseRestoreMismatch) {
		t.Fatalf("restoring a history ending at another version = %v, want ErrLeaseRestoreMismatch", err)
	}
	if len(lease.Replacements()) != 0 {
		t.Fatalf("history after a refused restore = %v, want nothing appended", lease.Replacements())
	}
	if err := lease.Restore([]ReplacementRecord{{State: ReplacementApplied}}); !errors.Is(err, ErrLeaseHistoryInvalid) {
		t.Fatalf("restoring a record naming no version = %v, want ErrLeaseHistoryInvalid", err)
	}

	history := []ReplacementRecord{
		{To: "m@v2", State: ReplacementApplied},
		{To: "m@v3", State: ReplacementRefused, Reason: "the rebuild failed"},
	}
	if err := lease.Restore(history); err != nil {
		t.Fatalf("restore a history ending at the held version: %v", err)
	}
	records := lease.Replacements()
	if len(records) != 2 {
		t.Fatalf("restored records = %v, want the two account entries", records)
	}
	for _, record := range records {
		if record.Schema != SchemaCurrent {
			t.Fatalf("restored record schema = %q, want the current schema stamped", record.Schema)
		}
	}
	if got := lease.CurrentVersion(); got != "m@v2" {
		t.Fatalf("current version after restore = %q, want the held version, never a restored refusal", got)
	}
	if recorded := lease.Replacements()[1]; recorded.State != ReplacementRefused || recorded.Reason == "" {
		t.Fatalf("restored refusal = %+v, want the refused account kept as recorded", recorded)
	}
}

// assertRefused pins the shape of one refused replacement record: refused, naming the
// version asked for under the model name in effect, with its reason recorded.
func assertRefused(t *testing.T, record ReplacementRecord, to string) {
	t.Helper()
	if record.State != ReplacementRefused || record.Reason == "" {
		t.Fatalf("record = %+v, want a refused attempt carrying its reason", record)
	}
	if record.To != to {
		t.Fatalf("refused record names %q, want the version asked for under the model in effect %q", record.To, to)
	}
}

// waitFor waits until one condition holds, so a close that runs on the stream's own
// goroutine is observed without a fixed sleep.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// leaseModel is one model instance a lease holds: it reports the identity it serves on
// every response, counts its closes and, when hold is set, keeps its stream open until
// hold is closed.
type leaseModel struct {
	name   string
	hold   chan struct{}
	closed atomic.Int64
}

func (m *leaseModel) Info() model.Info {
	return model.Info{Name: m.name, ContextWindow: 4096}
}

func (m *leaseModel) GenerateContent(ctx context.Context, _ *model.Request) (<-chan *model.Response, error) {
	out := make(chan *model.Response, 2)
	out <- &model.Response{
		Model:   m.name,
		Choices: []model.Choice{{Index: 0, Message: model.Message{Role: "assistant", Content: "served by " + m.name}}},
	}
	if m.hold == nil {
		out <- &model.Response{Model: m.name, Done: true}
		close(out)
		return out, nil
	}
	go func() {
		<-m.hold
		out <- &model.Response{Model: m.name, Done: true}
		close(out)
	}()
	return out, nil
}

func (m *leaseModel) Close() error {
	m.closed.Add(1)
	return nil
}
