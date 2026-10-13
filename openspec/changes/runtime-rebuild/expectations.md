# OKR 预期档（expectations.md）

> 角色：把用户授权的范围翻译成**可判定的预期（O + KR）**，供验收逐条对照。**不是进度报告**、**不自报完成率**、**不承诺性能或训练提升**。
> 判据来源：只读外部计划 `运行时_重设_实施计划_304e54c3.md` + 本 change 的 proposal/design/tasks/ORCHESTRATION + `golden/runtime/scenarios.json`。
> 单点分工（不互抄）：框架复用依据在 `trpc-reuse.md`、静态决策在 `design.md`、驱动与账在 `ORCHESTRATION.md`；本档只做**预期**。
> **三类证据互不替代**：规划格式（plan/policy/contract 的 `ok`）≠ 结构覆盖（implementation）≠ 运行行为（evidence/点名用例）；任一类通过都**不**推出另一类通过。
> **分母与口径**：真实分母是 65 叶任务（全 `planned`），分子须由**点名用例真实 `pass`** 支撑；本轮**不自报完成率**，不以汇总 PASS、`Skip` 或 `grep/head/tail` 退出码替代。
> **闸口径**：`G1`（共享签名与原生接口探针）**未运行**；`G2` = **实施前桌面设计推演**（正常/失败/并发/重启），**不是产品观测**（产品观测是未来 09 证据，两者分开说，不把未实现当设计闸循环）；`G3`（逐单精确文件授权）**未闭合**。三者未过前相应路径保持 `blocked`/候选。
> **判据格式**：凡结论须能落到**可复跑命令 + 原始退出码**；未附命令与退出码的不写“完成”。

## 0. 口径（先于一切计数）

- **共享签名可改，但要有证据与影响闭合**：不写“核心绝不可改”这类伪约束；改动须落 `trpc-reuse.md`/`contracts.md`/`00.3` 的签名与消费者，闭合影响面（谁被改、谁重验）后再动。
- **state 只持生效绑定与其领域现场**；`task`（转移/通知义务）与 `memory`（原文/投影）保留**写权威**，state 不代管它们的写面。
- **不伪称框架原生 / 无自动第二机制**：不得把历史 `tagent` 方法名伪称框架原生；执行按**统一事件调度**（同一 invocation 可多 decision）、**受理 ≠ 运行即成功**、**无自动第二工具循环**。
- **不引入新版本平台**：评估窗以**真实请求消费版本 + 首次消费激活证据**开窗；`register` 只**补** Git 关联、**不重贴**旧请求/反馈；缺关联标 `unknown`/`insufficient`，不猜；保全既有 Git 安全与四态细节。
- **重大方向先 blocked**：Runner 承载、存储真源/DB/WAL 更换、跨进程动态插件、采样训练引擎——未获用户裁定前相应路径 `blocked`，**先做其余独立工作**，不等全局完成才交裁。
- **外部计划只读**：固定计划不禁止有证据的内部设计修订；`65` 只是**当前索引**，不是冻结上限；不新增叶编号、不改授权。

## 1. O1 —— 可持续演进与 tRPC 真复用

| KR | 可判定结果 | 真实生产消费者 | 任务 IDs | 场景/验收证据 | 当前待核 |
|---|---|---|---|---|---|
| O1-1 | 框架原生能力经**真实生产消费者**调用（非 import/类型别名） | 04.1 原生 file/skill 第二接入；03.2 实际后端；05.3 OTel 提交后接点；06.2 capture 包装 | 04.1, 03.2, 05.3, 06.2 | `TestNativeToolsAndSkillState`(04.1)、`TestStorageCapabilities`(03.2)、`TestObservationDoesNotOwnState`(05.3)；S03/S04/S08/S09 对照；**00.3 指定的扩展测试即映射到既有 04.1/03.2/05.3 等任务，不新增编号** | G1 探针：`model.Model`/`IterModel`/provider 形态（`trpc-reuse.md` §待核） |
| O1-2 | 扩展不改**被复用的身份/提交/生命周期机制**（02.1/02.2/02.9 冻结面） | 04.1/04.3/04.4 消费 02.1/02.2/02.9 | 04.1, 04.3, 04.4 | `TestNativeToolAdmission`、`TestPluginPhaseOrdering` | 插件相位元素类型/顺序在 `AgentBinding.Plugins` 的承载（G1） |
| O1-3 | 共享签名修订具备**证据 + 影响闭合**（谁改、谁重验） | 00.3 合同；01.4/02.6 消费 | 00.3, 01.4, 02.6 | `openspec validate`（规划级）；`trpc-reuse.md` 待核 1–5 | 各域签名未过探针前保持候选 |
| O1-4 | **不为举例造新包**：不预建 `skill` 包、不预建未登记根 `tool` 包 | 04.1（复用框架 `FSRepository`/`CallableTool`）；03.8 归 `workspace` | 04.1, 03.8 | `check_runtime_architecture --check-policy` | 03.8 候选路径归 `workspace/externalize.go` |

