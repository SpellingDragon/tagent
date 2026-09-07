package evolution

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/log"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
)

// ==================== GitEvolution（self-evolution-git-native 装配单元）====================
//
// 单一构造单元（S3）：git 纯函数 + improvement/evaluation 事件 + 评估定时 + 版本章缓存。
// 事件即窗口（log.Record(sha, ts) 复用 eval.go 的 ActivationLog 作为通用时刻表——写入方
// 从 ReleaseManager.SetActive 换为 register）；judge/guardrail 仅产 evaluation 事件
// （建议式，P4——框架不动手）。

// GitEvolutionConfig 是 git 原生自进化的运行参数（config.go EvolutionConfig 映射）。
type GitEvolutionConfig struct {
	WorkDir        string        // 运行目录（git 仓根=cwd；受控路径相对它归一）
	ProtectedPaths []string      // 受控路径 patterns（默认三目录）
	JudgeDelay     time.Duration // register 后评估延迟（0=立即）
}

// GitEvolution 是装配单元：工具 + 生命周期。
type GitEvolution struct {
	cfg   GitEvolutionConfig
	store memory.MemoryStore // improvement/evaluation 事件直写（feedback.go 模式，不经 governance 包）
	pid   int                // 事件分区（entry）

	judge   Evaluator      // 可 nil（仅 guardrail）
	guard   Guardrail      // 可 nil（仅 judge）
	log     *ActivationLog // 窗口时刻表（sha→ts）
	stopCh  chan struct{}  // M4：评估 goroutine 抢占通道（Stop 即时收敛）
	mu      sync.Mutex     // M4：Register/Stop 并发下保护 wg.Add 与 stopped
	wg      sync.WaitGroup
	stopped atomic.Bool

	latestSha atomic.Value // string——版本章缓存（S2：性能层，真源=improvement 事件）
	recovered atomic.Bool  // 重启惰性恢复只做一次
}

// NewGitEvolution 构建装配单元。store/judge/guard 可为零值——buildAgent 阶段经
// BindRuntime 延迟绑定（memStore/model 就绪后，同旧 BindPosterior 时序——S3）。
func NewGitEvolution(cfg GitEvolutionConfig) *GitEvolution {
	if len(cfg.ProtectedPaths) == 0 {
		cfg.ProtectedPaths = []string{"resources/prompts/**", "skills/**", "scripts/**"}
	}
	return &GitEvolution{cfg: cfg, log: NewActivationLog(), stopCh: make(chan struct{})}
}

// BindRuntime 延迟绑定运行时依赖（entry memStore + 评估器对）。幂等。
func (g *GitEvolution) BindRuntime(store memory.MemoryStore, pid int, judge Evaluator, guard Guardrail) {
	g.store = store
	g.pid = pid
	g.judge = judge
	g.guard = guard
}

// IsRepo 启动自检（Warn 不阻断——非 git 仓下改文件仍生效，只是无留痕/评估）。
func (g *GitEvolution) IsRepo() bool { return GitIsRepo(g.cfg.WorkDir) }

// Log 暴露窗口时刻表（装配层共享给 StoreEvidenceSource——W4 锚点接线）。
func (g *GitEvolution) Log() *ActivationLog { return g.log }

// Stop 收敛评估 goroutine（生命周期挂 TagentAgent——K4；M4：经 stopCh 抢占，
// 不再等满 judge_delay）。
func (g *GitEvolution) Stop() {
	g.mu.Lock()
	if !g.stopped.Load() {
		g.stopped.Store(true)
		close(g.stopCh)
	}
	g.mu.Unlock()
	g.wg.Wait()
}

// LatestSha 返回最新 improvement 的 sha（版本章来源——SetBundleIDProvider 接它）。
// 空串 = 无改进（不盖章）。首次调用做一次惰性恢复（S2：查最新 improvement 事件）。
func (g *GitEvolution) LatestSha() string {
	if v := g.latestSha.Load(); v != nil {
		return v.(string)
	}
	if g.recovered.CompareAndSwap(false, true) {
		if sha := g.recoverLatest(); sha != "" {
			g.latestSha.Store(sha)
		}
	}
	if v := g.latestSha.Load(); v != nil {
		return v.(string)
	}
	return ""
}

