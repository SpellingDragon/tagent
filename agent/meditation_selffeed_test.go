package agent

import (
	"testing"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/model"
)

// Switch-combination regression for the meditation self-feed guard
// (resident-remaining-hardening 3.4, archived 7.4).
//
// "无用户新颖性不自馈电": a meditation must never arm its own novelty gate.
// The gate is INPUT-side — only source=="user" injections advance
// lastUserInput (armMeditationNoveltyGate). A meditation fires by injecting
// with source=="meditation", which is NOT user, so it cannot re-arm novelty and
// a second meditation is structurally impossible until real user input lands.

// sourceInjector records BOTH the source and the message of every injection.
type sourceInjector struct {
	entries []injEntry
}

type injEntry struct {
	source string
	msg    model.Message
}

func (s *sourceInjector) InjectMessageWithSource(source string, msg model.Message) {
	s.entries = append(s.entries, injEntry{source: source, msg: msg})
}

func TestSwitchCombo_MeditationDoesNotSelfFeed(t *testing.T) {
	inj := &sourceInjector{}
	mgr := NewMeditationManager(MeditationConfig{MinGap: time.Millisecond, PromptText: "reflect"}, inj)

	// Real user novelty + an idle gap → first meditation fires.
	mgr.UpdateLastUserInput(time.Now().Add(-2 * time.Second))
	mgr.UpdateLastTurnEnd(time.Now().Add(-time.Second))
	mgr.checkAndMeditate()

	if len(inj.entries) != 1 {
		t.Fatalf("expected exactly one meditation injection, got %d", len(inj.entries))
	}
	// The self-injection's source is "meditation", never "user" — so under the
	// input gate it cannot advance lastUserInput.
	if inj.entries[0].source == "user" {
		t.Fatalf("meditation self-injection must not use the user source (would re-arm novelty)")
	}
	if inj.entries[0].source != "meditation" {
		t.Fatalf("expected source 'meditation', got %q", inj.entries[0].source)
	}

	// The meditation's own output completes a turn (advances lastTurnEnd, a
	// lineage-agnostic gate) but supplies no new USER input → a re-check must
	// NOT fire a second time. This is the self-feed loop broken at its source.
	mgr.UpdateLastTurnEnd(time.Now().Add(-time.Second))
	mgr.checkAndMeditate()
	if len(inj.entries) != 1 {
		t.Fatalf("meditation self-fed a second round without user input: %d injections", len(inj.entries))
	}

	// Genuine user input re-arms the gate; only then does the next fire happen.
	time.Sleep(2 * time.Millisecond)
	mgr.UpdateLastUserInput(time.Now())
	mgr.UpdateLastTurnEnd(time.Now().Add(-time.Second))
	mgr.checkAndMeditate()
	if len(inj.entries) != 2 {
		t.Fatalf("after real user input the gate should re-arm and fire, got %d injections", len(inj.entries))
	}
}
