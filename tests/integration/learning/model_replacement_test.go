package learning

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/features/rl"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/state"
)

// TestModelReplacementLifecycle drives the model replacement lease over the formal runtime entry and checks its lifecycle.
//   - a replacement publishes one new binding generation, so later requests run the new version and earlier records keep theirs
//   - a failed rebuild publishes no binding at all, records the attempt as refused beside its reason and leaves the version untouched
//   - a request already in flight keeps its version until its stream ends, and that version is closed only once the stream ended
//   - a restarted lease reads the persisted version back and refuses a history that ends on another version
//
// 契约: docs/wiki/rl/rl-architecture.md#swappable-model
func TestModelReplacementLifecycle(t *testing.T) {
	t.Run("替换后新请求绑新版本且旧记录不改: a replacement binds later requests to the new version and leaves the earlier records as they were", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		dir := t.TempDir()
		writer, err := rl.NewFileCaptureWriter(dir, 0)
		if err != nil {
			t.Fatalf("build the capture writer: %v", err)
		}
		sink, err := rl.NewCaptureSink("cap-replacement", rl.SinkConfig{}, writer)
		if err != nil {
			t.Fatalf("build the capture sink: %v", err)
		}
		first := newReplacementModel("model-a", "version one answer")
		lease, err := rl.NewModelLease(first, "v1")
		if err != nil {
			t.Fatalf("build the model lease: %v", err)
		}
		if lease.CurrentVersion() != "model-a@v1" {
			t.Fatalf("the lease holds %q, want the initial version it was built with", lease.CurrentVersion())
		}
		run := replacementRun(t, ctx, lease.Current(), lease.CurrentVersion(), sink)

		if turn := replacementInvoke(t, ctx, run, "inv-replacement-1", "session-replacement-1", "ep-replacement-1"); turn.Terminal != model.KindCompleted {
			t.Fatalf("turn terminal = %v, want the completed turn of the version in effect", turn.Terminal)
		}

		second := newReplacementModel("model-a", "version two answer")
		published := 0
		record, err := lease.Replace(ctx, "v2", func() (model.Model, error) { return second, nil }, replacementPublisher(ctx, run, &published))
		if err != nil {
			t.Fatalf("replace the model: %v", err)
		}
		if record.State != rl.ReplacementApplied || record.From != "model-a@v1" || record.To != "model-a@v2" || record.Reason != "" {
			t.Fatalf("replacement record = %+v, want the applied replacement from v1 to v2", record)
		}
		if published != 1 {
			t.Fatalf("the replacement published %d bindings, want exactly the one it recorded", published)
		}
		if lease.CurrentVersion() != "model-a@v2" {
			t.Fatalf("the lease holds %q, want the version the replacement installed", lease.CurrentVersion())
		}
		if closed := first.closed(); closed != 1 {
			t.Fatalf("the retired version was closed %d times, want the one release of a version no stream still holds", closed)
		}

		if turn := replacementInvoke(t, ctx, run, "inv-replacement-2", "session-replacement-1", "ep-replacement-1"); turn.Terminal != model.KindCompleted {
			t.Fatalf("turn terminal = %v, want the completed turn of the installed version", turn.Terminal)
		}
		if _, err := sink.Seal(ctx); err != nil {
			t.Fatalf("seal the capture: %v", err)
		}
		attempts := replacementAttemptsOf(t, dir)
		if len(attempts) != 2 {
			t.Fatalf("stored attempts = %d, want one per decision the two versions served", len(attempts))
		}
		if attempts[0].ModelVersion != "v1" || attempts[1].ModelVersion != "v2" {
			t.Fatalf("stored attempt versions = (%q, %q), want the version that served each attempt", attempts[0].ModelVersion, attempts[1].ModelVersion)
		}
	})

	t.Run("重建失败不发布绑定: a failed rebuild publishes no binding and records its reason", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		records := &collector{}
		first := newReplacementModel("model-a", "version one answer")
		lease, err := rl.NewModelLease(first, "v1")
		if err != nil {
			t.Fatalf("build the model lease: %v", err)
		}
		run := replacementRun(t, ctx, lease.Current(), lease.CurrentVersion(), records)
		replacementInvoke(t, ctx, run, "inv-refused-1", "session-refused", "ep-refused")

		published := 0
		cause := errors.New("the rebuild could not build the model")
		record, err := lease.Replace(ctx, "v2", func() (model.Model, error) { return nil, cause }, replacementPublisher(ctx, run, &published))
		if !errors.Is(err, rl.ErrLeaseRebuildFailed) || !errors.Is(err, cause) {
			t.Fatalf("failed rebuild error = %v, want the named rebuild refusal beside its cause", err)
		}
		if published != 0 {
			t.Fatalf("a failed rebuild published %d bindings, want none at all", published)
		}
		if record.State != rl.ReplacementRefused || record.From != "model-a@v1" || record.To != "model-a@v2" {
			t.Fatalf("replacement record = %+v, want the refused attempt from v1 to v2", record)
		}
		if !strings.Contains(record.Reason, "could not build the model") {
			t.Fatalf("refused record reason = %q, want the reason the rebuild failed", record.Reason)
		}
		if lease.CurrentVersion() != "model-a@v1" {
			t.Fatalf("the lease holds %q, want the version in effect untouched by the refused rebuild", lease.CurrentVersion())
		}
		if closed := first.closed(); closed != 0 {
			t.Fatalf("the version in effect was closed %d times, want it open beside a refused replacement", closed)
		}

		rejected := newReplacementModel("model-a", "unused answer")
		record, err = lease.Replace(ctx, "v3", func() (model.Model, error) { return rejected, nil }, func(rl.ModelIdentity, model.Model) error {
			return errors.New("the publication refused the generation")
		})
		if !errors.Is(err, rl.ErrLeasePublishFailed) {
			t.Fatalf("refused publication error = %v, want the named publication refusal", err)
		}
		if record.State != rl.ReplacementRefused || !strings.Contains(record.Reason, "refused the generation") {
			t.Fatalf("replacement record = %+v, want the refused attempt beside the publication refusal", record)
		}
		if lease.CurrentVersion() != "model-a@v1" {
			t.Fatalf("the lease holds %q, want the version in effect untouched by the refused publication", lease.CurrentVersion())
		}
		if closed := rejected.closed(); closed != 1 {
			t.Fatalf("the unpublished model was closed %d times, want the one release of what the attempt acquired", closed)
		}

		history := lease.Replacements()
		if len(history) != 2 || history[0].State != rl.ReplacementRefused || history[1].State != rl.ReplacementRefused {
			t.Fatalf("replacement history = %+v, want the two refused attempts in order", history)
		}
		replacementInvoke(t, ctx, run, "inv-refused-2", "session-refused", "ep-refused")
		snap := records.snapshot()
		if len(snap.attempts) != 2 {
			t.Fatalf("attempt records = %d, want the calls of both decisions", len(snap.attempts))
		}
		for i, attempt := range snap.attempts {
			if attempt.ModelVersion != "v1" {
				t.Fatalf("attempt %d model version = %q, want the version in effect that no refused replacement replaced", i, attempt.ModelVersion)
			}
		}
	})

	t.Run("在途请求持旧版本至流结束: an in-flight request keeps its version until its stream ends", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		records := &collector{}
		held := make(chan *model.Response)
		first := newReplacementModel("model-a", "version one answer")
		first.stream = held
		lease, err := rl.NewModelLease(first, "v1")
		if err != nil {
			t.Fatalf("build the model lease: %v", err)
		}
		run := replacementRun(t, ctx, lease.Current(), lease.CurrentVersion(), records)

		done := make(chan error, 1)
		go func() {
			_, err := run.Invoke(ctx, tagent.InvokeRequest{
				InvocationID: "inv-inflight",
				Episode:      "ep-inflight",
				Address:      event.Address{AgentID: "agent-a", SessionID: "session-inflight"},
				Input:        []model.Message{{Role: model.RoleUser, Content: "learn me"}},
			})
			done <- err
		}()
		waitForReplacement(t, "the model call of the in-flight request", func() bool { return first.calls() == 1 })
		if lease.InFlight() != 1 {
			t.Fatalf("the lease serves %d streams, want the one the in-flight request opened", lease.InFlight())
		}

		second := newReplacementModel("model-a", "version two answer")
		published := 0
		if _, err := lease.Replace(ctx, "v2", func() (model.Model, error) { return second, nil }, replacementPublisher(ctx, run, &published)); err != nil {
			t.Fatalf("replace the model beside an in-flight request: %v", err)
		}
		if lease.CurrentVersion() != "model-a@v2" {
			t.Fatalf("the lease holds %q, want the version the replacement installed", lease.CurrentVersion())
		}
		if closed := first.closed(); closed != 0 {
			t.Fatalf("the version of an in-flight request was closed %d times, want it open until the stream ends", closed)
		}

		held <- learnFinalTurn("version one answer")
		close(held)
		if err := <-done; err != nil {
			t.Fatalf("invoke beside a replaced model: %v", err)
		}
		waitForReplacement(t, "the release of the retired version", func() bool { return first.closed() == 1 })
		if lease.InFlight() != 0 {
			t.Fatalf("the lease serves %d streams after the stream ended, want none", lease.InFlight())
		}

		snap := records.snapshot()
		if len(snap.attempts) != 1 {
			t.Fatalf("attempt records = %d, want the one attempt of the in-flight decision", len(snap.attempts))
		}
		if got := snap.attempts[0].ModelVersion; got != "v1" {
			t.Fatalf("the in-flight attempt was recorded as served by %q, want the version it started on", got)
		}

		replacementInvoke(t, ctx, run, "inv-inflight-2", "session-inflight", "ep-inflight")
		snap = records.snapshot()
		if len(snap.attempts) != 2 {
			t.Fatalf("attempt records = %d, want the attempts of both decisions", len(snap.attempts))
		}
		if got := snap.attempts[1].ModelVersion; got != "v2" {
			t.Fatalf("the attempt that began after the replacement was recorded as served by %q, want the installed version", got)
		}
	})

	t.Run("重启后按持久化版本恢复且不跨版本串绑: a restarted lease reads the persisted version back and refuses a cross-version history", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		origin := newReplacementModel("model-a", "version one answer")
		running, err := rl.NewModelLease(origin, "v1")
		if err != nil {
			t.Fatalf("build the model lease: %v", err)
		}
		before := replacementRun(t, ctx, running.Current(), running.CurrentVersion(), &collector{})
		published := 0
		if _, err := running.Replace(ctx, "v2", func() (model.Model, error) {
			return newReplacementModel("model-a", "version two answer"), nil
		}, replacementPublisher(ctx, before, &published)); err != nil {
			t.Fatalf("replace the model before the restart: %v", err)
		}
		history := running.Replacements()
		if len(history) != 1 || history[0].State != rl.ReplacementApplied {
			t.Fatalf("replacement history = %+v, want the one applied replacement to persist", history)
		}

		restored := newReplacementModel("model-a", "version two answer")
		lease, err := rl.NewModelLease(restored, "v2")
		if err != nil {
			t.Fatalf("build the restarted model lease: %v", err)
		}
		if err := lease.Restore(history); err != nil {
			t.Fatalf("restore the replacement history: %v", err)
		}
		if lease.CurrentVersion() != history[len(history)-1].To {
			t.Fatalf("the restarted lease holds %q, want the version the persisted history ends on %q", lease.CurrentVersion(), history[len(history)-1].To)
		}
		records := &collector{}
		after := replacementRun(t, ctx, lease.Current(), lease.CurrentVersion(), records)
		replacementInvoke(t, ctx, after, "inv-restart", "session-restart", "ep-restart")
		snap := records.snapshot()
		if len(snap.attempts) != 1 || snap.attempts[0].ModelVersion != "v2" {
			t.Fatalf("restarted attempts = %+v, want the one attempt served by the persisted version", snap.attempts)
		}

		stale := newReplacementModel("model-a", "version one answer")
		mismatched, err := rl.NewModelLease(stale, "v1")
		if err != nil {
			t.Fatalf("build the mismatched model lease: %v", err)
		}
		if err := mismatched.Restore(history); !errors.Is(err, rl.ErrLeaseRestoreMismatch) {
			t.Fatalf("restore beside a version the history does not end on = %v, want the named mismatch refusal", err)
		}
		if mismatched.CurrentVersion() != "model-a@v1" || len(mismatched.Replacements()) != 0 {
			t.Fatalf("the refused restarted lease holds %q beside %d records, want the version it was built with and no restored history", mismatched.CurrentVersion(), len(mismatched.Replacements()))
		}
	})
}