// recoverLatest 拉最近 governance 事件找最新 improvement（Content.decoded op=register）。
func (g *GitEvolution) recoverLatest() string {
	if g.store == nil {
		return ""
	}
	refs, err := g.store.QueryEvents(memory.QueryOptions{
		// M2(独立评审):默认 asc 会取到最旧 improvement——必须倒序取最新。
		PartitionID: g.pid, EventTypes: []string{event.TypeGovernance},
		OrderBy: "timestamp_desc", Limit: 50,
	})
	if err != nil {
		return ""
	}
	keys := make([]int64, 0, len(refs))
	for _, r := range refs {
		keys = append(keys, r.EventKey)
	}
	events, err := g.store.GetEvents(keys)
	if err != nil {
		return ""
	}
	// GetEvents 保输入序（倒序 refs——QueryEvents desc），首条 register 即最新。
	for _, e := range events {
		var c improvementContent
		if json.Unmarshal([]byte(e.Content), &c) == nil && c.Op == "register" && c.Sha != "" {
			return c.Sha
		}
	}
	return ""
}

// improvementContent 是 improvement 事件 Content（控制面台账）。
type improvementContent struct {
	Op    string   `json:"op"`  // register / evaluation
	Sha   string   `json:"sha"` // commit sha（register=revert 前新 commit；evaluation=被评估窗口）
	Paths []string `json:"paths,omitempty"`
	Note  string   `json:"note,omitempty"`
	// evaluation 侧
	Verdict string `json:"verdict,omitempty"` // healthy / degraded / insufficient / pending
	Reason  string `json:"reason,omitempty"`
	Advice  string `json:"advice,omitempty"` // 建议文案（refine rollback <sha> 等）
}

// writeEvent 直写 governance 事件（feedback.go 模式——evolution↛governance 红线）。
func (g *GitEvolution) writeEvent(ic improvementContent, ts int64) {
	if g.store == nil {
		return
	}
	content, _ := json.Marshal(ic)
	key := memory.NewSnowflakeEventKey(g.pid, 0)
	_ = g.store.StoreEvent(key, memory.FullEvent{
		EventKey: key, PartitionID: g.pid, EventType: event.TypeGovernance,
		EventSummary: fmt.Sprintf("[self-improve:%s] %s", ic.Op, ic.Sha),
		Content:      string(content), Timestamp: ts,
		Metadata: map[string]string{"subtype": "improvement_" + ic.Op},
	})
}

// Register 执行登记：受控校验 → git commit → improvement 事件（即窗口）→ 评估定时。
// 返回 (sha, 提示消息, err)。ErrNothingToCommit 以特殊消息返回（N4：非 error 语义）。
func (g *GitEvolution) Register(paths []string, note string) (string, string, error) {
	if ok, bad := MatchProtectedPaths(g.cfg.WorkDir, paths, g.cfg.ProtectedPaths); !ok {
		return "", "", fmt.Errorf("越界路径（仅受控清单内可登记：%v）：%v", g.cfg.ProtectedPaths, bad)
	}
	if !g.IsRepo() {
		return "", "", fmt.Errorf("改进登记需 git 仓（当前运行目录不是 git 工作区；文件改动仍已生效，但无留痕/评估保护）")
	}
	sha, err := GitAddCommit(g.cfg.WorkDir, paths, note)
	if err != nil {
		if err == ErrNothingToCommit {
			return "", "受控文件无改动，无需登记（nothing to commit）", nil
		}
		return "", "", err
	}
	ts := time.Now().UnixMilli()
	g.writeEvent(improvementContent{Op: "register", Sha: sha, Paths: paths, Note: note}, ts)
	g.log.Record(sha, ts) // 事件即窗口：时刻表与事件同键（评估窗口锚）
	g.latestSha.Store(sha)
	g.scheduleEvaluation(sha, ts)
	return sha, fmt.Sprintf("已登记 %s（评估窗口已开，%s 后出结论；劣化将在下轮反思 digest 给出回滚建议）", shortSha(sha), g.cfg.JudgeDelay), nil
}

// scheduleEvaluation 一次性窗口快照（Q1 裁决）：judge_delay 到期评估一次。
// M4：Register/Stop 并发经 mu 串行化（防 WaitGroup Add/Wait 误用）；
// 到期等待可被 stopCh 抢占。
func (g *GitEvolution) scheduleEvaluation(sha string, ts int64) {
	if g.judge == nil && g.guard == nil {
		return
	}
	g.mu.Lock()
	if g.stopped.Load() {
		g.mu.Unlock()
		return
	}
	g.wg.Add(1)
	g.mu.Unlock()
	go func() {
		defer g.wg.Done()
		if g.cfg.JudgeDelay > 0 {
			t := time.NewTimer(g.cfg.JudgeDelay)
			defer t.Stop()
			select {
			case <-t.C:
			case <-g.stopCh:
				return // 抢占退出（评估未发生=窗口无结论，如实降级）
			}
		}
		if g.stopped.Load() {
			return
		}
		g.evaluate(sha, ts)
	}()
}

