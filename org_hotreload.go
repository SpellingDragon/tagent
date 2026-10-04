package tagent

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SpellingDragon/tagent/agent"
)

// appliedAgent 是已提交应用记录里的单 agent 条目：本代对它生效的完整热参，加
// 本代是否仍路由它。排水中的条目携带该 agent 最后一次生效值（被移除的 owner 绝不
// 被重新参数化成默认值），其作用是让读者直接看到"存在但在排水"，无需第二次查询。
//
// 契约: docs/wiki/platform/org-hot-reload.md#apply-record
type appliedAgent struct {
	Name     string
	Hot      agent.OrgHotParams
	Draining bool
}

// orgGeneration is one published organization version: the effective content
// fingerprint, the full config it was built from, the monotonic sequence
// number assigned at publish time, and the per-agent applied record —
// the ONE committed source owner hot-param reads resolve against.
type orgGeneration struct {
	seq         int
	fingerprint string
	cfg         *Config
	applied     []appliedAgent
}

// orgCoordinator 是一个常驻入口 agent 的**单一**组织版本簿记者，取代把状态散在
// 重载闭包里的做法。它拥有：有效内容指纹、发布序号、ring-2 上一代配置、最近一次
// 被拒候选、本轮逐 agent 回执与两个时间戳，以及应用记录的无锁读面。各量的含义、
// 前进时机与身份语义（序号与内容指纹之别）均以文档为唯一真源。
//
// 它不构造也不换入 runner：候选构造与发布由重载入口接入。
//
// 契约: docs/wiki/platform/org-hot-reload.md#generations
type orgCoordinator struct {
	mu      sync.Mutex
	current *orgGeneration
	prev    *orgGeneration
	// lastFail 最近一次被拒绝的候选（nil = 自上次成功发布以来无新失败）
	lastFail *OrgFailure
	// lastOKAt 最近一次成功应用的时间（含 numeric-only，D9 lastAppliedAt）
	lastOKAt time.Time
	// lastPubAt 最近一次结构发布／回滚的时间（D9 lastPublishedAt）
	lastPubAt time.Time
	// lastApply 最近一轮逐 agent 回执（整块替换）
	lastApply []OrgAgentApply
	// candFP 本轮检查看到的 desired 指纹（解析/指纹失败时为空）
	candFP string
	// seq 结构代（generation）——仅结构发布/回滚前进
	seq int
	// revision 完整应用计数（含 numeric-only），仅观测/提交，不作第二路由源
	revision int
	// applySig 最近一次完整应用的热参数摘要，用于「语义完全相同不轮转」（D9）
	applySig string

	// appliedView is the LOCK-FREE read face of the committed application record
	// (S-E, design 「atomic.Pointer 无锁化留
	// S-E 与 compressor 侧同源做」). S-C's lock-held read assumed hot params are
	// consumed rarely; once the compressor resolves its numeric group from the
	// record at every boundary of every live CM, that assumption puts the commit
	// critical section on the compression read path — contention at best, and a
	// re-entrancy trap the moment any commit-time code reads an owner's hot view.
	// Rotated ONLY together with `current` at the three commit points below, always
	// as one whole immutable snapshot, so readers see a complete generation and
	// never a half-rotation.
	appliedView atomic.Pointer[[]appliedAgent]
}

// publishViewLocked hands the record's lock-free read face a private copy of the
// just-committed applied record. Called from within the commit critical section
// (callers hold c.mu), and the copy is what makes the store safe: the reload path
// reuses its own slice across commits, and readers must never observe a slice
// being rewritten under them.
func (c *orgCoordinator) publishViewLocked(applied []appliedAgent) {
	view := make([]appliedAgent, len(applied))
	copy(view, applied)
	c.appliedView.Store(&view)
}

func newOrgCoordinator() *orgCoordinator { return &orgCoordinator{} }

// init records the startup generation. The sequence stays 0 so the first swap
// publishes generation 1（与历史 execGen 日志语义一致）。
func (c *orgCoordinator) init(fp string, cfg *Config) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.current = &orgGeneration{seq: 0, fingerprint: fp, cfg: cfg}
	c.candFP = fp
	c.applySig = hotSignature(cfg)
}

// sameAsCurrent reports whether fp equals the effective fingerprint（组织内容
// 未变：数值热参分支）。无current时视同空指纹（启动指纹失败的既有语义）。
func (c *orgCoordinator) sameAsCurrent(fp string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.current == nil {
		return fp == ""
	}
	return c.current.fingerprint == fp
}

