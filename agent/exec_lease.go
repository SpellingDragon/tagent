package agent

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/runner"
)

// LeaseKind names the usage a reference stands for, so diagnostics can report
// business turns, sub-calls and background executions separately (「后台／流／
// owner 引用分报」) instead of one blurred total.
type LeaseKind int

const (
	// LeaseTurn is a business turn (persistent-loop turn or an external Run that
	// inherits nothing and therefore acquired the effective generation).
	LeaseTurn LeaseKind = iota
	// LeaseSubCall is a derived invocation: synchronous / nested delegation and
	// its transport retries.
	LeaseSubCall
	// LeaseBackground is a post-ACK background execution whose producer outlives
	// the tool call that spawned it.
	LeaseBackground
)

// String is the diagnostic label; it must stay stable (payload key values).
func (k LeaseKind) String() string {
	switch k {
	case LeaseTurn:
		return "turn"
	case LeaseSubCall:
		return "subcall"
	case LeaseBackground:
		return "background"
	default:
		return "unknown"
	}
}

// leaseKinds is the fixed reporting order (diagnostics iterate it; adding a kind
// here is what makes it visible).
var leaseKinds = []LeaseKind{LeaseTurn, LeaseSubCall, LeaseBackground}

// leaseKindNoop is the sentinel for "evaluate the reclaim gate, drop nothing".
// release() only decrements a non-negative kind, so this is inert on the counts.
const leaseKindNoop = LeaseKind(-1)

// execBinding is one execution generation's identity plus its reference set.
// Locking discipline: executorMu may be held while taking mu (acquire reads the
// active binding under the executor read lock); mu and cm.retireMu are leaves and
// never take each other, and the runner Close always happens with neither held.
type execBinding struct {
	id      int64
	run     runner.Runner
	owner   string
	cm      *ContextManager
	created time.Time

	// face is the execution configuration this generation was published WITH.
	//  needs it: a re-entry (Resume/Relaunch of an existing task) initiated
	// by a call that still holds THIS generation must resolve its delegation
	// target against the face in force when that call started — reading the
	// current effective face instead would silently re-route a legal G1 call to
	// G2's targets (spec resident-continuity「重试也不改路由」, subagent-turn-execution
	// 「仍持 G1 租约的发起者不因 G2 删除目标而丢失其合法 G1 绑定」).
	// It dies with the binding: a retired generation is forgotten as soon as its
	// own references drain, so no historical face is retained indefinitely.
	face ContextManagerConfig

	// holds/heldBy carry the generation-level declaration right (3.2 trunk, D8 as
	// precision-approved round 90): `holds` lists the CHILD bindings this
	// generation's face declares; `heldBy` counts the parent generations still
	// alive that declare THIS binding. A declared generation is not reclaimed
	// while any declarer lives (reclaim = retired ∧ refs==0 ∧ heldBy==0), which
	// is what lets a pinned G1 parent reach ITS generation's child after a later
	// publish replaced it. Deliberately NOT part of refs: holding a declaration
	// is not work, so it never enters the obligation axes (J7/J8) and never
	// blocks an owner's idle retirement on its own.
	holds  []*execBinding
	heldBy int

	// runCfg 是本代装配好的 TagentConfig：在装配期（STAGING）设置，代际激活后不可变。
	// 声明式调用经自己的租约读它——它是逐代的执行描述，取代属主构造期的配置。
	// 惰性物化的代际（冷启动、手搭的测试 cm）没有它：此时沿用构造期配置源。
	runCfg *TagentConfig

	mu        sync.Mutex
	refs      map[LeaseKind]int
	total     int
	retired   bool
	retiredAt time.Time
	closed    bool
}

func newExecBinding(cm *ContextManager, id int64, r runner.Runner, face ContextManagerConfig) *execBinding {
	owner := ""
	if cm != nil {
		owner = cm.name
	}
	return &execBinding{
		id:      id,
		run:     r,
		owner:   owner,
		cm:      cm,
		created: time.Now(),
		face:    face,
		refs:    map[LeaseKind]int{},
	}
}