**O1 判据**：成功不是“引入了 tRPC 类型”，而是**真实生产请求路径**确实走到框架原生能力。已选真实扩展**一般局限于适配/组合**（加消费者，不改被复用的身份/提交/生命周期机制），须检验职责边界并确认**既有行为不漂移**；若真实需求要求核心契约修订，则**证据驱动评估 + 全部消费者回归**——**不存在“核心永不修改”的承诺**。任一“为有个包而建”的新包即违反 O1-4。

## 2. O2 —— 职责明确且可靠的执行/历史

| KR | 可判定结果 | 真实生产消费者 | 任务 IDs | 场景/验收证据 | 当前待核 |
|---|---|---|---|---|---|
| O2-1 | state 只持**生效绑定+领域现场**；`task`/`memory` 各保写权威 | 02.5 借同代绑定；05.1 读总图 | 01.4, 02.1, 03.1 | `TestStateOwnsPipeline`(01.4)、`TestDurableTaskRoundTrip`(03.3) | G1：`StatePlan`/`AgentBinding`/`Commit` 三对象形态 |
| O2-2 | **受理 Receipt ≠ 义务结清**：成功受理后义务保持至结清；仅未新增义务的 dedup/rejected 撤本次预登记 | 02.3 原生 wrapper；02.7 委派；02.9 关闭 | 02.3, 02.7, 02.9 | `TestNativeToolAdmission`、`TestCloseDrainsObligations` | 不同 mutation 各自按共同提交协议有序幂等对账（本波已改 02 spec） |
| O2-3 | **文件热读 + 后验**：文件改动下一请求边界生效且**不换代**；结构变更走前置门 | 01.6；07.2 文件直接生效；07.3 激活窗 | 01.6, 07.2, 07.3 | `TestFileAndStructuralChange`(01.6)、`TestGitNativeRefine`(07.2)、`TestPostActivationEvidence`(07.3)——**后两者不被 10 金样代替** | G1：三通道归属枚举（01.6）；07.3 激活证据时点 |
| O2-4 | **准确原文/副本**：原文逐字节一致；副本寿命与原文**分开**、可重物化 | 03.1 召回；03.6 workspace；03.4 投影入请求 | 03.1, 03.4, 03.6 | `TestOriginalReferenceIsolation`、`TestReplicaMaterialization` | G1：`Ref.Owner/Format` 去重作用域 |
| O2-5 | 未知恢复项/能力缺失 **fail-closed**，不伪成功、不静默 | 03.5 保留/回收；04.2 会话；05.4 阻塞解释 | 03.5, 04.2, 05.4 | `TestRecoveryBeforeReclamation` | `unknown≠dead` 只升级诊断，**次数阈值不作退出依据**（本波已改 04 spec） |

**O2 判据**：状态归属不混——state 持生效绑定与领域现场，task 持转移/通知义务，memory 持原文/投影写权威，三者不互相代管写面。可靠性判据是**受理 ≠ 结清**与 **unknown ≠ dead**。文件热读与结构前置门是两条独立通道：文件改动下一请求边界生效且**不换代**，结构变更走前置门。

## 3. O3 —— 统一授权运行总图

