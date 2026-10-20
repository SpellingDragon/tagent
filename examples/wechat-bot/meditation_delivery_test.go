// meditation_delivery_test 钉住 定时策展的投递红线：权威回合产出的正文经唯一出网通路送到
// 声明的目标——Tick 走同一条水位/观察/注入链；未启用的声明不建驱动器、不动模型、不写锚点，
// 等于没有这一段。声明解析归核心（meditation.PlanOf），本文件只盯宿主自己的投递面。
// 契约: docs/wiki/examples/wechat-bot-runtime.md#startup-and-routing
package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/evolution/meditation"
	"github.com/SpellingDragon/tagent/memory/ledger"
)

// meditationDocument builds one host document whose meditator observes the
// business agent's partition and delivers its reflection back to it. Both
// agents share the storage tier of a temporary root, so the anchor record of
// the reflection persists beside the facts it observed.
func meditationDocument(t *testing.T) *hostDocument {
	t.Helper()
	root := t.TempDir()
	prompt := filepath.Join(t.TempDir(), "meditation.md")
	if err := os.WriteFile(prompt, []byte("本轮只看业务分区有没有值得回流的新结论。"), 0o644); err != nil {
		t.Fatalf("write the meditation prompt: %v", err)
	}
	storage := config.MemorySpec{Type: string(ledger.StorageLocalFile), Path: root}
	doc := &hostDocument{Entry: "tagent", Model: "test-model", PromptDir: t.TempDir()}
	doc.Agents = map[string]agentEntry{
		"tagent": {Model: "test-model", Memory: storage},
		"meditator": {
			Model:  "test-model",
			Memory: config.MemorySpec{Type: storage.Type, Path: storage.Path, ReadNamespaces: []string{"tagent"}},
			Meditation: config.MeditationSpec{
				Enabled:            true,
				Interval:           time.Minute,
				PromptFile:         prompt,
				ObservedNamespaces: []string{"tagent"},
				DeliverTo:          []string{"tagent"},
			},
		},
	}
	return doc
}