// sameFullAsCurrent reports whether a candidate is semantically identical to the
// effective generation on BOTH axes — same structural fingerprint AND same hot
// signature. Rollback uses it (not a fingerprint-only check) to decide whether
// there is anything to restore: a numeric-only application leaves the structure
// fingerprint equal but the five hot params different, so an fp-only short
// circuit would wrongly refuse to roll the numbers back.
func (c *orgCoordinator) sameFullAsCurrent(fp string, cfg *Config) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.current != nil && c.current.fingerprint == fp && c.applySig == hotSignature(cfg)
}

// swap records a successful whole-org (STRUCTURAL) replacement and returns
// (oldFP, newGen). The superseded current becomes the ring-2 rollback source.
// A structural publish advances the generation, the full-apply revision, and
// BOTH success timestamps (D9: lastAppliedAt ⊇ lastPublishedAt).
func (c *orgCoordinator) swap(fp string, cfg *Config, applied []appliedAgent) (string, *orgGeneration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	oldFP := ""
	if c.current != nil {
		oldFP = c.current.fingerprint
	}
	c.seq++
	c.revision++
	now := time.Now()
	gen := &orgGeneration{seq: c.seq, fingerprint: fp, cfg: cfg, applied: applied}
	c.prev = c.current
	c.current = gen
	c.publishViewLocked(applied)
	c.candFP = fp
	c.applySig = hotSignature(cfg)
	c.lastFail = nil
	c.lastOKAt = now
	c.lastPubAt = now
	return oldFP, gen
}

// recordHotApply records a SUCCESSFUL numeric-only full application (org
// structure unchanged). Per D9 (L-3): every successful apply — numeric-only
// included — rotates the coordinator's full effective config and its previous
// copy, so a later rollback can restore the pre-edit numeric values, and
// advances lastAppliedAt. It does NOT advance the structural generation (seq)
// nor lastPublishedAt (no structure moved); it only bumps the independent
// revision used for full-commit bookkeeping and observation (never a routing
// source). A semantically identical re-apply (same hot signature) does NOT
// rotate or advance anything and returns false, per 「语义完全相同的应用不轮转」;
// a real change rotates the ring, bumps revision, advances lastAppliedAt (never
// lastPublishedAt) and returns true.
func (c *orgCoordinator) recordHotApply(cfg *Config, applied []appliedAgent) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	sig := hotSignature(cfg)
	if sig == c.applySig {
		return false
	}
	if c.current != nil {
		c.prev = c.current
		c.current = &orgGeneration{seq: c.current.seq, fingerprint: c.current.fingerprint, cfg: cfg, applied: applied}
		c.publishViewLocked(applied)
	}
	c.applySig = sig
	c.revision++
	c.lastOKAt = time.Now()
	return true
}

// spawnerTTLSource binds an ActionTool's spawn-time default lifetime (the TTL a
// spawn gets when the model omits `ttl`) to an owner's LIVE hot view (, the
// spawner axis of 6.4): the committed application record is the authority, so a
// numeric-only rotation reaches every later spawn without anyone pushing a
// number into the tool. A nil owner or a record with no entry for it reads zero,
// which the tool resolves to its construction default.
func spawnerTTLSource(a *agent.TagentAgent) func() time.Duration {
	if a == nil {
		return nil
	}
	return func() time.Duration {
		hp, ok := a.HotSnapshot()
		if !ok {
			return 0
		}
		return hp.TaskDefaultTTL
	}
}

// currentHotFor reads ONE agent's applied hot bundle from the committed record
// : the read-side of the single application record. Draining entries carry
// the agent's LAST effective values (J8). ok=false when no record exists yet or
// the name is absent (standalone/never-routed) — callers fall back to their
// construction snapshot.
//
// S-E: LOCK-FREE. The compressor resolves its numeric group through
// this read at every consumption boundary of every live CM, so the record's read
// face must never take c.mu — a lock-held read would put the commit critical
// section on the compression path. It loads the atomic snapshot the commit points
// publish together with the generation, so it is both race-free and rotation-free.
func (c *orgCoordinator) currentHotFor(name string) (agent.OrgHotParams, bool) {
	if c == nil {
		return agent.OrgHotParams{}, false
	}
	view := c.appliedView.Load()
	if view == nil {
		return agent.OrgHotParams{}, false
	}
	for i := range *view {
		if (*view)[i].Name == name {
			return (*view)[i].Hot, true
		}
	}
	return agent.OrgHotParams{}, false
}

// rollbackSource returns the ring-2 generation, or nil when nothing to roll
// back to（首代未换过）。
func (c *orgCoordinator) rollbackSource() *orgGeneration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.prev
}

