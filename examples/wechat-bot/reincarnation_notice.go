package main

// Reincarnation notice — openspec/changes/wechat-bot-reincarnation-notice.
//
// After an insurance-chain self-replacement the NEW process detects the fresh
// REINCARNATION_NOTICE marker at startup (D1) and injects a "转世通报" into its own
// event stream via the meditation source (D2), so the agent's first turn knows:
//   - it was reincarnated (metadata archive REINCARNATION_NOTICE, D3)
//   - what it was doing when the old process died (WAL tail scene block, D8)
//   - where the last turn broke off (breakpoint marker, D8)
//
// Consumption is marked by renaming REINCARNATION_NOTICE → *.notified (D5).
// Degradation is never silent: every downgrade path is stated in the notice
// text itself and logged by the caller.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/SpellingDragon/tagent/memory"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

const (
	// restartDoneFreshWindow: D1 freshness gate. the NOTICE must be younger
	// than this for the startup to count as a reincarnation (not cold start).
	restartDoneFreshWindow = 10 * time.Minute

	// walTailEventLimit: how many newest events the scene block carries (D8
	// token discipline ~2-4K tokens: key+type+summary only, no full content).
	walTailEventLimit = 12

	// walTailSummaryMaxChars caps each event summary line in the scene block.
	walTailSummaryMaxChars = 160

	// noticeConsumedSuffix: D5 rename target.
	noticeConsumedSuffix = ".notified"
)

// noticeInjector is the minimal surface of *agent.TagentAgent this feature
// needs (satisfied by the tagent.New return value used in main.go).
type noticeInjector interface {
	InjectMessageWithSource(source string, msg model.Message)
	MemStore() memory.MemoryStore
}

// detectReincarnation implements D1 (execution-period revision): the
// insurance chain stages REINCARNATION_NOTICE BEFORE spawning the new
// process — no race, nobody rewrites it — so a fresh NOTICE file IS the
// reincarnation signal. restart.done is NOT used for detection: the health
// gate writes it only AFTER spawn and it always carries a live PID (the new
// PID = self), which made PID-based detection a race we could lose.
// run.sh cold start never stages a NOTICE → never false-positives. A parse
// failure degrades the notice TEXT, never the DETECTION (existence suffices).
func detectReincarnation(noticePath string, now time.Time) bool {
	st, err := os.Stat(noticePath)
	if err != nil {
		return false
	}
	return now.Sub(st.ModTime()) <= restartDoneFreshWindow
}

// readNoticeMetadata parses run/REINCARNATION_NOTICE "key: value" lines (D3).
// Missing/unreadable file → nil map; the caller's notice text will carry the
// degraded marker (never silent).
func readNoticeMetadata(path string) map[string]string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	meta := make(map[string]string)
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if i := strings.Index(line, ":"); i > 0 {
			k := strings.TrimSpace(line[:i])
			v := strings.TrimSpace(line[i+1:])
			if k != "" {
				meta[k] = v
			}
		}
	}
	if len(meta) == 0 {
		return nil
	}
	return meta
}

// tailQuerier is the narrow interface fetchWALTail actually needs (Go idiom:
// accept what you use). The full memory.MemoryStore satisfies it, and so do
// test fakes without implementing dozens of unrelated store methods.
type tailQuerier interface {
	QueryEvents(memory.QueryOptions) ([]memory.EventReference, error)
}

// fetchWALTail queries the newest events time-desc, limited (D8). Errors are
// returned for logged degradation — D8 forbids swallowing them.
func fetchWALTail(store tailQuerier, agentName string, limit int) ([]memory.EventReference, error) {
	if store == nil {
		return nil, fmt.Errorf("memstore unavailable")
	}
	// Partitioned store contract (segment_store.go resolvePartitions): a query
	// without PartitionIDs scans zero partitions and silently returns empty.
	// The agent's own events live in PartitionIDFromName(agentName) — same
	// derivation as build_agent.go wiring — so pass it explicitly.
	pid := memory.PartitionIDFromName(agentName)
	return store.QueryEvents(memory.QueryOptions{
		PartitionIDs: []int{pid},
		Limit:        limit,
		OrderBy:      "timestamp_desc", // segment_store.go: valid value
	})
}

// hasOpenBreakpoint reports whether the last turn was cut off mid-flight (D8):
// events arrive newest-first; chronologically, the turn is "open" when the
// newest event is a thinking_plan (or similar in-flight marker) with no
// agent_output after it. A trailing agent_output means the turn closed.
func hasOpenBreakpoint(refs []memory.EventReference) bool {
	if len(refs) == 0 {
		return false
	}
	// refs[0] is the newest event (timestamp_desc).
	switch refs[0].EventType {
	case "thinking_plan", "action_command":
		return true // died thinking or mid-tool, no closing output
	default:
		return false
	}
}