// TestMeditationTickDeliversTheReflectionToTheDeclaredTarget drives one tick through the assembled reflection chain to the declared chat.
//   - the plan carries the declared cadence, observation face, white list and anchor path
//   - the reflection turn carries the declared prompt and the digest
//   - the curated answer reaches the declared target's bound chat and advances the persisted watermark
//   - the next tick reports no novelty instead of reflecting on its own output
func TestMeditationTickDeliversTheReflectionToTheDeclaredTarget(t *testing.T) {
	ctx := context.Background()
	doc := meditationDocument(t)
	rig := newRoutingRig(t, doc)

	if rig.host.meditationPlan == nil {
		t.Fatal("the document enables meditation but the assembly resolved no plan")
	}
	plan := rig.host.meditationPlan
	if plan.Agent() != "meditator" || plan.Interval() != time.Minute || plan.MinGap() != 0 {
		t.Fatalf("plan = %s/%s/%s, want meditator at the declared one-minute cadence", plan.Agent(), plan.Interval(), plan.MinGap())
	}
	wantAnchors := filepath.Join(doc.Agents["tagent"].Memory.Path, "meditation", "meditator", "anchors.json")
	if plan.AnchorsPath() != wantAnchors {
		t.Fatalf("anchors path = %q, want %q", plan.AnchorsPath(), wantAnchors)
	}
	if got := plan.ObservedNamespaces(); len(got) != 1 || got[0] != "tagent" {
		t.Fatalf("observed namespaces = %v, want the declared tagent face", got)
	}
	if got := plan.DeliverTo(); len(got) != 1 || got[0] != "tagent" {
		t.Fatalf("delivery white list = %v, want the declared tagent target", got)
	}

	rig.model.then(finalTurn("先把旧选题放一放。"))
	if _, err := rig.host.HandleInbound(ctx, inboundMessage{Key: "med-user", ChatID: "chat-a", Text: "这条回答还是老选题"}); err != nil {
		t.Fatalf("drive the observed business turn: %v", err)
	}

	rig.model.then(finalTurn("建议把选题换成用户真正问过的问题。"))
	if err := rig.host.startMeditation(ctx); err != nil {
		t.Fatalf("start the scheduled reflection: %v", err)
	}
	if rig.host.meditation == nil {
		t.Fatal("startMeditation built no driver beside an enabled declaration")
	}

	outcome := rig.host.meditation.Tick(ctx)
	if outcome.Decision.Disposition != meditation.TickFired {
		t.Fatalf("tick disposition = %q (%s), want %q", outcome.Decision.Disposition, outcome.Decision.Reason, meditation.TickFired)
	}
	if outcome.Injection == nil || outcome.Injection.Key == "" || outcome.Injection.At.IsZero() {
		t.Fatalf("fired tick carried no injection evidence: %+v", outcome.Injection)
	}

	requests := rig.model.recorded()
	if len(requests) < 2 {
		t.Fatalf("model calls = %d, want the business turn beside the reflection turn", len(requests))
	}
	if !requestCarries(requests[1], "本轮只看业务分区有没有值得回流的新结论。") {
		t.Fatalf("the reflection request does not carry the declared prompt: %+v", requests[1].Messages)
	}
	if !requestCarries(requests[1], "[reflection]") {
		t.Fatalf("the reflection request does not carry the reflection digest: %+v", requests[1].Messages)
	}

	delivered := false
	for _, text := range rig.out.texts() {
		if text.Target == "chat-a" && text.Body == "建议把选题换成用户真正问过的问题。" {
			delivered = true
		}
	}
	if !delivered {
		t.Fatalf("text deliveries = %+v, want the reflection delivered to chat-a", rig.out.texts())
	}

	watermark, err := rig.host.run.ReflectionGate().Current(ctx)
	if err != nil {
		t.Fatalf("read the reflection watermark: %v", err)
	}
	if watermark.Moment.IsZero() {
		t.Fatal("the consumed reflection left the watermark at zero")
	}
	if _, err := os.Stat(wantAnchors); err != nil {
		t.Fatalf("the anchor record was not persisted at %s: %v", wantAnchors, err)
	}

	before := len(rig.out.texts())
	again := rig.host.meditation.Tick(ctx)
	if again.Decision.Disposition != meditation.TickNoNovelty {
		t.Fatalf("second tick disposition = %q (%s), want %q", again.Decision.Disposition, again.Decision.Reason, meditation.TickNoNovelty)
	}
	if got := len(rig.out.texts()); got != before {
		t.Fatalf("second tick produced %d deliveries, want none", got-before)
	}
}

// TestMeditationDisabledKeepsTheHostStill wires no gate, starts no driver and calls no model beside a disabled declaration.
func TestMeditationDisabledKeepsTheHostStill(t *testing.T) {
	ctx := context.Background()
	doc := meditationDocument(t)
	entry := doc.Agents["meditator"]
	entry.Meditation.Enabled = false
	doc.Agents["meditator"] = entry

	rig := newRoutingRig(t, doc)
	if rig.host.meditationPlan != nil {
		t.Fatalf("plan = %+v, want none beside a disabled declaration", rig.host.meditationPlan)
	}
	if rig.host.run.ReflectionGate() != nil {
		t.Fatal("a disabled meditation still wired a reflection anchor store")
	}
	if err := rig.host.startMeditation(ctx); err != nil {
		t.Fatalf("start a disabled meditation: %v", err)
	}
	if rig.host.meditation != nil {
		t.Fatal("a disabled meditation started a driver")
	}

	rig.model.then(finalTurn("business only"))
	if _, err := rig.host.HandleInbound(ctx, inboundMessage{Key: "still-user", ChatID: "chat-a", Text: "hello"}); err != nil {
		t.Fatalf("drive the business turn: %v", err)
	}
	if got := rig.model.callCount(); got != 1 {
		t.Fatalf("model calls = %d, want only the business turn", got)
	}
	if got := len(rig.out.texts()); got != 1 {
		t.Fatalf("text deliveries = %d, want only the business answer", got)
	}
	anchors := filepath.Join(doc.Agents["tagent"].Memory.Path, "meditation", "meditator", "anchors.json")
	if _, err := os.Stat(anchors); !os.IsNotExist(err) {
		t.Fatalf("a disabled meditation wrote an anchor record at %s: %v", anchors, err)
	}
}