// recordRollback republishes the rolled-back generation as a NEW sequence,
// restoring BOTH structure and the five hot params (the caller rebuilds the
// executor AND re-applies the numeric bundle from `cfg`, which is the previous
// full effective config). Ring 保持不变（与历史行为一致：连续回滚仍面向同一
// prev）。A rollback is a structural publish: it advances generation, revision
// and lastPublishedAt (D9). Storing the restored `cfg` (not only when
// prev.fingerprint == fp) keeps the rollback ring's full-config truth exact even
// after a numeric-only application made the fingerprints diverge.
func (c *orgCoordinator) recordRollback(fp string, cfg *Config, applied []appliedAgent) *orgGeneration {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seq++
	c.revision++
	gen := &orgGeneration{seq: c.seq, fingerprint: fp, cfg: cfg, applied: applied}
	c.current = gen
	c.publishViewLocked(applied)
	c.candFP = fp
	c.applySig = hotSignature(cfg)
	c.lastFail = nil
	now := time.Now()
	c.lastOKAt = now
	c.lastPubAt = now
	return gen
}

// noteDesired 记录本轮检查看到的 desired 指纹，所以下一次 recordFailure 能说出
// “被拒的是哪一份”。由重载器在每次进入时置空、算出指纹后赋值。
func (c *orgCoordinator) noteDesired(fp string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.candFP = fp
}

// recordApply 保存最近一轮的逐 agent 应用回执（D9）。它不改变任何版本语义：
// 仅把“谁真的收到了新参数、谁被故意没碰”从只会流进日志的副产物变成可诊断形状。
func (c *orgCoordinator) recordApply(rs []OrgAgentApply) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastApply = append([]OrgAgentApply(nil), rs...)
}

// recordFailure stores the most recent rejected candidate reason（解析/指纹/
// 拓扑/构建失败）供诊断；不改变 effective，也不前进序号。
func (c *orgCoordinator) recordFailure(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastFail = &OrgFailure{
		Generation: c.seq,
		Desired:    short(c.candFP),
		Error:      err.Error(),
		At:         time.Now(),
	}
}

// lastFailure returns the most recent rejected candidate, or nil.
func (c *orgCoordinator) lastFailure() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lastFail == nil {
		return nil
	}
	return errors.New(c.lastFail.Error)
}

// status 给出有界代际诊断快照（D4）。它与 swap/record* 同锁，所以读者看到的总是
// “已发布代 + 它的时间 + 最后一次失败”的一致组合；无历史、无队列。
func (c *orgCoordinator) status() OrgStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := OrgStatus{Generation: int64(c.seq), Revision: int64(c.revision), LastApplied: c.lastOKAt, LastPublished: c.lastPubAt, Desired: short(c.candFP)}
	if c.current != nil {
		st.Fingerprint = short(c.current.fingerprint)
	}
	if c.lastFail != nil {
		fail := *c.lastFail
		st.LastFailure = &fail
	}
	st.Agents = append([]OrgAgentApply(nil), c.lastApply...)
	return st
}

// short truncates a fingerprint for compact logs.
func short(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

// hotSignature digests the five hot-applicable numeric params over the agent set
// (raw AgentConfig fields, entry included) to a stable hex string. It backs
// D9's 「语义完全相同的应用不轮转」 on the numeric-only path: an identical re-save
// must not rotate the rollback ring or advance revision/lastAppliedAt. This is
// pure change-detection bookkeeping — NOT a routing source and NOT the
// structural fingerprint (which deliberately excludes these fields, see
// agentSubset). An edge exists: two configs differing only in "unset vs
// explicit parsed default" hash apart here and would rotate — harmless, since
// the applied effective values are equal, only a redundant revision bump.
func hotSignature(cfg *Config) string {
	if cfg == nil {
		return ""
	}
	names := make([]string, 0, len(cfg.Agents))
	for n := range cfg.Agents {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString(cfg.Entry)
	b.WriteByte('#')
	for _, n := range names {
		ac := cfg.Agents[n]
		b.WriteString(n)
		b.WriteByte(':')
		b.WriteString(strconv.FormatFloat(ac.CompressThreshold, 'f', -1, 64))
		b.WriteByte('|')
		b.WriteString(strconv.Itoa(ac.MaxTokens))
		b.WriteByte('|')
		b.WriteString(strconv.Itoa(ac.KeepRecentTasks))
		b.WriteByte('|')
		b.WriteString(ac.TaskTerminalTTL)
		b.WriteByte('|')
		b.WriteString(ac.TaskDefaultTTL)
		b.WriteByte(';')
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}
