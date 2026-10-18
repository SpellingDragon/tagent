package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// BudgetConfig configures the budget window and the per-grade limits one budget
// manager counts over. The defaults are the ones the governance semantics fix:
// a one hour window cut into six buckets, admitting twenty high-grade and two
// hundred medium-grade operations per window.
//
// 契约: docs/wiki/agent/governance-enforcement.md#budget-epoch
type BudgetConfig struct {
	// Window is the width of the sliding window the budget is counted over.
	Window time.Duration
	// BucketCount is how many buckets the window is cut into.
	BucketCount int
	// MaxHighRisk is how many high-grade operations one window admits.
	MaxHighRisk int
	// MaxMediumRisk is how many medium-grade operations one window admits.
	MaxMediumRisk int
}

// withDefaults fills every unset knob with its documented default.
func (c BudgetConfig) withDefaults() BudgetConfig {
	if c.Window <= 0 {
		c.Window = time.Hour
	}
	if c.BucketCount <= 0 {
		c.BucketCount = 6
	}
	if c.MaxHighRisk <= 0 {
		c.MaxHighRisk = 20
	}
	if c.MaxMediumRisk <= 0 {
		c.MaxMediumRisk = 200
	}
	return c
}

// ErrBudgetExhausted reports a window whose budget for that risk grade is used
// up. A call beside it never runs: an exhausted budget refuses it whatever the
// enforcement grade says.
var ErrBudgetExhausted = budgetExhaustedError{}

type budgetExhaustedError struct{}

func (budgetExhaustedError) Error() string {
	return "governance: operation budget exhausted for this window"
}

// BudgetOption configures a budget manager at construction.
type BudgetOption func(*BudgetManager)

// WithBudgetClock injects the clock the manager reads its window position
// from, so a test drives the sliding window deterministically instead of
// waiting on wall time.
func WithBudgetClock(now func() time.Time) BudgetOption {
	return func(b *BudgetManager) { b.now = now }
}

// BudgetManager is the sliding window budget gate: it counts high-grade and
// medium-grade operations over rolling buckets and refuses the call that finds
// its window spent. The critical and low grades are never counted — the
// critical grade is held by the approval channel and a read-only call needs no
// budget.
//
// The manager is concurrent safe, and its epoch is persisted with its buckets
// so a restart resumes the window instead of refreshing it: a snapshot that is
// missing, unparseable or carries a zero epoch never overwrites the current
// epoch, because letting a missing value zero the epoch would read accumulated
// consumption as a fresh start and leave the gate worthless. A manager built
// with an empty dir holds its state in memory only and never touches disk.
//
// 契约: docs/wiki/agent/governance-enforcement.md#budget-epoch
type BudgetManager struct {
	cfg  BudgetConfig
	path string
	now  func() time.Time

	mu      sync.Mutex
	epoch   int64
	buckets map[RiskLevel][]int64
}

// NewBudgetManager builds one budget gate over the given configuration. A
// non-empty dir keeps the snapshot under <dir>/budget.json and resumes the
// epoch and the buckets it finds there; an empty dir keeps the manager in
// memory only. The dir is judged here exactly as it arrives — the caller must
// not pre-join directory fragments, because an empty piece joined in yields a
// relative path and the budget would be written into the process directory.
func NewBudgetManager(cfg BudgetConfig, dir string, opts ...BudgetOption) *BudgetManager {
	cfg = cfg.withDefaults()
	b := &BudgetManager{
		cfg:     cfg,
		now:     time.Now,
		buckets: make(map[RiskLevel][]int64),
		epoch:   time.Now().Unix(),
	}
	for _, opt := range opts {
		opt(b)
	}
	b.epoch = b.now().Unix()
	if dir != "" {
		b.path = filepath.Join(dir, "budget.json")
		b.load()
	}
	return b
}

// Admit rules on one operation of the given grade and, when it is admitted,
// counts it into the window. The critical and low grades pass uncounted; a
// high-grade or medium-grade call that finds its window spent is refused with
// ErrBudgetExhausted.
func (b *BudgetManager) Admit(level RiskLevel) error {
	if level != RiskHigh && level != RiskMedium {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.roll()

	limit := b.cfg.MaxMediumRisk
	if level == RiskHigh {
		limit = b.cfg.MaxHighRisk
	}
	buckets := b.buckets[level]
	var total int64
	for _, c := range buckets {
		total += c
	}
	if total >= int64(limit) {
		return ErrBudgetExhausted
	}
	idx := b.currentBucket()
	buckets[idx]++
	b.buckets[level] = buckets
	b.persistLocked()
	return nil
}

// Usage reports how much of the window's budget the given grade has spent, for
// diagnostics and observability.
func (b *BudgetManager) Usage(level RiskLevel) int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.roll()
	var total int64
	for _, c := range b.buckets[level] {
		total += c
	}
	return total
}

