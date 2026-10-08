package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SpellingDragon/tagent/agent/reliability"
	"github.com/SpellingDragon/tagent/agent/task"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"

	"github.com/SpellingDragon/tagent/prompt"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// MeditationConfig is the runtime configuration for the meditation manager.
// Converted from config.MeditationConfig (string durations) by tagent.go; the
// observation surface reaches this layer already resolved by the composition root.
type MeditationConfig struct {
	Enabled    bool
	Interval   time.Duration
	MinGap     time.Duration
	PromptText string
	// PromptSource 用 prompt.Getter 接口（而非具体 *prompt.Source）：保持冥想提示词可注入
	// （Getter 缝，C6 遗产）；git-native 后冥想提示词同为文件即真源（mtime 热重载直生效），
	// 改动经 refine register 登记纳入评估保护。
	// *prompt.Source 满足 Getter，既有构造点零改动；Source.Get 有 nil-receiver 守卫。
	PromptSource prompt.Getter

	// DigestExtra：可选的 digest 附加段生成器——
	// 冥想自我状态摘要末尾追加（如巩固候选清单）。nil = 无附加（现状）。
	// 由装配层注入（根包 tracker），保持 agent 包对巩固机制零依赖。
	DigestExtra func() string

	// AnchorPath 是冥想门控锚点持久化路径（T-G AnchorStore）。非空则跨重启保留三锚点
	// （novelty/idle/last-meditation），重启后不立即误触发冥想；空 = 纯内存（现状，重启失忆）。
	AnchorPath string

	// ObservedNamespaces 是外部观察形态冥想的观察面（memory namespace 名），非空即切换形态：
	// novelty 判据改读这些分区事实链上的非自管谱系新事件（经 NoveltyReader）。
	// 组合根给定最终集合——缺省回落 read_namespaces 与 observed ⊆ read 的授权校验都发生在装配期，
	// 本层只消费给定的集合。
	//
	// novelty 判据按形态并存两套，不是冗余：同 agent 的注入锚是输入侧最便宜且不可被任务层
	// 洗白的真源，跨分区则只有事实链上入库时盖章的归因可读。清空本字段即回切输入侧锚。
	// 契约: docs/wiki/reliability/durable-delivery.md#lineage-visibility
	ObservedNamespaces []string

	// DeliverTo 声明冥想产出可投递的目标 agent 白名单：缺省空 = 拒绝一切投递（fail-closed）。
	// 消费点在组合根的投递面，本 manager 只承载配置形态。
	DeliverTo []string
}

// NoveltyReader 是外部观察形态 novelty 判据的只读事实链缝，由组合根注入（通常是共享的
// memory store），agent 侧不经此缝触任何写面。
//
//   - QueryEvents 只回答轻量引用：EventReference 不带 Metadata，谱系只能靠 GetEvent 水合后读。
//   - 水合量由调用方的分页上界约束，命中即停。
//     契约: docs/wiki/memory/memory-architecture.md#read-paths
type NoveltyReader interface {
	QueryEvents(query memory.QueryOptions) ([]memory.EventReference, error)
	GetEvent(key int64) (*memory.FullEvent, error)
}

// observedPartition 是观察面的一个条目：声明名 + 由它确定的存储分区号。
type observedPartition struct {
	name string
	id   int
}

// resolveObserved 把声明的 namespace 名映射到存储分区（身份→分区的唯一推导轴是
// memory.PartitionIDFromName，与插件与装配面同轴，不另立第二套映射），并去掉落在同一分区
// 的重复声明（展示名取首个）。
func resolveObserved(names []string) []observedPartition {
	out := make([]observedPartition, 0, len(names))
	seen := make(map[int]bool, len(names))
	for _, n := range names {
		if n == "" {
			continue
		}
		id := memory.PartitionIDFromName(n)
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, observedPartition{name: n, id: id})
	}
	return out
}