// replacementModel is one scripted model version of these checks: it answers
// each call with the response it is scripted with, hands over a stream the check
// holds open when one is named, and counts the calls it served and the times it
// was closed.
type replacementModel struct {
	mu       sync.Mutex
	name     string
	answer   func(req *model.Request) *model.Response
	stream   chan *model.Response
	requests int
	closes   int
}

// newReplacementModel returns one scripted version answering with the content it
// is named with.
func newReplacementModel(name, content string) *replacementModel {
	return &replacementModel{
		name:   name,
		answer: func(*model.Request) *model.Response { return learnFinalTurn(content) },
	}
}

// GenerateContent counts the call it serves and hands over the held stream when
// the check named one, or the scripted response otherwise.
func (m *replacementModel) GenerateContent(_ context.Context, req *model.Request) (<-chan *model.Response, error) {
	m.mu.Lock()
	m.requests++
	answer, stream := m.answer, m.stream
	m.mu.Unlock()
	if stream != nil {
		return stream, nil
	}
	ch := make(chan *model.Response, 1)
	ch <- answer(req)
	close(ch)
	return ch, nil
}

// Info reports the scripted model's descriptor.
func (m *replacementModel) Info() model.Info {
	return model.Info{Name: m.name}
}

// Close counts the one release the lease takes on a retired version.
func (m *replacementModel) Close() error {
	m.mu.Lock()
	m.closes++
	m.mu.Unlock()
	return nil
}

