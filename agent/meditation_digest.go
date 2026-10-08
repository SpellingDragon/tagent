// 契约: docs/wiki/agent/compression-and-telemetry.md#self-state-digest
package agent

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/SpellingDragon/tagent/agent/task"
	tagentevent "github.com/SpellingDragon/tagent/event"
)

// digestMaxAttentionDetail bounds how many attention tasks are listed per line;
// the rest are summarized as a count so the meditation message stays bounded.
const digestMaxAttentionDetail = 8

// digestDescMax bounds a task description's rune length in the digest.
const digestDescMax = 60

// digestStatusOrder is the deterministic display order for status counts.
var digestStatusOrder = []task.TaskStatus{
	task.TaskRunning, task.TaskStable, task.TaskAliveDetached,
	task.TaskSuspect, task.TaskDead, task.TaskFailed,
	task.TaskCompleted, task.TaskCancelled,
}

// renderSelfStateDigest renders a deterministic self-state snapshot for the
// meditation event: task-layer health (per-status counts + attention tasks that
// are suspect/dead/failed) and idle duration. It is a pure function over the
// given snapshot — no LLM, no I/O. Returns "" when there are no tasks so the
// meditation message degrades gracefully.
func renderSelfStateDigest(tasks []*task.Task, idle time.Duration) string {
	counts := make(map[task.TaskStatus]int)
	var attention []*task.Task
	for _, t := range tasks {
		if t == nil {
			continue
		}
		st := t.Status()
		counts[st]++
		if st == task.TaskSuspect || st == task.TaskDead || st == task.TaskFailed {
			attention = append(attention, t)
		}
	}
	if len(counts) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("## 自我状态快照（定时自省）\n\n")
	b.WriteString(fmt.Sprintf("- 空闲时长：%s\n", idle.Round(time.Second)))
	b.WriteString("- 任务层：" + formatStatusCounts(counts) + "\n")

	if len(attention) > 0 {
		b.WriteString(fmt.Sprintf("- 需关注（suspect/dead/failed，共 %d）：\n", len(attention)))
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
func formatStatusCounts(counts map[task.TaskStatus]int) string {
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

// digestMaxObservedPartitions bounds how many observed partitions a digest names
// per line; the rest are summarized as a count, the same bounded-render discipline
// as the task-layer attention detail.
const digestMaxObservedPartitions = 8

// renderObservedScanDigest renders the external observation form's digest: per
// partition event counts split by lineage, plus the newest non-self-managed
// activity. It is a pure function over the novelty pass's collected evidence — no
// LLM, no I/O — deterministic and bounded, and it never re-reads the fact chain.
// 契约: docs/wiki/agent/compression-and-telemetry.md#self-state-digest
func renderObservedScanDigest(s *observedScan, idle time.Duration) string {
	if s == nil || len(s.partitions) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("## 观察分区概况（跨域策展）\n\n")
	b.WriteString(fmt.Sprintf("- 空闲时长：%s\n", idle.Round(time.Second)))
	window := "首次冥想，自 epoch 起"
	if s.watermarkMs > 0 {
		window = "自 " + time.UnixMilli(s.watermarkMs).UTC().Format("2006-01-02 15:04:05") + " 起"
	}
	b.WriteString(fmt.Sprintf("- 判据窗口：%s；引用页 %d 条，本次水合 %d 条（命中即停）\n",
		window, s.referenceTotal, s.hydratedTotal))

	shown := s.partitions
	if len(shown) > digestMaxObservedPartitions {
		shown = shown[:digestMaxObservedPartitions]
	}
	for _, p := range shown {
		b.WriteString(fmt.Sprintf("- 分区 %s（id=%d）：引用 %d；水合样本 非自管=%d 自管/未知=%d\n",
			p.name, p.id, p.references, p.externalHits, p.selfManaged))
	}
	if rest := len(s.partitions) - len(shown); rest > 0 {
		b.WriteString(fmt.Sprintf("- …另有 %d 个观察分区未列出\n", rest))
	}
	if s.unknownLineage > 0 {
		b.WriteString(fmt.Sprintf("- 谱系未知（无持久 trigger_source）：%d 条，不计入新鲜度\n", s.unknownLineage))
	}
	if s.foreignPartition > 0 {
		b.WriteString(fmt.Sprintf("- 观察面外分区：%d 条，未计入\n", s.foreignPartition))
	}
	if s.recent != nil {
		b.WriteString(fmt.Sprintf("- 最近非自管活动：[%s] %s / %s @ %s（trigger_source=%s）：%s\n",
			tagentevent.FormatEventKey(s.recent.eventKey),
			s.recent.partition, s.recent.eventType,
			time.UnixMilli(s.recent.timestampMs).UTC().Format("2006-01-02 15:04:05"),
			s.recent.lineage, truncateRunes(s.recent.summary, digestDescMax)))
	}

	return b.String()
}
