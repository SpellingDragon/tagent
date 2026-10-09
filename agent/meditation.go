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

	// AnchorPath 是冥想门控锚点持久化路径（T-G AnchorStore）。非空则跨重启保留门控锚点
	// （lastTurnEnd/lastMeditation），重启后不立即误触发冥想；空 = 纯内存（现状，重启失忆）。
	AnchorPath string

	// ObservedNamespaces 是 novelty 判据的观察面（memory namespace 名）：判据读这些分区
	// 事实链上晚于水位（lastMeditation）的非自管谱系事件（经 NoveltyReader）。
	// 组合根给定最终集合——缺省（未声明）解析为 [agent 自身分区]（自察），显式声明可含
	// 自身与他人；本层只消费给定的集合。
	//
	// 判据只有一个：观察谁，就读谁水位后的非自管事件。反思动作恒为向本 agent 循环的
	// session 注入冥想输入事件，观察面配置不改变动作本身。
	// 契约: docs/wiki/reliability/durable-delivery.md#lineage-visibility
	ObservedNamespaces []string

	// DeliverTo 声明冥想产出可投递的目标 agent 白名单：缺省空 = 拒绝一切投递（fail-closed）。
	// 消费点在组合根的投递面，本 manager 只承载配置形态。
	DeliverTo []string
}

// NoveltyReader 是 novelty 判据的只读事实链缝，由组合根注入（通常是共享的 memory
// store），agent 侧不经此缝触任何写面。
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
// 的重复声明（展示名取首个）。观察面的缺省解析（未声明＝自身分区）在上游装配期完成，
// 本函数只映射与去重给定的名字。
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

// defaultObservedSurface 补齐观察面的缺省解析：已声明即原样返回，未声明即 [ownName]（自察）。
// 授权面不等于观察面，缺省不回落 read_namespaces；组合根之外的直接构造点与组合根共用这一条规则。
func defaultObservedSurface(ownName string, declared []string) []string {
	if len(declared) == 0 {
		return []string{ownName}
	}
	return declared
}

// noveltyScanPageLimit bounds one novelty pass: QueryEvents answers nothing without
// an explicit Limit, and the page size is the ceiling on hydrations (the pass stops
// at the first novelty hit; the worst case — a window holding only self-managed
// output — hydrates one page, negligible at a meditation-scale interval).
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
// - One mechanism over any observation surface: the novelty gate reads the observed partitions' fact chains for non-self-managed lineage events past the watermark, and a fire injects the reflection input into THIS agent's own loop session. Observing one's own partition (the default) is self-maintenance; observing others' is curation — only the configuration differs, never the mechanism.
// - The idle gate is lineage-agnostic: any turn end counts as busy.
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

	// noveltyReader is the injected fact-chain face behind the novelty gate (see
	// NoveltyReader). Set at assembly before Start, same discipline as
	// SetTaskController. Without a reader the gate stays closed (fail-closed) —
	// the fact chain is the novelty gate's only data face.
	noveltyReader NoveltyReader

	// observed is the resolved observation surface derived from cfg at
	// construction; the novelty gate reads its partitions' fact chains. An empty
	// surface (only reachable via direct construction, the composition root
	// always resolves one) keeps the gate closed.
	observed []observedPartition

	// lastTurnEnd is the idle-gate anchor (Unix ms): when the most recent turn
	// ended — any trigger source, including failed turns. Updated
	// unconditionally by runEventLoop after each RunFlow.
	lastTurnEnd atomic.Int64

	// lastMeditation tracks the most recent valid meditation timestamp and
	// doubles as the novelty watermark: the gate asks the fact chain for events
	// strictly newer than it, so a fire advances the window and locks itself.
	lastMeditation atomic.Int64

	// anchorStore 可选：持久化门控锚点（T-G AnchorStore），跨重启保留冥想门控连续性。
	// nil = 纯内存（现状，重启失忆）。经 SetAnchorStore 注入。
	anchorStore *reliability.AnchorStore

	// anchorMu 序列化「锚点更新 + persistAnchors 快照」——锚点分属不同 goroutine 更新
	// （turn end 事件循环 / meditation ticker），无锁则 persistAnchors 对各自 atomic
	// 分别 Load 可能持久化「回退」的旧快照（Suggestion：并发交错覆盖）。
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

// SetNoveltyReader wires the read-only fact-chain face behind the novelty gate.
// A configured meditation always gets a store behind it: the fact chain is the
// gate's only data face, the default self-observation surface included.
// Safe to leave unset: the gate then stays closed (fail-closed) instead of
// guessing. Set at assembly before Start, same discipline as SetTaskController.
func (m *MeditationManager) SetNoveltyReader(r NoveltyReader) {
	m.noveltyReader = r
}