// calls reports how many model calls this version served.
func (m *replacementModel) calls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.requests
}

// closed reports how many times this version was closed.
func (m *replacementModel) closed() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closes
}

// replacementDecl is the declaration of the single agent these checks run,
// referencing the model identity the caller names.
func replacementDecl(modelRef string) *config.Declaration {
	return &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: modelRef}},
	}
}

// replacementRun assembles one runtime over the model handle and declaration
// reference the caller names, with the record pipeline it writes through.
func replacementRun(t *testing.T, ctx context.Context, handle model.Model, modelRef string, records rl.Recorder) *tagent.Run {
	t.Helper()
	run, err := tagent.Assemble(ctx, replacementDecl(modelRef), map[event.AgentID]tagent.NativeAgent{
		"agent-a": {Model: handle},
	}, tagent.WithGovernance(formalPolicy{}), tagent.WithRecorder(records))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}
	return run
}

// replacementInvoke drives one invocation through the formal runtime entry.
func replacementInvoke(t *testing.T, ctx context.Context, run *tagent.Run, invocation event.InvocationID, session event.SessionID, episode event.EpisodeID) agent.Turn {
	t.Helper()
	turn, err := run.Invoke(ctx, tagent.InvokeRequest{
		InvocationID: invocation,
		Episode:      episode,
		Address:      event.Address{AgentID: "agent-a", SessionID: session},
		Input:        []model.Message{{Role: model.RoleUser, Content: "learn me"}},
	})
	if err != nil {
		t.Fatalf("invoke the runtime: %v", err)
	}
	return turn
}