// acquire 取一份指定种类的引用，返回其幂等的释放句柄。它是"继承式"取引用：
// 在已退役但仍被持有的代上合法（派生子调用继承发起方的那一次钉住）。
// 要启动新工作必须用 tryAcquireActive——它是唯一守住「已退役的代不接受新引用」的形式。
func (b *execBinding) acquire(kind LeaseKind) *ExecLease {
	b.mu.Lock()
	b.refs[kind]++
	b.total++
	b.mu.Unlock()
	return &ExecLease{b: b, kind: kind}
}

// tryAcquireActive takes a reference for NEW work only while this generation is
// still one that work may start on. It closes the window that the read-then-pin
// split in ContextManager.AcquireLease would otherwise leave: activeBinding()
// returns the binding with executorMu already released, and PublishExecutor calls
// retireBinding on the superseded generation after ITS unlock, so a concurrent
// acquire can land on a binding that was retired — and, having been idle, already
// had its runner closed — in between. Retrying on the next read is how the caller
// recovers (ok=false); the alternative, running a turn on a closed executor, is
// the defect this guard exists to prevent.
//
// Whichever order the two parties take, the outcome is safe: if the acquire wins,
// the retire's reclaim sees total>0 and does not close; if the retire wins, this
// refuses and the caller pins the generation actually in force.
func (b *execBinding) tryAcquireActive(kind LeaseKind) (*ExecLease, bool) {
	b.mu.Lock()
	if b.retired || b.closed {
		b.mu.Unlock()
		return nil, false
	}
	b.refs[kind]++
	b.total++
	b.mu.Unlock()
	return &ExecLease{b: b, kind: kind}, true
}

// retire marks the generation superseded: from here on it may still finish what
// already references it, but no NEW work starts on it (enforced by
// tryAcquireActive, not merely by convention).
func (b *execBinding) retire() {
	b.mu.Lock()
	if !b.retired {
		b.retired = true
		b.retiredAt = time.Now()
	}
	b.mu.Unlock()
}

// release drops one reference and, when the binding has become retired and
// unreferenced, closes its runner exactly once. Closing happens with no lock
// held, so a slow upstream Close never blocks another generation.
func (b *execBinding) release(kind LeaseKind) {
	b.mu.Lock()
	if kind >= 0 {
		if b.refs[kind] > 0 {
			b.refs[kind]--
			if b.refs[kind] == 0 {
				delete(b.refs, kind)
			}
			b.total--
		}
	}
	doClose := b.reclaimableLocked()
	drained := b.total == 0
	if doClose {
		b.closed = true
	}
	b.mu.Unlock()

	if doClose {
		b.finishReclaim()
	}
	if drained {
		b.cm.pokeRetirementDrain()
	}
}

// reclaimableLocked is the one spelling of "this generation may be reclaimed
// now": superseded, no work reference, and no live declarer holding it as the
// target ITS face declared (3.2 trunk — the heldBy term is what keeps a pinned
// parent's deferred delegation reachable after a newer generation replaced it).
// Caller holds b.mu.
func (b *execBinding) reclaimableLocked() bool {
	return b.retired && b.total == 0 && b.heldBy == 0 && !b.closed
}

// finishReclaim performs the terminal cleanup of one reclaimed generation:
// close the runner (idempotent upstream), drop it from the unconverged list so
// the bookkeeping tracks live debt, and release every declaration hold
// it recorded — a hold dies with its declarer, cascading the same check down
// the call graph (acyclic by construction: the DFS refuses declaration cycles).
func (b *execBinding) finishReclaim() {
	if b.run != nil {
		_ = b.run.Close()
	}
	b.cm.forgetBinding(b)
	for _, child := range b.heldBindings() {
		child.dropDeclaredHold()
	}
}

// heldBindings snapshots the declaration holds this generation recorded. The
// copy is taken under the lock so the cascade below runs lock-free (hold edges
// only ever flow parent→child along the call graph, so no reverse acquisition
// order exists and the snapshot cannot deadlock with a concurrent release).
func (b *execBinding) heldBindings() []*execBinding {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.holds) == 0 {
		return nil
	}
	out := make([]*execBinding, len(b.holds))
	copy(out, b.holds)
	return out
}

