package tagent

import (
	"fmt"
	"log"
	"os"
	"sync"
	"sync/atomic"
)

// hotOrgCoordinator implements lazy org-layer hot reload (design D1/D2/D4):
// on each Get it stats the config file; on mtime change it reloads + refingerprints
// and, only if the fingerprint actually changed, builds a new org snapshot and
// swaps it in atomically. Parse/build failures keep the previous snapshot
// serving (fail-closed) with a high-visibility degradation log + counter.
type hotOrgCoordinator struct {
	configPath string

	mu           sync.Mutex // serializes reload attempts (single-flight)
	mtime        int64      // last seen config mtime (unix nanos); -1 = unknown
	degradations uint64     // degradation counter (atomic); named degradations below
	fingerprint  string     // fingerprint of the ACTIVE snapshot

	active atomic.Value // *orgSnapshot — read path is lock-free
}

// newHotOrgCoordinator builds the coordinator around an initial snapshot
// produced by the normal startup path (behavior-equivalent: first load is a
// fingerprint miss → build).
func newHotOrgCoordinator(configPath string, snap *orgSnapshot) *hotOrgCoordinator {
	c := &hotOrgCoordinator{configPath: configPath, mtime: -1}
	if info, err := os.Stat(configPath); err == nil {
		c.mtime = info.ModTime().UnixNano()
	}
	c.active.Store(snap)
	c.fingerprint = snap.fingerprint
	return c
}

// Get returns the active snapshot, hot-reloading first if the config file
// changed. Read path: one atomic load; reload path: single-flight under mu.
func (c *hotOrgCoordinator) Get(reload func() (*orgSnapshot, string, error)) *orgSnapshot {
	if c.configPath != "" {
		if info, err := os.Stat(c.configPath); err == nil {
			mt := info.ModTime().UnixNano()
			if mt != atomic.LoadInt64(&c.mtime) {
				c.mu.Lock()
				// Re-check under lock: another goroutine may have reloaded.
				if info2, err2 := os.Stat(c.configPath); err2 == nil && info2.ModTime().UnixNano() != atomic.LoadInt64(&c.mtime) {
					atomic.StoreInt64(&c.mtime, info2.ModTime().UnixNano())
					newSnap, newFP, err := reload()
					switch {
					case err != nil:
						c.countDegradation(err) // D4: keep serving old snapshot
					case newFP == c.fingerprint:
						// mtime churn with identical org semantics — no rebuild.
						log.Printf("[org-hotreload] fingerprint unchanged, keeping snapshot")
					default:
						c.fingerprint = newFP
						c.active.Store(newSnap)
						log.Printf("[org-hotreload] org snapshot replaced (fp %s.. -> %s..)", short(c.fingerprint), short(newFP))
					}
				}
				c.mu.Unlock()
			}
		}
	}
	return c.active.Load().(*orgSnapshot)
}

// countDegradation records a failed reload (D4 observability).
func (c *hotOrgCoordinator) countDegradation(err error) {
	atomic.AddUint64(&c.degradations, 1)
	log.Printf("[ERROR] [org-hotreload] reload FAILED (%d consecutive-ish) — serving PREVIOUS config: %v", atomic.LoadUint64(&c.degradations), err)
}

// Degradations returns the number of failed reload attempts.
func (c *hotOrgCoordinator) Degradations() uint64 { return atomic.LoadUint64(&c.degradations) }

func short(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

// ensure compile-time hint that reload closures must return (snapshot, fp, err).
var _ = func() (o *orgSnapshot, fp string, err error) { _, _, _ = o, fp, err; return nil, "", fmt.Errorf("") }