// noveltyScanPageLimit bounds one external-form novelty pass: QueryEvents answers
// nothing without an explicit Limit, and the page size is the ceiling on hydrations
// (the pass stops at the first novelty hit; the worst case — a window holding only
// self-managed output — hydrates one page, negligible at a meditation-scale interval).
const noveltyScanPageLimit = 1000

// messageInjector is the interface for injecting messages into the event loop.
// *TagentAgent satisfies this interface.
type messageInjector interface {
	InjectMessageWithSource(source string, msg model.Message)
}

// MeditationManager periodically injects "meditation" external_input events into the event
// loop when the agent has been idle for at least MinGap AND the novelty gate says the world
// moved on since the last meditation.
//
// - Two forms share this manager, chosen by one switch with no middle state: in-loop (empty observation surface) is the host agent's own maintainer; external observation (non-empty ObservedNamespaces) is a cross-domain curator over other agents' partitions.
// - The idle gate is lineage-agnostic (any turn end counts as busy); the novelty gate has one data face per form — the input-side anchor in-loop, the observed partitions' persisted attribution via NoveltyReader in the external form.
// - Meditation-derived activity can therefore only delay the next meditation, never re-arm the novelty gate: the self-feeding perpetual-motion loop of "nothing happened" summaries is structurally impossible.
// - The event triggers the LLM to perform context cleanup and deep consolidation over the session.
type MeditationManager struct {
	cfg      MeditationConfig
	injector messageInjector

	// taskController, when set, provides a read-only task-layer snapshot for the
	// self-state digest prepended to the meditation prompt. nil → digest omitted
	// (graceful degradation when no task layer is wired).
	taskController task.TaskController

	// auditLine, when set, appends the behavior-audit trajectory line to the
	// self-state digest: time-series self-observation lives in the reflection
	// layer, not in the resident context. nil omits the section.
	// 契约: docs/wiki/agent/compression-and-telemetry.md#self-state-digest
	auditLine func() string

	// noveltyReader is the injected fact-chain face behind the external form's
	// novelty gate (see NoveltyReader). Set at assembly before Start, same
	// discipline as SetTaskController. An observation surface without a reader
	// keeps the gate closed — it never falls back to the input-side anchor.
	noveltyReader NoveltyReader

	// observed is the resolved observation surface derived from cfg at
	// construction; empty selects the in-loop form, whose gates read the anchors
	// only and never touch the fact chain.
	observed []observedPartition

	// lastUserInput is the novelty-gate anchor (Unix ms): the most recent
	// injection with source == "user". Updated only at the injection points
	// (inject.go) — input-side source is ground truth and cannot be laundered
	// by the task layer.
	//
	// - The external form reads the fact chain instead of this anchor, yet the injection rule keeps updating it: dropping the observation surface returns the gate to this anchor with its history intact, so a form switch needs no migration.
	lastUserInput atomic.Int64

	// lastTurnEnd is the idle-gate anchor (Unix ms): when the most recent turn
	// ended — any trigger source, including failed turns. Updated
	// unconditionally by runEventLoop after each RunFlow.
	lastTurnEnd atomic.Int64

	// lastMeditation tracks the most recent valid meditation timestamp. In the
	// external form it doubles as the novelty watermark: the gate asks the fact
	// chain for events strictly newer than it, so a fire advances the window.
	lastMeditation atomic.Int64

	// anchorStore 可选：持久化三锚点（T-G AnchorStore），跨重启保留冥想门控连续性。
	// nil = 纯内存（现状，重启失忆）。经 SetAnchorStore 注入。
	anchorStore *reliability.AnchorStore

	// anchorMu 序列化「锚点更新 + persistAnchors 快照」——三锚点分属不同 goroutine 更新
	// （user input 注入 / turn end 事件循环 / meditation ticker），无锁则 persistAnchors 对三
	// atomic 分别 Load 可能持久化「回退」的旧快照（Suggestion：并发交错覆盖）。
	anchorMu sync.Mutex

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewMeditationManager creates a MeditationManager.
// The injector is typically the *TagentAgent that owns this manager.
func NewMeditationManager(cfg MeditationConfig, injector messageInjector) *MeditationManager {
	return &MeditationManager{
		cfg:      cfg,
		injector: injector,
		observed: resolveObserved(cfg.ObservedNamespaces),
	}
}

// SetTaskController wires an optional read-only task controller used to render
// the self-state digest prepended to the meditation prompt. Safe to leave unset
// (digest is omitted — meditation behavior unchanged).
func (m *MeditationManager) SetTaskController(tc task.TaskController) {
	m.taskController = tc
}

// SetAuditLine wires the behavior-audit digest generator (see auditLine).
// Safe to leave unset; set at assembly before the loop starts, same
// discipline as SetTaskController.
func (m *MeditationManager) SetAuditLine(fn func() string) {
	m.auditLine = fn
}

// SetNoveltyReader wires the read-only fact-chain face behind the external form's
// novelty gate. Safe to leave unset: with an empty observation surface it is never
// consulted; with a non-empty one the novelty gate stays closed (fail-closed)
// instead of silently reading the input-side anchor. Set at assembly before Start,
// same discipline as SetTaskController.
func (m *MeditationManager) SetNoveltyReader(r NoveltyReader) {
	m.noveltyReader = r
}

// SetAnchorStore 注入锚点持久化存储（T-G AnchorStore），并 Load 恢复三锚点——跨重启保留冥想
// 门控连续性（重启后不立即误触发冥想、正确计算 novelty）。Load 失败保守用当前值（不阻断启动）。
func (m *MeditationManager) SetAnchorStore(s *reliability.AnchorStore) {
	m.anchorStore = s
	if s == nil {
		return
	}
	a, err := s.Load()
	if err != nil {
		log.Warnf("[Meditation] anchor load failed (%v), starting fresh", err)
		return
	}
	if a.LastUserInput > 0 {
		m.lastUserInput.Store(a.LastUserInput)
	}
	if a.LastTurnEnd > 0 {
		m.lastTurnEnd.Store(a.LastTurnEnd)
	}
	if a.LastMeditation > 0 {
		m.lastMeditation.Store(a.LastMeditation)
	}
	log.Infof("[Meditation] anchors restored across restart: lastUserInput=%d lastTurnEnd=%d lastMeditation=%d",
		a.LastUserInput, a.LastTurnEnd, a.LastMeditation)
}

// persistAnchors 保存当前三锚点到 anchorStore（若配置）。写失败仅告警（冥想门控降级为内存态，
// 不阻断主流程）。锚点更新（turn end / user input / meditation fire）时调用。
func (m *MeditationManager) persistAnchors() {
	if m.anchorStore == nil {
		return
	}
	a := reliability.MeditationAnchors{
		LastUserInput:  m.lastUserInput.Load(),
		LastTurnEnd:    m.lastTurnEnd.Load(),
		LastMeditation: m.lastMeditation.Load(),
	}
	if err := m.anchorStore.Save(a); err != nil {
		log.Warnf("[Meditation] anchor persist failed: %v", err)
	}
}

// Start launches the meditation ticker goroutine.
// Must be called after the owner's persistent event loop is active.
func (m *MeditationManager) Start() {
	m.ctx, m.cancel = context.WithCancel(context.Background())
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		ticker := time.NewTicker(m.cfg.Interval)
		defer ticker.Stop()
		log.Infof("[Meditation] manager started: interval=%s min_gap=%s observed_partitions=%d",
			m.cfg.Interval, m.cfg.MinGap, len(m.observed))
		for {
			select {
			case <-ticker.C:
				m.checkAndMeditate()
			case <-m.ctx.Done():
				return
			}
		}
	}()
}

