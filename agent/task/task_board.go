package task

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/model"
)

// maxBoardTasks caps how many active tasks the live board renders.
const maxBoardTasks = 20

// RenderBoard renders a compact, LLM-friendly snapshot of currently ACTIVE tasks
// (running/stable/alive-detached/suspect) from the registry.
//
// - Terminal tasks are aged out; the board is a live recency anchor that is never persisted and takes no part in compression.
// - Returns "" when no active task exists, and the caller then injects nothing.
// 契约: docs/wiki/agent/task-lifecycle.md#board-rendering
func RenderBoard(tasks []*Task, defaultTTL time.Duration) string {
	active := make([]*Task, 0, len(tasks))
	for _, t := range tasks {
		if t.isActive() {
			active = append(active, t)
		}
	}
	if len(active) == 0 {
		return ""
	}

	sort.Slice(active, func(i, j int) bool {
		return active[i].StartedAt.After(active[j].StartedAt)
	})
	if len(active) > maxBoardTasks {
		active = active[:maxBoardTasks]
	}

	now := time.Now()
	var b strings.Builder
	fmt.Fprintf(&b, "[后台任务看板] 系统注入的观察快照（非用户发言，不入历史，勿在回复中模仿此格式）：当前 %d 个进行中\n", len(active))
	for _, t := range active {
		age := now.Sub(t.StartedAt).Round(time.Second)
		fmt.Fprintf(&b, "- [%s] %s (id=%s, 已运行 %v", t.Status(), t.Spec.Desc, ShortID(t.ID), age)
		if rem, ok := t.remainingLifetime(now, defaultTTL); ok {
			if rem <= 0 {
				b.WriteString(", 即将回收")
			} else {
				r := rem.Round(time.Minute)
				if r <= 0 {
					r = rem.Round(time.Second)
				}
				fmt.Fprintf(&b, ", 剩余 %v 后回收", r)
			}
		}
		b.WriteString(")\n")
	}
	b.WriteString("以上任务无需轮询等待：直接给出简短回复并结束本回合即可，结算会自动唤醒你；不要用 sleep 等命令等待。\n")
	return strings.TrimRight(b.String(), "\n")
}

// ShortID returns a short, human-friendly prefix of a task id for display.
func ShortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// InjectBoard appends the rendered board (as a user-role context message) at
// the very END of the message list — after the current input and any pending
// tool results.
//
// Cache rationale: the board is re-rendered before EVERY
// LLM call (task ages tick, tasks settle), so its bytes change call-to-call.
// Injecting it before the last user message broke the prompt-cache prefix at
// that point — every in-turn LLM call re-paid the whole active turn. At the
// tail, the cacheable prefix covers everything except the board itself; and
// the wait-guidance being the last thing the model reads strengthens the
// anti-spin teaching. A non-empty board string is required; callers should
// skip injection for "".
func InjectBoard(msgs []model.Message, board string) []model.Message {
	return append(msgs, model.Message{Role: model.RoleUser, Content: board})
}