| KR | 可判定结果 | 真实生产消费者 | 任务 IDs | 场景/验收证据 | 当前待核 |
|---|---|---|---|---|---|
| O3-1 | 只读总图统计**从明细派生**（不把多维状态相加成“运行数”） | 05.1；04.6 拒绝账本/阻塞源 | 05.1, 04.6 | `TestRuntimeInventory`(05.1) | G1：一致 revision 水位来源 |
| O3-2 | 授权过滤 + **有界不可变快照** + 稳定分页 + 订阅 **gap/reset** | 05.2；宿主 08.1 | 05.2, 08.1 | `TestQueryAuthorizationAndCursor`(05.2) | 游标/revision 绑定与溢出阈值 |
| O3-3 | 观测**旁路不拥有状态**；**关闭 OTel 仍 Query 可用** | 05.3（`TestObservationDoesNotOwnState`）；真实 collector（`TestRealCollectorDelivery`） | 05.3 | S08（确定性）；09.5 collector 子项（资源门） | 真实 collector 为**资源欠账**；不以 06.3 封账冒充 |
| O3-4 | 每条阻塞给**下一推进者**（谁推进它） | 05.4；07.1 反思输入 | 05.4, 07.1 | `TestExplainRuntimeBlockers`(05.4) | — |

**O3 判据**：总图是**只读投影**，不是第二状态真源；统计从明细派生，不把多维状态相加成“运行数”。授权过滤在有界不可变快照上生效，游标绑定 revision 与过滤条件，订阅溢出/过期给具名 gap/reset。**关闭 OTel 仍 Query 可用**是硬判据。每条阻塞须给“下一推进者”；连续 `unknown` 只升级诊断、不作判死退出依据。

## 4. O4 —— 自然 RL 数据 + 真实 SFT 消费

| KR | 可判定结果 | 真实生产消费者 | 任务 IDs | 场景/验收证据 | 当前待核 |
|---|---|---|---|---|---|
| O4-1 | **运行时自然采集**：请求边界快照、secrets 不落盘、logprobs 三态 | 06.1/06.2；消费 02.4/02.5 | 06.1, 06.2 | `TestCaptureFidelityAndRedaction` | Native logprobs 支持声明来源 |
| O4-2 | 有界队列 + **封账** + `no seal` 记 `partial`（不伪完整） | 06.3；09.5 消费 | 06.3 | `TestCaptureAdmissionAndSeal` | 打开文件数/落盘上限待核 |
| O4-3 | **版本化 reward + 逐样本 manifest + episode≠session 分组防泄漏** | 06.4/06.5；07 反思消费 | 06.4, 06.5 | `TestAuthorizedTrainingExport`、`TestSFTCleaningContract` | 新记录 schema 由 **00.3 审定**；旧 v2 仅只读导入 |
| O4-4 | **真实 SFT 消费**（真实 tokenizer/chat template/collator，非常量替身） | 06.6；09.5 SFT 子项 | 06.6 | S10（**资源门**）；`TestSFTConsumer` | **设计/消费者未定 → `design-blocked`；设计已定但缺真实制品/授权 → `resource-gated`**；两者可分别登记，不混为一词 |
| O4-5 | **用途矩阵**：DPO/采样 RL 的资格合同（用途/证据/结论/缺失原因） | 06.5；不接引擎 | 06.5 | 06 offline-training-export delta（本波已补）；`TestSFTCleaningContract` | 结论三态：真正缺证据=`unknown`、已知不满足必要条件=`ineligible`、消费者与条件齐备=`qualified`；**无实际采样算法消费者不得标 `qualified`** |
| O4-6 | **反馈/模型替换闭环**：重建失败不发布错误绑定 | 06.7/06.8；消费 01.5 | 06.7, 06.8 | `TestFeedbackHTTPContract`、`TestModelReplacementLifecycle` | RL 真实端点/allowlist 授权 |

> 中性 SFT 消费合同：轨迹记录的 SFT 消费**不指定**固定 tokenizer/制品，也不默认 AReaL 新依赖；旧 AReaL 兼容仅作 **v2 只读导入**说明（本波已改 06 spec）。

