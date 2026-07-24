package agent

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/model"
)

// maxBoardTasks caps how many active tasks the live board renders.
const maxBoardTasks = 20

// renderTaskBoard renders a compact, LLM-friendly snapshot of currently ACTIVE
// tasks (running/stable/alive-detached/suspect) from the registry. Terminal
// tasks (completed/failed/cancelled/dead) are aged out — they were already
// surfaced once via task_settled events, so keeping them on the board would be
// stale noise.
//
// The board is regenerated fresh each turn at BeforeModel time and never
// persisted, so it does NOT participate in context compression (D6): it is a
// live recency anchor of current async state. Returns "" when no active tasks
// exist (the caller then injects nothing).
func renderTaskBoard(tasks []*Task) string {
	active := make([]*Task, 0, len(tasks))
	for _, t := range tasks {
		if t.isActive() {
			active = append(active, t)
		}
	}
	if len(active) == 0 {
		return ""
	}

	// Most-recently-started first, capped for bounded size.
	sort.Slice(active, func(i, j int) bool {
		return active[i].StartedAt.After(active[j].StartedAt)
	})
	if len(active) > maxBoardTasks {
		active = active[:maxBoardTasks]
	}

	var b strings.Builder
	fmt.Fprintf(&b, "## 后台任务看板 (%d 个进行中)\n", len(active))
	for _, t := range active {
		age := time.Since(t.StartedAt).Round(time.Second)
		fmt.Fprintf(&b, "- [%s] %s (id=%s, 已运行 %v)", t.Status(), t.Spec.Desc, shortID(t.ID), age)
		if t.Status() == TaskSuspect {
			b.WriteString(" ⚠ 长时间无输出，可能假死，需确认")
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// shortID returns a short, human-friendly prefix of a task id for display.
func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// injectTaskBoard inserts the rendered board (as a user-role context message)
// immediately before the last user message — i.e. after the latest agent
// output, just before the current input — so it reads as fresh state framing
// the request. When there is no user message it is appended at the end. A
// non-empty board string is required; callers should skip injection for "".
func injectTaskBoard(msgs []model.Message, board string) []model.Message {
	boardMsg := model.Message{Role: model.RoleUser, Content: board}
	lastUser := -1
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == model.RoleUser {
			lastUser = i
			break
		}
	}
	if lastUser < 0 {
		return append(msgs, boardMsg)
	}
	out := make([]model.Message, 0, len(msgs)+1)
	out = append(out, msgs[:lastUser]...)
	out = append(out, boardMsg)
	out = append(out, msgs[lastUser:]...)
	return out
}