// addDeclaredHold records that this generation's face declares the child
// generation `child` (wired at publish time by WireOrgGeneration — never on a
// hot path). The hold is symmetric bookkeeping: it appears in this binding's
// `holds` and in the child's `heldBy`.
func (b *execBinding) addDeclaredHold(child *execBinding) {
	if b == nil || child == nil || b == child {
		return
	}
	b.mu.Lock()
	b.holds = append(b.holds, child)
	b.mu.Unlock()
	child.mu.Lock()
	child.heldBy++
	child.mu.Unlock()
}

// dropDeclaredHold releases ONE declarer of this binding (its parent
// generation was reclaimed). Running the reclaim check here is what makes the
// cascade terminate at the right depth: a binding whose last declarer just
// died AND whose own references have drained is reclaimed exactly here.
func (b *execBinding) dropDeclaredHold() {
	b.mu.Lock()
	if b.heldBy > 0 {
		b.heldBy--
	}
	reclaim := b.reclaimableLocked()
	if reclaim {
		b.closed = true
	}
	b.mu.Unlock()
	if reclaim {
		b.finishReclaim()
	}
}

// acquireDeclared takes a call reference on the generation this wrapper's face
// DECLARED (3.2 trunk). Unlike tryAcquireActive it is legal on a
// retired-but-held generation — that is precisely the deferred-delegation right
// D8 grants a pinned caller — and refuses only once the generation has actually
// been reclaimed (ErrExecClosed), which is the honest failure when no declarer
// kept it alive.
func (b *execBinding) acquireDeclared(kind LeaseKind) (*ExecLease, error) {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil, ErrExecClosed
	}
	b.refs[kind]++
	b.total++
	b.mu.Unlock()
	return &ExecLease{b: b, kind: kind}, nil
}

// ErrExecUnconverged is what a bounded Close returns when executions are still
// outstanding. It is never a clean close: the generations whose producers failed
// to confirm a stop stay EXPLICITLY HELD (spec runtime-resource-ownership
// 「公开关闭调用有界返回，未确认停止者继续显式持有并执行既有 poisoned 保护，
// 不无限等待或强关」), and their holders keep the shared resources they ride on —
// releasing a store lease under a possibly-live writer is the failure mode this
// error exists to prevent.
var ErrExecUnconverged = errors.New("execution generations unconverged: held, not force-closed")

// ErrExecClosed is what the execution gate returns for NEW work arriving on a
// generation whose terminal Close has already CONVERGED — runner closed, drain
// reported clean, nothing left to wait for. It is deliberately distinct from the
// mid-close case: while a close is still draining, AcquireLease DOES register the
// reference so the bounded drain surfaces ErrExecUnconverged instead of pretending
// the shutdown was clean. After convergence, handing out the closed executor
// would be precisely the failure tryAcquireActive exists to prevent (「running a turn
// on a closed executor」), so new work is refused instead — 's
// 「关闭后的 Run/Inject/Acquire 真正再进一次且被拒」.
var ErrExecClosed = errors.New("execution generation already closed: new work refused")

// isClosed reports whether this generation's runner has already been closed by a
// converged reclaim (as opposed to merely retired, still draining).
func (b *execBinding) isClosed() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closed
}

// snapshot returns the generation's observable reference state (diagnostics only;
// no execution path reads it — D9 forbids a second truth source).
func (b *execBinding) snapshot() GenerationRefs {
	b.mu.Lock()
	defer b.mu.Unlock()
	g := GenerationRefs{
		Generation: b.id,
		Owner:      b.owner,
		Retired:    b.retired,
		Closed:     b.closed,
		Age:        time.Since(b.created),
		Refs:       map[string]int{},
	}
	for _, k := range leaseKinds {
		if n := b.refs[k]; n > 0 {
			g.Refs[k.String()] = n
		}
	}
	g.Total = b.total
	return g
}

// subagentWrapper resolves a delegation target on THIS generation's own face.
// Sharing one scan (`subagentWrapperIn`) with ContextManager.SubagentWrapper is
// what keeps the effective face and a pinned generation's face from being two
// different routing truths.
func (b *execBinding) subagentWrapper(name string) *AgentToolWrapper {
	if b == nil {
		return nil
	}
	return subagentWrapperIn(b.face.Tools, name)
}

