package evolution // import "github.com/SpellingDragon/tagent/evolution"

Package evolution 实现 agent 的自我改进通道：改动默认即生效，本包负责留痕（git 原生改进 commit）、后验评估（canary
证据 ＋ 确定性指标闸 ＋ LLM 评审双触发）与 安全回滚（仅回滚带改进标记的提交，劣化只出建议不自动动手）。

VARIABLES

var DefaultProtectedPaths = []string{"resources/prompts/**", "skills/**", "scripts/**"}
    DefaultProtectedPaths 是自改进受控清单的单一真源：evolution 的登记边界、认知资产
    漂移审计的文件集与资产写审批规则共用本清单，消费方不得复制路径字面量。

var ErrNothingToCommit = fmt.Errorf("nothing-to-commit")
    ErrNothingToCommit：受控文件无改动（N4）——调用方以 result 渗透，不按 error。

FUNCTIONS

func GitAddCommit(dir string, paths []string, note string) (sha string, err error)
    GitAddCommit 对受控路径文件执行 add+commit（message 带改进标记），返回新 commit sha。 仅 add 显式
    paths（绝不 -A——防混入用户工作区改动）。nothing-to-commit 由调用方 按 GitNothingToCommit 判定并以
    result 呈现（N4，非 error）。

func GitCommitHasTag(dir, sha string) (bool, string, error)
    GitCommitHasTag 校验目标 commit 的 message 行首带改进标记（rollback 安全闸： 防误 revert
    用户提交）。sha 可为前缀。

func GitIsRepo(dir string) bool
    GitIsRepo 报告 dir 是否在 git 工作区内（启动自检用，Warn 不阻断——闸不是墙）。

func GitRevertSafe(dir, sha string) (string, error)
    GitRevertSafe 校验标记后执行 git revert --no-edit。冲突时返回冲突详情 （exit!=0 的
    CombinedOutput），由 agent 决定后续——建议式哲学。

func MatchProtectedPaths(cwd string, paths, patterns []string) (bool, []string)
    MatchProtectedPaths 校验 paths（相对运行 cwd 归一后）全部落在 patterns 内。 返回 (ok,
    越界路径列表)。pattern 按 / 分段：`**` 匹配任意段序列，`*` 段内通配。

func NewRefineTool(g *GitEvolution) tool.Tool
    NewRefineTool 构建 git 原生 refine 工具（entry only，装配层先于治理包裹追加——A3）。

TYPES

type ActivationLog struct {
	// Has unexported fields.
}
    ActivationLog 记录 bundle（或改进 sha）的激活时刻，供证据采集当作窗口起点：
    只看激活后的表现，而非固定回看窗。它是内存态——重启后清零，对重启前激活者回退 固定回看窗（有意降级，理由见文档）。由发布管理器与证据源共享同一实例。

func NewActivationLog() *ActivationLog
    NewActivationLog 构建激活时刻表。

func (a *ActivationLog) Record(bundleID string, ts int64)
    Record 记录 bundle 激活时刻（UnixMilli）。nil-safe。

func (a *ActivationLog) Since(bundleID string) (int64, bool)
    Since 返回 bundle 激活时刻（UnixMilli）；未记录返回 (0,false)。nil-safe。

type EvalResult struct {
	Score  float64
	Pass   bool
	Reason string
}
    EvalResult 是后验评估结果。

type Evaluator interface {
	Evaluate(ctx context.Context, key string) (EvalResult, error)
}
    Evaluator 是后验评估器：对一个改进窗口（key=commit sha）的实际表现打分。 nil = 跳过 judge（仅
    guardrail）。

type Evidence struct {
	BundleID string `json:"bundle_id"`
	// TurnCount 窗口内真实 turn 数（用户输入型事件；C1 口径修正）
	TurnCount int `json:"turn_count"`
	// DenialCount 治理拒绝数（行为变差信号：agent 频试危险操作）
	DenialCount int `json:"denial_count"`
	// CriticalCount critical 挂起数
	CriticalCount int `json:"critical_count"`
	// NegFeedback negative feedback 数（D1：任务成败/用户反馈负评）
	NegFeedback int   `json:"neg_feedback"`
	WindowMs    int64 `json:"window_ms"`
}
    Evidence 是 canary 期间的表现证据（后验评估/Guardrail 的输入）。

