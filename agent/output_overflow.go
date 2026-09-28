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

// outputSendGrace is how long RunFlow waits for a stalled consumer before
// persisting the event to disk and moving on.
//
// 消费者停滞不会阻塞主循环；事件仍被完整持久化，票据携带头尾摘要，全文可从磁盘找回。
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
		resp := *ticket.Response
		resp.Choices = append([]model.Choice(nil), ticket.Response.Choices...)
		ticket.Response = &resp
		msg := &resp.Choices[len(resp.Choices)-1].Message
		c := msg.Content
		if len(c) > overflowTicketHead+overflowTicketTail {
			msg.Content = c[:overflowTicketHead] + "\n...[output stalled, full content persisted]...\n" + c[len(c)-overflowTicketTail:]
		}
		msg.Content += fmt.Sprintf("\n[全文已存 %s，可 exec cat 取回]", path)
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
		select {
		case cm.outputCh <- evt:
			return true
		default:
		}
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
		_ = cm.persistBusEvent(&AgentEvent{
			ID:        fmt.Sprintf("overflow-%d", time.Now().UnixNano()),
			Type:      "external_input",
			Timestamp: time.Now(),
			Message: &model.Message{
				Role:    model.RoleUser,
				Content: fmt.Sprintf("溢出全文已存 %s（可 exec cat 取回）", path),
			},
		})
		select {
		case cm.outputCh <- overflowTicket(evt, path):
		default:
		}
		return false
	}
}
