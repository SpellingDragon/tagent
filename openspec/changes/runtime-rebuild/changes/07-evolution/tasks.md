# 07-evolution — 任务清单（三级孙任务）

> 本文件是二级子变更 `07-evolution` 的任务清单（孙任务级）。编号 `07.1–07.4` 全局唯一，**全部未勾选**（`- [ ]`），实施时逐条勾选。不得导入旧 change 编号或旧完成率。
> 命令口径（计划 §7）：确定性用例统一展开为 `go test ./tests/integration/<域> -run '^<完整测试名>$' -count=10 -json`；并发/状态任务加 `-race`。真实资源用例独立 `-count=1`，只在启用门 + 授权资源齐备时运行。
> 判定：点名测试不存在、零匹配、子项 Skip **不算通过**；选择器未匹配任何用例不得因退出码 0 记通过。测试名为**未来将落盘**的正式用例名，不得改名替代。

## 前置与边界

- 实现前置（跨域）：`04.7`（检索读缝）、`05.4`（阻塞解释）、`03.3`、`01.5`、`01.6`、`02.4`、`05.3`。这些是**实现依赖**，与签名级**接口常数依赖**（见 `proposal.md` §边界与依赖）分开对待。
- 写白名单（未来实施）：`evolution/**`、`plugin/**`（反思/观测接点）、`resources/prompts/**`、`tests/integration/evolution/**`。**本轮不写任何产品/测试文件**。
- 禁止：新造 bundle 版本库/发布道；把 nil 恒真当真实评估；自动 git 回滚；卷用户 index；新建持久化账本；恢复退役训练桥；改主 spec/旧 change/prototype/wiki。

## 任务

- [ ] 07.1 实现反思主动输入：授权观察面（越界具名拒绝）、水位新鲜度 fail-closed、确定性自状态摘要，反思作为显式输入生产者注入本 agent 循环 session —— 前置：`04.7`、`05.4` —— 复用目标：`plugin` 回调（随 state 代借用）、`model` 原生终局 —— 复用注意点：观察面 `⊆ read_namespaces ∪ {自身}`、自管判定走 event 包 `SelfManagedLineage` 单源；读失败门保持关闭不回落备用判据；digest 零 LLM、不阻塞、有界、复用同一次扫描结果（无二次扫链）—— 真实测试：`go test -race ./tests/integration/evolution -run '^TestReflectionScopeAndWatermark$' -count=10 -json`。

- [ ] 07.2 实现 git 原生 refine 三操作：`register` 仅对显式 pathspec 内受控文件 `git add`+结构化 commit（`[self-improve]`）、`status` 改进台账、`rollback` 标记校验；越界拒绝且不动 git —— 前置：`03.3`、`01.6` —— 复用目标：宿主机 `git` CLI（经受控执行面）、`event`/governance 事件面 —— 复用注意点：MUST NOT 用 `git add -A`/`git commit -a` 或复用用户已 staged 内容（不卷用户 index）；非 git 仓返回具名错误但文件已按 mtime 热读生效；成功发 improvement 事件，**sha 仅作版本关联、不以 commit 时刻开评估窗**—— 真实测试：`go test ./tests/integration/evolution -run '^TestGitNativeRefine$' -count=10 -json`（**临时仓**，不动用户仓/用户 index）。

- [ ] 07.3 实现激活时刻证据窗与后验评估：窗口锚 = 改进版本**首次被真实请求消费**的激活证据时刻（**非 commit/register 时刻**），窗口内前后版本请求按**真实消费版本**归因（GitSHA 仅关联）、feedback 版本章（sha=S）AND 真实消费版本精确 join、`judge_delay` 保留、评估输出**四态**（健康/劣化/样本不足/未到期），劣化只产建议事件不自动回滚；缺激活证据即**样本不足**（不猜 commit 时刻）—— 前置：`07.2`、`05.3` —— 复用目标：`model` 公开 LLM-judge 接点、OTel 提交后观测 —— 复用注意点：文件按 mtime 热读无需 register 即生效，故 t0 改 < t1 消费 < t2 register 为常态，窗口 MUST NOT 从 t2 起算而漏 t1；样本不足 MUST NOT 判健康、治理未启用时 DenialCount/CriticalCount 判据显式标注不可用（不以静默零值冒充无劣化）；框架不动手 revert；真实 LLM-judge 制品单列资源门—— 真实测试：`go test ./tests/integration/evolution -run '^TestPostActivationEvidence$' -count=10 -json`。

- [ ] 07.4 实现结构评估门：Eval/Benchmark/ApplyGate 消费同一声明（OrgPlan）与同代执行绑定（StatePlan/租约），缺证据不报通过，门错误 `errors.Is/As` 因果保留 —— 前置：`01.5`、`02.4`；**不依赖**后验反思调度完成 —— 复用目标：组合根声明/绑定产物（跨域只读）、`model` 原生请求终局 —— 复用注意点：各门 MUST NOT 自建配置解析/影子结构；门失败保留具名根源 `Unwrap` 链（以可达性用例证明，不以文本包含代替）；无证据即 design-blocked/不通过；真实模型评估单列门控—— 真实测试：`go test ./tests/integration/evolution -run '^TestStructuralEvaluation$' -count=10 -json`；真实模型另列资源门 `-count=1`。

## 新增问题修复

（执行期发现的异常/缺口写在此节，格式：场景 → 根源 → 影响面。契约变化先回写 `design.md`，不静默塞进补丁。）
