package agent

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/SpellingDragon/tagent/agent/task"
)

// digestMaxAttentionDetail bounds how many attention tasks are listed per line;
// the rest are summarized as a count so the meditation message stays bounded.
const digestMaxAttentionDetail = 8

// digestDescMax bounds a task description's rune length in the digest.
const digestDescMax = 60

// digestStatusOrder is the deterministic display order for status counts.
var digestStatusOrder = []TaskStatus{
	TaskRunning, TaskStable, TaskAliveDetached,
	TaskSuspect, TaskDead, TaskFailed,
	TaskCompleted, TaskCancelled,
}

// renderSelfStateDigest renders a deterministic self-state snapshot for the
// meditation event: task-layer health (per-status counts + attention tasks that
// are suspect/dead/failed) and idle duration. It is a pure function over the
// given snapshot — no LLM, no I/O. Returns "" when there are no tasks so the
// meditation message degrades gracefully (task 1.2 / spec: graceful degradation).
func renderSelfStateDigest(tasks []*Task, idle time.Duration) string {
	counts := make(map[TaskStatus]int)
	var attention []*Task
	for _, t := range tasks {
		if t == nil {
			continue
		}
		st := t.Status()
		counts[st]++
		if st == TaskSuspect || st == TaskDead || st == TaskFailed {
			attention = append(attention, t)
		}
	}
	if len(counts) == 0 {
		return "" // no tasks — omit the digest section entirely
	}

	var b strings.Builder
	b.WriteString("## 自我状态快照（定时自省）\n\n")
	b.WriteString(fmt.Sprintf("- 空闲时长：%s\n", idle.Round(time.Second)))
	b.WriteString("- 任务层：" + formatStatusCounts(counts) + "\n")

	if len(attention) > 0 {
		b.WriteString(fmt.Sprintf("- 需关注（suspect/dead/failed，共 %d）：\n", len(attention)))
		// Oldest first — the most likely stuck.
		sort.Slice(attention, func(i, j int) bool {
			return attention[i].StartedAt.Before(attention[j].StartedAt)
		})
		shown := attention
		if len(shown) > digestMaxAttentionDetail {
			shown = shown[:digestMaxAttentionDetail]
		}
		now := time.Now()
		for _, t := range shown {
			age := now.Sub(t.StartedAt).Round(time.Second)
			b.WriteString(fmt.Sprintf("  - [%s] %s（id=%s，已 %s）\n",
				t.Status(), truncateRunes(t.Spec.Desc, digestDescMax), task.ShortID(t.ID), age))
		}
		if rest := len(attention) - len(shown); rest > 0 {
			b.WriteString(fmt.Sprintf("  - …另有 %d 条需关注任务\n", rest))
		}
	}

	return b.String()
}

// formatStatusCounts renders non-zero status counts in a deterministic order.
func formatStatusCounts(counts map[TaskStatus]int) string {
	parts := make([]string, 0, len(counts))
	for _, st := range digestStatusOrder {
		if n := counts[st]; n > 0 {
			parts = append(parts, fmt.Sprintf("%s=%d", st, n))
		}
	}
	if len(parts) == 0 {
		return "无"
	}
	return strings.Join(parts, " ")
}

// truncateRunes truncates s to at most n runes (rune-safe for CJK), appending
// an ellipsis when cut.
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
