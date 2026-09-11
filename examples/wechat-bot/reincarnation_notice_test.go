package main

// Unit tests for the reincarnation notice feature — pure filesystem logic,
// no network, no WeChat (design R6). Covers: D1 freshness/PID quadrants,
// D3 metadata parse + missing-archive degradation, D8 WAL tail + breakpoint
// marker + degraded scene block, D5 rename idempotency.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/memory"
)

func stageNotice(t *testing.T, dir string, mtime time.Time, body string) string {
	t.Helper()
	p := filepath.Join(dir, "REINCARNATION_NOTICE")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDetectReincarnation(t *testing.T) {
	now := time.Now()

	t.Run("hit_fresh_notice", func(t *testing.T) {
		dir := t.TempDir()
		p := stageNotice(t, dir, now.Add(-time.Minute), "old_pid: 1111\n")
		if !detectReincarnation(p, now) {
			t.Fatal("fresh NOTICE must count as reincarnation")
		}
	})
	t.Run("miss_stale_notice", func(t *testing.T) {
		dir := t.TempDir()
		p := stageNotice(t, dir, now.Add(-11*time.Minute), "old_pid: 1111\n")
		if detectReincarnation(p, now) {
			t.Fatal("stale NOTICE must not count")
		}
	})
	t.Run("miss_missing_notice", func(t *testing.T) {
		if detectReincarnation(filepath.Join(t.TempDir(), "none"), now) {
			t.Fatal("missing NOTICE must not count (cold start)")
		}
	})
	t.Run("hit_even_unparsable", func(t *testing.T) {
		dir := t.TempDir()
		p := stageNotice(t, dir, now.Add(-time.Minute), "garbage")
		if !detectReincarnation(p, now) {
			t.Fatal("existence is the signal; parse failure degrades text, not detection")
		}
	})
}

func TestReadNoticeMetadata(t *testing.T) {
	t.Run("parses_key_value", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "REINCARNATION_NOTICE")
		os.WriteFile(p, []byte("reincarnated_at: 2026-09-11 13:58:12\nold_pid: 2185305\nbuild_sha: abc123def456\n"), 0o644)
		meta := readNoticeMetadata(p)
		if meta == nil || meta["old_pid"] != "2185305" || meta["build_sha"] != "abc123def456" {
			t.Fatalf("bad parse: %v", meta)
		}
	})
	t.Run("missing_archive_returns_nil", func(t *testing.T) {
		if meta := readNoticeMetadata(filepath.Join(t.TempDir(), "none")); meta != nil {
			t.Fatal("missing archive must return nil (degraded path)")
		}
	})
}

// fakeStore adapts a canned QueryEvents result for D8 contract tests.
type fakeStore struct {
	refs []memory.EventReference
	err  error
}

func (f *fakeStore) QueryEvents(q memory.QueryOptions) ([]memory.EventReference, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.refs, nil
}

// TestFetchWALTail pins the D8 query contract: tail query with limit, error
// propagation for logged degradation (never swallowed), nil-store unavailability.
func TestFetchWALTail(t *testing.T) {
	refs := []memory.EventReference{{EventKey: 1, EventType: "agent_output"}, {EventKey: 2, EventType: "thinking_plan"}}
	got, err := fetchWALTail(&fakeStore{refs: refs}, 5)
	if err != nil || len(got) != 2 {
		t.Fatalf("tail query failed: %v %v", got, err)
	}
	if _, err := fetchWALTail(&fakeStore{err: os.ErrPermission}, 5); err == nil {
		t.Fatal("store error must propagate (D8 forbids silent degradation)")
	}
	if _, err := fetchWALTail(nil, 5); err == nil {
		t.Fatal("nil store must report unavailability")
	}
}

func TestBuildNoticeText(t *testing.T) {
	refs := []memory.EventReference{
		{EventKey: 1292834793, EventType: "thinking_plan", EventSummary: "全绿收官：BUILD=0，最后一步换装上线", Timestamp: time.Now().Add(-2 * time.Minute).UnixMilli()},
		{EventKey: 1292834781, EventType: "external_input", EventSummary: "[task settled] 门禁链 completed → BUILD=0 COMMIT_OK PUSH_OK", Timestamp: time.Now().Add(-4 * time.Minute).UnixMilli()},
	}

	t.Run("full_notice_with_breakpoint", func(t *testing.T) {
		meta := map[string]string{"old_pid": "1111", "build_sha": "abc123"}
		txt := buildNoticeText(meta, refs, nil)
		for _, want := range []string{"[转世通报]", "old_pid: 1111", "WAL 尾现场块", "thinking_plan", "中断于此"} {
			if !strings.Contains(txt, want) {
				t.Fatalf("notice missing %q\n%s", want, txt)
			}
		}
	})
	t.Run("degraded_metadata", func(t *testing.T) {
		txt := buildNoticeText(nil, refs, nil)
		if !strings.Contains(txt, "NOTICE 档案缺失") {
			t.Fatalf("missing-archive degradation not stated:\n%s", txt)
		}
	})
	t.Run("degraded_scene_block", func(t *testing.T) {
		txt := buildNoticeText(map[string]string{"old_pid": "1"}, nil, os.ErrPermission)
		if !strings.Contains(txt, "现场块不可用") {
			t.Fatalf("WAL-failure degradation not stated:\n%s", txt)
		}
	})
	t.Run("closed_turn_no_breakpoint", func(t *testing.T) {
		closed := []memory.EventReference{{EventKey: 7, EventType: "agent_output", EventSummary: "收尾", Timestamp: time.Now().UnixMilli()}}
		txt := buildNoticeText(nil, closed, nil)
		if !strings.Contains(txt, "已闭环") {
			t.Fatalf("closed-turn marker missing:\n%s", txt)
		}
	})
}

func TestHasOpenBreakpoint(t *testing.T) {
	mk := func(typ string) []memory.EventReference {
		return []memory.EventReference{{EventType: typ, Timestamp: time.Now().UnixMilli()}}
	}
	if !hasOpenBreakpoint(mk("thinking_plan")) {
		t.Fatal("newest=thinking_plan must be open")
	}
	if !hasOpenBreakpoint(mk("action_command")) {
		t.Fatal("newest=action_command must be open (mid-tool death)")
	}
	if hasOpenBreakpoint(mk("agent_output")) {
		t.Fatal("newest=agent_output must be closed")
	}
	if hasOpenBreakpoint(nil) {
		t.Fatal("empty refs must be closed (nothing to resume)")
	}
}