// holdsUsage reports whether this generation still justifies keeping an owner it
// declares callable: D8 counts the published slot itself (「发布槽…在即保有」) and,
// once retired, any execution/background reference still pinning it. A retired
// generation with zero references is on its way out and holds nothing.
func (b *execBinding) holdsUsage() bool {
	if b == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return !b.retired || b.total > 0
}

// BindingHolders counts the live execution generations across `agents` whose OWN
// published face still declares a callable wrapper for `owner` — the usage right
// of /D8. It is deliberately DERIVED rather than registered: the binding's
// face is already the single routing truth, so there is no second table to
// drift and no parallel notion of who may call whom; the caller supplies only the
// roster (who exists), which is the composition root's own fact, never the
// retirement decision.
//
// This is what protects a deferred delegation: a version that accepted a request
// while B was routed may still legitimately call B after a newer generation
// removed the route, so B must not be retired while such a generation lives.
// It grants no execution right of its own — holding a usage right does not create
// work in B, and it is not a second task domain (J7).
func BindingHolders(owner string, agents []*TagentAgent) int {
	holders := 0
	for _, a := range agents {
		if a == nil || a.name == owner {
			continue
		}
		cm := a.ContextManager()
		if cm == nil {
			continue
		}
		for _, b := range cm.allBindings() {
			if b.holdsUsage() && b.subagentWrapper(owner) != nil {
				holders++
			}
		}
	}
	return holders
}

// WireOrgGeneration performs the generation-level wiring of ONE org publish
// (3.2 trunk, D8 as precision-approved round 90). It must run after every owner
// of the publish has STAGED its next generation and before ANY of them is
// activated, so no execution path can observe a half-wired generation:
//
// - INCOMING: each staged face's wrappers are stamped with the STAGED child
// binding they declare, and the declaring generation records a hold on it.
// A call through that wrapper therefore resolves the child through the
// declaring generation's own execution view — never the child's "current"
// face, never a captured instance.
//
// - OUTGOING (retroactive): the previous generations' faces were wired when
// THEY were staged — except a cold-start owner whose binding was created
// lazily and never wired. Those wrappers are stamped against the
// still-active child bindings now, with the same holds, so an in-flight
// caller on the outgoing generation keeps reaching ITS generation's
// targets after this publish retires them. Stamps are idempotent: a
// wrapper already wired by an earlier publish keeps its (still correct)
// target.
//
// The holds never enter the obligation axes (J7/J8); they only gate the
// binding-level reclaim (retired ∧ refs==0 ∧ heldBy==0).
func WireOrgGeneration(owners map[string]*ContextManager, staged map[string]*StagedGeneration) {
	for name, s := range staged {
		if s == nil {
			continue
		}
		for _, w := range collectAgentToolWrappers(s.face.Tools) {
			target := w.DeclaredAgentName()
			if target == "" || target == name {
				continue
			}
			child, ok := staged[target]
			if !ok || child == nil {
				continue
			}
			w.setDeclared(child.binding)
			s.binding.addDeclaredHold(child.binding)
		}
	}
	for name, cm := range owners {
		if cm == nil {
			continue
		}
		b := cm.activeBinding()
		if b == nil {
			continue
		}
		for _, w := range collectAgentToolWrappers(b.face.Tools) {
			if w.declaredSet() {
				continue
			}
			target := w.DeclaredAgentName()
			if target == "" || target == name {
				continue
			}
			childCM, ok := owners[target]
			if !ok || childCM == nil {
				continue
			}
			cb := childCM.activeBinding()
			if cb == nil {
				continue
			}
			w.setDeclared(cb)
			b.addDeclaredHold(cb)
		}
	}
}

// ExecLease is one outstanding reference on one generation. The zero value and a
// nil lease are inert, so a derived path that found no parent lease does not
// have to branch.
type ExecLease struct {
	b       *execBinding
	kind    LeaseKind
	once    sync.Once
	closed  atomic.Bool
	refused error
}

// Err reports why this lease carries no execution authority. It is nil for a live
// reference; ErrExecClosed means the generation it was asked for had already
// converged shut, so the holder must refuse the work rather than run on it.
func (l *ExecLease) Err() error {
	if l == nil {
		return nil
	}
	return l.refused
}

