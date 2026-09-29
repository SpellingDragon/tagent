package governance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// BudgetConfig 配置预算窗口与各级上限。
type BudgetConfig struct {
	Window        time.Duration
	BucketCount   int
	MaxHighRisk   int
	MaxMediumRisk int
}

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

// ErrBudgetExhausted 表示窗口内该风险级别预算耗尽。
var ErrBudgetExhausted = budgetExhaustedError{}

type budgetExhaustedError struct{}

func (budgetExhaustedError) Error() string {
	return "governance: operation budget exhausted for this window"
}

// BudgetManager 是滑动窗口预算闸（并发安全，持久化 epoch 防重启刷预算）。
type BudgetManager struct {
	cfg  BudgetConfig
	path string

	mu      sync.Mutex
	buckets map[RiskLevel][]int64
	epoch   int64
}

// NewBudgetManager 构建预算闸。dir 非空时持久化到 <dir>/budget.json 并恢复 epoch。
// dir 为空即纯内存、绝不落盘。调用方要把 dir 原样交下来由本函数判断，不能先自行拼接目录片段：
// 空串参与拼接会得到相对路径（如 budget/name），预算被意外写进进程当前目录，违反「空 dir＝纯内存」。
func NewBudgetManager(cfg BudgetConfig, dir string) *BudgetManager {
	cfg = cfg.withDefaults()
	b := &BudgetManager{
		cfg:     cfg,
		buckets: make(map[RiskLevel][]int64),
		epoch:   time.Now().Unix(),
	}
	if dir != "" {
		b.path = filepath.Join(dir, "budget.json")
		b.load()
	}
	return b
}

// Admit 判定并（若放行）计入一次该级别操作。critical/low 直接放行（不占预算）；
// high/medium 超限返回 ErrBudgetExhausted。
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

// Usage 返回当前窗口各级别已用预算（诊断/可观测）。
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

// roll 按墙钟推进窗口，淘汰过期桶（epoch 持久化，重启不重置）。调用方持锁。
func (b *BudgetManager) roll() {
	now := time.Now().Unix()
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

// bucketSeconds 返回每桶的墙钟秒数（窗口 / 桶数，至少 1s）。
func (b *BudgetManager) bucketSeconds() int64 {
	s := int64((b.cfg.Window / time.Duration(b.cfg.BucketCount)).Seconds())
	if s <= 0 {
		s = 1
	}
	return s
}

func (b *BudgetManager) ensureBuckets() {
	for _, level := range []RiskLevel{RiskHigh, RiskMedium} {
		if len(b.buckets[level]) != b.cfg.BucketCount {
			bs := make([]int64, b.cfg.BucketCount)
			copy(bs, b.buckets[level])
			b.buckets[level] = bs
		}
	}
}

func (b *BudgetManager) currentBucket() int {
	bucketSecs := b.bucketSeconds()
	idx := int((time.Now().Unix() - b.epoch) / bucketSecs)
	if idx < 0 {
		idx = 0
	}
	if idx >= b.cfg.BucketCount {
		idx = b.cfg.BucketCount - 1
	}
	return idx
}

type budgetSnapshot struct {
	Epoch   int64              `json:"epoch"`
	Buckets map[string][]int64 `json:"buckets"`
}

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