func (e Evidence) CriticalRate() float64
    CriticalRate 返回 critical 操作率。

func (e Evidence) DenialRate() float64
    DenialRate 返回治理拒绝率（DenialCount / TurnCount）。无活动返回 0。

func (e Evidence) Sufficient(minSamples int) bool
    Sufficient 报告样本是否足以判定（不足则保守不判劣化）。

type EvidenceSource interface {
	Collect(ctx context.Context, bundleID string) (Evidence, error)
}
    EvidenceSource 收集 canary 证据。

type GitCommitInfo struct {
	Sha  string
	Note string
	// Time git 默认本地格式（status 展示用，不参与排序）
	Time    string
	Subject string
}
    GitCommitInfo 是一条改进 commit 的摘要。

func GitLogFiltered(dir string, limit int) ([]GitCommitInfo, error)
    GitLogFiltered 列出改进标记 commit（行首锚定，排除 Revert——K5）。limit<=0 取 20。

type GitEvolution struct {
	// Has unexported fields.
}
    GitEvolution 是装配单元：工具 + 生命周期。

func NewGitEvolution(cfg GitEvolutionConfig) *GitEvolution
    NewGitEvolution 构建装配单元；store/judge/guard 可为零值，运行时依赖经 BindRuntime 延迟绑定。

func (g *GitEvolution) BindRuntime(store memory.MemoryStore, pid int, judge Evaluator, guard Guardrail)
    BindRuntime 延迟绑定运行时依赖（入口 memStore ＋ 评估器对）。

func (g *GitEvolution) DigestSummary() string
    DigestSummary 供冥想 digest 渗透的自我改进摘要（M1/裁决 Q4 三来源之二、三）： 最近评估结论（劣化建议必现）+
    未登记产物清单。空串=无内容。

func (g *GitEvolution) EvaluateNow(sha string, ts int64)
    EvaluateNow 供测试/运维主动触发（跳过 delay）。

func (g *GitEvolution) Evaluations() map[string]improvementContent
    Evaluations 拉取全部 evaluation 事件（status join 用——S4：EventTypes 过滤+Content 解码）。

func (g *GitEvolution) IsRepo() bool
    IsRepo 启动自检：非 git 仓下改文件仍然生效，只是没有留痕与评估保护（告警不阻断）。

func (g *GitEvolution) LatestSha() string
    LatestSha 返回最新 improvement 的 sha（版本章来源——SetBundleIDProvider 接它）。 空串 =
    无改进（不盖章）。首次调用做一次惰性恢复（S2：查最新 improvement 事件）。

func (g *GitEvolution) Log() *ActivationLog
    Log 暴露窗口时刻表（装配层共享给 StoreEvidenceSource——W4 锚点接线）。

func (g *GitEvolution) Register(paths []string, note string) (string, string, error)
    Register 执行登记：受控校验 → git commit → improvement 事件（即窗口）→ 评估定时。 返回 (sha, 提示消息,
    err)。ErrNothingToCommit 以特殊消息返回（N4：非 error 语义）。

func (g *GitEvolution) SetGovernanceSignalsAvailable(fn func() bool)
    SetGovernanceSignalsAvailable 声明治理闸是否已接线：拒绝率与 critical 判据以治理事件
    为食，治理关闭时在结构上不可用，evaluate 必须显式说明而非让零计数冒充健康证据。 未设置（nil）= 不加注解的遗留行为。

func (g *GitEvolution) Stop()
    Stop 收敛评估 goroutine（挂在 agent 生命周期上）：经抢占通道即时收敛，无需等满
    评估延迟；被抢占的窗口不产生结论，如实降级而不伪造。

func (g *GitEvolution) Unregistered() []string
    Unregistered 提示受控路径中 mtime 晚于最后登记的文件（Q4：digest 三来源之一）。 简化实现：git status
    --porcelain 列受控路径下的已修改未提交文件（等价语义：有改动未登记）。