// SetAnchorStore 注入锚点持久化存储（T-G AnchorStore），并 Load 恢复门控锚点——跨重启保留冥想
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
	if a.LastTurnEnd > 0 {
		m.lastTurnEnd.Store(a.LastTurnEnd)
	}
	if a.LastMeditation > 0 {
		m.lastMeditation.Store(a.LastMeditation)
	}
	log.Infof("[Meditation] anchors restored across restart: lastTurnEnd=%d lastMeditation=%d",
		a.LastTurnEnd, a.LastMeditation)
}

// persistAnchors 保存当前门控锚点到 anchorStore（若配置）。写失败仅告警（冥想门控降级为内存态，
// 不阻断主流程）。锚点更新（turn end / meditation fire）时调用。
func (m *MeditationManager) persistAnchors() {
	if m.anchorStore == nil {
		return
	}
	a := reliability.MeditationAnchors{
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

// UpdateLastTurnEnd records a turn-end timestamp — the idle-gate anchor.
// Called unconditionally by runEventLoop after every RunFlow, regardless of
// trigger source or success.
func (m *MeditationManager) UpdateLastTurnEnd(t time.Time) {
	m.anchorMu.Lock()
	m.lastTurnEnd.Store(t.UnixMilli())
	m.persistAnchors()
	m.anchorMu.Unlock()
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
// digest's "what moved" line.
type recentActivity struct {
	eventKey    int64
	partition   string
	eventType   string
	summary     string
	lineage     string
	timestampMs int64
}

// observedScan is one novelty pass: the bounded evidence the decision was made on,
// kept so the digest renders from the same pass rather than scanning the fact
// chain a second time.
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

// scanObservedNovelty answers the novelty gate from the fact chain: novelty is an
// event in an observed partition, strictly newer than the lastMeditation watermark,
// whose lineage is not self-managed.
//
//   - Lineage sense is derived by the event package's single source (SelfManagedLineage); this file holds no copy of any lineage list, and an event with no persisted trigger_source derives to unknown lineage, which never counts as novelty.
//   - References carry no metadata, so candidates are hydrated one by one and the pass stops at the first hit, newest first.
//   - An empty observation surface, a missing reader and a read failure all keep the gate closed: none of them is ever allowed to be reinterpreted as "nothing new" or "something new" — guessing would act on the guess rather than on the facts, and there is no alternate data face to fall back to.
func (m *MeditationManager) scanObservedNovelty() (*observedScan, bool) {
	if len(m.observed) == 0 {
		log.Debugf("[Meditation] novelty gate closed: empty observation surface")
		return nil, false
	}
	if m.noveltyReader == nil {
		log.Debugf("[Meditation] novelty gate closed: observation surface set, no NoveltyReader wired")
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
		log.Warnf("[Meditation] novelty query failed, gate stays closed: %v", err)
		return nil, false
	}
	scan.referenceTotal = len(refs)
	for _, ref := range refs {
		scan.countReference(ref)
	}

	for _, ref := range refs {
		fe, herr := m.noveltyReader.GetEvent(ref.EventKey)
		if herr != nil || fe == nil {
			log.Debugf("[Meditation] novelty pass: event %s unreadable (%v), skipped",
				tagentevent.FormatEventKey(ref.EventKey), herr)
			continue
		}
		if watermark > 0 && fe.Timestamp <= watermark {
			continue
		}
		at, known := scan.indexOf(fe.PartitionID)
		if !known {
			scan.foreignPartition++
			log.Debugf("[Meditation] novelty pass: event %s lands on partition %d outside the observation surface, not counted",
				tagentevent.FormatEventKey(ref.EventKey), fe.PartitionID)
			continue
		}
		scan.hydratedTotal++
		lineage := fe.Metadata[tagentevent.MetaKeyTriggerSource]
		if tagentevent.SelfManagedLineage(lineage) {
			scan.partitions[at].selfManaged++
			if lineage == "" {
				scan.unknownLineage++
				log.Debugf("[Meditation] novelty pass: event %s carries no persisted trigger_source, unknown lineage never counts",
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
// - Novelty gate (the single criterion): an event in an observed partition, strictly newer than the lastMeditation watermark, whose lineage is not self-managed — the fact chain is the gate's only data face, for every observation surface including the default self-observation one.
// - Idle gate (lineage-agnostic): gap since the last turn end >= MinGap. Any turn counts as busy, so meditation-derived turns merely delay, which is harmless and desirable while background work is churning.
// - No fire-time anchor reset is needed: storing lastMeditation advances the watermark and locks the novelty gate until the observed surface moves again.
func (m *MeditationManager) checkAndMeditate() {
	now := time.Now()

	scan, novel := m.scanObservedNovelty()
	if !novel {
		return
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
// The digest leads with the observed-surface overview rendered from that same
// novelty pass, followed by the optional own-task and behavior-audit sections.
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

	// Digest: observed-surface overview (rendered from the triggering pass — the
	// fact chain is never scanned twice), then the own task-layer section and the
	// audit line as independent optional sections. An empty section is skipped,
	// keeping behavior identical to a run with nothing to report.
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
