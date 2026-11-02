package meditation

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory"
)

// TestMeditationDriverAnswersEveryHostFacingCommit pins the one promise the event-driven face makes: a commit somebody awaits is answered.
// - positive: a commit outside the rhythm floor runs its pass at once
// - negative: a commit inside the rhythm floor is evaluated once the window opens instead of being dropped
// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
func TestMeditationDriverAnswersEveryHostFacingCommit(t *testing.T) {
	t.Run("a_commit_outside_the_floor_runs_its_pass_at_once", func(t *testing.T) {
		anchor := newDriverAnchor(t, 0)
		anchor.commit("user")
		anchor.awaitInjected(t, 1, 5*time.Second)
	})

	t.Run("a_commit_inside_the_floor_is_answered_once_the_window_opens", func(t *testing.T) {
		const (
			floor        = 200 * time.Millisecond
			nextTick     = 2 * time.Millisecond
			insideWindow = floor / 4
		)
		anchor := newDriverAnchor(t, floor)
		anchor.commit("user")
		anchor.awaitInjected(t, 1, 5*time.Second)

		time.Sleep(nextTick)
		anchor.commit("task")
		time.Sleep(insideWindow)
		if got := anchor.injected(); got != 1 {
			t.Fatalf("the driver ran %d pass(es) inside the rhythm floor, want the floor to hold the commit it answers", got)
		}

		anchor.awaitInjected(t, 2, 5*time.Second)
	})
}

// driverAnchor is one driver beside the faces it reads: a real anchor store as its
// watermark gate, the fact chain its passes hydrate and the injection face that
// settles every fired input as a consumed turn.
type driverAnchor struct {
	t      *testing.T
	driver *Driver
	chain  *driverChain
	input  *driverInput
	owner  event.Address
}

// newDriverAnchor assembles the driver of one observing agent over the rhythm floor
// the anchor judges.
func newDriverAnchor(t *testing.T, interval time.Duration) *driverAnchor {
	t.Helper()
	store, err := NewAnchorStore(filepath.Join(t.TempDir(), "anchors", "reflection.json"))
	if err != nil {
		t.Fatalf("open the anchor store: %v", err)
	}
	chain := newDriverChain("user")
	input := &driverInput{}
	reflection, err := NewReflection(ReflectionConfig{
		Loop:    ReflectionLoop{Agent: "user", Session: "session-user"},
		Face:    ReflectionFace{ObservedNamespaces: []string{"user"}, ReadNamespaces: []string{"user"}},
		Cadence: ReflectionCadence{Interval: interval},
		Prompt:  "reflect on the observed face",
	}, store, chain, input)
	if err != nil {
		t.Fatalf("assemble the reflection: %v", err)
	}
	driver := NewDriver(reflection, interval, 0, nil, "")
	t.Cleanup(driver.Stop)
	return &driverAnchor{
		t:      t,
		driver: driver,
		chain:  chain,
		input:  input,
		owner:  event.Address{AgentID: "user", SessionID: "session-user"},
	}
}

// commit files one host-facing fact and reports it to the driver the way the host's
// after pass does.
func (a *driverAnchor) commit(stamp string) {
	a.t.Helper()
	key := a.chain.append(event.Event{
		Type:      event.TypeExternalInput,
		Timestamp: time.Now(),
		Owner:     a.owner,
		Meta:      map[string]string{event.MetaKeyTriggerSource: stamp},
	})
	if err := a.driver.ObserveCommit(context.Background(), key); err != nil {
		a.t.Fatalf("observe the commit %s: %v", key, err)
	}
}

// injected reports how many reflection inputs the driver has injected.
func (a *driverAnchor) injected() int {
	return a.input.count()
}

// awaitInjected waits until the driver has injected want reflection inputs.
func (a *driverAnchor) awaitInjected(t *testing.T, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for a.injected() < want {
		if time.Now().After(deadline) {
			t.Fatalf("the driver injected %d reflection input(s) within %s, want %d: the commit it holds was never answered",
				a.injected(), timeout, want)
		}
		time.Sleep(time.Millisecond)
	}
}

// driverChain is the observed face one driver anchor reads: every fact it holds is
// filed under the one namespace the observing agent owns, so an appended fact is
// exactly what the host's own chain would answer.
type driverChain struct {
	mu        sync.Mutex
	partition int
	facts     []event.Event
}

// newDriverChain returns the empty chain of one namespace.
func newDriverChain(name string) *driverChain {
	return &driverChain{partition: memory.PartitionIDFromName(name)}
}

// append files one fact and reports its identity, the way a commit reports one.
func (c *driverChain) append(fact event.Event) event.EventID {
	c.mu.Lock()
	defer c.mu.Unlock()
	fact.Key = event.EventID("fact-" + strconv.Itoa(len(c.facts)+1))
	c.facts = append(c.facts, fact)
	return fact.Key
}

// QueryEvents answers the reference page of the window the pass reads: the facts
// filed under the scanned partition, newest first.
func (c *driverChain) QueryEvents(_ context.Context, opts memory.QueryOptions) ([]memory.EventReference, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	scanned := false
	for _, partition := range opts.PartitionIDs {
		if partition == c.partition {
			scanned = true
			break
		}
	}
	refs := make([]memory.EventReference, 0, len(c.facts))
	if !scanned {
		return refs, nil
	}
	for i := len(c.facts) - 1; i >= 0; i-- {
		fact := c.facts[i]
		ref := memory.EventReference{
			Key:         fact.Key,
			PartitionID: c.partition,
			Type:        fact.Type,
			Summary:     fact.Meta[event.MetaKeyEventSummary],
			Timestamp:   fact.Timestamp.UnixMilli(),
		}
		if ref.Timestamp < opts.StartTime {
			continue
		}
		refs = append(refs, ref)
		if opts.Limit > 0 && len(refs) >= opts.Limit {
			break
		}
	}
	return refs, nil
}

// CommittedFact reads the fact one identity names.
func (c *driverChain) CommittedFact(_ context.Context, id event.EventID) (event.Event, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, fact := range c.facts {
		if fact.Key == id {
			return fact, nil
		}
	}
	return event.Event{}, fmt.Errorf("anchor: no committed fact carries %q", id)
}

// driverInput is the injection face of one driver anchor: it records every input a
// fired pass injected and settles it as a consumed turn, so the watermark advances
// the way a loop side that really read the input advances it.
type driverInput struct {
	mu       sync.Mutex
	injected []event.Event
}

// InjectReflection records one injected input and answers the consumed outcome.
func (i *driverInput) InjectReflection(_ context.Context, evt event.Event) (ReflectionReceipt, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.injected = append(i.injected, evt)
	return ReflectionReceipt{
		Key:     event.EventID("reflection-" + strconv.Itoa(len(i.injected))),
		At:      evt.Timestamp,
		Outcome: ReflectionConsumed,
	}, nil
}

// count reports how many reflection inputs were injected.
func (i *driverInput) count() int {
	i.mu.Lock()
	defer i.mu.Unlock()
	return len(i.injected)
}