// replacementPublisher is the publication entry a replacement installs its
// rebuilt model through: the declaration references the identity of the version
// being installed and the runtime publishes it as the generation a request that
// begins afterwards borrows. Every call is counted, so a rebuild that publishes
// nothing is observable.
func replacementPublisher(ctx context.Context, run *tagent.Run, calls *int) func(rl.ModelIdentity, model.Model) error {
	return func(identity rl.ModelIdentity, next model.Model) error {
		*calls++
		outcome, err := run.Publish(ctx, replacementDecl(identity.Name()), map[event.AgentID]tagent.NativeAgent{
			"agent-a": {Model: next},
		})
		if err != nil {
			return err
		}
		if outcome.Result != state.PublishApplied {
			return fmt.Errorf("the publication of %q ended as %v", identity.Name(), outcome.Result)
		}
		return nil
	}
}

// replacementAttemptsOf reads the attempt records one sealed capture holds, in
// stored order.
func replacementAttemptsOf(t *testing.T, dir string) []rl.AttemptRecord {
	t.Helper()
	var attempts []rl.AttemptRecord
	for _, stored := range storedRecordsOf(t, dir) {
		if stored.line.Kind == rl.RecordKindAttempt {
			attempts = append(attempts, *stored.line.Attempt)
		}
	}
	return attempts
}

// storedRecord is one line of the sealed record file beside the raw bytes it
// was written as, so a stored attempt is reconciled against the very bytes the
// writer sealed.
type storedRecord struct {
	raw  []byte
	line rl.Line
}

// storedRecordsOf reads the sealed record file of one capture directory back,
// line by line.
func storedRecordsOf(t *testing.T, dir string) []storedRecord {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(dir, rl.SinkRecordsFile))
	if err != nil {
		t.Fatalf("read the stored records: %v", err)
	}
	var out []storedRecord
	for _, raw := range bytes.Split(content, []byte("\n")) {
		if len(raw) == 0 {
			continue
		}
		line, err := rl.ParseLine(raw)
		if err != nil {
			t.Fatalf("parse stored record %q: %v", raw, err)
		}
		out = append(out, storedRecord{raw: append([]byte(nil), raw...), line: line})
	}
	return out
}

// waitForReplacement waits until the condition holds or the wait runs out, so a
// check that depends on a goroutine never reads a half-settled state.
func waitForReplacement(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("%s did not arrive within the wait", what)
}