// Stop signals the meditation goroutine to stop and waits for it.
func (m *MeditationManager) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
	m.wg.Wait()
	log.Info("[Meditation] manager stopped")
}

// UpdateLastUserInput records a source=="user" injection timestamp — the
// novelty-gate anchor. Called from the injection points only (inject.go);
// non-user sources (meditation/task/tmux) must never arm this gate.
//
//   - Both forms run this update: the in-loop form decides on it, the external form
//     does not read it yet keeps the history continuous for a switch back.
func (m *MeditationManager) UpdateLastUserInput(t time.Time) {
	m.anchorMu.Lock()
	m.lastUserInput.Store(t.UnixMilli())
	m.persistAnchors()
	m.anchorMu.Unlock()
}

// UpdateLastTurnEnd records a turn-end timestamp — the idle-gate anchor.
// Called unconditionally by runEventLoop after every RunFlow, regardless of
// trigger source or success.
func (m *MeditationManager) UpdateLastTurnEnd(t time.Time) {
	m.anchorMu.Lock()
	m.lastTurnEnd.Store(t.UnixMilli())
	m.persistAnchors()
	m.anchorMu.Unlock()
}

// observing reports the external observation form: the observation surface is the
// single switch between the two novelty data faces, with no intermediate state.
func (m *MeditationManager) observing() bool {
	return len(m.observed) > 0
}

