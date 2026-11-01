// 契约: docs/wiki/agent/event-flow.md#runner-flow
//
// The strength anchor of the cross-source batch: the existing cross-source anchor
// reads a request that carries both inputs, which one decision reading the material
// of one source and the history of the other can satisfy. This anchor measures the
// merge itself — one model request reads the material of two sources as fresh
// material — and that the merge never confuses the responsibility range of them.
//
// - positive: the decision that reads the parked call's result carries the material of both sources in one request, each exactly once, in the order the chain holds them
// - positive: no request serves one source alone, so the two sources are one batch and not one request per source
// - negative batch_never_mixes_responsibility: each source's fact keeps its own range and its own task attribution, and the batched decision answers under the segment's own range rather than under the range of one source
// - consumer: Host.Invoke opens the turn over a host booted from a real document, and the two sources land on that turn's chain through the run's own write face — the face a cross-session delivery lands its message by — so the merge is the very aggregation the consumption loop runs, never a transcript the anchor assembled
package execution

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/runtime/agent"
)

// batchStrengthSession names the session the two sources of the batch share, and the
// material of either one: each source carries its own range and its own task, so the
// merged decision can be read for the attribution of either.
const (
	batchStrengthSession = intakeSession + "-batch-strength"
	batchStrengthOpening = "batch-strength: the message that opens the turn"
	batchStrengthFirst   = "batch-strength: the material of the first source"
	batchStrengthSecond  = "batch-strength: the material of the second source"
	batchStrengthRangeA  = "inv-batch-strength-a"
	batchStrengthRangeB  = "inv-batch-strength-b"
	batchStrengthTaskA   = "task-batch-strength-a"
	batchStrengthTaskB   = "task-batch-strength-b"
)

// batchStrengthSource is one source of the batch: the range it submits under, the task
// its material belongs to, and the material itself.
type batchStrengthSource struct {
	invocation event.InvocationID
	task       string
	body       string
}

// batchStrengthSources reports the two sources in the order the anchor lands them, so a
// check reads the chain order from the same list the drive used.
func batchStrengthSources() []batchStrengthSource {
	return []batchStrengthSource{
		{invocation: batchStrengthRangeA, task: batchStrengthTaskA, body: batchStrengthFirst},
		{invocation: batchStrengthRangeB, task: batchStrengthTaskB, body: batchStrengthSecond},
	}
}

// batchStrengthScene is one run of the anchor: the turn the entry opened, the material
// read back from the model endpoint, the run's chain and its outbound face.
type batchStrengthScene struct {
	addr     event.Address
	turn     agent.Turn
	err      error
	requests []intakeRequest
	facts    []event.Event
	targets  []event.Address
	bodies   []string
}

// batchStrengthDrive runs the anchor once: it opens a turn whose first decision issues
// the slow call, lands the material of the two sources on that turn's chain while the
// call is parked, releases the call, and reads back what the model and the run recorded.
//
// The material lands through the run's own write face, which wakes no loop: the parked
// call's settlement is then the one wake the loop reads, so both sources stand in the
// chain before the pull that reads them and the request they are read by is one request
// and not a race between two wakes.
func batchStrengthDrive(t *testing.T) *batchStrengthScene {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	provider := newIntakeProvider(t)
	sender := newIntakeSender()
	host := intakeBoot(t, provider, sender)
	scene := &batchStrengthScene{addr: event.Address{AgentID: intakeEntryAgent, SessionID: batchStrengthSession}}

	opened := make(chan agent.Turn, 1)
	openErr := make(chan error, 1)
	go func() {
		turn, err := host.Invoke(ctx, assembly.InvokeRequest{
			InvocationID: intakeFirstInvocation,
			Address:      scene.addr,
			Input:        []model.Message{{Role: model.RoleUser, Content: batchStrengthOpening}},
		})
		opened <- turn
		openErr <- err
	}()
	provider.anchor.awaitParked(t)

	for _, source := range batchStrengthSources() {
		if _, err := host.Run().CommitToSession(ctx, scene.addr, event.Event{
			Type:         event.TypeExternalInput,
			Message:      model.Message{Role: model.RoleUser, Content: source.body},
			InvocationID: source.invocation,
			Meta:         map[string]string{event.MetaKeyTaskID: source.task},
		}); err != nil {
			t.Fatalf("commit the material %q of the source %s onto the chain of the turn in flight: %v",
				source.body, source.invocation, err)
		}
	}
	provider.anchor.release()

	select {
	case scene.turn = <-opened:
		scene.err = <-openErr
	case <-time.After(intakeSettleWindow):
		t.Fatalf("the turn in flight never ended after its call was released")
	}
	scene.requests = provider.requests()
	scene.facts = host.Run().SessionFacts(scene.addr)
	sender.mu.Lock()
	scene.targets = append([]event.Address(nil), sender.targets...)
	scene.bodies = append([]string(nil), sender.bodies...)
	sender.mu.Unlock()
	return scene
}