// SubagentWrapper resolves a delegation target against the face of the generation
// this lease pins — the version the holder's work was selected on. 's re-entry
// rule uses it so an initiator keeps ITS OWN generation's targets instead of
// whatever is published by then.
func (l *ExecLease) SubagentWrapper(name string) *AgentToolWrapper {
	if l == nil || l.refused != nil {
		return nil
	}
	return l.b.subagentWrapper(name)
}

// Runner is the executor this lease pins; the holder runs its turn on THIS
// instance for its whole life (drain-free at turn granularity).
func (l *ExecLease) Runner() runner.Runner {
	if l == nil || l.refused != nil {
		return nil
	}
	return l.b.run
}

// Generation identifies which published version this reference belongs to.
func (l *ExecLease) Generation() int64 {
	if l == nil {
		return 0
	}
	return l.b.id
}

// belongsToOwnerOf reports whether this lease pins a generation OF the given
// context manager — i.e. the wrapper that armed it resolved the DECLARED target
// on this very owner (3.2 trunk), as opposed to a caller's lease inherited
// through the tool-execution context.
func (l *ExecLease) belongsToOwnerOf(cm *ContextManager) bool {
	if l == nil || l.b == nil || cm == nil {
		return false
	}
	return l.b.cm == cm
}

// declaredRunConfig 返回该租约所钉住代际的装配配置；代际未经装配期物化（惰性或手搭）
// 时返回 nil，调用方此时沿用构造期配置源。
func (l *ExecLease) declaredRunConfig() *TagentConfig {
	if l == nil || l.b == nil {
		return nil
	}
	return l.b.runCfg
}

// Kind is what this reference stands for (turn / subcall / background).
func (l *ExecLease) Kind() LeaseKind {
	if l == nil {
		return LeaseTurn
	}
	return l.kind
}

// Derive takes an additional reference of kind on the SAME generation and
// returns its own idempotent release handle. Call it BEFORE the derived work
// starts (D6「派生前获取」), so the generation cannot be reclaimed in the gap
// between handing work over and it actually running.
func (l *ExecLease) Derive(kind LeaseKind) *ExecLease {
	if l == nil {
		return nil
	}
	if l.refused != nil {
		return l
	}
	return l.b.acquire(kind)
}

// WithContext returns ctx carrying this lease, so calls made inside it inherit
// the binding instead of re-reading whatever is published now.
func (l *ExecLease) WithContext(ctx context.Context) context.Context {
	if l == nil || ctx == nil {
		return ctx
	}
	return context.WithValue(ctx, execLeaseCtxKey{}, l)
}

// Release drops the reference. Idempotent: the first call wins, so a turn that
// both returns normally and hits a cleanup defer cannot double-count (
// 「全路径恰一次」).
func (l *ExecLease) Release() {
	if l == nil {
		return
	}
	l.once.Do(func() {
		l.closed.Store(true)
		if l.refused != nil {
			return
		}
		l.b.release(l.kind)
	})
}

type execLeaseCtxKey struct{}

// execLeaseFromContext returns the lease the enclosing call is running under.
func execLeaseFromContext(ctx context.Context) (*ExecLease, bool) {
	if ctx == nil {
		return nil, false
	}
	l, ok := ctx.Value(execLeaseCtxKey{}).(*ExecLease)
	return l, ok && l != nil
}

// GenerationRefs is one generation's diagnostic row.
type GenerationRefs struct {
	Generation int64          `json:"generation"`
	Owner      string         `json:"owner"`
	Retired    bool           `json:"retired"`
	Closed     bool           `json:"closed"`
	Age        time.Duration  `json:"ageNanos"`
	Total      int            `json:"totalRefs"`
	Refs       map[string]int `json:"refs"`
}

// UnconvergedRef names a generation that is still held when a bounded close
// gives up. It is a report, not a
// force-close: the resources stay held until their producer confirms the stop.
type UnconvergedRef struct {
	Generation int64          `json:"generation"`
	Owner      string         `json:"owner"`
	HeldFor    time.Duration  `json:"heldForNanos"`
	Refs       map[string]int `json:"refs"`
}