// partitionCounts is one observed partition's tally in a novelty pass: how much of
// the reference page belongs to it, and how the hydrated sample splits across the
// lineage single source.
type partitionCounts struct {
	name string
	id   int
	// references counts the partition's entries on the reference page, which the
	// store answers without any hydration.
	references int
	// externalHits counts hydrated events whose lineage is not self-managed — the
	// novelty evidence.
	externalHits int
	// selfManaged counts hydrated events the lineage source calls self-managed,
	// unknown attribution included by the same derivation.
	selfManaged int
}

// recentActivity is the newest non-self-managed event a novelty pass found: the
// external form's "what moved out there" line.
type recentActivity struct {
	eventKey    int64
	partition   string
	eventType   string
	summary     string
	lineage     string
	timestampMs int64
}

// observedScan is one external-form novelty pass: the bounded evidence the decision
// was made on, kept so the digest renders from the same pass rather than scanning
// the fact chain a second time.
type observedScan struct {
	watermarkMs      int64
	partitions       []partitionCounts
	recent           *recentActivity
	referenceTotal   int
	hydratedTotal    int
	unknownLineage   int
	foreignPartition int
}

// newObservedScan seeds a pass with one tally line per observed partition, so a
// partition that produced nothing is still reported.
func newObservedScan(observed []observedPartition, watermarkMs int64) *observedScan {
	s := &observedScan{watermarkMs: watermarkMs, partitions: make([]partitionCounts, 0, len(observed))}
	for _, p := range observed {
		s.partitions = append(s.partitions, partitionCounts{name: p.name, id: p.id})
	}
	return s
}

// partitionIDs lists the observed surface in declared order — the query's partition
// filter, which is also what keeps an unauthorized partition out of the decision.
func (s *observedScan) partitionIDs() []int {
	ids := make([]int, 0, len(s.partitions))
	for _, p := range s.partitions {
		ids = append(ids, p.id)
	}
	return ids
}

// indexOf locates a partition's tally line by storage id, reporting whether the
// partition is on the observation surface at all.
func (s *observedScan) indexOf(partitionID int) (int, bool) {
	for i := range s.partitions {
		if s.partitions[i].id == partitionID {
			return i, true
		}
	}
	return 0, false
}

// countReference tallies one reference-page entry across the whole page, which costs
// no read: lineage splitting waits for hydration, a reference carries no metadata.
func (s *observedScan) countReference(ref memory.EventReference) {
	if at, ok := s.indexOf(ref.PartitionID); ok {
		s.partitions[at].references++
	}
}