**O4 判据**：价值在**真实 SFT 消费**（真实 tokenizer/chat template/collator、labels/动作 mask），不在采集条数。用途资格矩阵按用途给可判定合同：SFT 资格**不推出** DPO/采样 RL 资格；DPO 需同输入候选对 + 偏好依据/版本/分组；采样 RL 取决于实际算法与行为策略/采样证据（不笼统要求所有算法都有 logprobs）。**不接训练引擎、不伪造资格、不承诺训练提升**。

## 5. O5 —— 完整宿主交付与可信验收

| KR | 可判定结果 | 真实生产消费者 | 任务 IDs | 场景/验收证据 | 当前待核 |
|---|---|---|---|---|---|
| O5-1 | 两 module 公共入口 + 微信**独立 module** 装配 | 08.1/08.2；消费 02.9/05.2 | 08.1, 08.2 | 结构覆盖；宿主装配 | 域白名单**未收敛**（G3 blocked） |
| O5-2 | 真实宿主文件/诊断/微信面可复用 | 08.3 文件收发；08.4 诊断可见性；08.5 真实微信 | 08.3, 08.4, 08.5 | `TestFileRoundTrip`(08.3)、`TestRuntimeVisibility`(08.4)、`TestRealWechat`(08.5) | 与 03.8 外置边界；08.5 需**明确发送授权**（未授权仅不执行，非“不在预期内”） |
| O5-3 | 场景 runner 断言**值/消息/身份关系**（非只布尔键），不写成期望值复制器 | 09.1；消费全部被验收任务 | 09.1 | S01–S10；`check_runtime_evidence --evidence/--test-log` | runner **未实现**、**未命名**；字段为观测合同 |
| O5-4 | 迁移**读旧写新、源字节不变**；回退**不伪 restore** | 09.2 | 09.2 | `TestOfflineMigration`(09.2) | — |
| O5-5 | 完整门禁：两 module build/vet/test + 第一方全包 race | 09.3/09.4 | 09.3, 09.4 | 逐条原始退出码；`TestSoakContinuity`(09.4) 新锚 | 本轮**不跑产品** |
| O5-6 | 真实资源逐项验收 + 未裁项**如实保持未完成** | 09.5/09.6 | 09.5, 09.6 | 各资源子项 `-count=1` | 资源授权与预算逐项具名 |

**O5 判据**：宿主交付是**两个 module 的公共入口 + 微信独立 module**。可信验收的关键是**观测合同 ≠ 期望值**：runner 从正式 API 输出派生观测，不能照抄 expected；断言值/消息/身份关系而非只布尔键；`event_refs` 不算真伪认证。迁移读旧写新、源字节不变、回退不伪 restore。完整门禁逐条原始退出码。**未裁项如实保持未完成**。

## 6. 金样与域验证覆盖边界（与 09 的 10 场景合同对齐）

- 合同恒含 **10 场景**（`golden/runtime/scenarios.json`，唯一 scope 登记）：**S01–S09 = `deterministic`**、**S10 = `resource`**。
- **S10 是学习数据场景**（episode 跨 session split、真实 SFT 批次、反馈与模型替换），是**真实资源 gate**；未授权保持 `remaining-required`，**不得用合成替身凑 S10**。S10 **不是**宿主/微信或后验窗口的证明；宿主面见 O5-2（08.3/08.4/08.5），后验窗口见 O2-3（07.3）。
- **域覆盖边界（谁验什么）**：01→S01；02→S02/S03/S05/S06；03→S07；04→S04；05→S08；06→S09/S10；07→评估窗（S10 部分）；08→宿主装配；09→全部场景观测与门禁。
- **金样不替代点名用例**：10 场景是观测合同，O2-3/O3-4/O5-2 等点名的域用例（`TestGitNativeRefine`、`TestPostActivationEvidence`、`TestExplainRuntimeBlockers`、`TestRealWechat` 等）仍须各自真实 `pass`。
- **观测合同 ≠ 期望值**：runner 从**正式 API 输出**派生观测，**不能照抄 expected 填报**；`event_refs` **不算真伪认证**。

## 7. 明确非预期（non-goals）