// evaluate 执行评估并写 evaluation 事件（结论四态——K7：样本不足不得冒充健康）。
func (g *GitEvolution) evaluate(sha string, ts int64) {
	ic := improvementContent{Op: "evaluation", Sha: sha, Verdict: "pending"}
	if g.guard != nil {
		if breached, reason := g.guard.Breach(sha); breached {
			ic.Verdict = "degraded"
			ic.Reason = "guardrail: " + reason
			ic.Advice = fmt.Sprintf("证据显示劣化，建议 `refine rollback %s`（如无把握可先 exec git diff 复核）", shortSha(sha))
			g.writeEvent(ic, time.Now().UnixMilli())
			log.Warnf("[GitEvolution] window %s degraded (guardrail): %s", shortSha(sha), reason)
			return
		}
	}
	if g.judge != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		res, err := g.judge.Evaluate(ctx, sha)
		if err != nil {
			ic.Verdict = "insufficient"
			ic.Reason = "评估失败（如实降级）: " + err.Error()
		} else if !res.Pass {
			ic.Verdict = "degraded"
			ic.Reason = "judge: " + res.Reason
			ic.Advice = fmt.Sprintf("LLM-judge 判劣化，建议复核后 `refine rollback %s`", shortSha(sha))
		} else {
			ic.Verdict = "healthy"
			ic.Reason = res.Reason
		}
	} else {
		ic.Verdict = "healthy"
		ic.Reason = "无评估器，仅记录窗口"
	}
	g.writeEvent(ic, time.Now().UnixMilli())
}

// EvaluateNow 供测试/运维主动触发（跳过 delay）。
func (g *GitEvolution) EvaluateNow(sha string, ts int64) { g.evaluate(sha, ts) }

// Evaluations 拉取全部 evaluation 事件（status join 用——S4：EventTypes 过滤+Content 解码）。
func (g *GitEvolution) Evaluations() map[string]improvementContent {
	out := map[string]improvementContent{}
	if g.store == nil {
		return out
	}
	refs, err := g.store.QueryEvents(memory.QueryOptions{
		// M2:同上——倒序,否则近期评估被最旧 100 条挤出。
		PartitionID: g.pid, EventTypes: []string{event.TypeGovernance},
		OrderBy: "timestamp_desc", Limit: 100,
	})
	if err != nil {
		return out
	}
	keys := make([]int64, 0, len(refs))
	for _, r := range refs {
		keys = append(keys, r.EventKey)
	}
	events, err := g.store.GetEvents(keys)
	if err != nil {
		return out
	}
	for _, e := range events {
		if e.Metadata["subtype"] != "improvement_evaluation" {
			continue
		}
		var ic improvementContent
		if json.Unmarshal([]byte(e.Content), &ic) == nil && ic.Sha != "" {
			out[ic.Sha] = ic // 后写覆盖前写（倒序遍历=先旧后新，终值最新）
		}
	}
	return out
}

// Unregistered 提示受控路径中 mtime 晚于最后登记的文件（Q4：digest 三来源之一）。
// 简化实现：git status --porcelain 列受控路径下的已修改未提交文件（等价语义：有改动未登记）。
func (g *GitEvolution) Unregistered() []string {
	out, err := gitCmd(g.cfg.WorkDir, "status", "--porcelain")
	if err != nil {
		return nil
	}
	var files []string
	for _, line := range strings.Split(out, "\n") {
		if len(line) < 4 {
			continue
		}
		p := strings.TrimSpace(line[3:])
		// rename 行（R old -> new）取新路径。
		if idx := strings.Index(p, " -> "); idx >= 0 {
			p = p[idx+4:]
		}
		ok, _ := MatchProtectedPaths(g.cfg.WorkDir, []string{p}, g.cfg.ProtectedPaths)
		if ok {
			files = append(files, p)
		}
	}
	return files
}

// DigestSummary 供冥想 digest 渗透的自我改进摘要（M1/裁决 Q4 三来源之二、三）：
// 最近评估结论（劣化建议必现）+ 未登记产物清单。空串=无内容。
func (g *GitEvolution) DigestSummary() string {
	var b strings.Builder
	evals := g.Evaluations()
	infos, err := GitLogFiltered(g.cfg.WorkDir, 5)
	if err == nil {
		for _, c := range infos {
			if ev, ok := evals[c.Sha]; ok && ev.Verdict == "degraded" {
				fmt.Fprintf(&b, "改进 %s 劣化：%s", shortSha(c.Sha), ev.Reason)
				if ev.Advice != "" {
					b.WriteString("｜" + ev.Advice)
				}
				b.WriteString("\n")
			}
		}
	}
	if un := g.Unregistered(); len(un) > 0 {
		fmt.Fprintf(&b, "⚠ 未登记产物（已改动未 register，无评估保护）：%s\n", strings.Join(un, ", "))
	}
	return b.String()
}

func shortSha(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}
