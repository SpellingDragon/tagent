package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// ==================== outputCh 溢出落盘（F2, design-report-closeout） ====================
//
// RunFlow 向 outputCh 发送事件设 2s 宽限；消费者停滞超限时把事件全文落盘到
// <workspace>/tool-output/output-overflow/ 并非阻塞投递摘要票据（路径 + 首尾片段），
// 绝不阻塞主循环、不静默丢弃——与 task_settled 大结果转储同构（票据可找回全文）。

// outputSendGrace is how long RunFlow waits for a stalled consumer before
// persisting the event to disk and moving on.
const outputSendGrace = 2 * time.Second

// overflowTicketHead/Tail bound the summary excerpt carried by the ticket event.
const (
	overflowTicketHead = 512
	overflowTicketTail = 256
)

// dumpOverflowEvent persists the full event as JSON under dir and returns the
// file path. dir is created on demand.
func dumpOverflowEvent(dir string, evt *event.Event) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir output-overflow: %w", err)
	}
	data, err := json.Marshal(evt)
	if err != nil {
		return "", fmt.Errorf("marshal overflow event: %w", err)
	}
	path := filepath.Join(dir, fmt.Sprintf("overflow-%d.json", time.Now().UnixNano()))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("write overflow event: %w", err)
	}
	return path, nil
}

// overflowTicket builds the lightweight summary event replacing the stalled
// delivery: first/last excerpt of the message content plus the retrieval
// path in StateDelta. Never fails; degenerates to a pointer-only notice.
func overflowTicket(evt *event.Event, path string) *event.Event {
	ticket := cloneEventForDelivery(evt)
	if ticket.Response != nil && len(ticket.Response.Choices) > 0 {
		// cloneEventForDelivery is shallow: copy Response + Choices so the
		// excerpt never mutates the original event's message.
		resp := *ticket.Response
		resp.Choices = append([]model.Choice(nil), ticket.Response.Choices...)
		ticket.Response = &resp
		msg := &resp.Choices[len(resp.Choices)-1].Message
		c := msg.Content
		if len(c) > overflowTicketHead+overflowTicketTail {
			msg.Content = c[:overflowTicketHead] + "\n...[output stalled, full content persisted]...\n" + c[len(c)-overflowTicketTail:]
		}
	}
	ticket.StateDelta["output_overflow_path"] = []byte(path)
	return ticket
}

// deliverEvent sends evt to cm.outputCh with the F2 grace period. On stall it
// persists the full event, attempts a non-blocking ticket delivery, and
// returns false (the caller must NOT block or abort the loop). ctx.Done()
// still aborts immediately.
func (cm *ContextManager) deliverEvent(ctx context.Context, evt *event.Event) bool {
	if cm.outputCh == nil {
		return true
	}
	select {
	case cm.outputCh <- evt:
		return true
	case <-ctx.Done():
		return false
	case <-time.After(outputSendGrace):
		if cm.overflowDir == "" {
			log.Warnf("[RunFlow] outputCh stalled >%s; event dropped (no overflow dir configured)", outputSendGrace)
			return false
		}
		path, err := dumpOverflowEvent(cm.overflowDir, evt)
		if err != nil {
			log.Errorf("[RunFlow] outputCh stalled >%s AND overflow persist failed: %v (event lost)", outputSendGrace, err)
			return false
		}
		log.Warnf("[RunFlow] outputCh stalled >%s; full event persisted to %s (retrievable via read_file)", outputSendGrace, path)
		// Non-blocking ticket: if the consumer is still stalled, the ticket
		// is skipped too — the file is the durable record either way.
		select {
		case cm.outputCh <- overflowTicket(evt, path):
		default:
		}
		return false
	}
}