- **不接** PPO/GRPO 训练引擎、**不新增**真实 on-policy 消费者、**不默认** AReaL 新依赖/自动选模型。
- **不复活**旧包结构/旧 Runner/`Declarative` 旧承载；**不建**第二工具循环、第二会话真源、bundle 版本库。
- **不承诺**性能/训练收益、权重质量或采样 RL 资格（无消费者与能力证据即标 `unknown`/`ineligible`）。
- **不做**未授权发送/付费/外部调用；**真实微信往返属 O5 必验**（08.5 + 09.5），未授权时**只是不能执行**（发送另需明确授权），**不是“不在预期内”**。
- **失败尝试与绕行代价如实呈现**：不以 `Skip`、`grep/head/tail` 退出码或汇总“PASS”替代点名用例真实通过。

## 8. 闸 / 资源点（谁未跑、谁待裁）

- **G1（共享签名与原生接口探针）——未运行**：`model.Model`/`IterModel`/provider 形态、`plugin.Manager` 与相位元素类型、`tool.Tool`/`CallableTool` 的一次受理契约、MCP `ConnectionConfig` 归属、独立 `evaluation` module 是否引入——均**未过探针**，保持候选（详见 `trpc-reuse.md` §待核）。没有探针**不得宣称 G1 通过**。
- **G2（构造/执行/失败/并发/重启推演）——设计推演/设计验收，非产品观测**：G2 是**实施前**的纵向推演，用于发现设计缺口；**产品观测是 09 证据**，不把未实现当设计闸循环。**G2 目前仍未确认，不得宣称通过**。
- **G3（逐单精确文件授权）——未闭合**：07/08/09 域候选写域仍为目录/通配，00.6 产品门候选未收窄，全部叶任务读闭包未实测 → 一律 blocked。
- **资源点（未授权即留欠账）**：rustviking 存储档、真实模型端点、MCP/A2A 服务、embedder 索引、collector/OTel、SFT 制品/tokenizer、微信发送通道、09.4 soak/崩溃恢复。未授权保持 `remaining-required`。

## 9. 术语（本档口径）

- **Receipt（受理）**：对一次调用/任务的受理回执；受理成功**不**等于义务结清（义务保留至结清）。
- **dedup / rejected**：未新增义务的去重/拒绝；仅此二者撤本次预登记，**旧既有义务不动**。
- **热读 / generation**：文件内容按请求边界读取并固化实际版本；结构绑定有 generation，但**文件内容版本 ≠ 结构 generation**。
- **drain（排空）**：关闭时把已受理义务投递/结清；**未止不能 drained**，止后能 drained。
- **unknown ≠ dead / unsupported**：未知只升级诊断或显式停止处理，不作判死；`unsupported` 需支持声明。
- **gap / reset**：订阅/游标窗口过期或溢出时的具名断档；不得静默漏报。
- **episode / decision / attempt**：episode 代表业务过程（≠ session，可重合但不强制等同或拆分）；decision/attempt 与实际请求边界同步产生。
- **qualified / ineligible / unknown**：训练用途资格结论词；缺证据=`unknown`、已知不满足必要条件=`ineligible`、条件齐备=`qualified`。
- **runner / observations / event_refs**：runner 从正式 API 输出派生 observations；`event_refs` 自报，不构成真伪认证。

## 10. 复验入口（口径单点，可复跑；不重复命令）

- 检查器自测：`python3 -B -m unittest discover -s scripts -p 'test_check_runtime_*.py'`（证明**检查器**，不证明产品）。
- 规划格式：`python3 scripts/check_runtime_plan.py --json`。
- 策略：`python3 scripts/check_runtime_architecture.py --check-policy --json`。
- 合同：`python3 scripts/check_runtime_evidence.py --check-contract --plan-graph openspec/changes/runtime-rebuild/task-graph.json --json`（`scenario_count=10`）。
- 规划级：`openspec validate runtime-rebuild --strict --no-interactive`。
- 口径：全部 `cwd=repo`、记**原始退出码**、不管道吞错；任一通过**不**推出真实产品行为通过（见 §0「三类证据互不替代」）。