// scanStartAfter encodes the strictly-greater watermark onto QueryOptions'
// inclusive lower bound (a zero watermark means "since epoch" — the first pass).
func scanStartAfter(watermarkMs int64) int64 {
	if watermarkMs <= 0 {
		return 0
	}
	return watermarkMs + 1
}

// scanObservedNovelty answers the external form's novelty gate from the fact chain:
// novelty is an event in an observed partition, strictly newer than the
// lastMeditation watermark, whose lineage is not self-managed.
//
//   - Lineage sense is derived by the event package's single source (SelfManagedLineage); this file holds no copy of any lineage list, and an event with no persisted trigger_source derives to unknown lineage, which never counts as novelty.
//   - References carry no metadata, so candidates are hydrated one by one and the pass stops at the first hit, newest first.
//   - A read failure keeps the gate closed: an unreadable fact chain is neither "nothing new" nor "something new", and guessing either would act on the failure rather than on the facts.
func (m *MeditationManager) scanObservedNovelty() (*observedScan, bool) {
	if m.noveltyReader == nil {
		log.Debugf("[Meditation] external novelty gate closed: observation surface set, no NoveltyReader wired")
		return nil, false
	}
	watermark := m.lastMeditation.Load()
	scan := newObservedScan(m.observed, watermark)

	refs, err := m.noveltyReader.QueryEvents(memory.QueryOptions{
		PartitionIDs: scan.partitionIDs(),
		StartTime:    scanStartAfter(watermark),
		OrderBy:      "timestamp_desc",
		Limit:        noveltyScanPageLimit,
	})
	if err != nil {
		log.Warnf("[Meditation] external novelty query failed, gate stays closed: %v", err)
		return nil, false
	}
	scan.referenceTotal = len(refs)
	for _, ref := range refs {
		scan.countReference(ref)
	}

	for _, ref := range refs {
		fe, herr := m.noveltyReader.GetEvent(ref.EventKey)
		if herr != nil || fe == nil {
			log.Debugf("[Meditation] external novelty: event %s unreadable (%v), skipped",
				tagentevent.FormatEventKey(ref.EventKey), herr)
			continue
		}
		if watermark > 0 && fe.Timestamp <= watermark {
			continue
		}
		at, known := scan.indexOf(fe.PartitionID)
		if !known {
			scan.foreignPartition++
			log.Debugf("[Meditation] external novelty: event %s lands on partition %d outside the observation surface, not counted",
				tagentevent.FormatEventKey(ref.EventKey), fe.PartitionID)
			continue
		}
		scan.hydratedTotal++
		lineage := fe.Metadata[tagentevent.MetaKeyTriggerSource]
		if tagentevent.SelfManagedLineage(lineage) {
			scan.partitions[at].selfManaged++
			if lineage == "" {
				scan.unknownLineage++
				log.Debugf("[Meditation] external novelty: event %s carries no persisted trigger_source, unknown lineage never counts",
					tagentevent.FormatEventKey(ref.EventKey))
			}
			continue
		}
		scan.partitions[at].externalHits++
		scan.recent = &recentActivity{
			eventKey:    ref.EventKey,
			partition:   scan.partitions[at].name,
			eventType:   fe.EventType,
			summary:     fe.EventSummary,
			lineage:     lineage,
			timestampMs: fe.Timestamp,
		}
		return scan, true
	}
	return scan, false
}

