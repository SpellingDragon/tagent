package main

import (
	"strings"
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/model"
)

// recordingInjector captures every internal event the host feeds back.
type recordingInjector struct {
	sources []string
	bodies  []string
}

func (r *recordingInjector) InjectMessageWithSource(source string, msg model.Message) {
	r.sources = append(r.sources, source)
	r.bodies = append(r.bodies, msg.Content)
}

// TestReceiptForTerminalGrading 钉住 扣留终态的回执分级：预期外静默必回执，契约内静默不回执，回执不递归。
// - 未声明/未知血统恒 WARN；error 血统 WARN；冥想仅在交付特征上 WARN；
// - delivery_receipt 自身消化不回执（防自激红线）；
// - 交付特征只参与分级：同一冥想血统，叙事静默、结算回执。
// 契约: docs/wiki/examples/wechat-bot-runtime.md#delivery-receipts
func TestReceiptForTerminalGrading(t *testing.T) {
	cases := []struct {
		name        string
		source      string
		deliverable bool
		content     string
		wantReason  string
		wantLevel   string
	}{
		{"undeclared lineage speaks", "async_result", false, "anything", "undigested-lineage", "WARN"},
		{"http without declaration speaks", "http", false, "回件已发，证据已回呈", "undigested-lineage", "WARN"},
		{"meditation narrative stays contracted", "meditation", true, "整理昨日教训", "", ""},
		{"meditation settle speaks", "meditation", true, "[task settled] 双件在 .tagent-workspace/delivery/a.mp3", "settled-delivery-held", "WARN"},
		{"error lineage speaks", "error", true, "boom", "error-output-held", "WARN"},
		{"receipt digestion never re-receipts", receiptLineage, false, "[delivery-receipt] nested", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			reason, level := receiptFor(c.source, c.deliverable, c.content)
			if reason != c.wantReason || level != c.wantLevel {
				t.Fatalf("receiptFor(%q) = (%q,%q), want (%q,%q)", c.source, reason, level, c.wantReason, c.wantLevel)
			}
		})
	}
}

// TestReportWithheldFeedsBackInternalLineage 钉住 预期外静默以 delivery_receipt 血统回流并携带终态事实；契约内静默零注入。
// 契约: docs/wiki/examples/wechat-bot-runtime.md#delivery-receipts
func TestReportWithheldFeedsBackInternalLineage(t *testing.T) {
	inj := &recordingInjector{}

	reportWithheld(inj, "meditation", true, "", "just reflecting")
	if len(inj.sources) != 0 {
		t.Fatalf("contracted silence must not inject, got %v", inj.sources)
	}

	reportWithheld(inj, "meditation", true, "", "[task settled] deliver at delivery/vocal.mp3")
	if len(inj.sources) != 1 || inj.sources[0] != receiptLineage {
		t.Fatalf("receipt must come back on the internal lineage, got %v", inj.sources)
	}
	if !strings.Contains(inj.bodies[0], "settled-delivery-held") || !strings.Contains(inj.bodies[0], "vocal.mp3") {
		t.Fatalf("receipt must carry the terminal and the withheld content, got %q", inj.bodies[0])
	}

	reportWithheld(inj, receiptLineage, false, "", "[delivery-receipt] nested")
	if len(inj.sources) != 1 {
		t.Fatalf("receipts must never cascade, got %d injections", len(inj.sources))
	}
}

// TestEmitReceiptRecordsTargetAndGrade 钉住 发送失败终态以 ERROR 级回执点名目标（用户在场却没收到）。
// 契约: docs/wiki/examples/wechat-bot-runtime.md#delivery-receipts
func TestEmitReceiptRecordsTargetAndGrade(t *testing.T) {
	inj := &recordingInjector{}

	emitReceipt(inj, "send-failed", "ERROR", "user", "chat-9", "hello")

	if len(inj.sources) != 1 || inj.sources[0] != receiptLineage {
		t.Fatalf("want one receipt on the internal lineage, got %v", inj.sources)
	}
	if !strings.Contains(inj.bodies[0], "send-failed") || !strings.Contains(inj.bodies[0], "chat-9") {
		t.Fatalf("receipt must name the terminal and the target, got %q", inj.bodies[0])
	}
}