// buildNoticeText composes the full notice (D3 metadata + D8 scene block +
// breakpoint marker). Degradation ladder, each rung explicit:
//  1. meta present + WAL ok        → full notice
//  2. meta nil                     → "NOTICE 档案缺失" marker
//  3. walErr != nil / empty refs   → "现场块不可用" marker (with reason)
func buildNoticeText(meta map[string]string, refs []memory.EventReference, walErr error) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[转世通报] 检测到保险链自替换换装（D1 命中：REINCARNATION_NOTICE 新鲜；PID 详情见元数据段）\n\n")

	b.WriteString("== 元数据 ==\n")
	if meta == nil {
		b.WriteString("（NOTICE 档案缺失——降级：检测依据 NOTICE 自身 mtime，详情查 logs/restart.log）\n")
	} else {
		keys := make([]string, 0, len(meta))
		for k := range meta {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(&b, "%s: %s\n", k, meta[k])
		}
	}

	b.WriteString("\n== WAL 尾现场块（最近事件，时间倒序）==\n")
	switch {
	case walErr != nil:
		fmt.Fprintf(&b, "（现场块不可用：%v——降级，禁静默）\n", walErr)
	case len(refs) == 0:
		b.WriteString("（事件存储为空或查询无结果——降级，禁静默）\n")
	default:
		for _, r := range refs {
			summary := r.EventSummary
			if len(summary) > walTailSummaryMaxChars {
				summary = summary[:walTailSummaryMaxChars] + "…"
			}
			summary = strings.ReplaceAll(summary, "\n", " ")
			fmt.Fprintf(&b, "- [evt_%x|%s] %s (%s)\n",
				r.EventKey, r.EventType, summary, time.UnixMilli(r.Timestamp).Format("15:04:05"))
		}
		if hasOpenBreakpoint(refs) {
			b.WriteString("\n断点判定：最后事件止于回合中途且无收尾 agent_output → 上一世最后一回合**中断于此**；\n续作前先核验该时刻的承诺是否已兑现（对照 WAL 与系统实态），勿凭通报默认成功。\n")
		} else {
			b.WriteString("\n断点判定：最后事件为收尾输出 → 上一世回合已闭环，无未兑现承诺迹象。\n")
		}
	}
	return b.String()
}

// maybeInjectReincarnationNotice is the one-shot startup hook (D4): wait for
// the event loop to settle, detect (D1), compose (D3+D8), inject via the
// meditation source (D2), then rename the marker (D5). Every step is logged;
// nothing here is allowed to crash the bot.
func maybeInjectReincarnationNotice(ta noticeInjector, agentName string, runDir string, delay time.Duration) {
	if !filepath.IsAbs(runDir) {
		// cwd can drift across launchers; anchors resolve relative to the binary.
		if exe, err := os.Executable(); err == nil {
			runDir = filepath.Join(filepath.Dir(exe), runDir)
		}
	}
	noticePath := filepath.Join(runDir, "REINCARNATION_NOTICE")

	time.Sleep(delay)

	if !detectReincarnation(noticePath, time.Now()) {
		return // cold start / stale NOTICE / already consumed: silent no-op
	}
	log.Infof("[reincarnation] D1 hit: fresh REINCARNATION_NOTICE at %s", noticePath)

	meta := readNoticeMetadata(noticePath)
	if meta == nil {
		log.Warnf("[reincarnation] NOTICE archive missing at %s — degraded metadata", noticePath)
	}
	refs, walErr := fetchWALTail(ta.MemStore(), agentName, walTailEventLimit)
	if walErr != nil {
		log.Warnf("[reincarnation] WAL tail query failed: %v — degraded scene block", walErr)
	}

	text := buildNoticeText(meta, refs, walErr)
	ta.InjectMessageWithSource("meditation", model.Message{
		Role:    model.RoleUser,
		Content: text,
	})
	log.Infof("[reincarnation] notice injected via meditation source (%d WAL events, meta=%v)", len(refs), meta != nil)

	// D5: mark consumed. Failure to rename is logged and non-fatal; a duplicate
	// notice next boot is acceptable, a crash here is not.
	if err := os.Rename(noticePath, noticePath+noticeConsumedSuffix); err != nil {
		log.Warnf("[reincarnation] consume-marker rename failed (re-notice on next boot): %v", err)
	} else {
		log.Infof("[reincarnation] NOTICE renamed → consumed marker")
	}
}