type GitEvolutionConfig struct {
	// WorkDir 运行目录（git 仓根=cwd；受控路径相对它归一）
	WorkDir string
	// ProtectedPaths 受控路径 patterns（默认三目录）
	ProtectedPaths []string
	// JudgeDelay register 后评估延迟（0=立即）
	JudgeDelay time.Duration
}
    GitEvolutionConfig 是 git 原生自进化的运行参数（config.go EvolutionConfig 映射）。

type Guardrail interface {
	Breach(key string) (breached bool, reason string)
}
    Guardrail 是确定性指标闸：breach 即劣化信号。nil = 不监控。 （git-native 后输出为建议式 evaluation
    事件——P4 框架不动手。）

type GuardrailConfig struct {
	// MaxDenialRate canary 窗口治理拒绝率上限（默认 0.3）
	MaxDenialRate float64
	// MaxCriticalRate critical 操作率上限（默认 0.2）
	MaxCriticalRate float64
	// MaxNegFbRate 率上限：0 取默认 0.3，负值表示显式禁用该判据（禁用无需再填一个不可达的 >1 值）。
	MaxNegFbRate float64
	// MinSamples 最小样本数（不足不判，默认 5，防抖动错杀）
	MinSamples int
}
    GuardrailConfig 是指标闸阈值。

type LLMJudgeEvaluator struct {
	// Has unexported fields.
}
    LLMJudgeEvaluator 用 LLM 对 canary 表现证据做质量裁决（实现 release.go 的 Evaluator）。

func NewLLMJudgeEvaluator(judge model.Model, src EvidenceSource, minSamples int, passThreshold float64, timeout time.Duration) *LLMJudgeEvaluator
    NewLLMJudgeEvaluator 构建 LLM 评审器。judge 为 nil 时 Evaluate 恒保守通过。 minSamples<=0
    取 5；passThreshold<=0 取 0.5；timeout<=0 取 60s。

func (e *LLMJudgeEvaluator) Evaluate(ctx context.Context, bundleID string) (EvalResult, error)
    Evaluate 实现 Evaluator：收集证据 → LLM 裁决 → EvalResult。保守：任何不确定均 Pass:true。

func (e *LLMJudgeEvaluator) WithEffort(effort string) *LLMJudgeEvaluator
    WithEffort sets the reasoning_effort knob for judge requests (optional).
    (tagent-unify-model-call-config.)

type MetricGuardrail struct {
	// Has unexported fields.
}
    MetricGuardrail 是确定性指标闸（实现 release.go 的 Guardrail 接口）：canary 表现超阈值 → breach
    → 快回滚。样本不足或收集失败 → 不 breach（保守，不误回滚）。无状态、并发安全。

func NewMetricGuardrail(src EvidenceSource, cfg GuardrailConfig) *MetricGuardrail
    NewMetricGuardrail 构建指标闸。

func (g *MetricGuardrail) Breach(bundleID string) (bool, string)
    Breach 实现 Guardrail：检查 canary 表现是否违约（确定性阈值，快、廉价，先于 LLM-judge）。

type StoreEvidenceSource struct {
	// Has unexported fields.
}
    StoreEvidenceSource 从 MemoryStore 读最近 window 的事件算证据。canary hold 期间调用即 近似该
    bundle 激活后的表现（治理记录 + 事件量是主要信号）。

func NewStoreEvidenceSource(store memory.MemoryStore, partitionID int, window time.Duration) *StoreEvidenceSource
    NewStoreEvidenceSource 构建证据源。window<=0 取默认 10m（canary 观察窗）。

func (s *StoreEvidenceSource) Collect(ctx context.Context, bundleID string) (Evidence, error)
    Collect 读窗口事件算证据。store 为 nil 或查询失败返回空证据 + err（调用方保守不判劣化）。

func (s *StoreEvidenceSource) SetActivationLog(log *ActivationLog)
    SetActivationLog 注入激活时刻表（W4）：Collect 以 bundle 激活时刻为窗口起点，而非固定回看。
