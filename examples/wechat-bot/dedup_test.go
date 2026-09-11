package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SpellingDragon/wechat-robot-go/wechat"
)

func newTestStore(t *testing.T) (*SeenStore, string) {
	t.Helper()
	dir := t.TempDir()
	return NewSeenStore(dir, slog.Default()), dir
}

func TestSeenStore_FirstMarkAndDuplicate(t *testing.T) {
	s, _ := newTestStore(t)

	if !s.CheckAndMark("k1") {
		t.Errorf("first CheckAndMark(k1) = false, want true")
	}
	if s.CheckAndMark("k1") {
		t.Errorf("second CheckAndMark(k1) = true, want false (duplicate)")
	}
	if !s.CheckAndMark("k2") {
		t.Errorf("first CheckAndMark(k2) = false, want true")
	}
}

func TestSeenStore_RestartRecovery(t *testing.T) {
	dir := t.TempDir()
	s1 := NewSeenStore(dir, slog.Default())
	s1.CheckAndMark("persist_me")

	// Simulate restart: fresh store from the same dir.
	s2 := NewSeenStore(dir, slog.Default())
	if s2.CheckAndMark("persist_me") {
		t.Errorf("after restart CheckAndMark(persist_me) = true, want false (restored)")
	}
	if !s2.CheckAndMark("fresh_after_restart") {
		t.Errorf("new key after restart should be true")
	}
}

func TestSeenStore_CorruptedFileDegradesToEmpty(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "seen.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := NewSeenStore(dir, slog.Default())
	if !s.CheckAndMark("after_corruption") {
		t.Errorf("corrupted file should degrade to empty set, key should be new")
	}
	if _, err := os.Stat(filepath.Join(dir, "seen.json.corrupt")); err != nil {
		t.Errorf("corrupt file should be preserved as .corrupt for post-mortem: %v", err)
	}
}

func TestSeenStore_CapacityEviction(t *testing.T) {
	dir := t.TempDir()
	s := NewSeenStore(dir, slog.Default())
	s.capacity = 5

	// Seed 5 keys with strictly increasing timestamps (deterministic order
	// for eviction; wall-clock seconds would tie within the same second).
	base := time.Now().Add(-time.Hour).Unix()
	s.mu.Lock()
	for i, k := range []string{"a", "b", "c", "d", "e"} {
		s.seen[k] = base + int64(i)
	}
	s.mu.Unlock()

	// Marking a 6th key persists, which evicts down to capacity — oldest ("a") first.
	if !s.CheckAndMark("f") {
		t.Fatal("new key should be true")
	}
	s.mu.Lock()
	_, aExists := s.seen["a"]
	_, fExists := s.seen["f"]
	size := len(s.seen)
	s.mu.Unlock()

	if aExists {
		t.Errorf("oldest key 'a' should have been evicted")
	}
	if !fExists {
		t.Errorf("newest key 'f' should be present")
	}
	if size != 5 {
		t.Errorf("size = %d, want capped at 5", size)
	}
}

func TestSeenStore_TTLExpiry(t *testing.T) {
	dir := t.TempDir()
	s := NewSeenStore(dir, slog.Default())

	s.mu.Lock()
	s.seen["old"] = time.Now().Add(-25 * time.Hour).Unix()
	s.seen["recent"] = time.Now().Unix()
	s.mu.Unlock()

	// Any CheckAndMark triggers persistence which prunes by TTL.
	s.CheckAndMark("trigger")

	s.mu.Lock()
	_, oldExists := s.seen["old"]
	_, recentExists := s.seen["recent"]
	s.mu.Unlock()

	if oldExists {
		t.Errorf("TTL-expired key should be pruned")
	}
	if !recentExists {
		t.Errorf("recent key should survive TTL pruning")
	}
}

func TestSeenStore_TTLExpirySurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	// Hand-write a seen.json with an expired and a fresh entry.
	m := map[string]int64{
		"expired": time.Now().Add(-48 * time.Hour).Unix(),
		"fresh":   time.Now().Unix(),
	}
	data, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(dir, "seen.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	s := NewSeenStore(dir, slog.Default())
	if !s.CheckAndMark("expired") {
		t.Errorf("expired entry should have been dropped at load; key should be new")
	}
	if s.CheckAndMark("fresh") {
		t.Errorf("fresh entry should have been restored; key should be duplicate")
	}
}

func TestDedupKey_ClientIDPriority(t *testing.T) {
	msg := &wechat.Message{ClientID: "abc-123", FromUserID: "u1"}
	if got := DedupKey(msg); got != "cid:abc-123" {
		t.Errorf("DedupKey = %q, want %q", got, "cid:abc-123")
	}
}

func TestDedupKey_FallbackHash(t *testing.T) {
	msg := &wechat.Message{FromUserID: "u1"}
	msg2 := &wechat.Message{FromUserID: "u1"}

	// Same user, empty text: stable key.
	if DedupKey(msg) != DedupKey(msg2) {
		t.Errorf("identical messages should produce identical keys")
	}

	// Different users, same text: different keys.
	m1 := &wechat.Message{FromUserID: "alice"}
	m2 := &wechat.Message{FromUserID: "bob"}
	if DedupKey(m1) == DedupKey(m2) {
		t.Errorf("same text from different users must not collide")
	}

	// Expected format: uid:<user>#<16 hex chars>
	sum := sha256.Sum256([]byte(""))
	want := "uid:alice#" + hex.EncodeToString(sum[:8])
	if got := DedupKey(m1); got != want {
		t.Errorf("DedupKey = %q, want %q", got, want)
	}
}
