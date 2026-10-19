// 契约: docs/wiki/examples/wechat-bot-runtime.md#delivery-receipts
package main

import (
	"strings"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"
)

// recordingInjector captures every internal notice the host feeds back.
type recordingInjector struct {
	addrs   []event.Address
	sources []string
	bodies  []string
}

// InjectInternal records one queued notice beside its address.
func (r *recordingInjector) InjectInternal(addr event.Address, source string, msg trpcmodel.Message) {
	r.addrs = append(r.addrs, addr)
	r.sources = append(r.sources, source)
	r.bodies = append(r.bodies, msg.Content)
}

// TestEmitReceiptRecordsAddressTargetAndGrade 钉住 回执回给产生它的会址，点名终态与目标并按级别记录。
// 契约: docs/wiki/examples/wechat-bot-runtime.md#delivery-receipts
func TestEmitReceiptRecordsAddressTargetAndGrade(t *testing.T) {
	inj := &recordingInjector{}
	addr := event.Address{AgentID: "host", SessionID: "s-1"}

	emitReceipt(inj, addr, "send-failed", "ERROR", "user", "chat-9", "hello")

	if len(inj.sources) != 1 || inj.sources[0] != receiptLineage {
		t.Fatalf("want one receipt on the internal lineage, got %v", inj.sources)
	}
	if inj.addrs[0] != addr {
		t.Fatalf("receipt address = %v, want the producing address %v", inj.addrs[0], addr)
	}
	if !strings.Contains(inj.bodies[0], "send-failed") || !strings.Contains(inj.bodies[0], "chat-9") {
		t.Fatalf("receipt must name the terminal and the target, got %q", inj.bodies[0])
	}
}

// TestEmitReceiptNoTargetLabel 钉住 目标缺席时的回执仍有具名占位，不生成空白字段。
// 契约: docs/wiki/examples/wechat-bot-runtime.md#delivery-receipts
func TestEmitReceiptNoTargetLabel(t *testing.T) {
	inj := &recordingInjector{}

	emitReceipt(inj, event.Address{AgentID: "host", SessionID: "s-1"}, "no-target", "WARN", "user", "", "hello")

	if len(inj.bodies) != 1 || !strings.Contains(inj.bodies[0], "(无目标)") {
		t.Fatalf("missing-target receipt must name the placeholder, got %v", inj.bodies)
	}
}

// TestEmitReceiptNeverRecurses 钉住 回执只落一层：回执血统自身的投递终态不产生回执。
// 契约: docs/wiki/examples/wechat-bot-runtime.md#delivery-receipts
func TestEmitReceiptNeverRecurses(t *testing.T) {
	inj := &recordingInjector{}

	emitReceipt(inj, event.Address{AgentID: "host", SessionID: "s-1"}, "no-target", "WARN", receiptLineage, "", "nested")

	if len(inj.sources) != 0 {
		t.Fatalf("receipts must never cascade, got %d injections", len(inj.sources))
	}
}