// TestCrossSourceBatchStrength measures the merge of two sources in one decision.
//
// - positive: the decision that reads the parked call's result carries both sources' material in one request, each exactly once
// - positive: the material of neither source is read by a request of its own, so the run serves two decisions in all
// - negative batch_never_mixes_responsibility: each source's fact keeps its own range, task and identity
// - negative batch_never_mixes_responsibility: the batched decision answers under the segment's own range
// - negative batch_never_mixes_responsibility: its answer leaves once, to the session the two sources share
// - consumer: Host.Invoke over a host booted from a real document, read back from the requests served and the run's own chain
//
// 契约: docs/wiki/agent/event-flow.md#runner-flow
func TestCrossSourceBatchStrength(t *testing.T) {
	scene := batchStrengthDrive(t)
	sources := batchStrengthSources()

	t.Run("positive: one decision reads the material of both sources", func(t *testing.T) {
		if len(scene.requests) != 2 {
			t.Fatalf("model requests = %d (%s), want 2: the decision that opens the turn and the one that reads both sources; "+
				"a third request means a source drove a decision of its own",
				len(scene.requests), intakeDescribe(scene.requests))
		}
		at, ok := intakeDecisionAfterCall(scene.requests)
		if !ok {
			t.Fatalf("requests = %s, want the decision that reads the result of %s", intakeDescribe(scene.requests), intakeSlowCallID)
		}
		batch := scene.requests[at]
		t.Logf("the decision that reads both sources = %s", batchStrengthTrace(batch))
		for _, source := range sources {
			if copies := intakeUserCopies(batch, source.body); copies != 1 {
				t.Fatalf("copies of the material of %s in the decision that reads the parked call = %d, want exactly 1 (%s)",
					source.invocation, copies, intakeDescribe(scene.requests))
			}
			read := 0
			for _, request := range scene.requests {
				read += intakeUserCopies(request, source.body)
			}
			if read != 1 {
				t.Fatalf("the material of %s was read by %d request(s), want exactly 1: "+
					"a second read is a request of its own beside the batch (%s)",
					source.invocation, read, intakeDescribe(scene.requests))
			}
		}
		firstAt, secondAt, merged := batchStrengthPlaces(batch, sources[0].body, sources[1].body)
		if merged >= 0 {
			t.Fatalf("message %d of the batched decision carries both sources in one body: "+
				"the merge is one request over the material of each source, never one message out of two (%s)",
				merged, intakeDescribe(scene.requests))
		}
		if firstAt < 0 || secondAt < 0 || firstAt > secondAt {
			t.Fatalf("material of the two sources stands at messages (%d, %d) of the batched decision, want the chain order (%d then %d) (%s)",
				firstAt, secondAt, 0, 1, intakeDescribe(scene.requests))
		}
	})

	t.Run("batch_never_mixes_responsibility", func(t *testing.T) {
		if scene.err != nil || scene.turn.Terminal != model.KindCompleted {
			t.Fatalf("the turn of the batch = (%v, %v), want it to end by its own terminal", scene.turn.Terminal, scene.err)
		}
		for _, source := range sources {
			landed := intakeFactsCarrying(scene.facts, source.body)
			if len(landed) != 1 {
				t.Fatalf("facts carrying the material of %s = %d, want the one fact its source landed as",
					source.invocation, len(landed))
			}
			if landed[0].InvocationID != source.invocation {
				t.Fatalf("the fact of %s belongs to the range %q, want %q: "+
					"the merge is a decision over two ranges and never one range rewritten as the other",
					source.body, landed[0].InvocationID, source.invocation)
			}
			if task := landed[0].Meta[event.MetaKeyTaskID]; task != source.task {
				t.Fatalf("the fact of %s carries the task %q, want %q: batching a decision MUST NOT move material into another task",
					source.invocation, task, source.task)
			}
		}
		first := intakeFactsCarrying(scene.facts, sources[0].body)
		second := intakeFactsCarrying(scene.facts, sources[1].body)
		if first[0].Key == second[0].Key {
			t.Fatalf("the two sources landed as one fact key %q, want one fact per source: "+
				"a merged decision reads two facts and never two sources collapsed into one", first[0].Key)
		}
		if scene.turn.InvocationID == sources[0].invocation || scene.turn.InvocationID == sources[1].invocation {
			t.Fatalf("the batched turn answered under the range %q of one source, want the segment's own range: "+
				"the merge answers under the turn in flight and never under the responsibility range of one input",
				scene.turn.InvocationID)
		}
		if len(scene.bodies) != 1 {
			t.Fatalf("outbound bodies = %d (%v), want the one answer of the batched decision: "+
				"one merge answers once, and never once per source", len(scene.bodies), scene.bodies)
		}
		if len(scene.targets) == 1 && scene.targets[0] != scene.addr {
			t.Fatalf("the answer of the batched decision left for %+v, want the session %+v the two sources share",
				scene.targets[0], scene.addr)
		}
	})
}

// batchStrengthTrace renders the user material of one request, so a reader of a
// passing run sees the merged batch itself and not only the verdict about it.
func batchStrengthTrace(request intakeRequest) string {
	users := make([]string, 0, len(request.Messages))
	for _, message := range request.Messages {
		if message.Role == string(model.RoleUser) {
			users = append(users, message.Content)
		}
	}
	return fmt.Sprintf("#%d users=%q", request.Index, users)
}

// batchStrengthPlaces reports where the material of the two sources stands in one
// request, and the message that carries both of them when one does. A negative
// position reads as absent, so a caller names the material it never read.
func batchStrengthPlaces(request intakeRequest, first, second string) (firstAt, secondAt, merged int) {
	firstAt, secondAt, merged = -1, -1, -1
	for at, message := range request.Messages {
		if message.Role != string(model.RoleUser) {
			continue
		}
		holdsFirst := strings.Contains(message.Content, first)
		holdsSecond := strings.Contains(message.Content, second)
		if holdsFirst && holdsSecond && merged < 0 {
			merged = at
		}
		if holdsFirst && firstAt < 0 {
			firstAt = at
		}
		if holdsSecond && secondAt < 0 {
			secondAt = at
		}
	}
	return firstAt, secondAt, merged
}
