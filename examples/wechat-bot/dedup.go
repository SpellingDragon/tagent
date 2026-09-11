package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/SpellingDragon/wechat-robot-go/wechat"
)

// SeenStore provides message-level idempotency across restarts.
//
// Dedup key layering (design D3; assumption A1 — the gateway message model
// carries no msg_id; if one appears upstream, prefer it as the primary key):
//  1. ClientID when non-empty (client-generated idempotency key),
//  2. fallback: "uid:" + FromUserID + "#" + first 16 hex chars of sha256(text).
//
// Persistence: seen.json next to the bot config; loaded on start (restart
// recovery), pruned by TTL and capacity on every save, written atomically
// (tmp+rename). A corrupted file degrades to an empty set with a warning —
// dedup never blocks the message pipeline on its own failure.
type SeenStore struct {
	path     string
	ttl      time.Duration
	capacity int
	mu       sync.Mutex
	seen     map[string]int64
	logger   *slog.Logger
}

// NewSeenStore loads (or initializes) the seen set from dir/seen.json.
func NewSeenStore(dir string, logger *slog.Logger) *SeenStore {
	s := &SeenStore{
		path:     filepath.Join(dir, "seen.json"),
		ttl:      24 * time.Hour,
		capacity: 10000,
		seen:     make(map[string]int64),
		logger:   logger,
	}
	s.load()
	return s
}

func (s *SeenStore) load() {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if !os.IsNotExist(err) {
			s.logger.Warn("[Dedup] failed to read seen.json, starting empty", "error", err)
		}
		return
	}
	var m map[string]int64
	if err := json.Unmarshal(data, &m); err != nil {
		// Corrupted file: degrade to empty set, keep the bad file aside for
		// post-mortem instead of silently overwriting it.
		s.logger.Warn("[Dedup] seen.json corrupted, starting empty", "error", err)
		_ = os.Rename(s.path, s.path+".corrupt")
		return
	}
	now := time.Now().Unix()
	for k, ts := range m {
		if now-ts <= int64(s.ttl.Seconds()) {
			s.seen[k] = ts
		}
	}
	s.logger.Info("[Dedup] seen.json restored", "entries", len(s.seen))
}

// CheckAndMark returns true if the key was new (message should be processed),
// false if it was already seen (duplicate — drop). Marking is persisted
// immediately (atomic write) so a crash right after cannot replay the message.
func (s *SeenStore) CheckAndMark(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.seen[key]; ok {
		s.seen[key] = time.Now().Unix() // sliding TTL refresh; duplicate either way
		return false
	}
	s.seen[key] = time.Now().Unix()
	s.persistLocked()
	return true
}

// persistLocked prunes (TTL, then capacity) and atomically writes the set.
// Caller must hold s.mu.
func (s *SeenStore) persistLocked() {
	now := time.Now().Unix()
	ttlSec := int64(s.ttl.Seconds())
	for k, ts := range s.seen {
		if now-ts > ttlSec {
			delete(s.seen, k)
		}
	}
	if len(s.seen) > s.capacity {
		// Drop the oldest entries. O(n log n) with stdlib sort; happens only
		// when the cap is exceeded, and 10k entries sort in microseconds.
		type kv struct {
			k string
			v int64
		}
		list := make([]kv, 0, len(s.seen))
		for k, v := range s.seen {
			list = append(list, kv{k, v})
		}
		sort.Slice(list, func(i, j int) bool { return list[i].v < list[j].v }) // oldest first
		for i := 0; i < len(list)-s.capacity; i++ {
			delete(s.seen, list[i].k)
		}
	}

	data, err := json.Marshal(s.seen)
	if err != nil {
		s.logger.Warn("[Dedup] marshal seen.json failed", "error", err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		s.logger.Warn("[Dedup] ensure seen dir failed", "error", err)
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".seen-*")
	if err != nil {
		s.logger.Warn("[Dedup] create seen tmp failed", "error", err)
		return
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		s.logger.Warn("[Dedup] write seen tmp failed", "error", err)
		return
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		s.logger.Warn("[Dedup] close seen tmp failed", "error", err)
		return
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		os.Remove(tmpName)
		s.logger.Warn("[Dedup] rename seen.json failed", "error", err)
		return
	}
}

// DedupKey builds the layered dedup key for an inbound message (see SeenStore
// doc for the layering rationale).
func DedupKey(msg *wechat.Message) string {
	if msg.ClientID != "" {
		return "cid:" + msg.ClientID
	}
	sum := sha256.Sum256([]byte(msg.Text()))
	return fmt.Sprintf("uid:%s#%s", msg.FromUserID, hex.EncodeToString(sum[:8]))
}