// roll advances the window along the wall clock and drops the buckets that fell
// out of it. The epoch persists with the buckets, so a restart resumes the
// window it left instead of starting a new one. The caller holds the lock.
func (b *BudgetManager) roll() {
	now := b.now().Unix()
	bucketSecs := b.bucketSeconds()
	elapsed := now - b.epoch
	if elapsed < 0 {
		b.epoch = now
		elapsed = 0
	}
	advanced := int(elapsed / bucketSecs)
	if advanced == 0 {
		b.ensureBuckets()
		return
	}
	if advanced >= b.cfg.BucketCount {
		b.epoch = now
		b.buckets = make(map[RiskLevel][]int64)
		b.ensureBuckets()
		return
	}
	b.epoch += int64(advanced) * bucketSecs
	for _, level := range []RiskLevel{RiskHigh, RiskMedium} {
		bs := b.buckets[level]
		if len(bs) == 0 {
			continue
		}
		shifted := make([]int64, b.cfg.BucketCount)
		copy(shifted, bs[advanced:])
		b.buckets[level] = shifted
	}
	b.ensureBuckets()
}

// bucketSeconds reports the wall width of one bucket: the window over the
// bucket count, never shorter than one second.
func (b *BudgetManager) bucketSeconds() int64 {
	s := int64((b.cfg.Window / time.Duration(b.cfg.BucketCount)).Seconds())
	if s <= 0 {
		s = 1
	}
	return s
}

// currentBucket reports the bucket slot the current moment lands in.
func (b *BudgetManager) currentBucket() int {
	bucketSecs := b.bucketSeconds()
	idx := int((b.now().Unix() - b.epoch) / bucketSecs)
	if idx < 0 {
		idx = 0
	}
	if idx >= b.cfg.BucketCount {
		idx = b.cfg.BucketCount - 1
	}
	return idx
}

// ensureBuckets sizes every counted grade's bucket ring to the configured
// count, keeping whatever is already counted.
func (b *BudgetManager) ensureBuckets() {
	for _, level := range []RiskLevel{RiskHigh, RiskMedium} {
		if len(b.buckets[level]) != b.cfg.BucketCount {
			bs := make([]int64, b.cfg.BucketCount)
			copy(bs, b.buckets[level])
			b.buckets[level] = bs
		}
	}
}

// budgetSnapshot is the persisted form of one budget window: the epoch the
// buckets are counted from, and the buckets themselves. The epoch travels with
// the buckets on purpose — the two are only meaningful together.
type budgetSnapshot struct {
	Epoch   int64              `json:"epoch"`
	Buckets map[string][]int64 `json:"buckets"`
}

// persistLocked writes the window behind an atomic rename, so a reader never
// observes half a snapshot. The caller holds the lock.
func (b *BudgetManager) persistLocked() {
	if b.path == "" {
		return
	}
	snap := budgetSnapshot{Epoch: b.epoch, Buckets: map[string][]int64{
		"high": b.buckets[RiskHigh], "medium": b.buckets[RiskMedium],
	}}
	raw, err := json.Marshal(snap)
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(b.path), 0o755)
	tmp := b.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err == nil {
		_ = os.Rename(tmp, b.path)
	}
}

// load resumes the window from the persisted snapshot. A snapshot that is
// missing, unparseable or carries a zero epoch never overwrites the current
// epoch or the current buckets: a missing value must not zero the epoch, or
// accumulated consumption would read as a fresh start and a restart would
// refresh the very budget the gate exists to bound.
func (b *BudgetManager) load() {
	raw, err := os.ReadFile(b.path)
	if err != nil {
		return
	}
	var snap budgetSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil || snap.Epoch == 0 {
		return
	}
	b.epoch = snap.Epoch
	if h, ok := snap.Buckets["high"]; ok {
		b.buckets[RiskHigh] = h
	}
	if m, ok := snap.Buckets["medium"]; ok {
		b.buckets[RiskMedium] = m
	}
	b.ensureBuckets()
}