// checkAndMeditate evaluates whether a meditation should fire: both gates must pass.
//
// - Novelty gate, one data face per form: the in-loop form reads the injection anchor (input-side source is ground truth for the same agent); the external form reads the observed partitions' persisted attribution through NoveltyReader, because across partitions the fact chain is the only lineage face available. The form is decided by the observation surface, one switch, no middle state.
// - Idle gate (lineage-agnostic): gap since the last turn end >= MinGap. Any turn counts as busy, so meditation-derived turns merely delay, which is harmless and desirable while background work is churning.
// - No fire-time anchor reset is needed: storing lastMeditation locks the novelty gate in both forms — against the input anchor in-loop, against the watermark in the external form.
func (m *MeditationManager) checkAndMeditate() {
	now := time.Now()

	// scan is the external form's collected evidence for this fire; nil in the
	// in-loop form, whose digest stays exactly as it has always been.
	var scan *observedScan
	if m.observing() {
		var novel bool
		if scan, novel = m.scanObservedNovelty(); !novel {
			return
		}
	} else {
		lastUserMs := m.lastUserInput.Load()
		if lastUserMs == 0 {
			return
		}

		if lm := m.lastMeditation.Load(); lm > 0 && lastUserMs <= lm {
			log.Debugf("[Meditation] skipping: no new user input since last meditation")
			return
		}
	}

	lastTurnMs := m.lastTurnEnd.Load()
	if lastTurnMs == 0 {
		return
	}
	idle := now.Sub(time.UnixMilli(lastTurnMs))
	if idle < m.cfg.MinGap {
		log.Debugf("[Meditation] skipping: idle=%s < min_gap=%s", idle, m.cfg.MinGap)
		return
	}

	msg := m.buildMeditationMessage(now, idle, scan)
	m.injector.InjectMessageWithSource("meditation", msg)
	m.anchorMu.Lock()
	m.lastMeditation.Store(now.UnixMilli())
	m.persistAnchors()
	m.anchorMu.Unlock()

	log.Infof("[Meditation] triggered: idle=%s since last turn end", idle)
}

// buildMeditationMessage constructs the meditation external_input message.
// The message includes a [meditation] marker, timestamps, and the prompt text.
// If PromptSource is configured, the prompt is re-read from disk (hot-reload).
// The digest forks by form: a nil scan (in-loop) keeps the task-layer digest, a
// non-nil scan (external observation) leads with the observed partitions' overview
// rendered from that same pass.
func (m *MeditationManager) buildMeditationMessage(now time.Time, idle time.Duration, scan *observedScan) model.Message {
	var lastMed string
	if lm := m.lastMeditation.Load(); lm > 0 {
		lastMed = time.UnixMilli(lm).UTC().Format("2006-01-02 15:04:05")
	} else {
		lastMed = "首次冥想"
	}

	promptText := m.cfg.PromptText
	if m.cfg.PromptSource != nil {
		if loaded, err := m.cfg.PromptSource.Get(); err == nil && loaded != "" {
			promptText = loaded
		}
	}

	// Self-state digest (task-layer health + idle, or the observed-partition
	// overview in the external form) — prepended BEFORE the prompt so the LLM
	// reflects on real runtime state first. An empty section is skipped, keeping
	// behavior identical to a run with nothing to report.
	var digest string
	if scan != nil {
		digest = renderObservedScanDigest(scan, idle)
	}
	if m.taskController != nil {
		self := renderSelfStateDigest(m.taskController.List(), idle)
		if digest != "" && self != "" {
			digest += "\n"
		}
		digest += self
	}
	if m.cfg.DigestExtra != nil {
		if extra := m.cfg.DigestExtra(); extra != "" {
			if digest != "" {
				digest += "\n"
			}
			digest += extra
		}
	}
	if m.auditLine != nil {
		if line := m.auditLine(); line != "" {
			if digest != "" {
				digest += "\n"
			}
			digest += line
		}
	}

	header := fmt.Sprintf("[meditation] 这是一次定时冥想事件。\n\n"+
		"上次有效冥想时间：%s\n当前时间：%s",
		lastMed, now.UTC().Format("2006-01-02 15:04:05"))
	parts := []string{header}
	if digest != "" {
		parts = append(parts, digest)
	}
	parts = append(parts, promptText)

	return model.Message{
		Role:    model.RoleUser,
		Content: strings.Join(parts, "\n\n"),
	}
}
