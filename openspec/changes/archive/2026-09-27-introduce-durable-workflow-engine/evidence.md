# 证据台账：introduce-durable-workflow-engine（轮六十八重整版）

> **纪律**：每任务记录命令、真实退出码与实际断言；先红后绿（纯结构变更须声明替代验收）；不冒充完成、不缩小过滤。历史轮次的「完成／全绿／收口」陈述不自动成为当前结论——凡被后续撤回者以 §3 撤回台账为准。现行状态以 §0、design.md、tasks.md 为准。
>
> **重整说明（2026-09-26，轮六十八）**：原 2749 行逐轮流水经逐块通读后重整为「状态速览＋轮次索引＋裁决/撤回台账＋按阶段聚合的轮证据」；过程叙述与重复门禁表压缩为单行，**红→绿事实、真实退出码、用户裁决、撤回记录、未跑边界全数保留**。tasks/design 中「evidence 轮N」引用一律经 §1 轮次索引解析。原文已不保留逐字版本（git 未跟踪工件，重整即替代）。

## 0. 现行状态速览（2026-09-26）

| 项 | 值 |
|---|---|
| 进度 | **34/34**（轮一百零七 §5.4 终门准出，§5.63；轮一百零六收口 5.3，§5.62；轮一百零五 5.3 测量部分，§5.61；轮一百零四收口 5.2，§5.60；轮一百零三收口 5.1，§5.59；轮一百收口 3.3，§5.56；轮九十九收口 4.2，§5.55；轮九十 2.3/3.2 双勾，§5.46） |
| 代码基线 | **轮一百零九（死代码清理＋负载边界修）**：build/vet 0/0、`agent -race` 82.318s、root `-race -short` 74.207s、全仓 `-short` 修后 2/2 绿（30 包）、TestMonitor33 单跑 12.688s、tmux 无残留。删除＝7 死函数（Released/RunOrgReloader/BindingFace/LastBatchOutcome/computeMemoryFingerprint/currentFingerprint/ensureUserPrompt）＋ensure_user_prompt_test.go；迁移＝quiescent/orgLastDiscardOrder 移入测试文件、3 处 currentFingerprint 测引用、Clone 保真改用 agentMemoryFingerprint；TestMonitor33 断言改有界重取（负载下 List() 再裁决是设计路径）。既往：轮一百零七三门与全仓 race 全绿；轮一百零八 specs 审计。**34/34 保持；未提交（staged 0、HEAD b0e053f）** |
| 已勾主段 | 0.x、1.x、2.1、2.2、**2.3（轮九十联合勾）**、**2.4（轮九十二）**、3.1(旧口径)、**3.2（轮九十）**、3.3(旧口径)、**3.3（轮一百重开收口，§5.45／§5.47／§5.51／§5.53／§5.56）**、**3.4（轮九十八，§5.52／§5.53／§5.54）**、4.2(旧口径)、**4.2（轮九十九重开收口，§5.50／§5.55）**、4.3（轮八十五，§5.41）、**4.1（轮九十三，§5.49）**、4.4(旧口径)、5.1(旧口径)、**5.1（轮一百零三重开收口，§5.57／§5.58／§5.59）**、**5.2（轮一百零四重开收口，§5.60）**、**5.3（轮一百零六重开收口，§5.61／§5.62）**、**5.4（轮一百零七终门准出，§5.63）**、6.1/6.2/6.3/**6.4（轮八十真收口）**/6.5/6.6/6.7、**7.1/7.2/7.3（S3m 链，轮六十六转勾）** |
| 余项（0） | 全部 34 项转勾，第 1–7 章闭合。终门后仍待**授权**的两件外部事（非本变更待办）：①提交与 `/opsx:archive`；②6.3 已记的上游 PR／移动 tag／撤 replace（需单独授权，本轮未做）。另留两条已记录、不在本变更范围的后续观察：热更主项是 YAML 重解析（§5.61，若要优化需另立变更）；real-LLM 端到端未跑（凭据门控） |
| 上游 | fork `github.com/SpellingDragon/trpc-agent-go v1.11.2-tagent.1`（release 分支＝官方 v1.11.2 `5a0030b62`＋4 修复提交 cherry-pick；两模块 replace 钉版；上游 PR 合并／撤 replace 回官方另计 5.4） |

**门禁口径（历史教训固化，后续轮沿用）**：
① `./tests/` 真实 LLM 集成测须 `-short`（输出非确定，HEAD 即红）；② 全包 `-count>1` 不可用（`RegisterPlainTool` 固定名 panic），重复门须配 `-run` 过滤；③ 取消驱动的 race 门是概率门，不得以一次通过声称 race 通过（U-2 教训：轮七绿被轮九打脸）；④ 每轮至少一次干净 HEAD worktree `go vet ./...`（B-1 教训：未跟踪 `wf_facts.go` 断链被连续九轮掩盖）；⑤ 红证探针（TEMP RED PROBE / 变异体）用后必 `grep` 确认还原（退出码 1＝无残留）；⑥ wechat-bot 独立模块 build/vet/test 三门；⑦ 本仓日志写 stdout 与 bench 交错，取数须 `grep "ns/op"`；⑧ benchmark 协议含 N=1 预热，守卫断言会预热误红——测量类断言用测试形态。

## 1. 轮次索引（tasks/design「evidence 轮N」引用锚）

效力列：**现行**＝结论被现行 34 项计划承认；**历史**＝仅过程证据，其完成声明已被重开/撤回；**已撤回**＝明确作废。

| 轮 | 日期 | 主题 | 效力 | 节 |
|---|---|---|---|---|
| — | 09-22 | 原耐久方案台账（阶段0–7） | 已撤回（试验清理） | §9 |
| — | 09-22 | 现行范围修订／第二轮重定向／实施基线(0.1/0.3) | 历史（基线表仍被引用） | §8 |
| — | 09-22 | 阶段1(A：组织配置与编译 1.1–1.4 含两轮评审) | 已撤回（原型随 §1 删除） | §8.3 |
| 一 | 09-22 | 0.3 基线＋§1 原型撤回（15 文件）＋orgCoordinator 收敛 | 历史 | §7.1 |
| 二 | 09-22 | §2.1/2.2/2.4 候选构造与发布分离（HEAD 探针红） | 历史 | §7.2 |
| 三 | 09-22 | §3.1/3.2 turn 边界取版；发现上游竞态 U-1 | 历史 | §7.3 |
| 四 | 09-22 | §4.4 回收轮次选代＋U-1 定位细化 | 历史 | §7.4 |
| 五 | 09-22 | §5.1 部分（诊断面）＋tests 门禁口径定形 | 历史 | §7.5 |
| 六 | 09-23 | §4.3 子树热增删（ResidentTopology COW） | 历史 | §7.6 |
| 七 | 09-23 | §5.1 闭合＋修 §4.3 引入的静默重参数化 | 历史 | §7.7 |
| 八 | 09-23 | §5.3 基准＋§5.4 文档／wechat 首测 | 历史 | §7.8 |
| 九 | 09-23 | §5.2 六场景＋纠正自报 2.3 失实＋发现 U-2 | 历史 | §7.9 |
| 十 | 09-23 | CodeReview 整改（B-1 HEAD 断链/M-1 测试污染等） | 历史 | §7.10 |
| 十一 | 09-23 | §2.3 以测量结案（热更 ~2ms，解析为成本中心） | 历史 | §7.11 |
| 十二 | 09-23 | §4.2 悬红轮收尾（剥 OutputLimitTool）＋发现 W-1 死接线 | 历史 | §7.12 |
| 十三 | 09-23 | 上游 v1.11.2 升级，U-1/U-2 结案（50/60 迭代） | 历史（结案有效） | §7.13 |
| — | 09-23 | 整体 review 与计划修订（31 项，重开 9＋新增 6.1–6.7） | 历史（被 09-25 再修） | §6.0 |
| 十四 | 09-23 | 6.1 入口身份拒绝＋6.2 被动排除撤 TTL | 现行 | §6.1 |
| 十五 | 09-23 | 2.1 R06 getter/发布双向隔离＋2.3 范围核定 | 现行(2.1) | §6.2 |
| 十六 | 09-23 | 2.3 R01 事务半（候选私有 overlay/单一提交点） | 部分（2.3 仍 [ ]） | §6.3 |
| 十七 | 09-23 | 2.3 D3 调度半（屏障证短锁，撤均值声明） | 部分 | §6.4 |
| 十八 | 09-23 | 6.3 L0 能力门判定（官方 v1.11.2 不足） | 现行 | §6.5 |
| 十九 | 09-23 | 6.6 关闭全部 race 豁免 | 现行 | §6.6 |
| 二十 | 09-23 | 6.7 测试落盘卫生（testStore PID 根） | 已被轮41取代 | §6.7 |
| 二十一 | 09-23 | 6.5 W-1 透明装饰穿透接线 | 现行 | §6.8 |
| 二十二 | 09-23 | 2.4 L-3 完整有效配置与回滚（revision/两时间） | 部分（2.4 重开） | §6.9 |
| 二十三 | 09-23 | 6.4 M-3 热参消费面核验＋死源清除 | 部分（6.4 重开） | §6.10 |
| 二十四 | 09-23 | CodeReview 整改（H-1 真 race/M-1 stopped 闸门/L-1 镜像） | 现行 | §6.11 |
| 二十五 | 09-23 | 计划修订轮（只读核验，重开 2.4/6.4/6.5 缺口） | 历史 | §6.12 |
| 二十六 | 09-23 | 6.3 L1+L2 最小 fork 本地补丁与验收 | 现行 | §6.13 |
| 二十七 | 09-23 | 2.4 缺口闭合（回滚钩子装配期安装） | 部分 | §6.14 |
| 二十八 | 09-23 | 6.4 缺口闭合（hotSnapshot/liveCMs 播种+扇出） | 部分（6.4 再重开：将改消费边界拉取） | §6.15 |
| 二十九 | 09-23 | 6.5 缺口闭合（投影经调用上下文） | 现行 | §6.16 |
| 三十 | 09-23 | 6.3 R1+R2 fork tag 发布与两模块钉版（远端授权） | 现行 | §6.17 |
| 三十一 | 09-23 | 3.2+4.1 联合（execBinding/ExecLease 逐代租约、有界 Close） | 部分（3.2/4.1 重开） | §6.18 |
| 三十二 | 09-23 | 3.3 全路径矩阵（同步/重试/多级/异步/本地/工厂/A2A） | 部分（3.3 重开：分类贯通） | §6.19 |
| 三十三 | 09-23 | 4.2 重入统一版本源＋组织 Close 覆盖；退役半实现→取证→完整回退 | 部分（4.2 重开） | §6.20 |
| 三十四 | 09-23 | 4.3 owner 排空/退役（三轴义务账本） | 部分（4.3 重开） | §6.21 |
| 三十五–三十七 | 09-24 | 无独立台账条目（历史缺口，如实标注；内容佚/并入邻近） | — | — |
| 三十八 | 09-24 | 5.3 性能有界重验（发现 agent 测包不可编译并修） | 部分（5.3 重开） | §6.22 |
| 三十九 | 09-24 | 5.4 最终回归「31/31 收口」 | **已撤回**（轮41） | §6.23 |
| — | 09-25 | 架构复核修订（第一次）＋同构修订（第二次，现行基准/RV1–7） | 现行 | §4 |
| 四十一 | 09-25 | 6.7 测试生命周期根（t.TempDir，取代轮20） | 现行 | §5.1 |
| 四十二 | 09-25 | 7.1 去隐式传参（activeBus＋外部上下文按调用装配） | 现行 | §5.2 |
| 四十三 | 09-25 | 7.1 B-1 提取 processTurn＋7.2 首片 D3 spawner 归属 | 现行 | §5.3 |
| 四十四 | 09-25 | 7.1 B-2 Run 经共享 processTurn（maxRetries 形参） | 现行 | §5.4 |
| 四十五 | 09-26 | 7.x S1 关联把手（invocation_id，behavior-neutral） | 现行 | §5.5 |
| 四十六 | — | 无独立条目（历史缺口） | — | — |
| 四十七 | 09-26 | code review：B-2 空/多模态输入契约（W-1/W-2）＋S-1 clamp | 现行 | §5.6 |
| 四十八 | — | 无独立条目（历史缺口） | — | — |
| 四十九 | 09-26 | S3 被调方执行模型裁决→采纳 M2（纯工件） | 现行 | §5.7 |
| 五十 | 09-26 | S2m 越窗归属路由打底（Origin 携 invocation_id） | 现行 | §5.8 |
| 五十一 | 09-26 | S3m-a 越窗 settle 路由（sink registry，无绑回落 bus） | 现行 | §5.9 |
| 五十二 | 09-26 | S3m-b 尝试→② 门红→完整回退（inflight==0 不安全） | 现行（教训） | §5.10 |
| 五十三 | 09-26 | D-b 定案＝投递对账原语（pending/noteSpawn/quiescent） | 现行 | §5.11 |
| 五十四 | 09-26 | code review：W-1 契约/W-2 真缺陷（SourceTask 门）/W-3/W-4/S-5/S-6 | 现行 | §5.12 |
| 五十五 | 09-26 | W-1 真正修复＝可靠 append 投递（tryFinish/wait） | 已被 c.1 收敛取代 | §5.13 |
| 五十六 | 09-26 | S3m-b.2 环本体（M2 越窗存活+续写，② 门首绿） | 已被 c.1 收敛取代（行为仍由 d8 锁） | §5.14 |
| 五十七 | 09-26 | S4/D-c 取消·超时契约＋I1 并发不串（d9/d10） | 现行 | §5.15 |
| 五十八 | 09-26 | family-3 Run 侧 session 去共享写 | 现行 | §5.16 |
| 五十九 | 09-26 | 7.2② 被调方并发端到端（d11，tag 互异） | 现行 | §5.17 |
| 六十 | 09-26 | 用户原则输入→S3m-c 立项（零码改） | 现行 | §5.18 |
| 六十一 | 09-26 | 「理想形态、一次写对」→S3m-c 定稿＋高维审计（零码改） | 现行 | §5.19 |
| 六十二 | 09-26 | 讨论项(a) 注入目的地裁决「保持现状」 | 现行 | §5.20 |
| 六十三 | 09-26 | 讨论项(b) retry budget 裁决「统一」 | 现行 | §5.20 |
| 六十四 | 09-26 | S3m-c.1 芯片（绑定表+共享壳+budget 统一，红→绿） | 现行 | §5.21 |
| 六十五 | 09-26 | S3m-c.2 输入入管线（单管线，行为保持） | 现行 | §5.22 |
| 六十六 | 09-26 | S3m-c.3 宿主形门（d12 修复）＋7.1/7.2/7.3 转勾 | 现行 | §5.23 |
| 六十七 | 09-26 | 执行回顾审计（对照用户要求）＋6.4 范围结论 | 现行 | §5.24 |
| 六十八 | 09-26 | 本重整轮（零码改） | 现行 | 本节 |
| 六十九 | 09-26 | 工件三件套防跑偏重写（核心思想卡＋判例卡＋Order-A 程序；零码改） | 现行 | §5.25 |
| 七十 | 09-26 | P0 设计定稿：核心簇理想形态入 design＋切片 S-A~S-F 入 tasks（纯工件） | 现行 | §5.26 |
| 七十一 | 09-26 | P1/S-A 构造拆分＋entry 去壳（红→绿，四锚；全 agent -race 80.4s 绿） | 现行 | §5.27 |
| 七十二 | 09-26 | P1/S-B 有序责任表（差集推断废除；逆序回退锚绿；全 agent -race 80.7s 绿） | 现行 | §5.28 |
| 七十三 | 09-26 | P1/S-C 前半：锚1 热增路径专项测钉住（绿＝机制已覆盖，如实核销）；appliedRecord 本体留待下轮 | 现行 | §5.29 |
| 七十四 | 09-26 | P1/S-C 收尾：数值-only 提交闸门接入（记录轮转与提交同锁）；锚2 挂起红→双轴分歧真断言绿；root -race 恢复真绿 | 现行 | §5.30 |
| 七十五 | 09-26 | S-D/3.2 范围与接缝核销（只读判定，零码改；红锚跨 3.2/4.3、D8 使用权 read 侧属 P2→呈接缝待裁） | 现行 | §5.31 |
| 七十六 | 09-26 | P1/S-E 首增量：compressor 拉取契约（HotNumbers/WithHotSource/liveNums＋per-call 同代下行）＋常驻/私有消费者接线；锚2 按 §5 迁移为源旋转语义；root/agent -race 全绿 | 现行 | §5.32 |
| 七十七 | 09-26 | S-E ⑤ recordView 无锁化（appliedView 三提交点发布私有副本；红锚＝持 coord.mu 期间读须推进）；关闭轮七十六残余重入风险 | 现行 | §5.33 |
| 七十八 | 09-26 | **P1/S-E 收尾**：push→pull 反转完成（四路 push＋hotSnapshot＋hotOverlayConfig 全删、TTL 源、恒装源、6 测族迁移）→ **6.4 勾检 22/34** | 现行 | §5.34 |
| 七十九 | 09-26 | **自纠：6.4 假完成重开（22→21）**——轮78「无独立 spawner TTL 权威」为误（grep 漏 root）；`ActionTool` TTL 改消费边界现读、`SetDefaultTaskTTL` 与三处 push 删除、红→绿 | 现行 | §5.35 |
| 八十 | 09-26 | 6.4 spawner 轴端到端锚（变异探针证判别性）＋死件 `ApplyParams` 删除＋5 处失实注释改正＋tmux 并发 flake 定性 → **6.4 真收口 22/34** | 现行 | §5.36 |
| 八十一 | 09-26 | **S-D 首增量**：D8 使用权以 `BindingHolders` 派生视图＋sweep 第四轴落地（零新登记表面）；延后委派红锚三段转绿；root -race 零回归 | 现行 | §5.37 |
| 八十二 | 09-26 | 4.3「释放即续排」欠账兑现：通知＝世代引用跃迁归零（非 forgetBinding）＋owner 全覆盖自臂；mid-close／最终退出两态分离并各钉锚；顺带关闭单飞合并洞 | 现行 | §5.38 |
| 八十三 | 09-26 | 4.3 菱形退役锚落地（P1/P2 探针证判别性）；**全量 -race 推翻轮82「合并洞已关闭」**⇒ 撤掉未证明的级联循环，改为排空尾「门开后再自觉一次」；2×全量＋11 锚 ×5 race 复验 | 现行 | §5.39 |
| 八十四 | 09-26 | 4.3 末两件收口：真实 poisoned 热增闭路（P4 判别）＋候选在飞时 org Close 覆盖（P5/P6 各打一条性质）＋**实测出自家 flock 探针会拆锁**（脚手架自毁 ⇒ 改登记面非破坏检查）＋§5.36 归因收窄 | 现行 | §5.40 |
| 八十五 | 09-27 | **4.3 转勾（23/34）**：共享 store 的 Close 面锚定（PC 有牙）；**实测否证轮八十四「提前拆体会以 Close 报错现形」**⇒「等所有借用者」改钉可观测处（`ErrStoreLocked`，PA2 有牙）；本轮生产零 diff | 现行 | §5.41 |
| 八十九 | 09-27 | **3.3 工厂能力门开门**：真实调用点清点（工厂仅一处生产消费、分类已单点）＋特征测钉既有承诺；据此**测出并修掉工厂分支永久泄漏 store 写者名额**（新增 `AdoptMemStoreRelease` 复用同一退出槽与 §4.1 守卫；双臂实验：对照 PASS／工厂红→修后双绿）；3.3 不转勾 | 现行 | §5.45 |
| 八十八 | 09-27 | **D-b 根治转绿**（后序构建，修前 0/6→修后 6/6；非确定性缺陷按多次计）；拓扑测量取代 §5.43 的 D-a 机制表述（只换子时 `resident[c]` 连实例都没换）；D-a 仍红（临时解除 Skip 实测后复原）；**主干需先裁决「被宣告的那个代不得在宣告者存活期回收」这一持有协议扩展** | 现行 | §5.44 |
| 八十七 | 09-27 | **3.2 主干的失败契约钉死**（声明式 Skip 当 DoD）：嵌套跳跨发布**静默**服务旧代目标；对照臂推翻我「祖先 face 陈旧」的第一推断 ⇒ 定位 D-a（未变父不重建 face）＋D-b（壳循环 map 序烤进旧子）两条同源缺陷；生产零改动 | 现行 | §5.43 |
| 八十六 | 09-27 | 3.2「关闭后被拒」一条**挖出真实缺陷并修复**：已收敛世代仍交出死执行器＋排空后复登记义务 ⇒ 世代闸门新增具名 `ErrExecClosed`（不登记/不给 runner/派生继承拒绝）＋四处消费点显式化＋环内不重试不报退化保留 claim；另纠正自己对 Inject 的过度指定（实为 `ErrLoopTerminated`） | 现行 | §5.42 |
| 九十 | 09-27 | **3.2 主干＋2.3 联合勾（25/34）**：持有扩展经用户批准（`/opsx:apply` after 建议）——每个可达 owner 执行视图随发布推进（stage→wire→activate）、wrapper 盖 declared＋声明沿代持有（heldBy 不入义务轴）、Run 按声明代装配；过渡壳与 `structuralRebuildOrder` 删除；NestedHop 取消 Skip 转绿（含断言②显式迁移：钉跳回执＝G1 之 C）；root 2×-race ok | 现行 | §5.46 |
| 九十一 | 09-27 | **②工厂公开合同一次迁移落地**：`ToolAgentFactory` 改返回 `*TagentConfig`（构造/发布归唯一路径），封死轮九十引入的 D-f1 孤儿整只构造与 D-f2 工厂委派读陈旧配置；`AdoptMemStoreRelease` 缝随唯一用户删除；3.3 不转勾（有状态工具行待逐腿核验） | 现行 | §5.47 |
| 九十二 | 09-27 | **§2.4 回滚收口（26/34）**：删除专用重建分支，两入口共用候选 overlay（commit 于唯一提交点／abandon 逆序回退）；**实测红**＝后段失败的回滚把未发布 owner 连同租约留在在线清册，修后绿；既有 L3 四测保绿；root 3×-race ok |
| 九十三 | 09-27 | **§4.1 收口（27/34）**：有界返回后由同一尾部完成最终退出（runner／store lease／owner 登记各恰一次）；先判收敛再拆 still-used 资源；**撤回把「永不完成」写成合同的旧断言**并记因；P41/P41b 证判别性；root 3×-race ok | 现行 | §5.49 |
| 九十四 | 09-27 | **§4.2 重估（不改代码）**：多级重入四态在生产 Spawn 入口全部成立 ⇒ 不写新机制；P-PA/P-PB 证每态非空洞；结论与「WAL 重建入口」剩余腿写回本单 | 现行 | §5.50 |
| 九十五 | 09-27 | **3.3「有状态工具」逐腿核验→发现并修掉轮九十去壳丢掉的会话 tracker 重挂**；P-PC 实测「摘掉后全家族仍全绿」证缺口真实且无测可抓；补提交后单点重挂（两入口共用）＋机理性单测；org 级端到端锚待补，3.3 不转勾 | 现行 | §5.51 |
| 九十六 | 09-27 | **3.4 最难一行落地**：真实 ACK＋屏障跨发布⇒settle 抬起的新 turn 用 G2（P-PS 证判别性）；**并记一次险些立案的假缺陷**——settle 实为 user 角色通知回流，旧探测形态对其失明；逐行核实后 3.4 余两行，保持未勾 | 现行 | §5.52 |
| 九十七 | 09-27 | **3.4 行①「远端重试过程中发布」闭合**：发布与 503 重试同步；**该行挖出真实缺陷**——remote-only 声明被 §5.11 门与发布循环误判为「引用未定义」，使此类部署热更/回滚永久 fail-closed；以 §5.44 同一谓词修复（混合可达仍拒），四态守卫单测钉边界 | 现行 | §5.53 |
| 九十八 | 09-27 | **3.4 最后一行「热增后真实数据归属」闭合→本项转勾**：五步同链取证（宿主返回／自己 store 的 agent_output／跨 owner 不可见／真落盘／Close 后注册撤销）；两次红基线均为我方探测形态错误（model 优先级、分区隔离契约），查清后未盖产品；P-PD2 打在真轴证咬合；并把远端行的资源尾部收回同链消除两层拼接 | 现行 | §5.54 |
| 九十九 | 09-27 | **4.2 转勾**：WAL 重建入口的多级重入补齐——org 级跨进程重启 harness（xproc，崩溃形状不手搓记录），态①重启折回 b 自己 board 后经生产 relaunch_task 真跑到深度 2 的 C；态②摘路由后按名拒绝且零执行；P-PW1 摘掉重建入口 owner ⇒ 态①精准红；另记一次意外的前置断言判别证明 | 现行 | §5.55 |
| 一百 | 09-27 | **3.3 转勾**：「换代不失监视」的 org 级端到端锚（三独立进程 boot＋真实 tmux resident 命名会话）；P-MM1/P-MM2 各打监视丢失与重挂缺失；护出两个自身错误：「热更不再重挂」的回归假设被读调用点证伪（未改产品码），以及本测自带的一处真数据竞争；覆盖范围限制写入 tasks 子弹 | 现行 | §5.56 |
| 一百零一 | 09-27 | **P3 · 5.1 前半**：实现句逐句核对（诊断确为单次读取、引用债务确已分报）；删 `TagentAgent.SwapExecutor` 纯转发＋`LiveCMs` 收缩为包内访问器；**普查方式差点造出假事实**——`RecordResidentSession` 是以方法值接给 sink 的，删前改用不带括号的引用检查才救回；残留三个发布入口并存（其中两个已无生产调用方）作为公开契约问题停下上报，5.1 不转勾 | 现行 | §5.57 |
| 一百零二 | 09-27 | **发布入口收敛落地——但先证伪了我上轮的普查**：`PublishExecutor` 实为 37 用户／13 文件的受支持线性化点（我误读 `head -8`），故只删语义不完整的 `cm.SwapExecutor`（换 runner 不换 face）；三接缝测语义不变重指 `PublishExecutor` 并随迁 `fakeRunner`；`PublishExecutor`/`ActivateExecutor` 的线性化重复转入 5.3 具名项。已入 §3 台账 | 现行 | §5.58 |
| 一百零三 | 09-27 | **5.1 转勾**：三条诊断腿落地，并**量出一个次序缺陷**——`applyHotAll` 跑在 commit 前，使结构发布新增的 owner 既无回执也无记录源（要等下次 numeric-only）；移到 commit 后修复。载荷新增 `liveDebt{capturedAt,…}`／`close{initiated,resourcesExited}` 两组，使实时读取与原子记录在键名上可区分；两条锚含非回声三重守卫。另**撤回我上轮的假纠正**（`owner_retirement.go` 一直存在且原文无 `agent/` 前缀），已入 §3 台账 | 现行 | §5.59 |
| 一百零四 | 09-27 | **5.2 转勾**：九行「必测」逐条 grep 对锚后只两处真差集——**运行对象别名**（`kind:` 省略≡显式；此前只钉了配置键别名）与**热增 owner 的记录源**（§5.59 缺陷的持久守卫）；三锚两条变异各证判别性。自查纠出一个空洞：②的第一版在修复前后都会绿，加严为「结构发布当轮即须有回执」才咬住 | 现行 | §5.60 |
| 一百零五 | 09-27 | **5.3 分相与复杂度对照**：修掉一处把"提交"读成"构建"的混计（原 publish 基准在计时区内构造＋每轮轮询，虚高约 9×）；拆出读取/构建/放弃/提交/获取/释放/回收七相并实跑；新结论＝热更主项是 YAML 重解析（≈268µs），只记录不越界优化；身份法对照证明每代仅新建 5 个 face、25 项对象同实例，回滚同价；P-P53b 证其对"少发"亦敏感（P-P53 弱证据如实标注）。5.3 仅余线性化重复项 | 现行 | §5.61 |
| 一百零六 | 09-27 | **5.3 转勾（33/34）**：两条发布入口的线性化合并为一份；合并时发现二者**已漂移**——同 runner 重发时一条推进记录面、一条不推进，故这不只是去重而是纠偏；取文档所载语义，旧 helper 折叠后引用数归零；同表双入口测＋P-P54 证其在钉合并本身；提交相位成本未变。另把顺带量到的 `active==nil` 重发初始 runner 角落作为静态风险单列给 5.4，不顺手改 | 现行 | §5.62 |
| 一百零七 | 09-27 | **§5.4 终门准出，34/34**：三门＋全仓 `-short -race` 零豁免（30 包 ok／0 race）＋两模块 fork Replace 核实＋完整补丁成表＋哲学逐条复核；**终门当场抓到一处此前从未进门禁的测边界错**（`tests/` 的 inbox unlink 紧随收据、断言把先后当同时；单跑 PASS 全包 FAIL → 查到生产顺序后改到有界等待，合同未弱化）；并落地 5.3 移交的 `PublishExecutor` 裁决（保留＋显式守卫，撤守卫即双入口关掉在用 runner） | 现行 | §5.63 |
| 一百零八 | 09-27 | **归档就绪审计**：逐份核对 specs（归档时并入长期基线）点名的 API 是否仍在最终实现里 —— 修掉两处真背离（`swappable-executor` 的 Rollback 挂在已删的 `SwapExecutor` 名下；`workflow-config-compilation` 把已裁定的工厂合同写成「原契约／待裁决」）＋一处代码注释滞后；evidence 的历史提及与 specs 中的「壳」禁令**刻意不动**并说明理由；specs 陈旧名复扫 0 命中 | 现行 | §5.64 |
| 一百零九 | 09-27 | **死代码审计清理**（用户令「review整体实现，清理死代码」）：15 个零生产调用候选逐个甄别＝真死／被取代机制／测专 oracle／公开观测面四类；删 7 个死函数（含**从未接线的 R4 第一版 `computeMemoryFingerprint`**——现行是 changedMemoryAgents/residentMemFP，死的一套靠测死函数本身存活）＋1 死测文件，2 个测专 helper 移位，BeginTurn/OrgDiagnostics 等公开面**刻意保留**并记由；顺带修 TestMonitor33 的负载下观测边界（修前全仓 2/2 红→修后 2/2 绿，合同未弱化） | 现行 | §5.65 |

## 2. 用户裁决台账（全变更，按时间序）

| 日期 | 裁决 | 落点 |
|---|---|---|
| 09-22 | 编排与修复两条线分离；新请求绑编排版本；删 workflow 灰度开关与试验双路径 | §8.1 |
| 09-22 | 图只驱动显式 workflow/v1；默认 agentic 委派保持表示层（option 1，D13） | §8.3 |
| 09-23 | 「同意立案修复上游问题；先看远端是否已修；完成后打独立 tag」→ 远端 v1.11.2 已修，短路 fork | §7.13 |
| 09-23 | 逐项授权：6.3 L1「开始，基于 v1.11.2」；R1「授权：打 tag 并切 replace」（仅 fork 远端，不碰上游 origin） | §6.13/§6.17 |
| 09-23 | 整体 review 后「修订计划吧」「修订 openspec 文档，先不执行」 | §6.0 |
| 09-25 | 哲学纠偏：入口与子 agent 架构一致、各完整 tagent；撤销子 agent 降格/集中任务服务/冻结在途热参/仅文件回滚 | §4 |
| 09-25 | 「以静态任务单为准推进」（任务单固化，防压缩记忆偏差） | §4.5 |
| 09-25 | 解释 A（design D2）：共享非持久核心；派生子调用不入 durable 信封 | design D2 |
| 09-25 | 「先 7.2 再 B-2+②」 | 轮43/44 |
| 09-26 | 「B：真常驻＋配置加载预处理」→ M2 逐调用同构事件环（排除 M1）；D-a 两段式 | §5.7 |
| 09-26 | 7.x 闭合选「补宿主形独立等价测」 | §5.17 |
| 09-26 | 总纲原则（事件驱动/独立 loop/管线框架协调/内或外）→ S3m-c 立项 | §5.18 |
| 09-26 | 「按最理想形态完成修订…一次写对别再返工；冲突以我的表述为准，有疑问或更优设计可提出讨论」 | §5.19 |
| 09-26 | 注入目的地「保持现状，子 agent 不直接暴露用户调用」 | §5.20 |
| 09-26 | retry budget「统一」（两形 persistentTurnRetryBudget=3） | §5.20 |
| 09-27 | 轮九十建议后 `/opsx:apply`＝批准 **① 持有协议最小扩展**（被宣告的那个代在任一宣告者存活期间不得回收，不入义务计数）并落地 3.2 主干 | tasks 3.2 轮九十段／design D8／§5.46 |
| 09-27 | 轮九十一建议后 `/opsx:apply`＝批准 **② 工厂公开合同一次迁移**（ToolAgentFactory 返回 *TagentConfig，构造/发布归唯一路径；否并行双注册面） | tasks 3.3 轮九十一段／design D1／§5.47 |

## 3. 撤回与自纠台账（防「历史绿」误读；读历史节前必看）

| 撤回/自纠 | 原声明 | 更正 | 轮 |
|---|---|---|---|
| **31/31 收口整体撤回** | 轮39「全 31 项收口／FINAL_RACE_EXIT=0」 | 轮41 明示为「任务单已作废的历史过度完成声明」，不继承其证据；31 项中 14 项被 09-25 修订重开 | 39→41 |
| U-1「必现」措辞 | 轮4–9 称委派 race「必现」 | 定量 10 迭代 1 中；仍为实质阻塞但非必现 | 10 |
| **6.4 勾检撤回（假完成）** | 轮78 称「push→pull 反转完成／J6 六场景各有绿锚」，并「读码纠正 P0：不存在独立 spawner TTL 权威」 | **该纠正为误**：grep 只覆 `agent/`＋`build_agent.go`，漏了 root——`ActionTool.SetDefaultTaskTTL` 三处 push 确在（`tagent.go:713/1021`、`build_agent.go:648`）且 `declarative.go:148` 在 spawn 时读它定 `spec.TTL`＝绕过唯一记录的第二份 TTL 真值（非原子字段，reload 写／业务 turn 读）。J6「后续新任务实际 TTL」仅测到 manager 轴，spawner 轴无锚。6.4 重开为 `[ ]`；已落三面反转不重做 | 79 |
| **「单飞合并洞已关闭」撤回** | 轮82 称 `sweepRetirements` 的「本轮有退役则再扫一遍」已关闭级联合并窗口；同轮 P3 探针（撤循环）单线程仍绿，我据此判该循环「未被证明必要」 | **两点皆错**：全量 root `-race` 复现级联停住（菱形锚 line 90 `Condition never satisfied`，套件内稳定、单跑绿）——循环只在「本趟有退役」时续扫，趟间落下的通知照样丢；且「锚不复现」被误当成「洞不存在」。改为排空尾「放门后再自觉一次」（`retireablePending`），循环撤销；复验＝2×全量 -race ＋ 11 锚 `-count=5 -race` | 82→83 |
| **「PublishExecutor 只剩 d6 测」撤回** | 轮一百零一称 `cm.PublishExecutor` 与 `cm.SwapExecutor` 同为「无生产调用方的轮九十前入口」，并据此建议整体折叠删除 | **前判对、后判错**：`PublishExecutor` 实有 **37 处调用、跨 13 个测试文件**，且文档称其为「organization version switch 的 ONE linearization point」——我 grep 后只读了 `head -8` 的前几行便概括。裁决收窄为只删 `cm.SwapExecutor`（3 个测试用户、无生产用户、只换 runner 不换 face）；`PublishExecutor` 保留，其与 `ActivateExecutor` 的线性化重复转入 5.3 具名项。**普查必须数完，不能截断预览**（同轮 §5.57 的「方法值漏查」是同一族错误：观测手段先于结论出错） | 101→102 |
| **「`agent/owner_retirement.go` 不存在」假纠正撤回** | 轮一百零一称 5.1 代码范围指向一个不存在的文件，并据此在 tasks 5.1/5.4 两处写下“纠正” | **该“纠正”是凭空造的**：本单原文写的是 `owner_retirement.go`（无 `agent/` 前缀），文件在仓库根存在（`retirementLedger.diagnostics()` 所在）。链条＝脑补前缀→按其 grep→查不到→断言指针失实→写入工件。**先造错再“纠正”比原错更危险**（形似已核查），故单独立行。轮一百零三已撤两处文本并加核查规矩：下“不存在/零用户”结论前，搜索串必须取自被引文本原文，且读到计数为零为止 | 101→103 |
| **去壳发布丢了 tracker 重挂（自纠）** | 轮九十称主干「三面同落」、过渡壳消亡后发布路径完成 | 主干让已存在 owner 不再经 `wireAgent`，而**会话 tracker 重挂只写在 `wireAgent` 里**（其注释本身规定「换代 ActionTool 后…须重接」）⇒ 3.3「已纳管任务不因工具换代失监视」这条既有规则在发布路径上静默失守。**实测缺口非假设**：P-PC 摘掉重挂后，热更／委派／退役／回滚／退役／deshell／lifecycle 全家族 `-race` 仍**全绿**——没有任何测会抓到它。轮九十五补重挂于唯一提交后时机（`activateOwnerGenerations`，两入口共用）并以 `tool/action` 单测钉住失效机理；org 级端到端锚仍待补（需真实 tmux，同既有 tmux 族） | 90→95 |
| 轮七 race 门 | 该族 `-count=2 -race` ok | 概率门运气；轮九同命令 FAIL（U-2，4/20） | 9 |
| 2.3「acquire 后立即登记」 | 自报已闭合 | 从未实现；BeginTurn 注释断言错误；补登记（183ns/2allocs） | 9 |
| 「type:memory 不落盘」「冷启动已绑投影」等 | 注释/台账声称 | 均失实（M-1/W-1），已改 | 10/12 |
| 均值短锁证明 | `require.Less(per,10ms)` 以耗时均值当正确性门 | 撤断言，改阻塞屏障结构证明 | 17 |
| HEAD 可编译 | 轮1–9 各门全绿 | 干净 HEAD `vet` 红（未跟踪 wf_facts.go 断链）——门禁方法失误，新增④号口径 | 10 |
| 5.3 已过门 | 先前各轮 `./agent -race 0` | 轮38 发现 `executor_perf_boundedness_test.go` 不可编译（在它加入前成立），净零生产改动修复 | 38 |
| OnBatchRetire 抑制 OnSettle | 轮51 过度推断 | 仅 retirement 批路径抑制；正常单 settle 走 onSettle（探针证实） | 52 |
| harness 猜 request 形状 | 轮59 首版按 RoleTool/ToolCalls 判轮次 | 框架把工具结果呈 user 角色、剥 assistant ToolCalls；改非 system 计数（表示无关） | 59 |
| c.1 首笔判据 | `quiescent`（未绑定→false） | 手搓 ta 无 registry→壳恒阻塞 Pull（5m hang）；改 `awaiting`（绑定∧pending>0） | 64 |
| d12「不动」行 | design 清理表 | d12 于 c.3 按计划测修；表行已补正 | 67 |

## 4. 现行基准：同构 tagent 事件协作修订（2026-09-25 第二次，tasks.md 引用的「现行基准节」）

### 4.1 授权与纠偏
用户纠正：按设计，子 agent 与入口 agent 架构一致，各有事件总线，能像入口一样以自己的任务管理器处理自己的任务；输入都走各自事件管线，差别主要是输出交给谁；「无状态调用」由后续 session 自然解决。当日仅修订工件（proposal/design/tasks/6 delta/elimination-inventory/evidence），源码测试依赖索引数据远端不动。

### 4.2 静态依据
`prototype/agent.go` 无主子类型之分；`NewTagentAgent` 每实例建 bus/projection/TaskManager、OnSettle 发回本实例 bus——同构基础已在；脱节在接线：`session.go::Run` 建私有 CM 直调 RunFlow 绕过本 agent 事件消费、CM 未接本 agent taskController。

### 4.3 RV1–RV7（上轮审阅映射，tasks.md 头部引用）
| 编号 | 当前代码路径与后果 | 消除任务 |
|---|---|---|
| RV1 | 新 owner 在私有缓存，shell 只查在线 resident 后回退 entry store，工具使用错误资源域 | 2.3 去壳、统一候选解析域 |
| RV2 | 祖先 binding 持有≠子 owner 自身 CM 活跃；未调用的合法子依赖不在 resident 义务 | 3.2 owner 使用权、4.3 单一生命周期 |
| RV3 | rollback 空 rebuilt 缓存递归重建在线共享子；map 遍历≠获取顺序 | 2.3 同一事务、2.4 仅换配置输入 |
| RV4 | 任务重入只扫祖先 lease 直接工具表，A→B→C 中 B 重入 C 被误拒 | 3.2 owner 执行视图、4.2 正确工具集 |
| RV5 | resident 与 shell 各有 hotSnapshot/liveCMs，更新 resident 而子调用读 shell | 6.4 消费边界读取 |
| RV6 | remote-only 可合法加载，reachableAgents 仍当本地新增 owner | 3.3 单一分类 |
| RV7 | Close 超时跳过 store release；迟后归零只关 runner；缓存 Close 结果阻断最终退出 | 4.1 同一尾部、4.3 完成后撤登记 |

### 4.4 撤回与计划调整
撤回未经采纳推论（子 agent 纯定义/集中任务服务/一次性执行架构/冻结在途热参/仅文件回滚）；五热参、prompt 热读、显式完整配置回滚维持。tasks 增 7.1/7.2/7.3（34 项）；同日第一次修订（架构复核）另识别上表 RV 并保留 31 任务 ID、重开 14 项。验证：`--strict` 0；`instructions apply` total=34/complete=17；跟踪源码 diff（59 文件）与 staged（0）不变，未跑 Go。

### 4.5 任务单固化（同日第三次，用户指令「以静态任务单为准推进」）
tasks 头部增权威与记忆偏差防护、术语与事实卡、防跑偏八条；每个 [x] 补保留合同、每个待办项红基线落当前真实失败行为（B 工具错落 entry store／回滚空缓存重复建 Q／G1 未调 B 即删后调用失败／B 重入 C 被误拒／remote-only 结构热更失败／热增后 numeric-only 新调用读旧值／Close 超时 released 恒零——均「先钉红」）。34＝17 保留＋17 待实施。

## 5. S 阶段证据（现行 34 项计划，轮四十一～六十七）

> 各轮门禁除特别注明外均含：`go build ./...` 0、`go vet` 0、改动文件 `gofmt -l` 净、全 `./agent/... -race` ok（0 FAIL/0 panic/0 DATA RACE，典型 77–82s）、root `go test .` ok、`openspec validate --strict` 0；下文只记差异项。

### 5.1 轮四十一：6.7 测试生命周期根（取代轮二十 PID 根方案）
- 场景/红：`testStore` 按 PID+name 取根→同进程多用例/`-count` 重复互相见字节；`$TMPDIR` 遗留 488 个 `tagent-test-stores-<pid>`。临时探针测 RED_EXIT=1（同名 B 看到 A 的 marker）。
- 修法：`testStore(t testing.TB, name)` 以 `t.TempDir` 建根＋`sync.Map` 用例内记忆＋`t.Cleanup` 回收；6 文件 YAML 构造器与 package var 统一改接 TB。
- 结果：新合同测 `TestTestStore_IsolatesPerCase` GREEN；受影响 34 项回归 REG_EXIT=0；focused `-count=3` 0；整包 `go test .` 0（7.4s）；前后目录对照零新增污染；TMPDIR 计数 488→488（历史遗留不擅删）。
- 边界：未跑 `-race`（归各实施项与 5.4 门）；已跟踪 `hottest-*`/`own-*` 取消跟踪待独立索引授权。

### 5.2 轮四十二：7.1 去隐式传参（activeBus＋外部上下文按调用装配）
- 场景/红：`Run` 曾 `setActiveBus/restorePersistentBus` 改写共享 `ta.activeBus`；`IngestExternalEvents` 裸写读清 `ta.pendingExternalEvents`——并发委派数据竞争＋跨调用污染。临时 8-goroutine 测 `RED_EXIT=1`（2×DATA RACE，已删不留主动 racing 用例）。
- 修法：私有 bus 只经 `cm.bus` 携带，`activeBus` 构造期一次性置 persistentBus；外部上下文 RuntimeState→本地 slice→`applyExternalContext` 纯函数；legacy Ingest 经 `drainPendingExternalEvents` 原子取走（单次交接语义保留）。
- 结果：`external_context_isolation_test.go` 三测定向 `-race` PASS；全 `agent -race` ok 79.7s；root 0。
- 边界：私有 CM/RunFlow 直调的消除属 Step B；家族(3) session 留 7.3。

### 5.3 轮四十三：7.1 B-1 提取 processTurn＋7.2 首片 D3 spawner 归属
- B-1：`runEventLoop` 拆薄壳＋`processTurn(ctx,cm,events) turnDisposition`（冻结/丢弃→durable 门→BuildInvocation→投影/trigger/metadata→turn lease→RunFlow 有界重试→outcome 归约→finish/receipt/ack→idle 锚）。纯搬移无红测，以全 `agent -race` ok 76.97s 为证。未勾 7.1（Run 仍直调 RunFlow；B-2 阻塞查明＝AcquireLease 非 ctx 感知）。
- D3.3（红→绿）：`Run` 私有 invCM 无 taskController→父 spawner 遮蔽，B 的任务落 A manager。红：`d3_spawner_ownership_test` 改前 B manager 0 项（RED_EXIT=1）；修：`invCM.taskController = ta.taskManager`（nil 守卫防 typed-nil 接口 panic——首版返工实录）。绿＋全量绿。
- 边界：越窗 ACK→输出、贯穿门未做。

### 5.4 轮四十四：7.1 B-2 Run 经共享 processTurn
- 修法：输入包成单个 volatile external_input 事件交 `processTurn`；无 claim→durable 全惰（解释 A）。
- 两处非显然发现（既有回归测暴露）：① 执行器选错——初版传 invLease 令子调用跑 owner runner/投影（RequestOrdering 红）；正解＝私有 invCM 无 orgReloader，`cm.BeginTurnLease()` 即其自身构造代，与旧 RunFlow standalone 逐字等价→撤销 lease 形参。② 同步子调用被重复调模型（EmptyFinalResponseCompletes 红 10s）→ 按「执行配置可不同」增 `maxRetries` 形参（常驻 3／子调用 0）。
- 结果：定向 EXIT=0；全 `agent -race` ok 79.185s；root ok 7.069s。

### 5.5 轮四十五：S1 关联把手（behavior-neutral）
- `metaKeyInvocationID` 入 controlMetaKeys（永不入 meta_*/模型）；`newDelegationEvent` 盖 invocation_id。`d4_correlation_test` 三测（携带/nil 安全/控制键不外泄）。行为中性＝全量绿（agent -race ok 77.781s）。

### 5.6 轮四十七：code review（W-1/W-2/S-1）
- W-2 真缺陷：Run 归一化 `Content==""` 整体替换丢 ContentParts（图片-only 输入被抹）；修＝条件收紧 `&& len(ContentParts)==0`。
- W-1：B-2 使空输入命中 durable 空批跳过→通道零事件关闭（不可解释空结果）；修＝Run 边界显式拒绝空委派输入（早于任何 CM/lease/bus 创建）。经核无既有测试以空输入调 Run。
- S-1：maxRetries clamp（retryDelays 定长 3 防越界 panic）。
- 测：`d2_run_input_contract_test` 两测（媒体-only 不丢／空输入显式拒绝）。全 `agent -race` ok 77.853s。

### 5.7 轮四十九：S3 被调方执行模型裁决 → M2（纯工件）
走查关键发现：`execBinding.face` 本就逐 owner、`subagentWrapperIn(b.face.Tools)` 已实现按发起代解析；design 线 120 冻结「不共享可变 CM」→ M1（单共享 owner 消费者）被既有隔离不变量排除。**用户裁决：采纳 M2（逐调用同构事件环）＋D-a 两段式 ACK→补最终**。推论：无进程级常驻消费者→S2 泄漏门与 3.4 退役耦合基本消解。子决策：D-b 低风险默认、D-c＝S4、D-d 留裁。工件重构 S1(✅)→S2m→S3m→S4。

### 5.8 轮五十：S2m 越窗归属路由打底（behavior-neutral）
`withInvocationID/invocationIDFromContext`（ctx 线程，仿 withCallProjection，空 id no-op）；`extractDelegationInvocationID`；Origin stamp 并入 `cp`（与 trace/trigger 同构）。红证：临时禁 stamp 行 `d5:53` FAIL 还原绿。模型安全：invocation_id∈controlMetaKeys，回流 turn 不外泄。全 `agent -race` ok 77.290s。

### 5.9 轮五十一：S3m-a 越窗 settle 路由
`settleSinkRegistry`（register/unregister/route 非阻塞）＋`deliverTaskSettled`（route 成功即返，否则回落 persistentBus——entry owner 逐字节不变）＋`ta.register/unregisterSettleSink` 接缝。踩坑：真 ta 的 OnBatchRetire 抑制逐条 onSettle（retirement 批路径），集成测改循 bare-tm 序列。行为中性实证＝全量绿（79.133s）。端到端留 S3m-b ②门。

### 5.10 轮五十二：S3m-b 尝试→② 门红→完整回退（不 ship racy 核心）
实现 inflight==0 终止 tail；② 测红＝**task 先转终态、settle 后投递**——inflight==0 非「无更多在途 settle」的安全信号（终态置位 happen-before 投递）。处置：回退 session.go、删红测、保留 S3m-a/S2m（绿、中性）；复验全绿（79.3s）。正确解方向＝投递对账或投递先于终态（→轮五十三定案）。

### 5.11 轮五十三：D-b 投递对账原语（中性待用件）
registry 加 `pending/noteSpawn/quiescent`（route 成功后受保护递减；无 sink 不建账）。确定性无竞态测锁「terminal-before-delivery 竞态被消除」。生产无 sink 注册→inert。全量绿（78.6s）。

### 5.12 轮五十四：code review（W-1~W-4/S-5/S-6）
W-1 契约校正（回落 bus 不回退 pending→注释改 GUARANTEE+PRECISION＋S-6 测）；**W-2 真缺陷**：`newTaskSettledEvent` 整份拷 Origin→回收 turn 误继承子调用 id（未来错投递）→`evt.Source != SourceTask` 门＋d5 断言；W-3 补顺序非粘性测（并发留 d10）；W-4 顺序契约入 doc；S-5 clamp Warnf。全量绿（78.5s）。

### 5.13 轮五十五：W-1 真正修复＝可靠 append 投递
sink 改 `events 队列+cap-1 notify`；route append 恒成功（无需接收者）；`tryFinish` 同锁取队∪判 pending（原子）；`wait` notify∧ctx。W-4 由构造消除。`ReliableAppendNeverStrands`/`TryFinishAtomic` 新增。行为中性（生产无 sink）。（注：队列/notify/tryFinish/wait 已于轮六十四 c.1 被绑定表+bus 投递收敛取代，见 §5.21 清理。）

### 5.14 轮五十六：S3m-b.2 环本体（M2 核心行为变更，② 门首绿）
`countingSpawner`（spawn 前 noteSpawn；inline `res.Settled` 则 voidSpawn）；`Run` 注册 sink＋初答 `processTurn(...,0)`＋`runInvocationTail`（tryFinish/wait 循环续写，persistentTurnRetryBudget；close(ch) 在 tail 后）。② `TestS3mB_WindowCrossingContinuation` 真 Run→真 OnSettle→route→续写，first→continuation→关通道；containment 测锁非异步子调用＝逐字节旧行为。全 `agent -race` ok 79.7s。（注：tail 本体已于 c.1 被共享壳取代；行为由 d8 持续锁定。）

### 5.15 轮五十七：S4/D-c 契约测＋I1 并发不串（纯测试片）
`d9`：调用方 cancel→通道必关（仅凭 ctx）＋taskManager 仍非空（不级联不 Close B）＋注销后晚到 Emit 不 panic；短超时 ctx→deadline 关通道。`d10`：8 路并发 invocation 路由隔离（provenance 前缀全自身、屏障各自 quiescent）；同 callee 不同 handle 不合并（各恰 50）。W-3 闭合。root 首跑 1 次 FAIL→5 次复刻不可复现，判既有 wall-clock flake，如实记录未掩盖。

### 5.16 轮五十八：family-3 Run 侧 session 去共享写
端到端核验后删 `Run` 头 `setSessionContext`（对委派路径为死写，仅污染共享态）；`invCM.SetUserIDSessionID` 改局部值。红→绿：`d8 RunDoesNotStompSharedSessionContext`（sentinel 不被覆）。`lastSessionID` 字段保留（entry/StartLoop 单消费者，非并发面）。

### 5.17 轮五十九：7.2② 被调方并发端到端（d11）
test-infra 处置（实证非猜测）：`sequenceMockModel` 共享有状态不可并发→`echoModel` 纯函数式（**非 system 消息数 ≤1→toolCall，>1→final echo 末条**——框架把工具结果呈 user 角色、剥 assistant ToolCalls，按角色判不成立）；probe 空 Key（避幂等 dedup 塌一）；cancelable ctx 兜底。绿证：inv-A/inv-B 各收初始答＋恰 1 条各自 tag 续写，tags 互异＝无串。全 `agent -race` ok 77.4s。7.2 暂不勾：宿主形一侧待独立等价测或 3.4（预告替代路径）。

### 5.18 轮六十：用户原则输入 → S3m-c 立项（零码改）
用户总纲：「tagent 是事件驱动；一次输入事件的输出目的地都应当是确认的；实例＝一个个独立 agent loop；输入输出管线框架协调，要么内部要么外部」。逐条核验发现 S3m-b 三偏差（旁路队列/定制环/首答直调）→ S3m-c 管线收敛立项。

### 5.19 轮六十一：S3m-c 定稿＋高维审计（零码改）
用户指示「按最理想形态完成修订…一次写对…冲突以我的表述为准」。定 I-1~I-4 不变量、理想形态（初始事件与 settle 同管线同环）、清理清单表、高维审计（注入目的地同类问题＋maxRetries 讨论项）、c.1/c.2/c.3 三片。

### 5.20 轮六十二/六十三：两项讨论裁决（零码改）
(a) 注入目的地——用户：「保持现状，tagent 的子 agent 不直接暴露用户调用」→ InjectMessage 恒发 persistentBus 判为按设计正确（用户输入唯一入口＝entry 管线）。(b) retry budget——用户先要求「先说清楚问题」，查清两类重试（传输级退避；退化轮恰一次）与红测真相（loopMockModel 耗尽挂起，非语义不可行）后裁决「**统一**」＝两形 persistentTurnRetryBudget。

### 5.21 轮六十四：S3m-c.1 芯片（红→绿）
红＝`d13_pipeline_convergence_test`（build-fail：旧 append 队列无法表达 bus 投递契约）。实现：registry 收缩为绑定表 `invID→*EventBus`＋对账（删 events/notify/wait/drain/tryFinish）；route＝查绑定→`bus.Publish`（**publish 先于递减 pending**，锁外发布防死锁）；`runAgentLoop(ctx,bus,cm,loopSpec{invocationID})` 共享壳（entry 包装 `runEventLoop`；invocation 终止＝`awaiting` 判据：绑定∧pending>0 才阻塞，否则 TryPull 空即退——修正首笔 `quiescent` 缺陷：手搓 ta 无 registry 曾 5m hang）；budget 统一（processTurn 删 maxRetries 形参）；删 `runInvocationTail`。测试迁移：删 d6 TryFinishAtomic/d7 ReliableAppendNeverStrands；d6/d7/d10 迁 bus 语义；`EmptyFinalResponseCompletes` 补第二空响应。全 `agent -race` 85.7s 仅 d12 红（c.3 预期）；root `go test .` ok 11.0s。

### 5.22 轮六十五：S3m-c.2 输入入管线（单管线，行为保持）
删首答直调快路径——`invBus.Publish(inputEvent)` 由共享壳首迭代消费；`firstCtx=withInvocationID(ctx,invID)` 令壳持身份贯穿每一轮（续写批次经 W-2 门无 id 也不丢归属；`withInvocationID` 空 id no-op）。新契约测 `d14 MultiLevelWindowCrossing`（两级越窗三答同通道按序）：c.1 即绿→c.2 保持绿＝行为保持（纯结构变更无可红测，如实以绿前绿后+全量红线验收）。全 `agent -race` 85.99s 仅 d12；`-count=3 -race` 确定性绿。

### 5.23 轮六十六：S3m-c.3 宿主形门＋7.x 转勾（S3m-c 收口）
d12 修复＝根因确为「从未调用 InjectMessage」，补注入驱动常驻 owner turn；`-count=5 -race` 绿（0.05s）。宿主形全链（InjectMessage→persistentBus→runEventLoop→spawn→无 invID 晚 settle 回落 bus→续写→outputCh）与被调方形经同一 `runAgentLoop`+`processTurn` 证主子同构。**7.1/7.2/7.3 按既定验收转 [x]**（逐点映射见 tasks.md 各条；7.2 走轮五十九预告的「宿主形越窗独立成测」替代路径，跨发布/多代正确性仍归 3.4；7.3 session 存储为设计内非目标）。**全 `agent -race` 整包首次全绿 82.1s**；root ok 9.4s；进度 18→21/34。

### 5.24 轮六十七：执行回顾审计＋6.4 范围结论（零码改）
对照用户昨晚要求审计：总体遵循；四偏差如实列示（c.1 首笔判据/c.2 无字面红测/design d12 行陈旧→已补正/6.4 建议轻率）。6.4 读码结论：现制＝`applyHotAll→ApplyOrgHotParams` 单一提交点四路 push（resident 可变压缩态/Set*TTL/hotSnapshot 播种/liveCMs 扇出）；6.4 需反转消费模型并改 `compress.SmartCompressor` 公共契约＋重写 4+ push 语义回归测；数据源与 2.3 耦合。呈 Order A（核心簇专项设计后切片，倾向）/Order B（6.4 消费者侧先行）待裁。

### 5.25 轮六十九：工件三件套防跑偏重写（零码改；用户令「结合认知重写工作计划」，续令「proposal和design部分呢」）
**触发**：用户在轮六十八重整 evidence 并要求「去伪存真」陈述设计理解后，令重写工作计划防跑偏，并追问 proposal/design 同步——Order A 的 P0 前置（规划先行）就此落实于全部三件工件。
**重写内容**：
- **tasks.md**：①新增**核心思想卡**（一句话本质＝消灭第二套机制＋四层思想）；②新增**判例卡 J1–J14**（易混错读→正确判断→锚），条目以 J# 引用，与防跑偏总则八条、evidence §0 门禁口径构成三层自查；③执行顺序重排 **Order-A 程序**（P0 设计定稿→P1 2.3→3.2→6.4→2.4（3.3 并行窗口）→P2 4.3→4.2→4.1→P3 验收矩阵），附 mermaid 与 Order-B 备选注；④13 待办逐条重写剔陈旧（3.2 已成果标注勿重做；4.2 旧阻塞被 M2 消解→先重估；6.4 全重写为 push→pull 目标＋现状四路 push＋必改回归测）；已完成 21 项保留合同原样，S 阶段分解压缩为收口注。
- **proposal.md**：头部改「三件套权威＋现状口径（21/34，S 收口，整包绿基线）」；Why 首加核心一句；What Changes 九条加落地状态标（✅S 阶段/待 P1/P2）；Impact 剔「本轮只改工件」陈旧授权句；Capabilities 原样。
- **design.md**：①头部权威注更新＋「核心思想」镜像节；②**全局不变量节**：I-1~I-4 自 S3m-c 子节提升为设计级＋配套生命周期不变量（pin-fresh/回滚新代/所属域/draining/投递对账）；③**消撞号**：S3 选型门 I1–I5 更名 **V1–V5**（仅更名，防与全局 I-1~I-4 混淆）；④D1–D9 保留原文＋落地状态标（D2✅/D3 归属✅/D4✅范围 内/D7 待 P1/D8 待 P2）；⑤「验收与实施顺序」改 Order-A（原 S 先行序陈旧）；⑥S3/S3m-c 节保留并标收口（S3m-c 不变量定义改为指向全局节，避免双定义漂移）；⑦新增「核心簇理想形态——P0 待产」占位节（定稿清单＋验收基准＝全局不变量＋J6/J9）。
**验证**：`grep -c` tasks 勾选＝21 [x]＋13 [ ]；`openspec validate --strict` 0；`instructions apply` total=34/complete=21/remaining=13/state=ready（不变）。零码改、零提交。

### 5.26 轮七十：P0 设计定稿（纯工件；Order-A 首轮）
**前置读码实证（设计锚，非想象）**：`orgGeneration{seq,fingerprint,cfg}`＋coordinator{revision/applySig/两时间/prev ring-2}（org_hotreload.go:31/50）；`execBinding.face` 逐代且随 refs 释放（exec_lease.go:89）；push 热路径生产调用唯一＝`cm.ApplyOrgHotParams→ContextCompressor.ApplyHotParams→SmartCompressor.ApplyParams`；**`liveCMs` 有生命周期账消费者（owner_obligation.go:54 Invocations 轴）→ 集合保留、仅删其配置订阅用途**（修正 P0 前草表中「liveCMs 删」的笼统表述）；裸构造 ta 边界按 c.1 教训显式枚举（NewTagentAgent 恒装源，无无源状态）。
**定稿内容（design「核心簇…P0 定稿」节 §1–§9）**：①`appliedRecord`（不可变，含 Draining 条目携末值；`currentRecord()` 唯一读点，读闸门与提交短闸门分离）；②owner 只读函数面 hotNums/taskTTLs/execFace（recordView 单写者轮转；hotSnapshot 两步退役 S-C 单写者→S-E 删）；③compressor `WithHotSource` pull 契约（push 入口退出热路径；standalone 静态源同源）；④TaskManager/ActionTool TTL spawn 现读；⑤清理清单八行（含测迁移＝源旋转语义、显式判读）；⑥pin/fresh 语义表五行（J6 操作化）；⑦事务与候选域（构造拆分/身份域/单闸门 commit/回滚经同事务）；⑧**P1 切片 S-A~S-F 逐片红锚**；⑨边界与上报条件（ApplyParams 另有依赖/liveCMs 另有订阅者/混合形态→停分支上报）。
**tasks 同步**：执行顺序表 P0 行转 ✅ 轮七十；P1 行改切片序（S-A→S-B 可联合→S-C→S-D→S-E→S-F，3.3 工厂门 S-B 前并行）。
**验证**：`openspec validate --strict` 0；勾选 21/13 不变；零码改、零提交。**下一步＝P1/S-A（红锚：热更修改已有 agent 时 TagentAgent 构造计数＝0）**。

### 5.27 轮七十一：P1/S-A 构造拆分＋entry 去壳（红→绿）
**实现**（三件）：① `buildAgentDFS` 三分＝store 半＋`assembleAgentConfig`（中段原位：prompt/model/tools/decorators/治理/meditation→TagentConfig；工厂产物经 `assembledAgent.factory` 整只返回——3.3 门未落）＋`wireAgent`（尾段：NewTagentAgent＋post 接线）；② `agent/face.go`：`faceFromConfig`/`BuildExecutionFace`（与 `newContextManagerFromConfig` 同一拼法；运行时句柄留零——`buildExecutor` 恒以常驻 cm 覆盖 MemPlugin/SessionSvc），`TestBuildExecutionFaceMatchesCMFace` 逐字段锁等价（J11：不靠回声）；③ `buildAgentFace`（壳语义借 store＋复用中段→face 直出；故意不建 DegradationMgr——face 无该字段、壳上的本就是死重）＋reloader/doRollback 两路径改造（faceCache＝resident∪新增∪已变 agent 过渡壳；face 的 ActionTool 接线归**常驻** entry——修正旧壳僵尸接线，D1 正确落点）；`TagentAgentsConstructed()` 计数器（兼 5.3 复杂度对照数据源）。
**红→绿（四锚，`org_deshell_test.go`）**：entry-only 变→构造 0（终态）；已变子 agent→恰 1 过渡壳（终态 0 属 S-D）；热增→恰 1（J2 去壳≠去能力）；entry-only 回滚→0。**过程红实录（三处实施发现，非虚构）**：首版过渡壳循环传含目标的域缓存→`buildAgentDFS` 缓存优先命中常驻旧实例→换代替换被吞（ChangedSubAgent/HotAdd got 0、Entry got 1）——修＝构建时排除目标自身（与旧壳传空缓存同理）；entry 未排除出循环→entry-only 变化误建 1 壳——修＝entry 恒走 face 直出；fixture 两缺陷（sub3 未被引用不可达＝非热增；模板行尾 tab 致 YAML 解析拒绝）如实修正非绕过。
**门禁**：`go build ./...` 0；`go vet . ./agent/` 0；改动文件 gofmt 净（face_test 首版 -w 归正）；全 `./agent/... -race` **五包 ok（agent 80.377s）** 0 race；root `-race` ok 15.503s；tests -short 2 包 ok；wechat 三门 0；Deshell+热更族 `-count=3 -race` ok 2.701s；`--strict` 0。
**未竟**：S-B 域＋事务（2.3 保持 [ ]，S-A 仅首片）；过渡壳待 S-D 消亡；工厂 agent 仍整只构造（3.3 门）。
**状态**：21/34 不变（片内推进不勾顶栏）；下一步 P1/S-B（红锚：B 工具错落 entry store／回滚重复建 Q／屏障下半提交可见）。

### 5.28 轮七十二：P1/S-B 有序责任表（红→绿）
**红锚前置核销（三锚现状，如实记录）**：①「B 工具错落 entry store」——已被前序轮＋S-A 消解：热增 B 自有 store 有测锁（`TestOrgHotAdd_NewAgentMayCarryItsOwnMemorySection`、`sub2 owns its own store`），S-A 后已变 agent 走过渡壳借常驻 store，`buildAgentDFS` 的 entryMemStore 回落仅剩 resident 未命中的防御分支；②「回滚重复建 Q」——轮三十四径向重取＋rebuilt 缓存已修；③「屏障下半提交可见」——`orgCommitBarrier` 屏障测已锁（轮三十四①）。**S-B 真实剩余增量＝有序责任表替代差集推断＋reload/rollback 合入同一事务结构**。
**红（build-fail 型，先钉）**：新测 `org_candidate_txn_order_test.go::TestTxn_RefusedCandidateDiscardsInReverseAcquisitionOrder`——`orgLastDiscardOrder` 探针不存在＝**无序清理不可证**（map 遍历序恰是它历轮存活的原因），`go test` RED（undefined）。fixture：热增 aaa_probe＋zzz_probe（排序确定获取序），zzz 的 memory path 指 `/dev/null` 确定性中途失败——候选拥有两个可回退责任。
**实现（三件）**：① 新 `org_candidate_txn.go`：`candidateTxn` 责任表（acquire 先记后判错/幂等；nil＝失败父仅登记；discard 逆序＝撤登记→复位 residentMemFP→Close 建成者）＋TEST-only `lastDiscardOrder` 探针（同 `TagentAgentsConstructed` 家族，生产零读者）；② reload 结构分支：`ownerNames0` 差集推断＋`rollbackAdds` 闭包**整体删除**，改 `txn := newCandidateTxn(rc)`＋defer discard——失败父显式 `acquire(aname, nil)`；③ rollback `rebuilt` 半成功清理：map 无序 `for range` → 排序获取序＋`rbTxn` 逆序回退。`ownedAgentNames` 生产调用仅剩诊断快照（`SetStoreOwnerSnapshot`，合法保留）。
**绿**：两测 PASS（逆序 `[zzz_probe, aaa_probe]`＝失败父部分登记先撤、唯一建成者后 Close；owner 表零残留）。既有 R01 泄漏测/屏障测/Deshell 族不改一字全绿。
**门禁**：`go build ./...` 0；`go vet` 0；gofmt 净（txn 首版 -w 归正）；root `-race` ok 14.159s；Txn+Deshell+热更族 `-count=3 -race` ok 2.473s；全 `./agent/... -race` **五包 ok（agent 80.668s）**；tests -short 2 包 ok；wechat 三门 0；`--strict` 0。
**未竟（2.3 仍 [ ]）**：S-C 记录（appliedRecord/读闸门/recordView）；「reload/rollback 合入同一 prepare/commit/discard **类型**」仅完成回退语义统一（txn 共用），完整事务封套待 S-C 随记录一起定形。
**状态**：21/34 不变；下一步 P1/S-C（红锚：热增→numeric-only 后真实新调用读旧值；屏障下半提交不可见）。

### 5.29 轮七十三：P1/S-C 前半——锚1 热增路径专项钉住（绿＝核销）；appliedRecord 本体留待下轮
**锚1 核销（机制已在，专项测补位）**：热增 B 于结构发布提交点并入 resident → 后续 numeric-only 的 `applyHotAll` 以 `reachableAgents(freshCfg)` 为域（含 B）→ 四路 push 覆盖 B → 快照轮转含 B 新值 → B 新调用播种新值。新测 `TestSC_HotAddedAgentNumericOnlySeedsNextCall`（`org_sc_record_test.go`）：基线（无 sub3）→ 结构热增（max_tokens 4096，gen=1）→ numeric-only（8100，gen 冻结）→ 断 `residentCacheForTest(entry)["sub3"].OrgBudgetLine()==6480`（=8100×0.8，真 compressor 消费值）。**过程红实录（断言形状错，如实记录）**：首断 8100 vs 实际 6480——BudgetLine=maxTokens×threshold 公式，红源自断言错非机制缺；修正断言后绿（勿误读为「缺陷已修」——机制本已覆盖，本测为其热增路径钉住回归）。
**锚2 现状**：`orgCommitBarrier` 屏障测已锁「半提交对并发读不可见」（轮三十四①，测 resident 表/生效面/序号三面）；appliedRecord 尚不存在故「记录面半提交不可见」随本体落地时同闸门锁定。
**边界（诚实）**：S-C 本体（appliedRecord 结构/`currentRecord()` 读闸门/recordView 轮转/hotSnapshot 单写者化）为跨 `org_hotreload.go`(527行)+`tagent.go`(1161行)+agent 侧的大改，本轮上下文余量不足以安全完成「先红后绿＋全量门禁」，按切片纪律不启动半途大改——本体留下一轮完整实施。
**门禁**：`go test . -run TestSC_` ok 0.405s；gofmt 净；root `-race` ok 15.035s；`--strict` 0（21/34 不变）。

### 5.30 轮七十四：P1/S-C 收尾——数值-only 提交闸门（红→绿），记录轮转与提交同锁半提交不可见
**开工核验（行为压倒推读）**：`go build ./...` 0；全 `./agent/... -race` **五包 ok（agent 81.682s）** 与 §0 基线一致。核对 WIP 实态发现 appliedRecord 本体（`appliedAgent`/`orgGeneration.applied`/`currentHotFor` 读侧/`SetHotSource` 记录投影/`HotSnapshot` 记录优先解析）已在未记账的工作树里落地（轮七十三标「本体留待下轮」后被续写但未过门）。
**红（挂起型，先复现）**：`TestSC_RecordCommitsAtomicallyWithVersion`（锚2）在 root `-race`/普通测均**挂起 180s 超时红**（EXIT=1）——goroutine 栈定位主测阻塞于 `org_sc_record_test.go:141 <-parked`，reload 协程已完成 numeric-only 分支（日志「numeric-only full apply recorded」）。根因：`orgCommitBarrier` 仅在**结构发布分支**（`tagent.go:1022`，PublishExecutor/swap 前）触发，**数值-only 分支从不 park**——锚2 停在提交点观测记录面永远等不到屏障。此为未记账 WIP 引入的 root 门破坏（轮七十三 root 曾绿，因当时锚2 测尚不存在）。
**实现（生产，最小对称）**：数值-only 分支在 `applyHotAll`（push，含 compressor 新值）+ `retireUnrouted/sweepRetirements` 之后、`coord.recordHotApply`（记录轮转）之前接入 `orgCommitBarrier.Swap(nil)`——与结构分支同一「已 push、记录未轮转」提交点语义；生产恒 nil 零成本，每轮 reload 只走一支，Swap 一次性消费互不干扰（结构分支既有屏障测 `org_retirement_test.go:317` 不受影响）。
**测强化（红→绿且非空洞）**：重写锚2——先一次无屏障 numeric 提交（4096→5000）让唯一记录出现 sub3 条目，再置屏障做第二次 numeric 编辑（5000→8100）park 于提交点；屏障内**双轴分歧真断言**：`scHot().MaxTokens==5000`（记录读者经 `currentHotFor` 仍见上次提交＝半提交不可见）对照 `OrgBudgetLine()==6480`（push 侧 compressor 已取 8100×0.8 新值）——证明记录轮转独立于 push、只落 commit 闸门；释放后记录轮转到 8100。屏障未触发时以 `select`+10s 超时 `t.Fatal` **干净红**（替代原挂起）。
**门禁（真实退出码）**：改测未接生产屏障前 `-run TestSC_RecordCommitsAtomicallyWithVersion` **EXIT=1（10.03s 干净红）**；接入后 `go test . -run TestSC_` **EXIT=0**（两测 PASS，0.529s）；`go vet ./...` 0；root `go test . -race` **EXIT=0 ok 16.100s**（无 race/无 FAIL/无挂起，恢复 root 门为真绿）。gofmt 净。
**S-C 定形**：`appliedRecord`＋读闸门（`currentHotFor`，锁内读——热参在压缩/预算边界消费非 per-token，atomic.Pointer 无锁化留 S-E/后续，注释已明）＋recordView 轮转（S-C 阶段＝`hotSnapshot` 提交点单写者投影缓存 + `hotSource` 记录投影，设计 §2 两步走第一步）＝design §8 S-C 内容达成。
**未竟（2.3 仍 [ ]）**：2.3 准出「热更一次不产生第二套 TagentAgent 状态」的**终态**待 S-D/3.2 消亡已变子 agent 过渡壳（轮七十一 §5.27 明载「过渡壳待 S-D 消亡」，design §8 S-A 红锚「终态 0 属 S-D」）；S-C 为 2.3 记录面收尾，勾顶栏待 3.2 联合。四路 push（`ApplyOrgHotParams`）与 `liveCMs` 广播仍在，反转 pull＝S-E/6.4。
**状态**：21/34 不变（片内推进不勾顶栏）；下一步 Order-A 分支：3.3 工厂能力门（S-B 接口定型并行，P1 前置）或 S-D/3.2（过渡壳消亡＋执行视图接记录，联合闭 2.3 顶栏）。

### 5.31 轮七十五：S-D/3.2 范围与接缝核销（只读判定，零码改——停下上报设计接缝，非半途大改）
**开工核验**：`go build ./...` 0；分支 dev ahead 4（承轮七十四）。承 Order-A 取 S-D/3.2，按行为压倒推读先做端到端读码核销现机制：
- **退役判据现址**：`owner_retirement.go::sweep` 只依 `reach[name]`（本代路由）＋`owner.Obligations().Idle()`（**B 自身**三轴：Executions/Invocations/LiveTasks）决定退役。`execBinding` 已逐代快照 `face`（exec_lease.go:89，轮三十三 d42 已成），委派目标经 `subagentWrapperIn(face.Tools)` 单点解析；`AgentToolWrapper.agent` 直接持**被调 owner 实例**（tool_agent.go:130）。
- **红锚「G1 未调 B 被 G2 删后 G1 再调 B」实为跨片**：G1 若仅「合法可稍后调 B」而**尚未实际调用**，B 自身三轴皆 Idle→现制当场 Close B→G1 后续调用落在已关 owner 失败。design §8 S-D 红锚即此。修法＝D8「binding 为其本地可调用闭包（含未调但本版合法）取得 owner 使用权，binding 回收时释放」。**使用权 acquire 落 agent 包（binding 创建/reclaim 在 `ExecLease.Release`），而使用权 READ 落 root 包 `sweep`**——design D8 明标该资源保有/排空面「待 P2 落地」，其 read 侧正是 §4.3（P2，mermaid S32→C43）。故 **3.2 单独无法把此锚转绿**，须与 4.3 联合；先做 3.2 半边会留下「acquire 无 read」的悬空机制（违防跑偏总则 7「不留半成品」）。
- **「face 读记录」半**：现 `appliedAgent` 仅携 `Hot`/`Draining`，无 `Face`；design §1 草图含 `Face ContextManagerConfig`。补 Face 入记录＋过渡壳消亡耦合 M2 委派 `Run`（session.go）按 owner 当前 face 装配——同属需先定形再接的载体。
- **判定**：S-D/3.2 是本变更余下最**横切**的一片（使用权 acquire/release 跨 agent↔root 包＋记录承载 Face＋过渡壳消亡＋多 owner 验收矩阵），且其标志性红锚天然跨 3.2/4.3 两片；D8 设计明置 P2。据轮七十三同类边界先例（「上下文余量不足不启动半途大改」）与总则 7 停止条件，本轮不吞下跨包半成品，呈最小方案与接缝待裁。
**未跑门**：本轮零码改，无新增门禁；§0 root -race 真绿基线不变。
**状态**：21/34 不变；下一步待用户在「(a) 使用权机制定为 3.2+4.3 联合片、按 D8 一次性落 acquire＋read」「(b) 先做 3.3 工厂能力门前置（列调用点＋特征测，安全增量）」「(c) 其他」间裁定。

### 5.32 轮七十六：P1/S-E 首增量——compressor 拉取契约＋常驻/私有消费者接线（红→绿）
**开工核验**：`go build ./...` 0；21/34 ready。承轮七十五裁定路径：S-D 红锚跨包接缝需先定形，故按 Order-A 表中 **S-E 只依赖 S-C**（design §8）取 6.4 拉取半，走计划自订的 Order-B 切片序（「若用户改令 6.4 先行：仅调 P1 内切片序，判例与不变量不变」）。

**第一步端到端读码（行为压倒推读）**：把 push 面全部触点枚举成表——`ContextCompressor`（`maxTokens/thresholdBits/keepRecent` 三原子 + `ApplyHotParams` 内呼 `cc.compressor.ApplyParams` 做「同代换装」，即 hardening 5.3 的同步补丁）、`SmartCompressor`（`paramMu` 护 `maxTokens/triggerBudget/KeepRecentTasks`，热读点恰 5 处：`budget()` ×4 + keepRecent 1 处）、`CompressOptions`（已存在 per-call 通道，故同代传递无需新发明）；push 调用方 12 处（`task_record_sink.go` 四路扇出、`tagent.go:539` 提交点、`lifecycle.go` `ApplyOrgParams`、5 个测试族）。**结论**：删除任一 push 入口会同时打断其全部调用方，故拉取契约与消费者接线必须先落地、push 删除作为下一步整收（本片内部合法分层）。

**落地（pull 契约，`agent/compress`）**：
- `HotNumbers{ThresholdPct,MaxTokens,KeepRecent}` + `WithHotSource`/`SetHotSource`：装配期安装拉取源（`SetHotSource` 存在是因常驻 cm 早于 owner 字段成序构造，design §3 未列此件，属构造时序逼出的必要形状）。
- `liveNums()`：**每个消费边界整组一次读**（BudgetLine/Threshold/KeepRecentValue/Compress），源字段 >0 遮蔽构造原子、<=0 回退——把 hardening 5.3「外层触发线与内层压缩目标必须同代」从「记得同时 push 两侧」的**同步纪律**升为**结构不可能**：一次读出一个世代，撕裂窗口无从产生。
- `Compress()` 把本边界世代随调用下行（`CompressOptions{KeepRecentTasks,MaxTokens,TriggerBudget}`）；`compressSkeleton` 改用 per-call `opts.budget(sc)`，`SmartCompressor.ApplyParams` 降为构造/测试内部件（design §3 明文）。**内层热字段在源在场时不再被咨询**＝共享可变热态的读取面清零。

**落地（消费者接线，`agent`）**：`ContextManagerConfig.HotNumbersSource` 透传；常驻 cm 于 `NewTagentAgent` 装 `ta.liveHotNumbers`；`newContextManagerFromConfig` 的 owner 分支为**在途私有 CM** 绑同一 owner 源——热更此后经「下一次压缩边界」自然到达私有实例，不再需要逐 CM 扇出。`liveHotNumbers` 经 `HotSnapshot`（记录源优先）解析，故记录即压缩读权威。

**红→绿（两道）**：
1. `agent/compress/hot_source_test.go` 新契约测（源旋转无 push／部分零回退／无源回退）：先行落测得**编译红 exit 1（undefined: HotNumbers/WithHotSource）**，实现后 `TestHotSource*` 3 测绿。
2. S-C 锚2 按 design §5「回归测迁移：改源旋转语义（期望值断言不变、触发方式变）」**显式迁移**（非静默改测，理由入测注）：屏障内旧断言是「push 6480 vs 记录 5000 分歧」，接线后压缩器经记录解析，屏障内必为 **4000（5000×0.8）**——即「源遮蔽 applyHotAll 已写入构造原子字段的新值」的行为证据，读权威唯一；release 后两轴同至 8100/6480。

**挂起诊断与新增惯例（重要，后轮必用）**：首次跑 root 定向族**挂起**。我先假设「pull 经 `currentHotFor→coord.mu` 重入死锁」——**栈证据否证**：阻塞者是 `tagent.go:1093` 的 Close drain 等 reload 外层 `mu`，而 reload 停在屏障上；真因是迁移期断言 6480→4000 不符致 `t.FailNow`，deferred `Close()` 在 reload 仍 park 时等锁，**把一次失败伪装成超时**。两见：(1) reload 外层 `mu` ≠ `coord.mu`，故 `currentHotFor` 锁内读今日无重入路径（design §2 的 recordView 无锁化仍属 S-E 终态片，**残余风险已记账**：任何未来在提交临界区内读 owner 热参的路径都会立即踩雷）；(2) 固化惯例——**屏障停车类测必须注册幂等 release 的 `t.Cleanup`（LIFO 早于 Close 清理）**，已施于锚2，后续 S-D/S-E/S-F 红锚同此。

**门禁**：`go build ./...` 0；root `go test . -race` **ok 18.586s（全量）**；`agent -race` **ok 82.288s（全量，皇冠明珠未退化）**；`agent/compress -race` ok 4.132s；`agent -short` 全量 ok 40.616s（接线零回归）；`go vet ./...` 0；gofmt 净（`agent/agent.go` 经内置 gofmt 对齐）；`openspec validate --strict` valid。**未动**：tasks.md 勾选、design.md、独立计划文件、索引、用户数据、远端；全程未提交。

**S-E 余量（下轮清单，6.4 仍 `[ ]`）**：(1) 删 push 入口（`ContextCompressor.ApplyHotParams/UpdateMaxTokens/UpdateKeepRecent`、`cm.ApplyOrgHotParams/ApplyOrgParams`、`ta.ApplyOrgHotParams` 压缩半、liveCMs 配置扇出改「仅生命周期账」注释、`hotOverlayConfig` 压缩三轴播种）；(2) 迁移 5 个测试族（`m3_hot_consumption`/`org_hot_params`/`m34_subcall_hotthread`×2/`m3_race_regression`）到源旋转语义；(3) TTL 半（§4：taskManager 源 + 后续任务 spawn 现读 + `Set*TTL` 退出热路径）；(4) `hotSnapshot` 字段删除 + `NewTagentAgent` 恒装静态源（design §2「不存在无源状态」）；(5) recordView 无锁化（§2）；(6) J6 余下红锚（OutputLimitTool 构造期封顶不因 numeric-only 变／后续新任务实际 TTL／回滚／draining 末值）。


### 5.33 轮七十七：S-E ⑤ recordView 无锁化（design §2 明列属 S-E 的收尾项，红→绿）
**开工核验**：`go build ./...` 0；承轮七十六清单。因 ①/④/②（删 push＋删 hotSnapshot＋5 测族迁移）互为依据、必须整片同收，且本轮上下文余量不足以在保持「不得中途留半删」的前提下完成该整片，故本轮只取**独立且已被现实现变成必需**的第 ⑤ 项：轮七十六把压缩器读权威改到记录后，`currentHotFor` 从「偶尔读」变成「每个活 CM 的每个压缩边界都读」，S-C 当初「锁内读频率可接受」的前提已不成立——留着就是 contention（当轮七十六记的残余风险）。

**红→绿**：`TestSE_RecordReadIsLockFree`——测试**持有 `coord.mu` 期间**由另一 goroutine 读记录：锁内实现无法推进（2s 有界红，非挂起），无锁实现立即返回。先跑出 **红 exit 1（2.00s）**，实现后绿。

**落地（`org_hotreload.go`）**：`orgCoordinator.appliedView atomic.Pointer[[]appliedAgent]` = 已提交记录的无锁读面；`publishViewLocked` 在**三个提交点**（`swap`／`recordHotApply` 的真轮转分支／`recordRollback`）与 `current` 同一临界区内发布**私有副本**（reload 复用自己的 slice，不复制就会让读者看到被改写的 slice）；`currentHotFor` 改为纯 `Load` + 遍历，不再取 `coord.mu`。语义不变处：`recordHotApply` 的「语义完全相同不轮转」早退分支**不发布**（无提交即无轮转）；未提交过的协调器 `Load()` 为 nil → `ok=false`（standalone/never-routed 回落构造快照）。

**顺带收口轮七十六记的残余风险**：「任何未来在提交临界区内读 owner 热参的路径都会踩重入雷」——读面既已无锁，该风险类别整体关闭（S-D 的 face 读记录、S-F 的回滚读记录都会天然受益，不必再各自绕锁）。

**门禁**：`go build ./...` 0；root `go test . -race` **ok 14.946s（全量）**；`agent -race` **ok 80.701s**；`./agent/...` 各子包 -race 无非 ok 行；`go vet ./...` 0；gofmt 净（`org_hotreload.go` 经内置 gofmt 对齐；`tests/offline_bench` 的既存未格式化非本轮触点，未动）；`openspec validate --strict` 见下。**未动**：勾选、独立计划文件、索引、用户数据、远端；未提交。

**S-E 余量不变（①②③④＋⑥，下轮整片同收）**：删 push 入口与 `ta.ApplyOrgHotParams` 全灭 → TTL 源（已读码确认形状：`TaskManager.terminalTTL/defaultTTL` 两字段 + `reconcileTTL`/board `remainingLifetime` 读点；**无独立 spawner TTL 权威**，`TaskDefaultTTL` 只经 `TaskManagerConfig` 进 `defaultTTL` 并在 sweep 时动态生效，故 ③ 比 P0 预估更薄）→ `hotSnapshot`/`SetHotSnapshot` 删除 + `NewTagentAgent` 恒装静态源 → 5 测族迁移 → J6 余锚。附带已确认死件：`ta.ApplyOrgParams`（lifecycle.go）全仓零调用方，随 ① 一并删。

### 5.34 轮七十八：P1/S-E 收尾——push→pull 反转完成（6.4 勾检）
**落地**：① 删 push 全面（`ta.ApplyOrgHotParams`、`cm.ApplyOrgHotParams/ApplyOrgParams`、`ta.ApplyOrgParams`（零调用死件）、`ContextCompressor.ApplyHotParams/UpdateMaxTokens/UpdateKeepRecent/SeedKeepRecent`、`UpdateThreshold`→非导出 `seedThreshold`（仅构造播种）、`liveCMs` 配置扇出、`hotOverlayConfig` 整体）；④ `hotSnapshot` 字段＋`SetHotSnapshot` 删除，`NewTagentAgent` 以 `staticHotSource(initialHotParams(cfg))` **恒装源**（design §2「不存在无源状态」），`HotSnapshot()` 只经源解析；③ `TaskManager.SetTTLSource`＋`effTerminalTTL/effDefaultTTL`（读点：`reconcileTTL`、`pruneTerminal`、`TerminalTTL()/DefaultTTL()`→board 经此自然同源），owner 适配器 `ta.taskTTLs`。提交点自此**对热参零写入**，只产出记录条目。
**测迁移（显式，非静默）**：`org_hot_params`（新增 `rotateHot` 源旋转替身）、`m3_hot_consumption`×2、`m3_race_regression`（-race 竞态锚改源旋转）、`m34_subcall_hotthread`×2（改 `SetHotSource`＝生产提交点同形态）、`compress/hot_params_test` 整篇重写为源旋转（含 5.3 缩窗真压缩锚）、`task/m3_ttl_consumer`。
**记账的语义变更**：源读零值＝「无意见→回落构造值」，取代旧 push 的「保持上次下发值」；生产源恒携完整 desired bundle，粘滞无必要。~~**读码纠正 P0 一项**：不存在独立 spawner TTL 权威~~ **← 轮七十九判此句为误**（grep 漏 root）：`ActionTool.defaultTTL` 三处 push＋spawn 消费俱在，见 §3 自纠行；6.4 因此重开，本节其余结论不受影响。
**J6 六场景对锚**：在途下次压缩取新值且无 push＝m34（8100）；跨 CM 构造窗口并发＝m3_race -race；已有子调用下次实际压缩＝m34＋shrink-window；后续新任务实际 TTL＝m3_ttl_consumer；回滚＝TestRollback…（`recordRollback` 发布读面）；draining 末值＝S-C 锚1/J8 记录条目；OutputCap 不随 numeric-only 变＝TestM3 OutputCap（现存）。
**门禁**：`go build ./...` 0；`./agent/... -race` 全绿（agent 83.072s／compress 4.305s／task 2.902s／governance/reliability ok）；root `-race` ok 17.294s；复测定向族 ok 1.491s；`go vet ./...` 0；gofmt 净；`--strict` 见下。进度 **22/34**。未提交、未动索引/数据/远端/独立计划文件。



### 5.35 轮七十九：自纠 6.4 假完成 ＋ spawner TTL 轴补真（红→绿）
**缘起（本轮真正的产出是一件「发现自己错了」）**：按轮七十八建议续做 S-F/2.4，读 `doRollback` 时发现第 713 行 `rbParts.actionTool.SetDefaultTaskTTL(...)`。据此复查全仓（这次的 grep 覆到 root 与 `tool/`），否证了轮七十八写进 §5.34 的「不存在独立 spawner TTL 权威」——**那条「纠正 P0」本身就是错的**：`ActionTool.defaultTTL` 是绕过唯一记录的第二份 TTL 真值，由三处 push 维护（`build_agent.go:648` 构造、`tagent.go:1021` 结构发布、`tagent.go:713` 回滚），并在 spawn 时被 `resolveTTL`／`SpecFromDeclarative` 消费决定 `spec.TTL`；字段还是非原子的（reload 写／业务 turn 读）。J6「后续新任务实际 TTL」当轮只被 manager 轴测打到，spawner 轴**无任何锚**。⇒ 6.4 重开 `[ ]`（进度 22→21），§3 立自纠行、§5.34 原句加删除线更正；已落的压缩器/taskManager/记录三面反转经复核仍然有效，不重做。
**修复（spawner 轴转 pull）**：`ActionTool` 增 `ttlSource atomic.Pointer[func() time.Duration]` + `SetDefaultTTLSource`，`resolveTTL` 序＝显式 arg ＞ 源现读（＞0）＞ 构造默认 ＞ 10 分钟地板；**`SetDefaultTaskTTL` 删除**（全仓已无代码调用方），三处 push 站点改绑 root 适配器 `spawnerTTLSource(owner)`（经 `HotSnapshot`→记录，零值回落构造默认，与轮七十七定的源语义一致）。附带消掉该字段的非原子竞态面。
**红→绿**：`tool/action/spawner_ttl_source_test.go`——① 源旋转（无任何写入）须被下一次 spawn 现读看到、显式 arg 仍优先、零读回落构造默认；② 无源边界保持构造默认。先落测得**编译红 exit 1（`SetDefaultTTLSource` undefined）**，实现后 3 测（含迁移的 `TestResolveTTL`）全绿。`ttl_arg_test.go` 的「非正值保持上次下发值」断言按轮七十七已定的源语义**显式迁移**为「零读回落构造默认」（不再粘滞），理由写入测注。
**门禁**：`go build ./...` 0；`tool/action -race` ok 36.368s；root `-race` ok 17.241s；`agent -race` ok 81.824s；`agent/compress`・`agent/task` -race ok；`go vet ./...` 0；gofmt 净；`--strict` valid；`complete` 由 CLI 复核＝**21**（重开生效）。未提交、未动索引/数据/远端/独立计划文件。
**对 2.4 的影响（下一步）**：`doRollback` 现状已走 `newCandidateTxn`＋排序获取＋`discard()` 逆序回退（S-B 半已落），其 2.4 残留是 `rebuilt` 提前 `rc.resident.Add` 与「过渡壳」分支——后者按本单口径属 3.2/S-D（2.3 顶栏亦挂在此）。且 2.4 明列「依赖 2.3/3.2/6.4/4.3」而 design §8 的 S-F 行写「依赖 S-B/C」，两者对 2.4 能否先于 S-D 落地口径不一致：按本单为权威，2.4 的完整验收（含「移除父保留共享子后回滚」场景）需先裁 S-D 使用权形态。

### 5.36 轮八十：6.4 spawner 轴端到端锚（变异探针定性）＋ 热参清理残余收口
**主锚**：`org_se_spawner_ttl_test.go::TestSE_SpawnerTTLReachesRealSpawnSpec`——真 org／真常驻 sub1／真 `ActionTool` 实例，断**宿主结果**而非 getter：`SpecFromDeclarative` 产出的 `task.TaskSpec.TTL` 初始须为配置 30m（非构造地板 10m），numeric-only 旋转（`task_default_ttl` 只在 `hotSignature`、不在结构指纹，实测 generation 不前进）后**同一实例**下一次 spawn 须为 45m，且显式 `ttl` 参数仍优先。**判别性由变异探针证明**：把源临时改为「恒返 30m」（＝精确模拟旧 push 的构造烤值），首轮断言通过、轮转断言 `expected: 45m0s / actual: 30m0s` FAIL；还原后 `PROBE-R80` grep 残留 0，全测绿。
**读码/运行新事实（两处，均入单）**：① 常驻 agent 的 `Tools()` 里 ActionTool 被 `OutputLimitTool` 包裹（`agent.go:433` 装配期包装），而 TTL 源绑在**内层实例**——测试经 `Unwrap()` 取内层，顺带证明包裹是同一对象而非拷贝；② `SmartCompressor.ApplyParams` 在轮七十八「降为构造/测试内部件」的说法后其实**零调用方**（连测试也不用），已删除，连带把它当存活机制来陈述的注释改正。
**注释真值修复（5 处，均以现时态陈述已删机制）**：`agent.go:94`（liveCMs 广播——并如实记其**现无生产读取方**，存续理由待 D4/S-D）、`context_manager.go:73`（ApplyHotParams／atomic 为权威）、`tagent.go:380`（numeric 分支经 ApplyOrgParams）、`org_hotreload.go:578`（指纹排除理由引用 ApplyOrgHotParams）、`task_manager.go:1201`（TerminalTTL 文档称由 SetTerminalTTL 变更）＋`m3_race_regression_test.go` 测首段（原述 UpdateKeepRecent/ApplyParams 写者）。
**门禁**：`go build ./...` 0；`agent` -race ok 81.791s／`agent/compress` ok 4.630s／`agent/task` ok 2.880s／root ok 17.315s／`tool/action` 单包 -race ok 35.923s；`go vet ./...` 0；`--strict` valid；gofmt 本轮文件净（`tests/offline_bench/offline_bench_test.go` 的未格式化经 `git status tests/` 空输出证明**非本轮改动**，未触碰）。
**环境观察（定性，不改代码）**：一次「5 包并发 `-race` 一把梭」里 `TestActionTool_TmuxExec` FAIL(15.06s)；随后单跑 PASS(3.57s)、`tool/action` 单包全量 -race ok、该测 `-count=3 -race` 连跑 ok ⇒ 判定为 tmux 服务端在多测试二进制并发下的争用 flake（15s 量级＝等待/超时形态），与 §6.4 改动无关（该路径不读 TTL 源）。后续门禁沿用**逐包**跑法，不并五包。**〔轮八十四收窄：并发不是必要条件——同族 `TestCommandParsing` 在串行单包 `-race` 下亦偶发 FAIL（随后同包连跑两次 ok、solo 非 -race ok）。定性为「负载下偶发的子进程/终端争用」，「并行调用所致」这一归因过窄，见 §5.40。〕**
**进度**：6.4 转勾，**22/34**（本轮为真收口：判别性锚＋零存活 push 调用方的 grep 证据，非再凭一次绿）。

### 5.37 轮八十一：S-D 首增量——使用权（D8）以「派生视图＋sweep 第四轴」落地，延后委派红锚转绿
**先钉红**：`org_sd_usage_hold_test.go::TestSD_DeferredDelegationIsProtectedByUsageRight`（真 org／真常驻／真委派）——G1 路由 main→sub1 且被一个 `LeaseTurn` 钉住（＝请求已在 G1 接受、模型尚未发出委派），G2 删除该路由：断言① sub1 不得被提前退役、② 从 **G1 自己的面**解析出的 wrapper 真调 sub1 须成功、③ 租约释放后一次业务活动内须退役。**首轮实测红在①**（`residentCacheForTest["sub1"]` 已为 nil，即 sweep 把仍被合法保有的 owner 关掉）。
**落地形态（按轮八十建议，用户以 apply 认可该序）**：
- `agent/exec_lease.go`：`execBinding.holdsUsage()`（`!retired || total>0`，即 D8 的「发布槽／实际调用／后台引用在即保有」）＋**`BindingHolders(owner, agents)` 派生计数**——遍历各 agent 的存活 binding，用既有 `b.subagentWrapper(owner)`（与现效面共用同一扫描 `subagentWrapperIn`，§4.2 同一版本真源）判定，**不新建任何登记表**；owner 自身 generation 显式排除（不给自己保使用权）。
- `owner_retirement.go`：账本新增 `usageOf func(name string) int`（只读注入）；sweep 谓词由「三轴 Idle」改为「三轴 Idle **且** holders==0」，持有理由并进 `Why`（实测日志原文：`still draining — obligations remain: executions=0 invocations=0 live_tasks=0; usage rights held by 1 live generation(s)`）；`diagnostics()` 增 `usageHeldBy`，并把派生读**移到放账本锁之后**（诊断路径不得反转 ledger→agent 的锁序）。
- `tagent.go`：装配处注入 `usageOf`——**roster 由 root 出（谁存在是装配自己的事实），holders 由 agent 层算（谁保有谁是 binding 的事实）**，两侧各守其真源。
**验证**：三段断言全绿，退出路径日志为 `owner "sub1" retired — obligations converged; exclusive components closed, lease released, registrations revoked`（有界性成立）。
**测试接线的两次纠正（非机制问题，均记录以免后人误读）**：① yaml 原带 `model:/providers:`，`WithModel` 只作全局默认、子 agent 仍解析到真 provider → 真调打到 `localhost:1` dial refused；按 `delegYAMLSeq` 的既有约定**去掉 model/providers 段**后宿主 mock 才服务全部 agent。② 释放租约后仅靠 `CheckOrgReload()` 等不到退役：ops 同步入口 `reload()` 在 mtime 未变处「hot path: unchanged」早退，pending 排空只由懒检查（业务活动）那条分支驱动，故末段改为注入一个真实 turn 供活动。**⚠ 这不是可接受的终态**：tasks 4.3 实现条款明写「释放使用权/任务收尾经原生命周期轻量通知继续退役，**不必须再来一个业务 turn**」——本轮测试被迫供活动＝该条**仍欠**，已记入 4.3 余项，不以「设计如此」把欠账洗成既定语义。
**门禁**（逐包口径，轮八十固化）：root `-race` ok 18.291s（**sweep 谓词改动零回归**）；`agent -race` ok 82.612s；`agent/task` 2.905s／`agent/compress` 4.008s／`tool/action` 36.645s；`go vet ./...` 0；gofmt 净（`tests/offline_bench` 未格式化项非本轮改动，已排除）；`--strict` valid。进度仍 **22/34**（3.2/4.3 各自余项未做完，不冒领）。未提交、未动索引/数据/远端/独立计划文件。

### 5.38 轮八十二：4.3「释放即续排」欠账清掉——通知落在**世代引用归零**这个准确事件上
**先红**：`org_sd_drain_poke_test.go::TestSD_ReleaseContinuesRetirementWithoutAnotherTurn`——前置（G1 持使用权时不退役）通过，`lease.Release()` 后**不提供任何流量**（不 StartLoop、不 Inject、不再 CheckOrgReload）等退役：实测 `Condition never satisfied`，5s 超时 FAIL，exit 非零。这正是轮八十一记进 4.3 余项的那条欠账（原文「释放使用权/任务收尾经原生命周期轻量通知继续退役，不必须再来一个业务 turn」）。
**落地**：`agent` 侧 `ContextManager.retirementPoke`（`atomic.Pointer[func()]`）＋ `pokeRetirementDrain()`（只调用装配注入的懒检查，**不内联跑排空、不加计时 goroutine**；standalone agent 为 nil 即不通知）；`execBinding.release` 在 **`total` 降到 0 的跃迁**上通知；root 在 `New()` 臂 reloader 处把 `requestCheck` 存入 `rc.retirementPoke` 并就臂全部存活 owner，`buildAgent` 里其后再成形的 owner（热增／回滚重建）自臂——不遗漏任何 owner，且不改业务语义（复用 turn 走的同一条单飞路径）。
**两次自我纠正（都因把事件选窄/把旧时机当语义）**：
1. 通知点最初挂在 `forgetBinding`（仅**已退役世代**被遗忘时）。新的 4.3 状态测 `TestRetire_ReentryAfterFinalExitRebuildsFreshOwner` 因此 FAIL——被移除 owner 通常卡在**仍为 active 的世代引用**上，那条路径根本不走 forgetBinding ⇒ 通知点改为「引用数跃迁到 0」，`forgetBinding` 退回纯登记删除。
2. 该改动使既有 `TestRetire_ReentryIntoClosingOwnerIsRefused` FAIL（`s2` 竟被重新准入）。读码定性：**不是缺陷**——该测靠 `lease.Release()` 后「尚无人在排」的旧时机制造 mid-close 态（其注释自陈 "no further publish has swept it yet"），而续排落地后释放即完成**最终退出**，重入按「最终退出后按原恢复协议重建」被合法准入。修法＝**显式重建前置态**而非削弱断言：租约保持持有（mid-close 且仍在册），断言原样保留；并**新增** `TestRetire_ReentryAfterFinalExitRebuildsFreshOwner` 把 D7/4.3 那句第三态（复用／拒绝／**重建**）钉住——断言释放后无流量即最终退出、重入得到**新实例**（`NotSame`，已退出者绝不复活）。`lease.Release()` 提前到断言之后（Release 幂等，`t.Cleanup` 仍兜失败路径），该测耗时由 32.03s 回落到正常量级。
**顺带关闭单飞合并洞**：~~排空进行中落下的释放通知会被 `building` 单飞门丢弃 ⇒ 级联可能滞留到下一次事件，`sweepRetirements` 改为「本轮仍有退役则再扫一遍」即已关闭~~ **← 轮八十三判此声明为过**：该循环并未关闭合并窗口（P3 探针在单线程下不复现＝锚不足，而非无洞）；随后 root **全量 `-race`** 复现了级联停住。真实修法与本节勘误见 §5.39。
**门禁**：root `-race` ok **50.618s**／`agent` 81.195s／`agent/task` 3.341s／`agent/compress` 4.357s／`tool/action` 36.225s；`go vet ./...` 0；gofmt 净；`--strict` valid；两条新锚 `-count=2` 定向复跑 ok；`TestRetire_*|TestSD_*` 十锚全绿。进度仍 **22/34**（4.3 余：排空重入余项・真实 poisoned・菱形共享・末段 Close 全覆盖；3.2 余：执行视图接记录／请求级上下文）。未提交、未动索引/数据/远端/独立计划文件。



### 5.39 轮八十三：4.3「菱形共享依赖」锚落地，并据此推翻轮八十二的「合并洞已关闭」声明
**新增锚**：`org_retire_diamond_test.go::TestRetire_DiamondSharedDependencyWaitsForAllBorrowers`——main→{s1,s2}→s3 的菱形在**退役时**的语义（构建期菱形只有 `build_cycle_test` 钉过）：同代摘掉两条入口路由后，① 无引用的分支 s1 收敛；② 共享叶子 s3 因仍处在 s2 存活代的可调用闭包内**不得退役**（`CloseStarted()` 亦须为假）；③ s2 的在途引用退出后，s2 与 s3 须在**无新流量**下依次收敛。**判别性探针**：P1 把使用权轴置零 ⇒ 精准红在②（line 83）；P2 抽掉「释放即续排」通知 ⇒ 精准红在③（line 90）。两条都证毕。
**关键自纠（轮八十二的过度声明被实测推翻）**：轮八十二我写「顺带关闭单飞合并洞：本轮仍有退役则再扫一遍」，并用探针 P3（撤掉该循环）验证——**单线程下 P3 仍绿**，我据此把循环降格为「未被证明必要的兜底」。这个推断是错的：锚没复现不等于洞不存在。随后 root **全量 `-race`** 套件把它打了出来——`Condition never satisfied` at line 90（5.05s，套件内稳定复现；单跑该测则绿，因为 race 调度扰动才踩进窗口）。真实机制：释放落在排空趟**执行期间**时被 `building` 单飞门合并丢弃，而我那层循环只在「本趟有退役」时续扫，趟间落下的通知仍然丢失。
**修法（替换掉未被证明的循环）**：`retirementLedger.retireablePending(reach)` 复刻 sweep 的准入判据（未路由＋无使用权＋自身 Idle），**只读不改状态**；排空趟在 `mu` 下取该判定 → 以 LIFO defer 先放 `mu` 再放 `building` 门 → 门开后再自觉一次 `requestCheck()`。终止性由结构保证：判定为真意味着下一趟必退役至少一个名字，pending 集单调收缩。`requestCheck` 因自引用改为 `var` 具名声明（字面量自引用不合法）。级联循环整体撤销，不留下无法证明的必要物。
**门禁**：先复现后修复的证据链——修复前全量 `-race` FAIL(5.05s) ⇒ 修复后全量 `-race` 连续 **2 次** ok（53.460s／50.265s），定向 `TestRetire_*|TestSD_*` 11 锚 `-count=5 -race` ok（163.986s，即每条锚在 race 下各跑 5 遍）；`agent -race` ok 78.711s；`agent/task` 2.889s／`agent/compress` 4.661s；`go vet ./...` 0；gofmt 净；`--strict` valid；`complete` 复核 **22**。另：一次把 `task+compress+tool/action` 三包并在一行 `-race` 调用里时 `tool/action` FAIL，单跑 ok 36.014s ⇒ 归入 §5.36 争用族。**〔轮八十四更正：该归因当时不完整——同族在串行单包下亦可偶发失败，见 §5.40。〕**探针残留 grep 0。未提交、未动索引/数据/远端/独立计划文件。进度仍 **22/34**。

### 5.40 4.3 末两件收口：真实 poisoned 热增闭路（轮八十四）＋ 候选在飞时的 Close 覆盖，并测出自家锁探针会拆锁

§5.39 余项前两件。**两件都是「既有实现正确、缺判别锚」——用探针证明锚真能咬，不假绿。**

**① 真实 poisoned acquire（`TestRetire_RealPoisonedAcquireRefusesHotAddAndKeepsServing`）**
封路不用 mock：驱动资源层**自己的 §6.5 规则**（`ErrReclaimUnconfirmed` → 持锁＋封路径），
再由 org 热增一个 store 落在该路径上的新 agent。日志给出真实拒绝原因：
`hot-add build for "sub2" FAILED — serving previous (fail-closed): ... store is locked by another process (single-writer)`。
断言：世代不变／sub2 不常驻／工具面不被部分改写／旧 owner 同实例同 store 且未被牵连关闭／
**正向证据** `lastFailure` 记录点名 sub2 与该路径（缺此则整组断言可能在「reload 根本没走到热增分支」上空转通过）／
再来一次 apply 仍拒绝（不自动解封）。**判别性探针 P4/P4b**（放松 `acquireDirLock` 的拒绝＝放行第二写者）
→ 红在世代断言（终版形态复验一次）。

**② 候选在飞时的 org Close 覆盖（`TestOrgClose_CoversCandidatePublishedDuringDrain`）**
`org_owner_close_test.go` 已锚「每个常驻／热增／不替换」，缺的正是 4.3 说的**候选**面：
用既有测试基座 `newBuildPark` 把候选构建停在 `mu` 之下，启动 Close，再放行构建。
断言 Close 自食收敛＋**在 drain 期间才发布的 owner 也被扫到**（`CloseStarted`）＋
每个 owner 的 **store 写锁真的归还**（`assertStoreWriterFree`，即「恰一次释放组件/store lease/登记，无需下个用户请求」）。
**两条判别探针各打一条性质**：P5（stopper 不再排空 `mu`）红在前置断言（候选没能发布＝顺序被破坏）；
P6（owner sweep 漏一个名字）红在 `owner "sub1" escaped the org sweep`。

**本轮最重要的一条是自己测试脚手架的缺陷（实测推翻我的写法）**：
首版用 `flockExclusive` 探针**两次**检查「锁仍被他人持有」，`-count=1` 绿、`-count=3 -race` 第 2、3 轮红——
同进程内对同一文件另开 fd 做 `flock(LOCK_EX)` 会**转换并随后在 close 时释放**该锁（macOS 语义），
于是探针自己把封路拆了，后续迭代里 org 真的开成了第二个写者。**探针即干扰**。
改法：封路改用**登记面的非破坏检查**（`rr.acquire` 在封存路径上必须返回 `ErrResourcePoisoned` 且 open 不得运行），
「无第二写者」由该规则＋org 拒绝共同保证；`assertStoreWriterFree` 保留在 Close 之后使用（此时期望就是「无人持有」，不构成干扰）。
→ `§5.23` 那条「锚不复现 ≠ 洞不存在」再获一例：**只跑一次 `-count=1` 不足以暴露脚手架自毁**。

**门禁（探针全部还原，`grep PROBE-P4|P4b|P5|P6` 残留 0；`gofmt -l` 本轮文件净）**：
两锚 `-count=3 -race` ok（1.946s）；P4b 复验红于世代断言后还原；
Close/poison/diamond/reentry/lazy-check 家族 `-race` ok（33.847s）；
全量 root `-race` 两次 ok（**47.662s／49.566s**）；`agent -race` ok（82.245s）、`task` ok（3.015s）、`compress` ok（3.937s）；
build OK、`go vet ./`=0、`--strict` valid、`instructions apply` complete 复核 **22**（4.3 未转勾，见余项）。
**并更正 §5.36 的归因**：`tool/action` 的 `TestCommandParsing` 失败**不止**发生在「同一响应内并行三个调用」——
本轮一次**串行单包 `-race`** 也失败（3.58s）；随后同包 `-race` 连跑两次 ok（36.208s／36.335s）、
`-run "TTL|Spawner|ActionFactory|Command" -count=2 -race` ok、单测 solo 非 -race ok（7.15s）。
定性：**该测在负载下偶发超时**（纯解析用例耗时 7s 说明它在等真实子进程/终端机制），与本轮改动无交集
（轮八十四只动 root 包）。§5.36 中「并行调用所致」的表述按此收窄为「负载下偶发，串行亦可复现」。

**4.3 余项**：~~① 3.2 的 binding 执行视图（接 2.3 记录／请求级上下文）是 S-D 主线；② 4.2/4.1 的关闭
须待 S-D 收敛；③ 2.3 顶栏仍待过渡壳消亡。~~（轮八十四的口径；4.3 自身余项由**轮八十五**清空并转勾，见 §5.41。①③ 属 3.2/2.3，不受影响。）

### 5.41 轮八十五：4.3 最后一条锚定并**转勾**——「等所有借用者」在 Close 缝合处不可观测，改钉在可观测处；顺带否证轮八十四注释里的可观测性断言

**先说被推翻的东西**（本轮的红不在产品，在我上一轮写下的说法）：轮八十四我在 `org_close_shared_test.go` 的注释里断言
「共享后端若被提前拆走，会以 `entry.Close()` 报错现形」。**探针 PA/PB 直接打不红**：让首个释放者无视剩余租约就拆后端，
`Close` 依然返回 nil、路径照样能被新世代接手——后到的释放变 `stale no-op`，错误不上抛。
⇒ **「共享组件等所有借用者」这一半在 org Close 这个缝合处根本没有外部可观测面**。
这恰好是 4.3 实现条款那句「**单有 Close 返回/缓存错误不能判已退出**」的实测注脚——条款早就写对了，是我对它的观测推断写错了，注释已按实测改正。

**分两半钉（各自都有牙）**：
| 锚 | 钉的那一半 | 判别性探针 |
|---|---|---|
| `TestOrgClose_SharedStoreWaitsForEveryBorrower` | Close 末段**恰一次释放**：无错、两 borrower 皆下、路径可被新登记面干净接手（既非泄漏持有、亦非被封） | **PC**（末次释放不拆后端）⇒ 红于接手断言（`store is locked by another process`） |
| `TestRetire_SharedComponentWaitsForEveryBorrower` | **仍有借用者时不得拆后端**：首个 borrower 退役后新登记面必须撞 `ErrStoreLocked`，且**不是** `ErrResourcePoisoned`（区分「仍被持有」与「回收未确认」）；最后 borrower 退出后才准干净交接 | **PA2**（首个释放即拆）⇒ 精准红于 `ErrStoreLocked` 断言（同探针下上一条锚仍绿 ⇒ 两条锚各测各的，未互相冒充） |

**接线全部复用既有基座**（`ownerWriter`/`twoAgentsOneStore`/`buildRetireOrg`/`residentCacheForTest`/`seqStore`），零新增生产面；
本轮**未改任何生产源文件**——四处探针（PA、PB、PC、PA2）逐一还原，`grep PROBE-` 残留 0，`resources.go` 复原后逐字与原状一致（sed 目视核对）。
可观测性手法沿 §5.40 的教训：**判「锁仍被持有」用期望失败的登记面 `acquire`**（撞锁不改写持有者的锁，安全）；
判「锁已归还」用成功接手（此后无人需要它），绝不再用 `flockExclusive` 去测「仍被持有」。

**门禁**：两新锚 `-count=2 -race` ok（1.834s）；root 全量 `-race` ok（**48.207s**）；`go vet ./`=0；gofmt 净；
`--strict` valid；`instructions apply` progress 复核 **23/34**（4.3 转勾）。生产包本轮零 diff，故沿用它包轮八十四结果。

**S-D 之后的盘面（11 项）**：3.2（执行视图接 2.3 记录／请求级上下文，S-D 主线，去掉按陈旧 `ta.config` 先造后补）→
2.3（顶栏待过渡壳消亡后联合勾）→ 3.3／3.4／4.1／4.2／5.1／5.2／5.3／5.4 → **S-F(2.4)** 仍待 design §8 与 tasks 2.4 依赖口径统一（未获你点头前不改 design）。

### 5.42 轮八十六：3.2 的「关闭后被拒」一条**挖出真实缺陷**——已收敛世代仍把死执行器交给新 turn 并复登记义务

红不在文档、在产品：owner **完整收敛退出**（runner 已关、store-owner 登记已撤、离开常驻表）后，陈旧持有者（旧代工具表里残留的 wrapper 就是这个东西）调
`BeginTurnLease()` 会拿到那个**已关闭的 runner**——把 runner 结构打印出来实证 `closeOnce.done=0x1`、`runs:map[...]nil`；
同一次调用还在**排空已报干净之后**又登记了一条义务（`Obligations()` 从 Idle 变非 Idle）。
`tryAcquireActive` 的注释本身写着：交出已关闭执行器去跑 turn 是这道守卫**存在的理由**（「running a turn on a closed executor」）。

**根因（一句话）**：`AcquireLease` 的「读到的代已不可用且无后继」分支把两种状态混为一谈——
**仍在排空**（照旧登记是对的：让有界排空以 `ErrExecUnconverged` 报「没干净」而不是假装关机成功）与
**已收敛关闭**（此后没有任何东西可等，登记＋交出死执行器纯属错误）。锚：`org_exec_gate_closed_test.go::TestExecGate_WorkAfterConvergedCloseIsRefused`。

**修法（闸门具名拒绝＋消费点显式化）**：
| 面 | 改动 |
|---|---|
| `agent/exec_lease.go` | 新哨兵 `ErrExecClosed`；`execBinding.isClosed()`；拒绝租约 `refused` 字段＋`Err()`，且 `Runner()/SubagentWrapper()` 皆 nil、`Release()` 惰性（不 drop 未登记之物）、`Derive()` 继承拒绝（派生工作不得在死代上复活引用） |
| `ContextManager.AcquireLease` | 无后继分支：`isClosed()` → 返回 `kind=leaseKindNoop` 的拒绝租约（**不登记**）；否则维持原语义（排空中照旧登记→`ErrExecUnconverged`） |
| `RunFlowWithExecutor` | 继承臂与新取臂**合流处单点**拒绝（`pinned!=nil` 时 lease 为 nil，`Err()` 空接收者安全） |
| `session.Run`／`tool_agent` 重入 | 委派调用与任务重入各在取到租约后立即拒绝，错误上抛给调用方 |
| `event_loop.processTurn` | **拒绝不是瞬态 I/O**：不占传输重试预算、不报 model 退化（T-G 警告重试会放大失败计数使 FailThreshold 塌缩），仿 §5.1 中途关机分支——不形成 completion、**保留 claim**，交重建后的 owner 重处理该持久输入 |

**另一半纠正（我的过度指定）**：首版把 Inject 也断言成 `ErrExecClosed`，实测得到 `agent.ErrLoopTerminated`
（「persistent loop already terminated — create a new agent for a fresh loop」）——**输入接受面本就有自己的具名闸门**，
与世代闸门各归各。锚改为分别断言两个具名哨兵（宽泛 `Error` 会让日后重构悄悄改「被拒」的含义）。
`BeginTurnLease→拒绝`／`RunFlow→ErrExecClosed`／`Inject→ErrLoopTerminated` 三条都是**真再进一次闸门**后被具名拒绝：
RunFlow 那条尤其关键——环路 ctx 已携带拒绝租约，`inherited==true` 臂必须在合流处认出来，否则 `pinned` 为 nil 会一路走到已关闭执行器。

**判别性**：先红后绿本身即证（修复前 FAIL 于 `Runner()` 非 nil ＋ 义务非 Idle 两条独立断言；修复后 `-count=2` 绿）。

**门禁**（改到热执行路径，回归从全）：`agent -race` ok **81.348s**（本轮 4/6 处编辑在此包）；root 全量 `-race` 连续 2 次 ok（**47.351s／47.016s**）；
`TestExecGate_|TestRetire_|TestSD_|TestOrgClose_` 家族 `-count=3 -race` ok（**99.181s**）；`agent/task` 2.885s／`agent/compress` 4.052s／`tool/action` 36.160s 逐包串行 ok；
`go vet ./...` 0；gofmt 净；race 零豁免；未提交、未动索引/数据/远端/独立计划文件。**进度仍 23/34**（3.2 未转勾）。

**3.2 剩余**：验收「A→B→C 各见本代自身声明」「同 owner 两并发 session/输入/投影/输出不串」；实现主干＝binding 内按 owner 取**不可变执行描述**（接 2.3 记录）
＋请求级上下文不按陈旧 `ta.config` 先造后补，即 `tagent.go` 正向 974／回滚 677 两处注释点名等待的**过渡壳消亡**。已核过并有锚：延后委派、在途调用不误退役、无关旧代独立回收（`TestLease_/TestRecycle_UnrelatedGenerationReclaim*`）、关闭后被拒（本轮）。

### 5.43 轮八十七：3.2 主干的**失败契约已钉死**——嵌套跳跨发布永远服务旧代目标，且我的第一推断被对照臂推翻

**这条不是「又绿了一片」，是给主干下了定义。** 先说结论：验收项「A→B→C 各见本代自身声明」在
**嵌套跳跨发布**维度实测**不成立**，症状是**静默的**——无错误、无拒绝（与 §5.42 那条闸门缺陷无关，日志中零 `refused`）。

**锚**：`org_nested_hop_test.go::TestOrgDelegation_NestedHopKeepsTheInitiatingGenerationTarget`（现**声明式 Skip**，
主干落地即取消 Skip 当 DoD；不静默、不改写成「预期行为」）。三段判据：
① G1 全链跑通；② park 住 B（钉在 G1）→ 发布换掉 C → 放行 ⇒ B 的嵌套跳**必须**仍服务 G1 的 C；
③ 再来的新 turn **必须**服务新 C。② 通过 ✓（D5/§4.2 的钉在代语义在嵌套层也成立），③ **失败**。

**实测服务序列**（arm 1，标签即 prompt 首 token）：
`[11] SUB-B-PROMPT → [12] SUB-C-PROMPT`（新 turn 仍打在旧叶子上），此后每个新 turn 皆然。

**推断被否证的一刻**：我第一版解释是「未变更祖先的 face 捕获了旧子实例 ⇒ 整棵新子树不可达」。
为验证它，加了第三臂：A/B/C **全部**换代。结果**仍失败**——
`ENTRY-A-PROMPT-G2 → SUB-B-PROMPT-G2 → SUB-C-PROMPT`。**祖先全都换新了，叶子还是旧的** ⇒ 我的解释不充分。
（若不跑这一臂，我会把半个根因写进设计。）

**定位到点的两条同源子缺陷**（读 `tagent.go:986-1016` 而得，非猜）：
| 编号 | 机制 | 后果 |
|---|---|---|
| D-a | **未变更的父不重建 face**（`reflect.DeepEqual(prevAC,nextAC)` 即 `continue`），其 face 在构建时**捕获了旧子实例** | 子的新代从该父**永不可达**（arm 1 主症状） |
| D-b | 过渡承载（shell）循环按 **map 序**遍历已变 agent，`subCache` 只排除自身不排除「尚未建壳的子」 | 父壳先于子壳构建 ⇒ **旧子实例被烤进父 face**（arm 3 症状） |
⇒ 二者同一根：**wrapper 在构建期捕获实例，而不是在调用期按发起代从稳定 owner 取执行视图**。
**仅把循环改成自底向上不足以过契约**（D-a 依旧），这正是实现条款「wrapper 指向稳定 owner ＋ 按 owner 取不可变执行描述」
所必需的结构性改动，也是 `tagent.go:677/974` 两处注释「dies with the transitional shell」等待的东西。

**顺手核实的一条（避免重复劳动）**：3.2 验收「同 owner 两并发调用 session/输入/投影/输出不串」**并非无锚**——
`TestW1_ConcurrentCallsIsolateTheirProjections`（同一 agent 两并发真实调用，各自只从自己 call-private 投影自动注入、且不得改写已发布 binding）
＋`TestW1_CallIsolation`＋`TestI1ConcurrentDelegationsEndToEnd`＋`TestSettleSinkRegistry_ConcurrentPerInvocationIsolation` 已在。
故 3.2 的真实剩余＝上面那条失败契约 ＋ 主干实现，**不重复造锚**。

**〔轮八十八勘误（§5.44）：本节的 D-a 机制表述被更好的测量取代——真正的观测是「只换子时，`resident[c]` 连实例都没换、未变父的 face 根本不推进」；D-b 则已被修掉。原文保留以显示推理轨迹。〕**

**门禁**：本轮**生产零改动**（纯测试＋工件）；root 全量 `-race` ok **47.279s**；`go build`/`go vet ./`=0；gofmt 净；
`--strict` valid；progress 复核 **23/34**（3.2 保持未勾）；诊断脚手架全部还原：三臂 env 开关（`PROBE_B_TOO`/`PROBE_A_TOO`）
与 `SERVE[...]` 打印均已删除，`grep "os.Getenv|SERVE\[" org_nested_hop_test.go` 残留 0。未提交、未动索引/数据/远端/独立计划文件。



### 5.44 轮八十八：D-b（发布顺序抽签）**根治并转绿**；D-a 的真实机制由拓扑测量取代，并暴露主干所需的持有协议裁决点

**两次拓扑测量**（一次性探针跑完即删，事实留在下方——不再凭读码推断）：
1. **只换子（C）** 的发布之后：`resident[c]` 是**同一实例**（前后同为 `0x140001ed860`）、`holders(c)=1 holders(b)=1` 不变、
   旧 C `closeStarted=false / idle=true`。⇒ 真正的机制不是「谁捕获了谁的指针」那么轻：**这次发布既没有替换常驻 owner，
   也没有推进任何未变更父层的执行视图**——过渡壳只被装进 **entry 自己的新 face**。于是「深度 ≥2 的父 → 被换掉的子」这条路径，
   新代永不可达（D-a）。
2. **每一层都换** 的发布，同一份配置连发 6 次：`reached-new-leaf = false,false,false,true,false,false`（**1/6**）。
   日志给出因果：`agent "b" changed structurally — transitional carrier built` **先于** `agent "c" ...`。
   ⇒ D-b 确证为**独立的非确定性缺陷**：壳循环按 map 序遍历已变集合，而 `buildAgentDFS` **优先命中缓存里已存在实例**
   （`build_agent.go:105` 读、`:830` 写）⇒ 父先建就把**旧子烤进新 face**。

**D-b 的修法（根治，不是补运气）**：`structuralRebuildOrder(agents, reach)` 把已变集合按**可调用图后序**（子先于父）排出，
名字序做确定性 tiebreak；环回落到剩余序（真环由 DFS 自己报，排序器绝不丢名、绝不自旋）。正向与**回滚**两处壳循环同时改用。
契约：`TestOrgDelegation_AllLevelsRepublishedReachTheNewLeaf`（全层换代 ⇒ 下一个 turn 必须打到第三层的新叶子）。
**修前 0/6 通过 → 修后 6/6 通过**；非确定性缺陷的「先红」必须按多次计，单次绿/红都不足为证。

**D-a 仍在**（3.2 主干）：我为确认它没被顺带修好，**临时**解除 `TestOrgDelegation_NestedHopKeepsTheInitiatingGenerationTarget`
的 Skip 实测——仍红在同一处（`a fresh turn serves the new C` 超时），随后恢复 Skip（`grep -c t.Skip(` = 1）。

**主干必须先裁决的一点（扩大持有协议，故上报不自动定）**：D-a 的修法是让每个受影响 owner 的**执行视图**随发布推进
（未变父也换 face／executor，实例与 store 身份不动）。但一旦旧子被正常**取代并回收**，②「仍持 G1 租约的父必须还能调到 G1 的子」就会失守——
今天它靠「捕获实例」侥幸成立。最小方案是把 3.2 资源责任条款已有的话落实：**binding 交接前取得其全部本地可调用依赖 owner 的使用权**，
即使用权派生从「宣告者未退役」扩为「**被宣告的那个代**在任一宣告者存活期间不得回收」（不进义务计数，故不破坏 4.3 的空闲退役锚）。
这属新增持有语义，**未经批准我不落地**。

**门禁**：改到发布路径（正向＋回滚），回归从全——root 全量 `-race` 连续 2 次 ok（**52.447s／49.424s**）；
`TestHotReload|TestOrg|TestRetire|TestSD|TestExecGate|TestRollback` `-count=2 -race` ok（**70.313s**，身份不漂移／回滚退役／使用权等锚均未受扰动）；
`agent -race` 82.305s、`task` 2.982s、`compress` 3.891s、`tool/action` 36.321s 逐包串行 ok；`go build`/`go vet ./...`=0；gofmt 净；`--strict` valid。
探针文件 `scratch_topo_test.go` 已删除；探针标记 `grep "PROBE-P|PROBE_B_TOO|SERVE[" --include=*.go .` 残留 0（注：`os.Getenv` 在他处属既有合法用法，如 `elimination_latest_path_test.go` 的三阶段门，不可笼统称零）。**进度仍 23/34**（D-b 是 3.2 内部的缺陷修复，3.2 未转勾）。未提交、未动索引/数据/远端/独立计划文件。

### 5.45 轮八十九：3.3 工厂前置能力门开门——真实调用点清点＋特征测钉死，并据此**测出并修掉一处 store 租约永久泄漏**

**为什么现在做它**：tasks 3.3 写明「工厂前置能力门须在 P1 接口定型前完成，不到最后静默删兼容」。P1 因 3.2 主干仍未定型而没关门 ⇒ 门到期了。

**清点（穷举，非抽样）**：`ToolAgentFactory` 在组织装配里**只有一处生产消费点**（`build_agent.go:348` `registry.GetToolAgentFactory(name)`，受内置名保护门 `!builtinAgentNames[name]` 约束）；
`PlainToolFactory` 走 `buildPlainToolRef`。分类本身已是**单点**：`buildToolFromRef` 按 `tr.Kind` 一次分派，远端判定共用 `config.go` 的 `isRemoteRef()` 单一谓词
（`build_agent.go:931` 与 `Config.Validate` 同源，注释自证）⇒「分类只做一次」这一半**无需改动**，我以特征测把它钉住防回退。

**特征测（`factory_gate_test.go::TestFactory33_CommittedBehavior`，全绿＝钉住既有承诺）**：产物**整只使用**（`Info().Name` 即工厂所建）；
本分支**不构建声明的 `Tools`**（配一个不存在的 tool id 也不报错）；同缓存菱形**只调一次工厂**；
工厂失败包装形状 `agent %q: factory failed: %w` 且保留原始原因（热更 fail-closed 靠它匹配）。

**按「先测后断」查租约，测出真实缺陷**：工厂分支 `buildOK=true` 直接返回，**`memStoreRelease` 无人接手**；而 `ToolAgentFactoryConfig` 根本没有释放面
⇒ 工厂连想还都还不了。双臂实验：对照组（配置构建的 owner）关闭后租约归还 **PASS**；工厂组 **FAIL**——写者名额被永久持有。
后果不是"多占一把锁"那么轻：同进程内该路径此后**任何**再开（回滚重接、热重加、第二个组织）都会撞 `ErrStoreLocked`，违反计划规则「store 仍只由 RuntimeResources 退出」。

**修法（不破坏公开工厂合同）**：新增 `TagentAgent.AdoptMemStoreRelease(release)`——把组织代取的租约**填进配置路径同一个槽**，
于是退出序与守卫完全复用：**最后一步**（runner 关完之后）、**且执行未收敛时绝不归还**（§4.1 不许把写者名额从活写者脚下抽走），成功归还后照旧 `revokeStoreOwner()`。
我一度考虑挂 `RegisterCloser`，读码后否决：closers 在 runner 之前跑且绕开未收敛守卫，会把「泄漏」换成更坏的「拆活写者的台」。**拒绝覆盖已有绑定**（一租约不能两主），
冲突时构建**响亮失败**（原状态是静默泄漏）——工厂自开 store 又接手组织声明的 store，本就是矛盾声明。
agent 侧另钉 `TestAdoptMemStoreRelease_OwnershipRules` 三则：接管后由 Close 恰一次执行／二次绑定被拒且首次仍生效／空接管是错误而非静默成功。

**门禁**：`agent -race` ok 82.231s（含新测）；root 全量 `-race` ok **50.596s**；`agent/task` 2.973s／`agent/compress` 3.897s／`tool/action` 36.434s 逐包串行 ok；
`go build`/`go vet ./...`=0；gofmt 净；`--strict` valid。修前工厂臂红（`3.3 工厂门：工厂分支取了 store 租约就必须负责归还`）→ 修后双臂绿。
未提交、未动索引/数据/远端/独立计划文件。**进度仍 23/34**（3.3 不转勾：其「remote／A2A 与工厂跨面」验收项尚未逐条钉完，见 tasks）。

**仍需你裁决的两件事（都不在本轮擅动范围）**：① 3.2 主干的持有协议扩展（§5.44）；② 工厂**去壳**（产物整只、face 路径不适用）⇒ 若要它接同一「准备／激活」拆分，
必须改 `ToolAgentFactory` 的公开合同（返回声明而非构造好的实例），属「需改公开工厂合同→停下给最小兼容方案」那一类。最小兼容路径：保留现 API，
另开一个返回 `*TagentConfig` 的可选注册面，二者共存直至工厂用户迁移。**〔轮九十更新：①已由用户经建议陈述后以 `/opsx:apply` 批准并落地（§5.46）；②仍待裁。〕**

### 5.46 轮九十：3.2 主干落地＋过渡壳消亡＋**2.3/3.2 双转勾（25/34）**——持有扩展经用户批准后「先红后绿」全链闭合

**裁决记录**：用户在建议陈述（①批准最小持有扩展／②工厂合同一次迁移）后以 `/opsx:apply` 授权按建议①推进（本节＝①的落地；②仍待独立成轮）。
语义当日回写工件：tasks 3.2「轮九十裁决」段＋design D8「轮九十精确化」（先改单、再实施）。

**红证（先钉后修，真实退出码）**：取消 `TestOrgDelegation_NestedHopKeepsTheInitiatingGenerationTarget` 的声明式 Skip（DoD 标记）
⇒ `go test -count=1` **EXIT=1**，`org_nested_hop_test.go:192 "a fresh turn serves the new C" — Condition never satisfied`——与 §5.43/§5.44 的红**同一处**（断言③，D-a 症状；①②过）。

**实现（三面同落）**：
1. **agent 侧原语**（`exec_lease.go`/`context_manager.go`）：`execBinding` 增 `holds/heldBy/runCfg`——回收判据单点化为 `reclaimableLocked()＝retired∧refs==0∧heldBy==0`，
   `finishReclaim` 关 runner→forget→**级联释放持有**（沿调用图单向、DAG 无计数环）；`acquireDeclared`（合法于 retired-held，仅真回收后拒绝＝`ErrExecClosed`）；
   `StageExecutor/ActivateExecutor/Discard` 把 `PublishExecutor` 拆为「暂存（不可见）→激活（换入+退役）」，staged binding 携本代 `runCfg`；
   `WireOrgGeneration` 双向 wiring——**incoming**（staged 面 wrapper 盖 `declared`→被声明子同代 staged binding＋声明方记持有）与 **outgoing**（回填冷启动惰性代的未盖章 wrapper，
   指向仍 active 的旧子 binding＋同样持有；`activeBinding()` 物化惰性代使回填有载体，激活时随 retire 正确级联）。
2. **wrapper/Run 侧**（`tool_agent.go`/`session.go`）：`AgentToolWrapper.declared atomic.Pointer`（回填期与在途读并发安全；nil→binding 单向）；
   `Call` 首步经 declared 取 `LeaseSubCall` 并注入 ctx（同步/越窗后台 `bgLease` 同钉）——被声明代被回收则**具名拒绝**，绝不静默改道；
   `session.go Run` 的 `invCfg` 改为**声明代装配**（`belongsToOwnerOf`∧`declaredRunConfig`，惰性代回退 `ta.config`＝standalone/旧路径不变）——「不按陈旧 ta.config 先造后补」就此兑现。
**〔轮九十五勘误（§5.51／§3 台账）：本节「三面同落」未涵盖 `wireAgent` 尾段的会话 tracker 重挂——去壳后已存在 owner 不再经该尾段，规则失守，由轮九十五在提交后时机补回并实测确认「当时无测可抓」。〕**
3. **root 侧发布**（`tagent.go` 正向＋回滚同型）：共享 `stageOrgGenerations`——对**每个**可达 owner `buildAgentFace`（纯装配、零构造、wrapper 解析到常驻实例）
   ＋逐 owner ActionTool 接线（resident sink/TTL 源归各 owner，J7）→ 全员 stage → `WireOrgGeneration` → 激活循环。
   **过渡壳循环（正向/回滚两处）与 `structuralRebuildOrder` 整体删除**；`buildModeExecutorShell` 保留（face 装配语义＋两个直构测试仍用）。

**行为证据（一次性探针跑完即删，`scratch_hop_probe_test.go`）**：发布后服务序列逐条打印——被 park 的 B 其嵌套跳回执＝`"served:SUB-C-PROMPT"`（旧 C，钉代 ✓）；
随后**本委派自身的 settle** 驱动一个新 turn（external_input），该新 turn 取新代、服务 `"served:SUB-C-PROMPT-G2"`（D6 正确语义：task_settled 重入用当前有效面）。

**〔显式断言迁移（判读入测注，非静默改测）〕**：nested-hop 断言②旧式「第三个注入 turn 前零 G2 服务」把「被钉跳不被改道」与「无任何新 turn」混为一谈——
后者**只在 D-a 缺陷下成立**（新 turn 根本到不了新 C）。主干落地后 settle 驱动的新 turn 合法取新代，故②改为钉**跳自身回执**：
等待**新**的 B 带回执记录（turn 1 已有一条，计数判别）并按**序号**取第 hopsBefore+1 条（settle 新 turn 的记录严格晚于被钉跳的），断言其含 `"served:SUB-C-PROMPT"`（带引号精确，
-G2 变体不可前缀误配）且不含 `"served:SUB-C-PROMPT-G2"`；①旧 C 服务数递增保留为副证。

**DoD 与回归（真实退出码）**：`NestedHop` ＋`AllLevelsRepublished` `-count=5` **ok**（含新断言）；S-A 锚 `TestDeshell_ChangedSubAgentConstructsOneTransitional`
**显式迁至终态 0**（该锚自轮七十一即注明「终态 0 属 S-D」，本轮即其兑现）；全量 root `-race` 连续 2 次 ok（**49.631s／50.811s**）；
`agent -race` ok **81.912s**；`task` 2.951s／`compress` 3.900s／`tool/action` 36.279s 逐包串行 ok；
`TestOrgDelegation|TestExecGate_|TestRetire_Reentry|TestSD_` `-count=3 -race` ok **99.345s**（0 FAIL）；`go build`/`go vet ./...`=0；gofmt 净；探针文件已删、
`grep "structuralRebuildOrder"` 残留 0（定义随壳循环一并删除）。**进度 25/34**（2.3＋3.2 双勾）。未提交、未动索引/数据/远端/独立计划文件。

**仍开放**：② 工厂合同一次迁移（3.3 剩余面，下一轮）；S-F(2.4) 依赖已满足（2.3/3.2/6.4/4.3 全勾）可启；liveCMs 存续裁定随 3.3/5.x 收口。

### 5.47 轮九十一：②工厂公开合同一次迁移落地——工厂 owner 自此与 config-driven 同轨；**测出并封死轮九十引入的孤儿构造类**

**裁决记录**：用户在建议陈述后以 `/opsx:apply` 批准**直接改公开合同**（并行双注册面被否：违 D1「一次迁移仓内调用者」与 D9「不为旧签名保留永久双实现」）。当日回写 tasks 3.3 轮九十一裁决段＋design D1。

**开工即暴露的现制缺陷（本轮红证，非推断）**：轮九十把发布改成「每个可达 owner 都 stage→wire→activate」后，旧合同（工厂自产整只 agent、中段不交配置）留下两条工厂 owner 独有失效：
- **D-f1**：每次结构发布为工厂 owner 装配 face 时**再构造一个整只 TagentAgent**，只用来抄 `ExecutorConfig`，随即成孤儿（bus/TaskManager/runner 全新建、无人 Close）——直接违背 D1「修改 B 不复制它的 bus/TaskManager」与 2.3 准出「热更一次不产生第二套 TagentAgent 状态」。
- **D-f2**：staged 代 `runCfg` 对工厂 owner 恒 nil ⇒ 被钉委派回退 `*ta.config`（构造期配置）⇒ **工厂声明变了永不到达**——轮九十「不按陈旧 ta.config 先造后补」在工厂分支的未兑现半。
- 另有潜伏 nil 洞：旧代码工厂返回 `(nil, nil)` 时 `assembled.factory==nil` 落入 `wireAgent(nil cfg)` 必 panic。

**红→绿（真实退出码）**：`TestFactory33_ReloadConstructsNoOrphanAgents` 修前 `--- FAIL ... Should be zero, but was 1`；`TestFactory33_FactoryConfigChangeReachesDelegations` 修前 `Condition never satisfied`（`RED_EXIT=1`，两条各打中一个症状）。迁移后 5 测 `-count=1` 全 ok。

**实现**：`ToolAgentFactory` 签名改 `func(ToolAgentFactoryConfig) (*TagentConfig, error)`；工厂分支早退前只交声明，`Name` 尊重工厂所设（空回落注册名）、`MemoryStore` 空由组织借用 store 兜底、租约仍由 `wireAgent` 填进 `MemStoreRelease` 同一退出槽（§4.1「未收敛不归还」自动生效）；nil 声明按 `agent %q: factory failed:` 形状响亮拒绝。`assembledAgent.factory` 字段、`buildAgentDFS` 的整只返回分支、`buildAgentFace` 的 factory 特判**全删**（face 对工厂同轨，注释一并真值化）；**连带删除** `AdoptMemStoreRelease` 缝＋`agent/lease_adopt_test.go`（唯一用户消失即不留死公开 API；轮八十九的泄漏修复被结构取代，其租约臂留在 `factory_gate_test.go` 当回归）。

**承诺迁移（逐条显式判读，非静默改测）**：内置名保护／菱形单工厂调用／失败包装形状／「声明 Tools 不被构建」**原样保留**；「产物整只使用」重述为「**产物配置整只采用**」（`Info().Name` 仍取工厂所设，断言不动、依据变）；迁移点＝`agent/tool_agent_test.go` 2 测、`builtin_agent_protection_test.go` 2 厂、`factory_gate_test.go` 文件头＋租约臂文案＋4 处工厂字面量。**行为升级如实记账**：工厂 owner 现在也走 `wireAgent`，故获得 retirementPoke 自臂、closer 登记与 `ownsPersistentState` 下的任务板/投影重建接线（旧合同完全跳过）——这是 7.2/D7「热新增真实 agent 必须完整构造」对工厂路径的补齐，非新增机制。工厂自持工具不经治理包裹＝Minor⑥ 既有边界，本轮不改判。

**门禁**：root 全量非 race ok 40.052s；root `-race` 连续 2 次 ok（**47.055s／48.639s**）；`agent -race` ok **79.916s**；`TestDeshell_|TestOrgDelegation_|TestHotReload|TestOrgClose_|TestRetire_` `-count=3 -race` ok **100.553s**（0 FAIL）；`task` 2.918s／`compress` 4.019s／`tool/action` 36.279s 逐包串行 ok；`go build`/`go vet ./...`=0；gofmt 净；`--strict` valid。**注册名固定族不可 `-count>1`**（§0 口径②复现：`-count=3` 时无关的 `TestBuildAgent_ReadPartitionsIncludeOwnNamespace` panic `plain tool factory ... already registered`——非本轮缺陷，重复门改用无固定注册名的家族过滤）。未提交、未动索引/数据/远端/独立计划文件。**进度仍 25/34**：3.3 **不转勾**——「已纳管任务不因工具换代失监视」一条现无专锚（只有 detector 绑定／跨重启恢复两类既有测），按勾选规则待逐腿核验；remote 跨发布行归 3.4。

### 5.48 轮九十二：§2.4 回滚收口——删除专用重建分支，**测出并修掉「后段失败的回滚把未发布 owner 留在在线清册」的半改**

**为什么这是缺陷而不是风格问题**：正向热更自 S-B/S-C 起就是「私有候选 overlay → 唯一提交点 Add → 未发布即整体逆序回退」；回滚留着自己的分支——先 `rc.resident.Add(rebuilt)`（**提前公开**），再进 face 装配／激活，而那之后的失败**没有任何回退**：`rbTxn` 只在再获取失败分支里被使用，成功构建的 owner 根本没入责任表。有效代纹丝不动，在线清册里却多了一个活着、持有 store 写者租约、无人认领的 owner。违 2.3 防跑偏「不提前公开新增 owner」与 J9「回滚走同一发布事务」。

**红→绿（真实退出码，先钉后修）**：新增 `org_rollback_txn_test.go` 三测（对应 2.4 四行验收的后三行）：
- `(d)` `TestRollback24_LateStageFailureLeavesNoOwnerPublished`——**先实测红**：注入真实失败（plain 工具工厂按调用预算拒绝服务；预算由观测到的冷启动调用数标定，不猜常量），使回滚在**再获取成功、face 装配失败**的最后阶段停下 ⇒ 修前 `Expected nil, but got: &agent.TagentAgent{... memStoreRelease:(func() error)(0x10126ea90), memStoreOwned:true ...}`（**转储实证**：泄漏的 owner 连同其持有的租约还在清册里），修后绿。
- `(b)` 热增→numeric-only→**在途真实 B 调用跨回滚**→回滚：断宿主可见结果、**B 自身** `OrgKeepRecent`/`TaskManager().TerminalTTL()` 复原到环源值、`require.Same` 同址单 owner、回滚后新调用仍被服务——首跑即绿（该腿由轮九十主干达成，作为验收留档）。
- `(c)` 移除父保留共享子→回滚：共享子同址未重取，回滚真发布（代次前进）；若二次获取，单写者门会 fail-closed 在此。首跑即绿。
- 既有 `TestL3_*` 四测（启动代播种／numeric-only 首更挂钩／双轴不半换／revision 状态机）在重构后**全数保绿**——两轴语义与 ring 行为未动。

**实现（一次抽取，两入口共用）**：新增 `org_candidate_overlay.go`——`buildCandidateOwners` 把「reach 里缺失的 owner」构造进私有 overlay（在线快照∪本候选新增，跨 top 共享缓存禁第二 writer；每个建成/登记立即入有序责任表，先记后判错），`commit()` 只在调用方的唯一提交点把新增并入常驻表，`abandon()` 幂等且提交点后为 no-op、否则按获取逆序撤销登记/Close/复位指纹。正向分支改为调用它（`defer ov.abandon()`，提交点 `ov.commit()`，构建域 `ov.resolve()` 直接充当 face 解析域，原 `added/addedNames/txn/published` 手工簿记删除）；回滚分支删除 `rebuilt`/`rbTxn`/提前 Add/手写回退，改为同一 overlay。**结果**：后段失败在两个入口上语义相同（在线拓扑不动、资源归还），且 `residentMemFP` 登记不再只属于正向（回滚再获取的 owner 同样入账）。

**过程中的自纠（两处，都是我自己写的）**：① 首版锚点文件里我**杜撰了 API**（`entry.Rollback24With`、`agent.UnregisterPlainToolForTest`、`AgentToolWrapper.TargetOwner`）并留了占位符——按「以编译器为准」暴露后整文件重写为真实 API（`entry.Rollback()`／`residentCacheForTest(...).ContextManager().SubagentWrapper(...)`／包内既有 `mockCallableTool`）。② 新测自带固定注册名，`-count=3` 时**我自己复现了 §0 口径②的 panic**——改为 `sync.Once` 一次性注册＋每次运行重置计数器（预算/调用数），使该文件在重复门下可用而非申请豁免。

**门禁**：root 全量非 race ok 40.719s；root `-race` 连续 3 次 ok（**48.165s／48.937s／49.485s**，末次为含全部收尾改动的最终树）；`agent -race` ok **82.099s**；`TestDeshell_|TestOrgDelegation_|TestHotReload|TestOrgClose_|TestRetire_|TestSD_|TestExecGate_|TestRollback24|TestL3_` `-count=3 -race` ok **102.244s**（0 FAIL，含本轮三新测重复 3 轮稳定）；`task` 2.975s／`compress` 3.935s／`tool/action` 36.257s 逐包串行 ok；`go build`/`go vet ./...`=0；gofmt 净；`--strict` valid。未提交、未动索引/数据/远端/独立计划文件。**进度 26/34**（2.4 转勾；S-A~S-F 核心簇全部落地）。

### 5.49 轮九十三：§4.1 收口——有界返回之后恰一次最终退出，并**撤回一处把「永不完成」写成合同的旧断言**

**先说被撤回的东西**（本轮的红不在产品失效，在我自己写进测注的一条断言）：`TestLifecycle_UnconvergedExecutionHoldsStoreLease` 以
`assert.Zero(t, released, "the already-returned Close does not retroactively exit the store")` 收尾。前半（活写者未确认停止时**不**取 store 退出）是对的、保留；
后半把「返回之后再也回来」当成了契约——那正是 §4.1 要消除的放弃（写者名额与 owner 登记被永久占住）。tasks 4.1 的红→绿子句本就点名「当前停在 released 仍零，
须显式修订该旧断言并记原因」，故本轮按名修订：该测只留「有界持有＋诚实报告」两半，完成半交给新锚，并在测注写明撤回理由与其后继。

**新契约（`TestLifecycle41_BoundedReturnThenExactlyOneFinalExit`）**：一次 `Close()` 超时报告未收敛之后，**不发新请求、不再 Close、不起计时器**——
解除 producer 屏障即由同一 owner 的尾部完成最终退出，三样东西各恰一次：runner（`closed==1`）、store lease（`release==1`）、owner 登记（`revoke==1`）。
同时钉 §4.1 的前置半：在收敛判定出来**之前**，still-used 的工具 closer 不得被关（`closer.callsNow()==0` 于活写者期间）；尾部完成后初次报告仍可见
（重放的 `Close()` 仍 `ErrorIs ErrExecUnconverged`），最终完成另入 `DeferredCloseOutcome`；重复 Close 不重做清理（closer/release 计数不变）。

**实现**：
- `ContextManager`：`armFullyDrained(fn)`／`fireFullyDrainedIfQuiet()`——事件取自**回收路径本身**（`forgetBinding` 后判「无持有代 ∧ 引用归零」），
  `sync.Once` 保恰一次；arming 时若已静默则**当场执行**，故竞态不可能把尾部搁死。复用既有 `OutstandingRefs()`（不建第二 busy 计数，J7/防跑偏）。
- `TagentAgent.closeOnce`：把「先关 closer 再查收敛」的旧次序改成**先问收敛**（`OutstandingRefs()>0` ⇒ 工具 closer/recorder/store 全交给尾部）；
  收敛路径逐字保持原 §6.2 次序（`closers → runner → lease LAST` 的 trace 次序锚仍绿）。store 退出从内联 if/else 提成唯一 `exitMemoryStore()`，
  inline 与尾部共用同一函数 ⇒ 「同一退出槽、最后一步、未收敛不归还」只有一份拼写（§4.1/§6.3/M-2 语义不动）。
- 尾部 `deferFinalExit(carryClosers)`：每 owner **至多一个** continuation（`closeTail!=nil` 即拒第二次装载），执行体 `sync.Once`；
  hook 内 `go tail()`——工具 Close 可能阻塞在自己的 I/O 上，绝不在别的 binding 回收路径里内联跑（锁面：`DeferredCloseOutcome` 先读 CM 再取 `closeMu`，
  与回收路径不构成反向嵌套）。**无新增 goroutine 常驻、无轮询、无重试队列**（D8/4.1 禁止项逐条守住）。

**判别性（变异探针，用后还原并 grep 确认残留 0）**：P41（强制 `liveWork=false`，恢复旧次序）⇒ 精准红于「still-used 工具不得先于判定被关」；
P41b（尾部完全放弃＝§4.1 前形态）⇒ 精准红于「store 退出恰一次」。两条各打中一个断言腿，非空测。
**过程偏差如实记**：本轮新锚是在实现**之后**才写的（未先测红），故以上述两枚变异探针补足判别性证据，形态等同于「修前红」；不以此掩盖流程次序问题。

**门禁**：`agent -race` ok **82.644s**；root 全量 `-race` ok **46.252s／48.969s／50.772s**（末次为含探针还原与测注强化后的最终树）；
`TestRetire_|TestOrgClose_|TestLifecycle|TestRecycle|TestExecGate_|TestSD_|TestDeshell_|TestRollback24` `-count=3 -race` ok **100.349s**（0 失败）；
`TestLifecycle41_|TestLifecycle_Unconverged` `-count=5 -race` ok；`task` 2.987s／`compress` 3.908s／`tool/action` 36.346s 逐包串行 ok；
`go build`/`go vet ./...`=0；gofmt 净；`--strict` valid；探针标记 grep 残留 0。未提交、未动索引/数据/远端/独立计划文件。**进度 27/34**（4.1 转勾）。

**仍开放**：4.2（下一步，M2 后重估：先实证再决定，禁在重估前写新机制）；3.3 余「换代失监视」一条待逐腿核验；随后 P3 矩阵 3.4→5.1→5.2→5.3→5.4。

### 5.50 轮九十四：§4.2 重估——多级重入四态在生产入口**全部成立**，因此不写新机制；剩余腿精确记账

**这一轮的产物是结论，不是代码**（tasks 4.2 明写「实施先重估剩余面…禁止在重估前写新机制」）。本轮**生产零改动**：改动面＝一个新测文件＋两枚用后还原的变异探针。

**实证怎么做的**（行为压倒推读）：新建 `org_reentry_multilevel_test.go`，在 a→b→c 三级拓扑上、**经生产工具对象**（`relaunch_task`）驱动重入，b 的 board 上的 subagent 任务由真实异步委派自然产生（D3/7.2 spawner 归属）。四态：

| 态 | 结果 | 非空洞性证明 |
|---|---|---|
| ① 环存活期内 B 重入 C，且**在途发布删掉 C** | 绿（按发起代成功） | **P-PA**：令 `ResolveReentryDelegation` 的发起代分支失效 ⇒ 精准红于该断言，报出 spec 禁止的形态（改读有效面→拒绝合法 G1 绑定） |
| ② B 环静默后无发起者 relaunch | 绿（从 B 常驻 owner 面解析） | 与 ① 由「有无 ctx 租约」分流；P-PB 不影响它（它不依赖换代）⇒ 该态确证的是 owner 归属，非面推进 |
| ③ G2 删 C | 绿（具名拒绝 `EFFECTIVE orchestration generation`，被删目标零执行、不静默改道） | 前置断言在 P-PB 下即红（有效面未推进 ⇒ 前提不成立），说明测的确实是发布后的面 |
| ④ G2 改 C | 绿（无发起者重入打到**新代** C） | **P-PB**：去掉激活（＝轮九十前「未变父不推进 face」形态）⇒ 该态超时红。此态在主干前不可能成立，故锚同时是主干的回归件 |

**重估结论（写回本单，免下轮重复推导）**：4.2 的「resolver 增『任务所属 owner』明确定位」**无需新增机制**——归属已由既有三处共同达成且各有单点真源：(a) 工具经 `task.TaskControllerFromContext(ctx)` 取**调用者自己**的任务域，b 的任务不会出现在 a 的板上；(b) 活路径闭包 `subagentRelaunchClosure(w.parentCM, …)`／`subagentResumeClosure(w.parentCM, …)` 只捕**常驻 owner 的 cm**＋纯数据（不抓 wrapper/binding/私有 CM/旧 ctx）；(c) WAL 侧重投递捕 `ta.ContextManager()`（该 owner 自己的 cm）。三者都汇到 `ResolveReentryDelegation` 一处（§4.2「同一版本真源」，与 `subagentWrapperIn` 共用扫描）。

**探针纪律**：P-PA/P-PB 用后逐字还原，`grep "PROBE-P" --include=*.go .` 残留 **0**；还原后 root `-race` ok **49.924s**、`agent -race` ok **82.039s**（证明确无生产 delta 残留）。四态 `-count=3 -race` ok；`TestReentry42_|TestOrgDelegation_|TestOrgReentry_` 同跑稳定。

**4.2 不转勾**：验收子句要求「同步跑正常 Spawn 与 WAL 重建**两入口**」，本轮只钉了 Spawn 入口。WAL 重建入口的多级重入需要 org 级重启 harness（本仓仅有 agent 级重建测），记为剩余腿并已在 tasks 落清；另记既有边界：跨重启 subagent `Resume` 无 rounds 事件源＝硬拒（引导边界不扩展）。未提交、未动索引/数据/远端/独立计划文件。**进度仍 27/34**。

### 5.51 轮九十五：3.3「有状态工具」逐腿核验——**发现并修掉轮九十去壳丢掉的会话 tracker 重挂**，并实测证明「当时没有测能抓它」

**为什么现在做它**：3.3 的勾选前余项里唯一未核验的腿是「已纳管任务不因工具换代**失监视**」（轮九十一/九十二两轮均如实留账未凭印象勾）。核验方式＝读机制＋找锚＋变异探针，不读注释即信。

**发现的不是「缺锚」而是「缺实现」**：会话 tracker 是 `actionTool.IsTrackedSession` 的**绑定方法值**（捕获工具实例），而全仓唯一的重挂点 `tm.SetSessionTracker(...)` 在 `build_agent.go:730`——位于 `wireAgent` 尾段，其自己的注释写着「executorOnly 热重建换代 ActionTool 后，旧闭包指向旧 monitor，须重接（幂等）」。轮九十去壳后**已存在 owner 走 `buildAgentFace`＋`ActivateExecutor`，不再经 `wireAgent`** ⇒ 这条规则在发布路径上失守：换代装配了新工具，管理器仍读旧 monitor，存活会话被判「未跟踪」，而 §7 双通道回收（`RetireOrphans`／suspect→running 提升）正按该判定行动。

**缺口是真缺口（P-PC 实测）**：摘掉重挂后跑热更／委派／回滚／重入／退役／deshell／lifecycle 全家族 `-race` ⇒ **全绿 33.709s**。没有任何既存测会抓到这个回归——这正是它能在轮九十之后静默存在四轮的原因，也说明该腿必须补锚而非只改代码。

**修法（单点、提交后）**：新增 `activateOwnerGenerations(resolve, names, staged, parts)`，在**每个 owner 激活之后**紧接重挂 tracker；正向与回滚两处共用（两入口再同型一次）。时机是关键：若在候选尚未 commit 时重挂，一旦候选被丢弃，在线看板就指向了一个即将废弃的工具——故放在 `ov.commit()` 之后、与 `ActivateExecutor` 同循环。无 exec 工具的 owner 保持原 tracker（nil 守卫，与 `wireAgent` 同形）。`stageOrgGenerations` 因此多返回 per-owner 装配（`parts`），激活循环保留在调用方的提交序列里。

**失效机理的单测（无需 tmux，确定性）**：`tool/action/generation_tracker_test.go::TestActionTool33_MonitorsArePerGeneration` ——两台 ActionTool 各持独立 monitor（`require.NotSame`，若共享则重挂无关紧要、测的前提即不成立）；只在「当前代」那台上 `AddSession` 后，`prior.IsTrackedSession(sess)==false` 而 `live==true`，并直接以两台的方法值各调一次复现「存活会话被判未跟踪」的假阴性。它钉的是**危害的物理前提**，不是接线本身。

**该腿仍留一锚未补（如实记账）**：org 级端到端锚（真实热更＋真实纳管任务＋重启后裁决路径）需真 tmux 供给，属既有 tmux 测族（§5.36/§5.40 已记其为负载敏感家族）。**3.3 因此不转勾**：分类单点（§5.45）＋工厂门与合同迁移（§5.45/§5.47）＋工厂租约（§5.45→§5.47 结构取代）＋detector/重挂分离（`TestTaskIDBridge_*`／`TestCrossRestartResume_*`）＋disposer 归属（§5.40/§5.41）皆已有锚，唯此端到端腿待补。

**门禁**：root 全量 `-race` 连续 2 次 ok（**49.340s／50.761s**）；`agent -race` ok **81.849s**；`tool/action` `-race` ok 36.443s（含新测）；`task` 2.984s／`compress` 3.898s 逐包串行 ok；探针 P-PC 用后**逐字还原并与备份 diff 一致**（`IDENTICAL_TO_PRE_PROBE`），`grep PROBE-P` 残留 0；build/vet=0、gofmt 净；`--strict` valid。未提交、未动索引/数据/远端/独立计划文件。**进度仍 27/34**。

### 5.52 轮九十六：3.4 最难一行落地（真实 ACK→settle 新 turn 用 G2），并记一次**险些写入台账的假缺陷**

**差点误判（比新增一行更值得记）**：第一版锚按 `delegServed.ToolResults` 找 B 的答案，`-count=1` 得 `delivered=-1`，且任务板显示该任务 **completed** ——形状完全符合「异步结果失联」（项目保留原则「异步结果不失联」被破）。**若按静态推读就此立案，会写下一条不存在的缺陷**。改用「把 entry 每次请求的**全部消息**按角色 dump 出来」的探针重测，事实是：脱手路径正常投递，只是形态为
`role=user "[evt…|external_input] [task settled] ✓ b: work (id=…) completed → 结果: served:SUB-B-PROMPT"`
——settle 以 **user 角色通知**回流，不是 tool 结果，故既有 witness 结构对它天生失明。另一次自纠在同方向：第一版 dump 探针**忘记让 mock 真的阻塞在 gate 上**，于是它测到的是 inline 路径（差点得出与上一步相反的结论）。两次都是「行为压倒推读」必须配套「测的就是我以为在测的那条路径」。**结论：无产品缺陷**；本轮不立缺陷项，改的是测的探测方式。

**新增锚（3.4 点名『现有测试止于 inline 返回』的那一行）**：`org_cross_publish_vertical_test.go::TestOrgCrossPublish_SettleTurnRunsOnTheNewGeneration`。
- **真实 ACK**：`SetAsyncDenseDuration(30ms)` 令任务层真的脱手，发布前断言父 turn 已闭合且**没有任何 entry call 持有 B 的答案**（precondition，否则就退化成既有 inline 测）。
- **屏障跨发布**：B 的 producer 停在 gate 上，其间发布把 b 摘路由的新代（前置断言 `SubagentWrapper("b")==nil`），随后放行。
- **归属按因果**：新 witness mock 记录每个 entry call「其请求是否携带该 settle 通知」＋「该 call 被提供的工具集」。携带 B 答案的那个 turn 即 settle 抬起的新 turn，其工具集**含 c 不含 b** ⇒ 「新 turn 用 G2」；同时 B 全程**只被服务一次**（在途 G1 运行未被重跑也未被替代）。按 3.4 的告诫，**不**断言「C 总调用数必须零」——发布自身的 notice turn 在当前面委派是合法的。
- **判别性 P-PS**：摘掉 `deliverTaskSettled` 的无绑定 bus 回退（还原失联形态）⇒ 本测精准红于「settle 未抬起新 turn」（20.07s `Condition never satisfied`）；随后 `diff -q` 证 **byte-identical 还原**。

**逐行核实矩阵（不沿用文件头部的旧声称）**：`a2a_delegation_test.go` 中 `CheckOrgReload`/`Rollback` 出现 **0** 次 ⇒ 「远端重试过程中发布」在 org 入口**无锚**；该文件头原写「retry-across-publish 已钉在 `agent/turn_binding_test.go`」，核其内容仅覆盖**本地** pinned-executor 中途发布——声称与事实不符，已在本单改正为剩余项而非继续引用。另核得「热增后真实数据归属」仅到 store 身份（`NotSame`），无真实读写落位＋资源尾部 ⇒ 同列剩余。「多级重入跨发布」由轮九十四 `TestReentry42_*` 四态覆盖、「删除尚未调用的子 owner」由 §5.37 覆盖，本轮**不重做**。

**门禁**：`TestOrgCrossPublish_` 全族 `-count=5 -race` ok **3.020s**（0 失败——含时间敏感的 dense 窗与 gate，重复 5 轮稳定）；`agent -race` ok **82.067s**；root 全量 `-race` 连续 2 次 ok（**49.814s／50.429s**）；`diff -q` 证 `settle_routing.go` 与探针前备份逐字节一致；探针/一次性文件残留 grep **0**（`scratch_detach_probe_test.go` 已删）；build/vet=0、gofmt 净、`--strict` valid。**本轮生产零改动**（改动面＝一个新测＋两处已还原探针）。未提交、未动索引/数据/远端/独立计划文件。**进度仍 27/34**：3.4 余两行（远端重试跨发布／热增真实数据归属）未闭合，按勾选规则保持 `[ ]`。

### 5.53 轮九十七：3.4「远端重试过程中发布」闭合——**该行当场挖出一个使热更对 remote-only 部署永久不可用的产品缺陷**

**为什么现在做它**：3.4 在轮九十六逐行核实后余两行，①「远端重试过程中发布」是其中唯一直接压 D6/J10（传输重试固定端点与载荷）的一行，也是 3.3 余项②早年记「归 3.4」而未两处重复勾的那一行。

**红基线（两次，皆如实记）**：第一版测写完即红，但红因是**我自己的 harness 缺陷**——handler 内改写 yaml 用的 mtime 仅 `now+2s`，与初次 `crossWrite` 的 `now+2s` 同量级，`CheckOrgReload` 见 mtime 未严格变新便提前返回，发布成了 no-op（教训：预置「发布确已生效」的断言不是形式主义，它把一次静默 no-op 直接暴露成 `gen=0`）。改成 `now+90s` 后第二次红才是**真缺陷**：
```
[org-hotreload] NEW agent "knowledge" is referenced but not defined — serving previous (fail-closed)
```

**缺陷本体**：`org_candidate_overlay.go` 的「引用未定义」门（§5.11）与 trunk 的 `stageOrgGenerations` 发布循环都只认 `next.Agents[name]` 是否存在。remote-only 声明**没有本地定义却是完整定义**（§5.44 确立的铁律：校验域与构建域读同一事实，谓词就是 `ToolRef.isRemoteRef()`；`build_agent.go:941` 据此解析远程 wrapper 且**不建 executor**）。于是：**任何含 remote-only 子 agent 的部署，冷启动正常、首次热更即被永拒，回滚同样失守**——热更/回滚对这类拓扑永久不可用。属「第三个领域忘了那把共用尺子」的同类错误（§5.44 修的是校验 vs 构建，这次是 reload 发布 vs 同一谓词）。

**修法（复用谓词，不削弱门）**：新增 `remoteDeclarationOnly(next, name)`——名字本地未定义、且**所有**指向它的 agent 引用都是 remote 引用时才为真；混合可达（同时被非 remote 引用）仍照旧 fail-closed，因为那条引用确需一个真实 owner。候选 overlay 与发布两处循环据此**跳过无 owner 可建的声明**。`reachableAgents` 语义**不改**：该名字确实被 entry 拉入（委派真会发生），只是它不常驻——把「可达」与「有 owner」分开，才不削弱退役账/分区守卫等既有消费者。

**锚**：
- `TestRemoteRetryAcrossPublishKeepsTheDeclaredEndpoint`：在 503 handler 内**同步**发布一版把同名 agent 改指到**另一台端点**的配置，故重试发生时 G2 已生效（窗口由失败本身造成，不靠 sleep）。判据按因果而非计数——**父 turn 拿到答案之前，后继端点一旦被联系即红**（那意味着重投改道到新一代）；实测 `origRPCs=2 / succRPCs=0 / gen=1`，且每台记录的 RPC 载荷都含 `"retry across a publish"`。
- `TestRemoteDeclarationOnlyKeepsTheGate`：四态钉住新跳界的边界（纯 remote 为真／混合可达为假／本地已定义为假／`Remote` 无 URL 的 §5.44 误配为假），防「为放行一处而把门整体放宽」。

**门禁**：a2a 族 `-count=5 -race` ok（7.396s，0 失败——含 httptest 与多 goroutine）；root 全量 `-race` 连续 2 次 ok（**51.385s／50.988s**）；`agent -race` ok **82.467s**；`task` 2.953s／`compress` 3.936s／`tool/action` 36.324s 逐包串行 ok；热增/热删/回滚/候选/跨发布/nested-hop/deshell 家族 `-count=3` ok 2.568s；诊断插桩（`hookFired`/`DIAG`/`sync/atomic` 导入）用后**全部移除**，grep 残留 0；build/vet=0、gofmt 净、`--strict` valid。**本轮有生产改动**（`partition_collision.go` 新谓词＋两处循环跳过），未提交、未动索引/数据/远端/独立计划文件。**进度仍 27/34**：3.4 余「热增后真实数据归属」一行（须先定可判证形态，不在长轮末尾仓促钉空洞锚）。

### 5.54 轮九十八：3.4 最后一行「热增后真实数据归属」闭合，并消除矩阵的**两层拼接**形态（本项转勾）

**为什么现在做它**：3.4 前五条具名行里只剩「热增后真实数据归属——各给宿主返回与资源尾部证据」。此前所有热增证点停在 **store 身份**（`require.NotSame` 两台 store），而身份不同**不等于**数据落对地方：一个把子 agent 回合写进宿主 store 的实现，在身份断言下照样全绿——这一行如果只补「再断言一次 NotSame」就是空洞锚，所以它必须走完整数据流。

**判证形态（先定形态再写测，避免写出可能空洞的锚）**：冷配置只路由 sub1（sub2 定义在配置里但不可达 ⇒ 无 owner）；发布把 sub2 变为可达，其 owner 经热路径构造、store 为 `type: localfile`；然后**一条真实委派链**上取证五步：
1. **宿主返回**：entry 的后续请求里真的带着子 agent 载荷（`action_command` 工具结果）；
2. **真实落位**：子 agent 自己回合的 `agent_output` 从**它自己的 store** 读回（读的是落库记录本身，不是框架转述）；
3. **跨 owner 不可见**：宿主 store 在该子分区下**什么都看不到**；
4. **真落盘**：localfile 目录里的字节可寻到该载荷（物理归属，不是内存幻觉）；
5. **资源尾部**：Close 后这个数据的主人撤销其 store 注册、自身 close 序列真跑过。

**两次探测自我纠偏（都记为过程事实，非产品缺陷）**：
- 首版红在**第 1 步**，红因是 harness：`ownerYAML` 声明了 `model:`＋providers，而 `resolveAgentModel` 的**文档优先级第 2 条**只在 agent 未声明 model 时才采用宿主注入实例——真连 `http://localhost:1` 是预期行为。查清优先级后改用与本仓约定一致的「无 model/providers 段」（`delegYAML` 同一手法），不擅自「修」一个我误读的设计。
- 次版红在**第 2 步**且**两个 store 都查不到任何事件**，看着像归属大面积错乱；实为 `resolvePartitions` 的**隔离契约**：无 partition 过滤时「scan nothing」（两种 store 实现按 parity 合同一致）。真正的归属轴在 `plugin/memory_plugin.go:124`——`PartitionIDFromName(Invocation.AgentName)`，而 `cm.partitionID` 只管任务记录等另一族事件。据轴取分区后一次通过：`sub2@sub2 = ["agent_output|HOTADD-ANS-98"]`、`entry@main = ["action_command|\"HOTADD-ANS-98\""]`——正是「子回合归子、返回值归宿主」的正确双份证据。

**判别性（P-PD2）**：第一次探针打在 `cm.partitionID`（context_manager.go:448）上**没有**让本测变红——那确实不是回合记录的归属轴，探针无效而非锚点无效；改打真轴（把 `PartitionIDFromName(agentName)` 换成固定名，即所有主人写进同一命名空间）⇒ 本测**精准红**于「同一台 store 换主人分区应看不见」的自检断言。测内那条自检断言因此不是装饰：它是非空洞性的直接见证。

**消除两层拼接**：3.4 明令「结果不得是…两层测试拼接」。轮九十七的远端重试行其**资源尾部**当时依赖 agent 层 d6 租约见证，属拼接。本轮把尾部收回**同一条链**：捕获发布前那一代的 id，断言其携带在途远端调用被退役后、调用落地时该行从账面消失（`hasGeneration` 读的正是回收判据所用的账）。至此矩阵每一行的「宿主返回＋资源尾部」都在生产入口的同一条调用链内取证。

**转勾判断**：五条具名行——真实 ACK→settle 用 G2（§5.52，P-PS 判别）、远端重试跨发布（§5.53，红基线＋remote-only 永拒缺陷修复）、多级重入跨发布（§5.50 四态）、删除尚未调用的子 owner（§5.37）、热增真实数据归属（本轮，P-PD2 判别）——全部有入口级锚；「以来源/顺序区分而非总数零」由 §5.52 与新测共同遵守；常驻身份与真实事实续写由本轮第 2/3 步＋`TestOrgClose_DoesNotReplaceOwners`／`KeepsOwnerButStopsRouting` 覆盖。**如实标注见证形态**：「settle 新 turn 用 G2」的判据是该 turn 被提供的声明集合，「被提供⇒被调用」由同一入口的另一锚证明——两者是同一层的不同侧面，不是把一层测两遍。

**门禁**：`TestRemote|TestHotAdd34_|TestOrgCrossPublish_|TestOrgClose|TestOrgHotAdd` 家族 `-count=3 -race` ok **7.396s**（0 失败）；本轮内另有 `-count=5 -race` ok 3.561s；root 全量 `-race` 连续 2 次 ok（**50.989s／50.238s**）；两处生产探针（`agent/context_manager.go`、`plugin/memory_plugin.go`）用后 `diff -q` 证 **byte-identical 还原**，`grep PROBE-P|DIAG98` 残留 **0**，一次性文件 0；新增仅 `org_hotadd_data_test.go`（生产本轮零改动）；build/vet=0、gofmt 净、`--strict` valid。未提交（staged 0、HEAD 仍 b0e053f）、未动索引/数据/远端/独立计划文件。**进度 28/34**：2.x 全勾、3.1/3.2/3.4 已收，3.x 仅剩 3.3 的 tmux 端到端腿；另有 4.2（WAL 重建腿）与 P3 的 5.1–5.4。

**工件自检（过程失误，如实记）**：本轮一次批量改 §0 的脚本在中途 assert 失败，使一句「P2（2.x＋3.x）全部闭合」的**失实措辞**（3.3 实未勾）先写进了基线行与上面门禁段。复核 §0 三行一致性时发现，已改为准确表述。它没有流入任何勾选判断（转勾依据只看锚与门禁），但说明**批量写工件后必须逐行回读**——尤其进度/余项/基线三行彼此约束，半途失败的脚本会留下互相矛盾的组合。

### 5.55 轮九十九：4.2 的 WAL 重建入口闭合（org 级跨进程重启），本项转勾

**为什么现在做它**：4.2 的验收子句写的是「同步跑正常 Spawn 与 **WAL 重建两入口**」。轮九十四把 Spawn 入口四态钉死后如实记：重启入口「尚无 org 级重启 harness 与锚，本仓只有 agent 级重建测」。本轮就补那一句。

**harness 形态（不新造机制，用本仓已确立的 xproc 纪律）**：`org_wal_restart_reentry_test.go::TestWAL42_RelaunchAfterRestartResolvesOnTheCurrentFace` 与既有 `TestLatestPathOnly_ThreeBootStates` 同型——**每次 boot 各占一个独立进程**（`runBootChild` 复用），因为一次 boot 只有真实进程启动才算证据，同进程里翻动 New/Close 不是生产路径。三个角色：
- **spawn 子进程**：a→b→c，c 的生产停在测试屏障上（任务永不走到终态 settle），等 spawn 记录经 durable 路径落下后**不调 Close 直接 `os.Exit(0)`**——留下的就是「只有 task_spawned、无终态」的崩溃形状事实链。**不手搓内部记录格式**：WAL 状态由真实执行产生。
- **restart 子进程（态①合法重放）**：在同一批 localfile store 上重启，断言 b **自己 board** 上折回那个存量子 agent 任务（org 级 registry fold 成立），再用**生产 `relaunch_task` 工具对象**对 b 自己的 controller 重放 → 解析成功且深度 2 的 C **真被执行**（serve 计数增长）。重启后不存在在途发起者，走的是「无发起者」分支——它必须回退到 b 的常驻 owner 面。
- **restart_drop_c 子进程（态②摘路由拒绝）**：父进程在重启前把 c 从拓扑移除，同一存量任务必须**按名拒绝**且被删目标零执行——这正是 `wireAgent` redispatch 注释点名的事（「若传快照，后续代移除的目标仍会被旧代绑定静默复活」）。

**两个自我纠偏（都不是产品缺陷，都记为过程事实）**：
1. 首版把态①与态②串在**同一个 store 根**上：态①成功后任务已终结并落终态记录，态②的前置「board 仍带该任务」自然红。这**恰好是一次意外的判别证明**——fold 前置断言确实有牙（已终结任务折回为空是正确行为，不是被测对象）。改法：每个 scenario 独立持久根。
2. 判别性探针 **P-PW1** 先看第一版落点：把重建入口 redispatch 的 resident owner 摘掉（`build_agent.go:739` 传 nil）⇒ 态①精准红于「重建出的合法任务在重启入口必须能重放」，报出的正是 resolver 的 `carries no resident owner` 分支——证明锚真依赖 4.2 点名的接线，而不是绕道别处碰巧通过。

**四态在重启入口的可表达范围（如实标注，不冒充覆盖）**：重启后没有「G1 发起者在途租约」这种东西，故此处只能钉「可解析真执行」与「已摘路由拒绝」两态；「G1 发起者」「G2 改 C」属 Spawn 入口，已由 §5.50 钉住。既有边界不变：跨重启的 subagent **Resume** 无 rounds 事件源＝硬拒，本测不扩展该引导边界、也不把它算作通过。

**门禁**：`TestWAL42_` `-count=3 -race` ok **26.759s**（0 失败；子进程继承 race 检测，§6.6 无豁免）；root 全量 `-race` 连续 2 次 ok（**59.978s／59.521s**——较此前约 51s 的增量即真实跨进程重启的开销）；`TestReentry42_|TestOrgReentry_` `-count=3` ok 1.125s；P-PW1 探针 `diff -q` 证 `build_agent.go` **byte-identical 还原**，`grep PROBE-P` 残留 **0**；build/vet=0、gofmt 净、`--strict` valid。**本轮生产零改动**，新增仅一个 xproc 测试文件。未提交、未动索引/数据/远端/独立计划文件。**进度 29/34**：第 1–4 章仅剩 3.3 的 tmux 端到端腿，其余全部闭合；剩余 5 项＝3.3＋5.1–5.4（P3 矩阵／终门）。

### 5.56 轮一百：3.3「换代不失监视」的 org 级端到端锚（真实 tmux 纳管会话），本项转勾

**为什么现在做它**：§5.51 修掉去壳丢掉的 tracker 重挂后，如实记下 3.3 唯一未闭合项——「换代不失监视」缺 **org 级端到端锚**（当时只有 `tool/action` 的机理性单测：两台 ActionTool 的 monitor 相互独立 ⇒ 陈旧闭包对存活会话必假阴性）。本轮补那条腿。

**先量再写（不凭猜写 250 行）**：一次性探针实测出三件事实，写测后即删——exec 调用 **10.02s** 返回后台通知；板上任务 `kind=command status=running`；会话名 = `Declarative.TaskID`。这三条决定了锚的形态与代价。

**锚＝`org_monitor_reattach_test.go::TestMonitor33_LiveSessionStaysWatchedAcrossToolGeneration`**（三个独立进程 boot 共用一份持久根，真实 tmux）：
1. **spawn**：真实起一个 `mode=resident` **且带 `name=`** 的长命令会话，任务不终结，**不调 Close 直接退出**——tmux 会话属 server 不属进程，因此活到下一次 boot。
2. **restart**：新进程的当代 monitor 必须仍认得那个活会话，于是恢复为 suspect 的纳管任务被 TaskID 桥提升回 **running**。两枚变异各打一侧：**P-MM1** 令 `IsTrackedSession` 恒 false（监视信号换代丢失）⇒ 精准红「实得 status=suspect」；**P-MM2** 令启动重挂不跑（`residentReattachOnce` 支路短路）⇒ 同测红。这两枚合起来就是「已纳管任务不因工具换代失监视」＋「声明构造与恢复重挂/monitor 激活分离」的后果级证据。
3. **hotreload**：进程内发布新一代（generation 真前进）后，当代仍**持有并可寻址**那个活会话——同名第二次派生被拒、`command` 任务数仍为 1、原会话仍活；并带**非空洞性守卫**（记录哪一次调用真发出了工具调用）。

**命名会话是被测前提，不是细节（第一次红在这）**：`CleanupOrphanSessions` 只放过 `n-` 前缀（命名）会话，匿名 `prefix-<ts>` 会被下一实例收掉；而 `ReattachResidentSessions` 只重挂持久元数据里的 resident/interactive 会话。所以 `mode=resident` 不带 `name` 时，第二次 boot **正确地**把会话清了——我最初的「precondition: session must still be alive」红其实是自己踩了正确语义。工具 schema 本就写着「Named sessions are addressable across calls (restart/exists)…Recommended for mode=resident」。

**两次自纠（都不是产品缺陷，也都未流入勾选判断）**：
- **一个错误假设被自己的核查证伪**：我看到 `build_agent.go:418` 的重挂重入被 `mode.isExecutorShell()` 门控，据此推断「轮九十去壳后热更不再重挂＝第二个 trunk 回归」。读调用点后**错**：`buildAgentFace`→`assembleAgentConfig` 传的正是 `buildModeExecutorShell`（trunk 复用了这个 mode 值），重挂对每次 face 构建都生效；前台跑该相位亦见 `recovery: reattached 1 resident session(s)` 与 `executor generation 1 swapped` 同秒。假设撤回，**未改一行产品码**——这正是「忠实执行错误设计」的反面练习：先证伪自己，再动代码。
- **我自己的测里有一处真数据竞争**：hotreload 的轮询谓词无锁读 `m.calls`，而模型在有锁写。`-count=2 -race` 抓到（4 failures，全部来自这条），改为加锁访问器 `count()` 后连续两轮全绿。另有一次更弱的红：首版 hotreload 断言去 tool-role 消息里找拒绝文本（框架把工具错误另处投递），依赖了投递形态——改为「调用是否真发出工具调用」的形态无关守卫。

**覆盖范围（写进 tasks 子弹，防误读）**：hotreload 相位证明的是**所有权/可寻址性**跨发布仍在——重名拒绝读的是 tmux 地面真值 `SessionExists`，**不是**新 monitor 的跟踪集合。「新代是否真跟踪该会话」在**重启**代际边界被②＋P-MM1/P-MM2 钉死；要在**热更窗口**内让它可判红，须先把任务推进 suspect，而 `quiet_timeout` 下限被钉死在稳定窗（60s，TUI 90s），常驻该窗只会加入负载敏感红灯族（§5.36/§5.40），故**刻意不钉**。3.3 因此转勾，但勾的是四条子句各有入口级证据，而非「每个窗口都各有专测」。

**会话卫生**：清理只打本测自己的确定名字（`n-mon33svc`，非该名字一律拒杀并记日志）；spawn/hotreload 相位开头各做一次**幂等预清理**（命名会话会像跨重启那样跨失败运行存活，重名会被拒——不做预清理则该锚在一次失败后永不可重跑，此为探针期间实测）；全轮跑完 `tmux ls | grep -c mon33` = **0**。

**门禁**：`TestMonitor33_` `-count=2 -race` ok **32.137s**（0 失败 0 竞争）；单轮 `-count=1 -race` 12.3s；root 全量 `-race` 连续 2 次 ok（**73.771s／73.156s**——新增即真实 tmux 与子进程开销）；`tool/action -race` ok 36.315s（P-MM1/P-MM2 两枚探针均 `diff -q` 证 **byte-identical 还原**，`grep PROBE-M` 残留 0，一次性探针文件已删）；build/vet=0、gofmt 净、`--strict` valid。**本轮生产零改动**。未提交（staged 0、HEAD 仍 b0e053f）、未动索引/数据/远端/独立计划文件。**进度 30/34**：第 1–4 章全部闭合，余 4 项＝P3 的 5.1→5.2→5.3→5.4（终门）。

### 5.57 轮一百零一：P3 · 5.1 前半（诊断实现逐句核对＋导出面清理），并把发布入口问题停下上报

**为什么现在做它**：第 1–4 章已在轮一百闭合，按依赖表进 P3。5.1 有两半——**实现**句要求诊断一次读取已提交应用记录、把真实引用债务另明其实时性质、`inFlightTurns` 只数业务 turn，并「删仅供测试无生产价值的公开状态 setter／内部对象暴露」；**验收**句要求诊断与真实消费者互证。本轮做完可判定部分（核对＋清理），把需要裁决的公开契约问题停下上报。

**实现句逐句核对（读码确认，不是声称）**：
- 「一次读取已提交应用记录」与「不把多次无锁 getter 拼成原子成功快照」：`tagent.go:426` 注入的诊断闭包先 `st := coord.status()` **一次读齐**，cfg/revision/generation/两时间/逐 agent 回执**全部取自同一个 `st`** ⇒ 两句成立。
- 「`inFlightTurns` 只数业务 turn；子调用/后台另报；不把发布槽算业务」：`ExecutorRefs` 已是 `InFlightTurns`(LeaseTurn)／`SubCalls`／`BackgroundRuns`／`PendingRetirees` 分报结构，其文档自明「只读、无任何执行路径据它分支，故不会成为第二真源」⇒ 成立。
- 「draining 保最后值」「回执由真实消费者背书」：已有 `TestD51_ReceiptIsBackedByRealConsumers`／`TestD51_DrainingReceiptTracksHeldConsumer` 两锚（J11：不另堆同形测）。
- **未满足**的两点已列为剩余：债务键的实时性质标注（载荷把记录字段与实时 `executors` 并列，键名不可区分原子性）、以及「真实子调用预算/TTL」与诊断键的一致性互证锚（6.4 证了真实消费，未与诊断输出对齐）。

**本轮清理**：①删 `(*TagentAgent).SwapExecutor`——纯转发、**无生产调用方**、且**它自己的文档就把编排换代指向另一个入口**；唯一用户是 `org_hotreload_e2e_test.go` 一处断言，已显式迁到 CM 级入口并在测注记因（非静默改测）。②`LiveCMs` → 包内 `snapshotLiveCMs`——四个用户全在 `agent` 包内测，而 `agent.go:97` 早已注明该清册「无生产读取方」，向外暴露内部 `*ContextManager` 切片正是本子句要消除的形态；同包内收缩可见性零成本。③经核**保留**：`ExecutorConfig`、`LiveCMCount`（`owner_obligation.go:54` 用它报 Invocations）、`SetHotSource`/`HotSnapshot`（6.4 pull 真源）、`RecordResidentSession`。

**一次即将发生的错删（本轮最有价值的过程教训）**：普查导出面时我用 `grep '\.\s*Method('`，得出 `RecordResidentSession`、`LiveCMs` 「prod=0」，据此几乎要把 `RecordResidentSession` 当死 API 删掉（轮九十一「唯一用户消失即不留死公开 API」的先例极易被套用）。删除前改用**不带括号**的全量引用检查，才发现它是**以方法值**接进 sink 的：`tagent.go:1073`、`build_agent.go:652` 的 `SetResidentRecordSink(ta.RecordResidentSession)`。**结论：普查导出面使用必须覆盖方法值形态**，否则「零用户」是观测手段造成的假事实——与 §5.52「测的要真是我以为在测的那条路径」、§5.55「独立根避免把正确行为当被测对象」同源：错的是探针，不是被探物。

**停下上报的设计问题（未自行执行，需你裁决）**：清理后仍**三个发布入口并存**，而 trunk 唯一生产路径是 `StageExecutor → WireOrgGeneration → ActivateExecutor`；另两个 `cm.SwapExecutor`、`cm.PublishExecutor` ~~**现均无生产调用方**（前者只剩 `swap_executor_test.go`；后者只剩 d6 租约测与本轮刚迁入的那处 e2e），即它们是轮九十之前的发布入口~~——**【轮一百零二更正：后一半是失实计数】**`PublishExecutor` 实为 **37 处调用、跨 13 个测试文件**的受支持单 owner 发布原语（文档即「organization version switch 的 ONE linearization point」），我当时只看 `head -8` 的前几行便下断言。裁决因此收窄为：可删的是 `cm.SwapExecutor`（3 个测试用户、无生产用户，且它只换 runner 不换 `execCfg`＝face/runner 错配源），`PublishExecutor` **保留**；而它与 `ActivateExecutor` 各自实现同一条线性化（差别仅 binding 来源）这一真实重复，改列为 **5.3** 具名项（那里正题＝「验证实现确实更简单」），不混进 5.1 的删除项。选项：**(a) 折叠删除**并把三个接缝测重指到 Stage/Activate——其语义（turn 级 drain-free、并发无撕裂、常驻不变量）仍真实，但须以真 CM 重写而非裸 `&ContextManager{}`，属独立一轮工作量；**(b) 明确保留为受支持的单 owner 宿主 API**——则必须改写文档与注释。**我建议 (a)**：多入口即第二真源，与本变更在别处消除双事务/双机制（§5.46 去壳、§5.47 工厂单路径、§5.48 候选事务合一）方向一致；但它改动公开契约与测试形态，故按规停下，不自行推进。

**工件自纠（本轮两次）**：①批量写 evidence 的脚本文件本身含非法字节而中止，**未产生半成品写入**（写后 grep 计数 0 已核实），改用逐项编辑工具；②一次逐项编辑的旧串匹配到索引行**行尾**，把两行合并并留下一个游离标题——当场回读发现并即刻修复，同时改掉上一轮索引里的错字「交代不失监视」→「换代不失监视」。教训与 §5.56 同源：**写工件后必须逐行回读**，且锚串必须选在唯一且非行尾的位置。

**门禁**：root 全量 `-race` ok **76.451s**（0 失败 0 竞争）；`agent -race` ok **82.649s**（含迁移与改名的四处测试）；`TestOrgHotReload` ok；build/vet=0、gofmt 净、`--strict` valid。本轮**有生产改动**（删一个公开转发方法、把一个导出访问器收缩为包内）。未提交（staged 0、HEAD 仍 b0e053f）、未动索引/数据/远端/独立计划文件。**进度仍 30/34**：5.1 不转勾（入口裁决待你定，另两条腿未补）。~~另纠正 5.1 代码范围的失实指针：`agent/owner_retirement.go` **不存在**，退役与义务实际在 `agent/owner_obligation.go` 与 `agent/context_manager.go`。~~【**轮一百零三撤回：这条“纠正”本身是假的**——5.1 代码范围一直写的是 `owner_retirement.go`（无 `agent/` 前缀），该文件在仓库根确实存在。我按自己脑补的前缀去 grep、再据“查不到”断言指针失实，属「先造错、再纠正这个错」。详见 §5.59 与 §3 台账。】

### 5.58 轮一百零二：按建议 (a) 落地发布入口收敛——**但先证伪了我自己上一轮的普查**

**你以 `/opsx:apply` 授权建议 (a)（折叠删除多余发布入口）。开工第一步不是删，而是把上轮的计数重新做一遍**——结果发现上轮那条「`cm.PublishExecutor` 只剩 d6 租约测与一处 e2e」是**假的**：真实数字是 **37 处调用、分布在 13 个测试文件**（`executor_publish_test`／`executor_isolation_test`／`turn_binding_test`／`d42_reentry_test`／`exec_lease_retire_test`／`context_manager_recycle_test`／`relaunch_effective_generation_test`／`recovery_notice_boundary_test`／`org_cross_scenario_test`／`executor_perf_boundedness_test`／`executor_publish_bench_test`／`d6_lease_test`／`org_hotreload_e2e_test`），而它自己的文档就写着「the ONE linearization point of an organization version switch」。根因很朴素：我 grep 之后只看了 `head -8` 的前几行就下结论。**已入 §3 台账**（与 §5.57「方法值漏查」同一族错误：观测手段先于结论出错——一次是把活的判成死的，一次是把 37 个用户读成 2 个）。

**于是裁决收窄为一条明确的删除**：`cm.SwapExecutor`。它的真实问题不是「旧」，而是**语义上不完整**——只换 `cm.runner`、不写 `cm.execCfg`，于是那一代「实际执行的东西」与「记录的面」可以不一致（re-entry 解析、压缩器配置、工具声明都从 face 走）。这正是 §2.1/§3.2 立的「每 owner 只有一个线性化点」的反面，也是轮九十「不得再手工 SwapExecutor 二次换入（会把在用 runner 误 retire）」的出处。它只有 3 个测试用户、无生产调用方。

**迁移（语义不变，非降格）**：`swap_executor_test.go` 三测 → 新文件 `executor_publish_seam_test.go`，改走 `PublishExecutor`：
- `TestExecutorPublish_DrainFreeTurnLevel`（在途 turn 持旧 runner 引用跑完，下一 turn 起新）；
- `TestExecutorPublish_ConcurrentPublishVsRead`（50 次发布 vs 4 个读 goroutine，引用绝不为 nil／半换）；
- `TestExecutorPublish_ResidentInvariants`（taskController/projection/bus 原封＝状态⊥执行器）。
`fakeRunner` 随迁（`executor_publish_test.go` 也在用它，删旧文件会断）。旧文件删除。三测 `-count=5 -race` 全绿——注意迁移后它们比原来**更强**：`PublishExecutor` 会建代并退役上一代，所以 drain-free 现在是在真实绑定生命周期下被钉的。

**未删的部分给了正确的归属**：`PublishExecutor` 与 `ActivateExecutor` 各写了一遍同一条线性化（`publishBindingLocked` vs 内联 same-guard→adopt→face 赋值→换 runner→`retireBinding`，差别仅在 binding 来源）。这是**实现重复**，不是死 API，故转入 **5.3 具名项**（其正题正是「验证实现确实更简单」），并要求合并时逐项可证（同对象重发不产生第二身份／adopt 保留／`isolatedCopy` 不别名／退役仍在锁外）——不塞进 5.1 的删除动作里顺手做。

**门禁**：root 全量 `-race` 连续 2 次 ok（**75.898s／74.119s**，0 失败 0 竞争）；`agent -race` ok **82.137s**；接缝测 `-count=5 -race` ok；`agent/task` 2.963s／`agent/compress` 3.998s ok；build/vet=0、gofmt 净；三处陈旧注释同步（`executorMu` 字段注释、`buildRunner` 注释、`helpers.go` 的 race 说明）——保留 `org_hotreload.go:65` 的历史叙述不改写。工件：tasks 5.1 裁决子弹重写＋剩余三项重编号、5.3 新增具名项、5.4 文件清单的失实名纠正、evidence §3 台账行＋§5.57 就地更正。**进度仍 30/34**（5.1 余三条诊断腿；本轮删的是生产公开 API，属实质改动，非文档收口）。

### 5.59 轮一百零三：5.1 三条诊断腿收口（本项转勾）——顺带挖出一个**使新 owner 缺席回执与记录源**的次序缺陷

**为什么现在做它**：5.1 的验收子句写「热新增/**结构发布后**的真实子调用预算与 TTL（非 getter 回声）」——`TestD51_ReceiptIsBackedByRealConsumers` 钉的是已路由拓扑上的 numeric-only 应用，这一变体没人钉过。开工方式是照子句去读载荷，而不是先假设诊断面是对的。

**量出来的缺陷（不是推读）**：结构发布把 sub2 路由进来后 `resident=[leaf main sub1 sub2]`、`generation` 已前进，但逐 agent 回执只有 `[main sub1]`——**本轮刚装上的消费源恰恰没有回执**，而这正是「回执描述已安装消费源」的字面要求。根因是次序：`applyHotAll(snapshot)` 跑在 `ov.commit()` **之前**，那一刻候选新增的 owner 还不在常驻表里。后果不止诊断面：`applyHotAll` 同时负责给每个 owner **注入记录源**（`a.SetHotSource(coord.currentHotFor)`），于是新 owner 要等到**下一次 numeric-only 应用**才被接上唯一记录——中间它读不到自己的记录条目。修法＝把该调用移到 `ov.commit()` 之后、激活与 `coord.swap` 之前：仍在同一个 `mu` 临界区内，提交屏障语义不变，记录随同一次 swap 轮转。**修前红是实测**（回执集与 TTL 断言两处精准红），修后同测绿。

**载荷契约（把 §5.1 的实现句变成可读形状）**：新增两组——`liveDebt{capturedAt, executors, pendingRetirements}` 与 `close{initiated, resourcesExited}`；原先平铺的 `executors`/`pendingRetirements` 并入 `liveDebt`。这不再是“美观问题”：`OrgStatus` 来自**同一次** `coord.status()` 读取（原子），而引用账是**此刻**读的，混在一层里就等于邀请消费者把拼接视图当成成功快照（正是子句禁止的那件事）。两处读者显式迁移并记因：白名单键集、以及 `org_retirement_test.go` 由「键不存在」改判「列表为空」——缺键与零债务是两件事，前者无法与“诊断面没接上”区分。**契约接缝自证有效**：加键后白名单测立刻红于 `unexpected diagnostics key "close"`。

**两条新锚**：
- `TestD53_HotAddedOwnerReceiptMatchesRealConsumption`：回执数字 == 新 owner **自己** compressor 的预算线；TTL 在其消费者边界解析（sub2=3m）；**非回声三重守卫**＝同一次发布同时装上 `leaf`（未配 TTL ⇒ 内置 10m）与宿主（9m），一轮内三个 owner 三个不同值，全局 setter 广播不可能造出。任务的 `Spec.TTL=0` 如实断言为设计的「继承本 owner manager 默认」，我没有把它伪装成 per-task 覆盖（第一版正是这样误测，量出 0s 后改的）。
- `TestD53_CloseInitiatedIsDistinguishableFromResourcesExited`：用**真实在途引用**（`AcquireLease(LeaseBackground)`）构造 §4.1 的有界返回场景，钉四态 (发起,退出)＝(F,T)→(F,F)→(T,F)→(T,T)。第一版我把前置写错（先持租约再期待“已退出=true”，而 `DeferredCloseOutcome` 在 `OutstandingRefs≠0` 时给 false 是**正当**的）——是我的期望错，不是产品错，按事实改了顺序。

**再撤回我自己上轮的一条假纠正（本轮第三次同类错误，必须记）**：轮一百零一我写「5.1 代码范围的 `agent/owner_retirement.go` 不存在」。**两处皆错**：本单一直写的是 `owner_retirement.go`（无前缀），且该文件在仓库根存在（`retirementLedger.diagnostics()` 就在里面）。错因链条很清楚：我脑补了一个前缀 → 按它 grep → “查不到” → 断言指针失实 → 还把这条“纠正”写进 tasks 与 evidence。**先造错、再“纠正”这个错，比原始错误更糟**，因为它看起来像是已经核查过。已撤回 5.1/5.4 两处文本并在 §3 台账立案。与前两次（§5.57 方法值漏查、§5.58 `head -8` 截断）同族：**凡下“不存在/零用户”判断，先确认搜索串来自被引文件的原文，并读到计数为零为止。**

**门禁**：root 全量 `-race` 连续 2 次 ok（**74.440s／74.021s**）；`agent -race` ok **82.453s**；诊断／热增／回滚／候选／退役／S-C 家族 `-count=3` ok **98.883s**（0 失败）；新锚 `-count=3 -race` ok；`task` 2.981s／`compress` 3.915s；build/vet=0、gofmt 净、`--strict` valid。**本轮有生产改动**（次序修正＋诊断载荷形状）。未提交（staged 0、HEAD 仍 b0e053f）、未动索引/数据/远端/独立计划文件。**进度 31/34**：5.1 转勾；余 5.2（交叉矩阵差集）、5.3（测量边界＋本轮转入的线性化重复项）、5.4（终门）。

### 5.60 轮一百零四：5.2 差集收口（运行对象别名＋热增 owner 的记录源），本项转勾

**做法（J11 要求的形态）**：先把 5.2 的「必测」九行**逐条 grep 对到现存锚**（不采信我自己的记忆，也不采信测试文件头部的声称——§5.53 刚教过一次），再只补缺口。映射表已写进 tasks 5.2 子弹，其中七行早有入口级见证（§5.37／§5.48／§5.50／§5.52／§5.53／§5.55／§5.49），本轮**一行都没重做**。

**差集① 运行对象别名**。5.2 原文警告：「『配置别名』不得只解释为 YAML legacy 键而漏**运行对象别名**」。核 `org_config_alias_folding_test.go` 确认它钉的正是配置键别名（`compress.summary_model` → `compress.summary.model`）与指纹稳定性；而 `kind:` 省略 ≡ `kind: agent` 这一类**运行对象**别名，全靠 `AgentConfig.applyDefaults` 把它折叠成显式值，**此前无任何测**——一旦有人把 `Kind == "" || Kind == agent` 这类判定「简化」成只认显式值，同一份配置换种写法就会走出不同拓扑（§5.53 那个 remote-only 永拒缺陷，正是这条判定错一次的产物）。
- `TestD52_RuntimeObjectAliasIsNotAStructuralChange`：换拼写重发 ⇒ 代际不前进、不记 `lastFailure`、`sub1` 仍可达，且**已在服务的 owner 实例不被替换**（`require.Same`——别名重发的真正危害是悄悄重建 owner，光看指纹相等看不出来）。
- `TestD52_RemoteOnlyAliasSpellingStillPublishes`：remote-only 引用**省略 kind** 书写时冷启动合法、结构发布照样通过（§5.53 的放行对两种拼写一致）。
- 判别性 **P-P52b**：撤掉 `applyDefaults` 的 kind 归一 ⇒ **两条锚同时红**（前者红于「别名编辑被记为失败」，后者红在冷启动前置）。

**差集② 热增 owner 的记录源（＝§5.59 次序缺陷的持久守卫）**。`TestD52_HotAddedOwnerPullsTheRecordAfterNumericOnly`：**在结构发布当轮**就断言新 owner 已在回执集（`outcome=applied`、`MaxTokens=6000`）；随后用**真实租约**把它保持在途，期间做一次 numeric-only 编辑，断言它自己的消费者解析到新值（`8000×0.5=4000`、`keepRecent=9`），释放后解析稳定。判别性 **P-P52a**：把 `applyHotAll` 挪回 commit 之前 ⇒ 回执为空、精准红。

**一个自查纠出的空洞（如实记）**：这条锚的**第一版**只断言「numeric-only 之后新 owner 读到新值」——我在跑判别探针前先想了一遍“修复前它会红吗”，发现**不会**：还原次序后，那次 numeric-only 分支自己会遍历常驻表并补上记录源，于是新 owner 在这一版断言下照样通过。换言之第一版是**绿灯装饰**。加严成「结构发布当轮就要有回执」后，P-P52a 才真的红。教训与本轮主题一致：差集要按**边界**找，锚要按**反事实**验，不能按子句字面补一条能过的测。

**门禁**：`TestD52_` `-count=5 -race` ok **1.902s**（0 失败 0 竞争；含与既存三个 D52 别名测同前缀共跑）；root 全量 `-race` 连续 2 次 ok（**73.473s／75.489s**）；诊断／别名／D51／D53／委派／回滚／SD 家族 `-count=3` ok 2.797s；两枚探针（`tagent.go`、`config.go`）`diff -q` 证 **byte-identical 还原**，`grep PROBE-P` 残留 **0**；build/vet=0、gofmt 净、`--strict` valid。**本轮生产零改动**（新增仅 `org_cross_alias_test.go`）。未提交（staged 0、HEAD 仍 b0e053f）、未动索引/数据/远端/独立计划文件。**进度 32/34**：余 5.3（测量分相＋复杂度对照＋§5.58 转入的线性化重复项）与 5.4（终门）。

### 5.61 轮一百零五：5.3 分相与复杂度对照——**修掉一处把"提交"读成"构建"的边界错误，并量出热更真正的主项**

**为什么现在做它**：5.3 的正题是「修正测量边界并验证实现确实更简单」。此前 root/agent 两侧虽有基准，但两处不符合本子句：①没有**读取相位**的独立测（生产样本里 re-parse 只是顺带 `t.Logf`）；②`BenchmarkPublishExecutor` 在计时循环内构造候选并**每轮轮询** `ExecutorRefs()`——所谓"提交成本"实为构建成本。这类混计不是精度问题，而是会**把结论读反**：它会让人以为提交需要优化，而真正的主项在别处。

**拆相后的实测**（Apple M3 Pro，`-benchtime 20x`；用途是形状比较，不是门）：

| 相位 | 测 | 结果 |
|---|---|---|
| 读取（LoadConfig，生产形状 entry+4 worker，无编辑混入） | `BenchmarkConfigRead_ProductionShape`（新） | **267.6 µs**，48.5 KB，680 allocs |
| 构建（一个完整候选，不安装不退役） | root `BenchmarkCandidateConstruction_RealOrg`／agent `BenchmarkNewExecutorCandidate` | **4.71 µs** ／ 9.42 µs |
| 放弃（已构造候选的清理出口） | `BenchmarkCandidateAbandon`（新） | 579 ns |
| 提交（候选在计时区外备好，只换入） | `BenchmarkCommitPrepared`（替代原混计版） | **1.03 µs** |
| 获取（单独 acquire） | `BenchmarkLeaseAcquire`（新） | 160 ns |
| 获取＋释放配对（生产 turn 形状） | `BenchmarkBeginTurn`（保留） | 292 ns |
| 释放（单独） | `BenchmarkLeaseRelease`（新） | 2.32 µs |
| 回收（被引用保住的退役代，释放时死掉） | `BenchmarkRetirementReclaim`（新） | 1.67 µs |

**被混计掩盖的结论**：一次热更的主项是 **YAML 重解析（≈268 µs）**，比 5 个 owner 全部构造（≈24 µs）加全部提交（≈5 µs）**还大一个数量级**。本轮只把事实测出来并记录，**不顺手优化**——它不在本变更范围内，需要另立变更（已写进 tasks 的剩余项说明，避免被误当作"已处理"）。

**测自身的卫生（本子句明令）**：构造类基准原先把 b.N 个未安装 runner 留给进程结束——数字没错，但**测本身在漏它正在治的东西**；现每轮放弃上一候选、计时结束后清完最后一个，且放弃代价另立一测单列（不假装它是零）。agent 层每个基准 `b.Cleanup(func(){ _ = cm.Close() })` 关掉自己的 CM。配对释放以 `InFlightTurns==0` 收尾把关；`PendingRetirees==0` 只当**本相位边界检查**，明确不拿来推断"没有泄漏"（5.3 原文禁止那种推断）。回收观测一律放在计时区外。

**复杂度对照用身份法，不用组织级计数**（J1 禁止「全组织 TaskManager 变少」「子 agent 没有 bus」当证据——这类数字在"复用"与"每轮扔掉一份副本"两种实现下都同样平坦）。`TestD53_PerPublishObjectLifespan` 在同一生产形状上记 cold→reload→reload→rollback 每代新建了什么：

- **新 runner 恰 5 个**（每个可达 owner 一个）；
- **其余 25 项全为同一实例**（5 owner × {owner, ContextManager, TaskManager, MemStore, SessionSvc}）——即"没有第二套 agent 状态"从断言升级成了逐对象身份证据；
- 逐对象比对处附带**方向性检查**：任何非 runner 的对象被换掉都会红（"only the execution face may be reconstructed"）；
- **回滚与正向同型同价**（各 5 个新 face、0 个新 owner），这是 §2.4「回滚走同一函数」在对象层面的直接体现。

**判别性（两次探针，一强一弱，都如实记）**：**P-P53b**＝合法地少发两代（把两个 owner 从发布名单里切掉，发布仍成功）⇒ 精准红在"每 owner 恰一个新 runner"，说明该测对**少发**同样敏感、不是单向装饰；**P-P53**＝把发布名单换成不存在的 owner 名 ⇒ 红在**前置**（发布根本没发生）。后者作为判别证据是**弱的**，写在这里是为了不让它冒充前者。

**有界性子句逐条对锚**（不重复堆测）：不同名 owner＝本轮 5 个异名 owner 的跨代身份；合法依赖未调用＝`TestSD_DeferredDelegationIsProtectedByUsageRight`；共享资源＝`TestOrgClose_SharedStoreWaitsForEveryBorrower`＋`TestRetire_SharedStoreSurvivesSiblingRetirement`＋`TestRetention_ClosingOneAgentKeepsSharedStoreLease`；无新活动晚停＝`TestSD_ReleaseContinuesRetirementWithoutAnotherTurn`；六代独立回收已由 25 代的 `TestOrgGenerationsStructuresStayBounded` 覆盖；短锁屏障仍由 `org_d3_scheduling_test.go` 结构性证明（无均值门、无固定 10ms 正确性门，D3/D12 合同不变）。

**5.3 因此仍不转勾**：唯一未闭合的是 §5.58 转入的**线性化重复**——`PublishExecutor` 与 `ActivateExecutor` 各写一遍同一条提交线性化。它改动发布核心且需把三个接缝测重指，按「一项一轮」独立做；本轮不把它混进测量/对照工作里顺手改。

**门禁**：agent 层分相基准实跑通过（7 个 bench 全 PASS，数字见上表）；root 基准 3 个实跑通过；root 全量 `-race` 连续 2 次 ok（**75.227s／74.320s**，0 失败 0 竞争）；`agent -race` ok **82.454s**；有界性／对照／委派／退役／回滚家族 `-count=3` ok **100.502s**；`task` 2.995s／`compress` 3.913s；P-P53/P-P53b 探针 `diff -q` 证 **byte-identical 还原**、`grep PROBE-P` 残留 0、一次性脚本已删；build/vet=0、gofmt 净、`--strict` valid。**本轮生产零改动**（改动全在基准与一个新测）。未提交（staged 0、HEAD 仍 b0e053f）、未动索引/数据/远端/独立计划文件。**进度仍 32/34**：5.3 余线性化合并一项，之后是 5.4 终门。

### 5.62 轮一百零六：5.3 收尾——两条发布入口的线性化合并为一份（本项转勾，33/34）

**合并的动机不是美观**：§5.58 把这条列为具名项时我只写了「各写一遍同一线性化」。真去合并时才发现两条路径**已经漂移**到一处可观测行为上：同一 runner 被重发时，`PublishExecutor` 会推进**记录面**（绑定仍保它建成时的快照，所以「记录的」不等于「这代真正路由的」），而 `ActivateExecutor` 提前返回、**根本不推进**。这正是重复实现的典型后果——修一边忘一边，而且两边都有测各自的行为，谁都看不出矛盾。

**取哪一侧**：文档所载那一侧。`publishBindingLocked` 的注释明写「a re-publish that **only advances the recorded face** must not rewrite what this generation really routes」，`TestExecutorPublish_RepublishedSameFaceKeepsBehavior` 也钉这个意思；`ActivateExecutor` 的早退没有任何测支持（grep 全仓无测把同一候选再 Activate 一次并检查记录面）。故统一到「推进记录面、不产生第二代」。

**形状**：新体 `publishActiveLocked(face, r, prepared)` 承担唯一一次「记面 → 若同 runner 则不换代 → 需要时 adopt 裸 runner 为第一代 → 装代 → 换 runner → 交出待退役」；差别只以 `prepared` 表达——org 路径必须安装**纳管期已 wiring 好**的那个 binding（轮九十的前提），单 owner 路径没有这个前置，就在体内从刚记录的面快照新绑定。折叠后 `publishBindingLocked` **引用数归零**，一并删除，不留死码。

**新钉的测（同表双入口）**：`TestPublishBothEntriesShareOneLinearization` 用一张表跑两条入口，逐条断言 ①身份保持（不产生第二代，因而一个 runner 只有一次 Close）②记录面推进 ③其下不退役任何东西。判别性＝**P-P54**：把 `ActivateExecutor` 退回合并前的早退 ⇒ **只有 `ActivateExecutor` 子测红**，红在「both entries must agree that the RECORDED face advances」。这条测因此是在钉合并本身，不是顺带通过的绿。

**成本未变（合并的自检）**：提交相位仍 **1.027 µs**、`0 allocs` 增量——`isolatedCopy` 次数与合并前完全一致（stage 拷一次、激活再拷一次），没有为了通用化多拷一份面。回收／释放等其余相位亦同量级（见 §5.61 表；小样本噪声另在 tasks 申明，只作形状比较）。

**顺带量到的一个静态风险（不顺手改，单列给 5.4）**：`active == nil` 时把**冷启动那个初始 runner 原样再发布一次**，会先 adopt 出一个前身绑定、再把同一 runner 设为新代 ⇒ 仍在使用的 runner 被登记退役、可被 Close。它**生产不可达**（root 恒以新构造候选进入 Stage/Activate；`PublishExecutor` 自 §5.1 起无生产调用方），且**合并前后行为一致**，所以本轮既不当作新引入的缺陷、也不顺手加守卫；已写进 tasks 5.3，交由 5.4 终门就「`PublishExecutor` 作为公开契约面是否保留」一并裁决——若保留，该角落该由显式守卫守，而不是指望调用方避开。

**门禁**：合并触碰发布核心，故全量重跑：`agent -race` ok **82.431s**、root 全量 `-race` ok **75.207s**（均 0 失败 0 竞争；含 37 处 `PublishExecutor` 用户与整条 org 跨发布族）；表测 `-count=1` 双入口 PASS；P-P54 探针后 `grep PROBE-P` 残留 **0**，`context_manager.go` 回到合并后的干净态（探针块整段移除，非注释掉）；gofmt/vet 净、`--strict` valid。**本轮有生产改动**（发布核心合并）。未提交（staged 0、HEAD 仍 b0e053f）、未动索引/数据/远端/独立计划文件。**进度 33/34**：P3 仅剩 **5.4 终门**（三门全量含 `tests/` 与 wechat-bot、86 项未跟踪文件逐一映射、fork 版本核验、哲学共同准出，以及本轮留给它的 `PublishExecutor` 契约裁决）。

### 5.63 轮一百零七：§5.4 终门准出（34/34）——三门＋零豁免 race＋完整补丁＋哲学复核；**终门自己抓到一处此前从未进门禁的测边界错**

**终门的价值当场兑现了一次**：`tests/` 包此前从未进入逐轮门（我每轮跑的是 root／agent／task／compress／tool-action）。全仓门一跑就红：`TestResidentDurableE2E_FiveSurfaceReconciliation` 断言「ack 之后信封全部 unlink」，但它在**看到 inbox 收据事件的那一瞬**就检查，而生产路径是 `RecordReceipt → Ack(unlink) → releaseRetention` **顺序**执行（`agent/event_bus.go:835-840`）——收据可见并不蕴含 unlink 已落地。判定过程：单跑两次 `-count=2` **PASS**、全包跑 **FAIL** ⇒ 时序相关而非稳定坏；先查生产语义（确认 unlink 紧随收据、非永不调用）再动测。**修法**：把该断言挪到它自己的完成点做有界等待（20s／20ms 轮询），合同措辞一字未改，且**不更弱**——永不排空依然红。这与 §5.3 的「相位别混计」是同一类错误：把两个先后发生的观测当成同一时刻。

**三门与 race（退出码为实跑所得）**
- root：`go build ./...` **0**；`go vet ./...` **0**；`go test ./... -short -count=1` **0**（30 个含测包 ok，0 FAIL）。
- race 零豁免：`go test ./... -short -race -count=1` 跑完，**30 包 ok／0 FAIL／0 DATA RACE**；逐一确认 root／`agent`／`agent/task`／`tests`／`tool/action` 都在 ok 列表里（防「没跑到算通过」）。§6.6 撤销的两条 race 豁免文件仍处删除态，无任何豁免通道。
- wechat-bot（独立模块 `examples/wechat-bot`）：build **0**／vet **0**／`test -short` **0**。
- 空过滤防护：`-run 'TestOrgCrossPublish_'` 命中 **5** 个测、`-run 'TestReentry42_|TestWAL42_|TestMonitor33_|TestD52_|TestD53_'` 命中 **15** 个（按 `=== RUN` 计数）。
- 版本：两模块 `go list -m -json trpc.group/trpc-go/trpc-agent-go` 皆为 `v1.11.2` **Replace → `github.com/SpellingDragon/trpc-agent-go v1.11.2-tagent.1`**。未切回官方、未移动 tag、未推上游（6.3 已记：那两事需单独授权）。
- **未跑项（原样列明，不计入准出）**：非 `-short` 下 `tests/` 的 `TestPlanAgentCreateBehavior_RealPrompt`、`TestRealLLM_PlanReentry_ClarificationLoop` 会真跑并因无模型端点失败——终门口径是 `-short`，两者按设计 Skip；真实 LLM 端到端属另立授权范围。

**完整补丁清单（不以 HEAD-only 代替）**：新增生产 **8**（`agent/exec_lease.go`／`face.go`／`owner_obligation.go`／`settle_routing.go`、`event/wf_facts.go`、`org_candidate_overlay.go`、`org_candidate_txn.go`、`owner_retirement.go`）——与 5.4 点名的四项完全一致，其中 `owner_retirement.go` **在仓库根**（这也再次印证 §5.59 撤回的那条假纠正：原指针本就没错，是我读错了前缀）；修改生产 **31**（组合根 5＋`agent/` 12＋`agent/task` 2＋`agent/compress` 2＋`tool/action` 3＋`tool/task` 1＋`workspace` 1＋两模块 `go.mod/go.sum` 等）；删除 **3**（`race_disabled_test.go`／`race_enabled_test.go`＝§6.6 撤豁免；`swap_executor_test.go`＝§5.1 接缝测重指）；测文件 **109**（新 77／改 32），每个新增生产面都有入口级锚（映射逐条写入 tasks 5.4）。

**5.3 移交项在终门落地**：`PublishExecutor` 判定**保留**（37 处测用户、其文档即「ONE linearization point」、删除无收益），因此那个角落按「保留即显式守卫」处理——`publishActiveLocked` 增一条分支：首次发布构造期 runner 时**adopt 即安装**，绝不产出前身绑定。**红基线为实测**：撤守卫后**两条入口都会把仍在生效的 runner 关掉**（`TestPublishFirstGenerationOfInitialRunnerNeverClosesIt` 双入口红于「must never retire/close it」），加守卫后双入口 PASS 且 `closes==0`／`PendingRetirees==0`／active 已装。**这次也修正了我对该角落可达性的措辞**：`NewContextManager` 其实会在构造时装好一代，所以角落只在「裸装配 CM」形状下成立（正是 adopt 分支自己注释的那种）——不是生产可达，但作为保留的公开入口必须自己守住，而非指望调用方绕。

**哲学逐条复核（通过）**：真源唯一＝已提交应用记录（6.4 pull＋§5.59 次序修正后连新 owner 也接得上；§5.1 又补上「新代 monitor 仍认得活会话」的端到端）；执行权＝每 owner 一份活跃代＋声明沿代持有，无第二路由表；不重造＝旧入口（壳、`RebuildExecutor`、`cm.SwapExecutor`、`Set*TTL` push、双事务）逐一折叠而非并存；隔离未被破坏（J1）；簿记有界（J8，25 代不涨）；race 无豁免已复证。

**状态**：34/34 全部转勾，`--strict` valid。**代码未提交**（staged 0、HEAD 仍 b0e053f）、未动索引、未推远端、未动独立计划文件——提交与归档按授权另做。

### 5.64 轮一百零八：归档就绪审计 —— specs 与最终实现的两处背离（＋一处注释滞后）

**为什么终门之后还要这一轮**：`specs/` 下的 20 份增量在归档时会被**并入长期基线**。它们描述的是「实现应当是什么」，而最后十轮里我删过 API（`cm.SwapExecutor`、`TagentAgent.SwapExecutor`、`AdoptMemStoreRelease`、`RebuildExecutor`）、改过公开合同（`ToolAgentFactory`）、也重排过诊断键。所以准出前必须逐份问一句：**spec 点名的东西还在吗？** 此前各轮我只核 tasks/evidence 与代码的一致，没做这个方向（工件→实现）的核对，属于终门清单里「完整补丁」的同族遗漏。

**查到两处真背离**：
1. `specs/swappable-executor/spec.md` 的「Rollback 手动触发面」条款，主语写作 **`SwapExecutor` 的 Rollback 能力** —— 该方法已在 §5.1 删除。规范实质（手动触发可达、经唯一版本协调器、与普通热更共用构建/校验/发布、只影响后续调用、记 rollback 来源）全部成立且由 §5.48 的锚钉着，只是**挂在已亡之名下**。改写为「单 owner 走 `PublishExecutor`；组织走 `StageExecutor → ActivateExecutor`；二者共用同一条线性化；只换 runner 不换执行面的独立入口已废除」——规范内容一字未减。
2. `specs/workflow-config-compilation/spec.md` 要求工厂 SHALL 验证「**原契约**」，并把「不透明完整 agent 工厂无法安全准备」写成**未来条件句**（必须提出兼容性裁决）。但 §5.47 已完成那次裁决：`ToolAgentFactory` 的合同是返回完整配置声明、构造归唯一路径。原样并基线会把一条已定案的事写成待决，且暗示「返回完整 agent」的旧形态仍是契约。按最终形态改写，并**保留**「不能绕过工厂或暗中缩减支持」的禁令（它对未来仍有效）。

**顺带一处代码注释滞后**：`ToolAgentFactory` 自己的文档块在 §5.47 已更新，但紧邻的 `ToolAgentFactoryConfig` 仍写着 "provides everything a factory needs to **create a TagentAgent**"。改为说明它服务的是**配置声明**的产出，并显式否定「工厂构造 agent」。

**刻意不改的部分（并说明理由，避免误伤历史）**：`evidence.md` 里对 `AdoptMemStoreRelease`、`SwapExecutor` 的提及**全部保留** —— 那是逐轮过程记录（含「唯一用户消失即随删」这类后事），删改历史等于伪造轨迹；本变更的 §3 台账机制正是为「保留原声明＋标注撤回」而设。specs 中大量出现的「壳/shell」经逐条判读**均为禁令**（MUST NOT 再构造完整壳、反壳条款、已消除项列表），描述的是禁止而非现状，与 §5.46/§5.60 的方向一致，故不动。

**复核（客观核对，非自陈）**：
- specs 目录再 grep `SwapExecutor|AdoptMemStoreRelease|RebuildExecutor|LiveCMs` ⇒ **0 命中**；
- spec 点名的每个 API 都能在代码里找到且形态一致：`PublishExecutor`／`StageExecutor`／`ActivateExecutor`／`CheckOrgReload`／`Rollback`（`agent/context_manager.go`、`org_hotreload.go`），`ToolAgentFactory` 为 `func(ToolAgentFactoryConfig) (*TagentConfig, error)`（`agent/tool_agent.go:1099`）——与新写入 spec 的合同逐字对应；
- `resident-release-evidence/spec.md` 的分相条款（读取／完整候选构建／已准备候选提交／租约获取／具体回收，计时不混编辑探针断言日志轮询）与 §5.61 的七相实现一致；其「不得把 getter 测与 shell 单测拼成整链」由 §5.54–§5.56 的入口级锚满足；
- `--strict` 仍 valid；本轮改动为两份 spec 文本＋一条注释，`go build`／`go vet`／受影响包复跑通过（见下）。

**门禁**：`go build ./...` = **0**、`go vet ./...` = **0**、gofmt 净；`go test ./agent -count=1`（注释所在包）复跑 ok；`openspec validate --type change --strict` valid；`specs/` 陈旧引用复扫 0 命中。**进度 34/34 保持**；代码仍未提交（staged 0、HEAD b0e053f），提交／归档／上游仍待授权。

### 5.65 轮一百零九：整体复盘请求——死代码审计清理＋又一处负载下观测边界（§6.6 之后追加）

用户令：澄清问题与方案、梳理歧路、review 整体实现、**清理死代码**。本节记代码面两件事；复盘正文在对话报告。

**死代码审计方法**：`deadcode` 工具因库仓无 main 不可用，改为对 8 个新增生产文件＋关键修改文件逐函数做全仓引用计数（生产/测试分列），扫出 **15 个零生产调用候选**，再逐个甄别性质——因为「零生产调用」有三种完全不同的含义：真死、测专 oracle、公开观测面。

**清理结果（三类处置）**：
1. **删除（真死，0 生产 0 测试）**：`ExecLease.Released`、`RunOrgReloader`、`StagedGeneration.BindingFace`、`LastBatchOutcome`（后者注释自证是 §5.2/5.3 把 outcome 冻进 completion 后的遗留读取面——迁移完成了，读取面忘了删）。
2. **删除（被取代的第一版机制，靠「测死函数本身」存活）**：`computeMemoryFingerprint`——R4 第一版的全局 memory 指纹，生产从未接线；现行机制是 `changedMemoryAgents(fresh, rc.residentMemFP, freshReach)`（按 owner、可达域、粘性基准的 diff，`tagent.go:787`）。**其注释还在描述已被替代的机制**（自称是懒检查先序），两个测文件测着这个死函数——正是本变更要消灭的「第二套机制并存」，只是这次并存的是**死的那套**。处置：删函数；`TestMemoryFingerprint_DetectsMemoryOnlyChanges` 只删死断言半（memory 指纹必须变），保留活断言半（org 指纹白名单，`computeOrgFingerprint` 的 D3 合同）；`org_candidate_test.go` 的 Clone 保真迁移到活的 `agentMemoryFingerprint`。**memory 拒绝门的生产路径锚不丢**：`org_hotadd_test.go`／`hotreload_multiagent_test.go:137` 锚着真实 reload 形态。另删 `currentFingerprint`（3 处测引用迁 `c.current.fingerprint`）、`ensureUserPrompt`＋其专属测文件（Task 6.8 的调用点早已不存在）。
3. **移到测试文件（测专 oracle/内省）**：`quiescent`（c.1 被否的第一版判据的补集，生产只用 `awaiting`；d13/d7/d10 用它当终止 oracle——保留为测试方法，`awaiting` 文档改为指明 oracle 位置）；`orgLastDiscardOrder`（注释自标 TEST helper）。

**刻意保留（并记录理由）**：`BeginTurn`——`BeginTurnLease` 的便利形态（3 行委托），14 处测试与库用户消费者；同一入口的两个签名不构成第二机制（对比：`cm.SwapExecutor` 被删是因为它**换 runner 不换 face**，语义残缺）。`OrgDiagnostics`/`OrgBudgetLine`/`OrgKeepRecent`——跨包测试与库用户的公开观测面。`snapshotLiveCMs`——§5.1 收缩可见性的既定产物。`WithMCPToolSets`——公开注入选项。

**审计顺带发现并修复的观测边界（与 §5.63 同族，这次是负载维度）**：全仓 `go test ./... -short` 两次稳定红在 `TestMonitor33`（suspect≠running），而单跑／整包／-race 全绿。判别：跨包并行时整机承载全部包二进制，boot 的首次 tmux 校验可错过窗口；`List()` 会重跑 `reconcileDetached()`（设计的再裁决路径），故把断言改为有界重取（30s／100ms）。**判别性完整**：修前全仓 2/2 红、修后 2/2 绿、单跑仍即时提升（12.688s，无等待）、合同措辞一字未改、永不提升仍同消息红。工具事故两则入账：①zsh 嵌套引号致整条命令未执行（含未落盘的补丁）——改分步执行；②脚本删函数时按「列 0 收尾 `}`」匹配漏了单行体，误吞了生产在用的 `LastTurnDegenerate`——`go vet` 立即抓获，改用单行体感知的切割后重做（**这类批量编辑后必须 build+vet 先于任何测试**，本例即靠它止损）。

**门禁**：`go build ./...`／`go vet ./...` = 0/0、gofmt 净；`go test ./agent -count=1 -race` ok **82.318s**；`go test . -count=1 -race -short` ok **74.207s**；全仓 `-short` 修后 **2/2 绿（30 包 ok，FULL_RC=0）**；`TestMonitor33` 单跑 ok 12.688s；tmux 无残留（`no server running`）。清理后复扫：零生产调用项只剩上列**刻意保留**者。删除量：生产 7 函数＋1 测文件，约 90 行死码；迁移 4 处测引用＋2 个测专 helper 移位。未提交（staged 0、HEAD b0e053f）。

## 6. B/C 阶段证据（31 项计划，2026-09-23~24，轮十四～三十九）

> **读前必看**：本阶段轮三十九曾声明「全 31 项收口」，已被 2026-09-25 修订**整体撤回**（14 项重开，见 §3）；下列各轮证据为过程事实，其「完成」效力以 tasks.md 现行状态为准。门禁同 §5 总则。

### 6.0 整体 review 与计划修订（2026-09-23，非轮次）
整体 review 后修订：R01–R06/W-1/M-3/L-3 等发现映射为任务；重开 2.1/2.3/2.4/3.2/4.2/4.3/5.1/5.2/5.3 九项＋新增 6.1–6.7（total=31/complete=11）；撤销十条历史推断（不再把助手延期说明当禁令／不以均值证短锁／不以有限 race 通过当实际停止／不再声称零容忍已达成／不采所有数值入指纹等）。实跑记录（review 时）：root 定向 `-race` 0（1.017s）、agent 定向 `-race` 0（1.596s）、`go list -m` 官方 v1.11.2 无 Replace。

### 6.1 轮十四：6.1 入口身份拒绝＋6.2 被动排除撤 TTL（红→绿）
6.2：`wf_facts.go` init 为 wf.* 注册 `TTLDays:30` 覆盖全局 90 天→改 0（继承全局）；红＝`TestWFFacts_PassiveExclusionIntroducesNoTTL`（7 类型 30→want 0）＋`memory` 对照测（同龄 external_input 被淘汰证扫描真实运行）；既有守卫测订正（数量门 19→12）。6.1：entry 改名在资源构建前拒绝（红＝删旧定义后零配置 main 被发布、序号被推进）；`org_entry_identity_test` 两例。门：event/memory 全量 0、定向 `-race` 0、root 定向 0。

### 6.2 轮十五：2.1 R06 隔离＋2.3 核定
2.1（红→绿）：`ExecutorConfig()` 浅拷贝共享 Tools 容器/值指针、`PublishExecutor` 别名输入→`isolatedCopy()` 双向隔离；红＝`executor_isolation_test` 三测（A/B/C 分别钉 Tools 写穿/指针泄漏/发布别名）。2.3 核定为三块可分离重构（递归 owner 泄漏/半提交暴露/获取逆序），需独立轮并与 3.2/4.1 交叉，不仓促落地。

### 6.3 轮十六：2.3 R01 事务半
候选私有 overlay（共享 candCache）；失败父 owner 经 `ownedAgentNames()` 差集撤销（`rollbackAdds`）；唯一 `Add` 移入与 PublishExecutor/coord.swap 同一 mu 临界区；第二 writer 由 memoize 杜绝。红＝`TestOrgHotAdd_RefusedCandidateLeaksNoOwner`（递归依赖 zzz_dep＋失败父 aaa_parent 双泄漏）；正向＝共享依赖单次构造。2.3 仍不勾（D3 半未做）。

### 6.4 轮十七：2.3 D3 调度半
`org_d3_scheduling_test` 四测以阻塞屏障结构证明「懒检测不等待候选构建/手动检查不封业务获取/Close 排空不封获取」（对照测证屏障确持 mu）；撤回 `org_hotreload_bench_test` 均值断言（改 Sample 观测）。实现半先前已在位，以正反对照排除空测（诚实形态）。样本 6.1ms/轮（解析 3.1ms）。2.3 勾 [x]（旧口径）。

### 6.5 轮十八：6.3 L0 能力门判定
`stopCapProducer`（取消后停驻、忽略 ctx）实证官方 v1.11.2：`processedEventCh` 关闭时刻 `producerDone==false`——处理流关闭≠生产者停止，**能力门不通过**；资源释放门须自身逐代记账，覆盖框架内部生产者须走最小 fork。`-count=20 -race` 稳定（结构断言非统计）。

### 6.6 轮十九：6.6 关闭全部 race 豁免
前置实证 v1.11.2 上旧豁免对象已修（0 触发）→ boot-child 判负函数化（含纯上游 family 一律 fail，分类器仅诊断）；删 agent 包 8 处 `raceEnabled Skip`＋scaffolding 两文件；全局 stub 改 `t.Cleanup`。门：全 `./agent -race` ok 80.2s（无跳过）。

### 6.7 轮二十：6.7 测试落盘卫生（PID 根，后被轮41取代）
消除最后工作树相对写盘点（dropAgentYAML main 段）；`testStore` 去相对路径回退改 panic；行为证据＝受影响测前后 `hottest-*/own-*` 目录集合 diff 相同；完整补丁 vs HEAD 断链清单（19 个未跟踪必要 .go）；已跟踪 lock/journal 4 件不取消跟踪（需索引授权）。

### 6.8 轮二十一：6.5 W-1 穿透接线（红→绿）
根因：`agent.New` 对每工具包 `OutputLimitTool`→裸 `.(*AgentToolWrapper)` 断言在两处生产调用点恒落空→`parentProjection` 恒 nil→auto-inject 静默失效。修：`collectAgentToolWrappers` 经 `Unwrap()/Inner()` 匿名接口递归穿透。红＝变异体禁 peel 三测 FAIL（MUTATED_TEST_EXIT=1）。`w1_projection_wiring_test` 六测。

### 6.9 轮二十二：2.4 L-3 完整有效配置与回滚
三处旧违：numeric-only 不动协调器/回滚不下发热参/回滚守卫用结构指纹（numeric-only 误判无需回滚）。修：`revision`（完整应用计数含 numeric-only）/`lastPubAt`/`applySig`/`recordHotApply`/`sameFullAsCurrent`；回滚闭包加 `applyHotAll`＋`recordRollback`。契约测七场景（真消费者 OrgKeepRecent/OrgBudgetLine/TerminalTTL 断言）；变异红＝去 recordHotApply → step2 revision FAIL。

### 6.10 轮二十三：6.4 M-3 消费面核验＋死源清除
端到端追溯五热参（提交→push→播种→边界→真消费）结论：2.3/2.4/6.5/§4.3 已闭合运行时路径；交付＝删 `ContextManager.maxTokens` 死字段、`outputCapForMaxTokens` 单点化（OutputLimitTool 封顶＝构造期派生非热轴）、真消费者测（`m3_ttl_consumer`/`m3_hot_consumption`，变异红＝忽略 Spec.TTL FAIL）。（注：6.4 后于 09-25 修订再次重开，见 §6.15 与轮六十七。）

### 6.11 轮二十四：CodeReview 整改
**H-1 真数据竞争**：`UpdateKeepRecent` 裸写 vs `compressSkeleton` 持锁读（后台热应用移出后成真 race）——修走 `ApplyParams(0,0,n)`；回归测 `-race` 0，变异体 exit 1 命中竞争（§6.6「零 race」据此由疏漏性成立改真成立）。M-1：回滚闭包补 `stopped` 闸门。L-1：删 `cm.thresholdPct` 镜像（CM 不存热参镜像定则）。

### 6.12 轮二十五：计划修订轮（只读核验）
核验重开三缺口：2.4 回滚钩子仅装于结构发布分支（numeric-only 后 Rollback 静默 no-op）；6.4 子调用构造期播种缺失；6.5 共享 wrapper 并发重绑。design D6 增「最小 fork 分级流水线 L0/L1/L2/R1/R2」（本地段与发布段授权分离）。

### 6.13 轮二十六：6.3 L1+L2（用户放行「开始，基于 v1.11.2」）
L1：上游最小补丁（`runner.go` defer：cancel 前移＋close 前 join 排空 agentEventCh，+17/−3）红→绿（`TestRun_ProcessedCloseImpliesProducerDone`）；上游回归：唯一失败 duckduckgo＝macOS socket 环境问题（stash 对照同败）；既有上游 race（chainagent/session 等）stash 对照签名比对＝非本修复引入。L2：两模块临时本地 replace；行为反转证据＝旧探针 FAIL（fork 下 producer 未退出流不关）＋改写为正向能力门；全 `agent -race` 0。R1/R2 待逐项授权。

### 6.14 轮二十七：2.4 缺口闭合（红→绿）
红＝「启动→仅 numeric-only→Rollback」钩子从未安装静默 no-op（RED_EXIT=1）；修＝回滚闭包移至 reloader 装配期一次性安装（逐字迁移）；新验收＝numeric-only 后 Rollback→gen1/rev2、三真消费者回改前值；既有 L3 四测不改一字全绿。

### 6.15 轮二十八：6.4 缺口闭合（hotSnapshot 时代，后被 c 计划再重开）
owner `hotSnapshot atomic.Pointer` 构造播种；`ApplyOrgHotParams` 升级单一提交点（常驻 cm→taskManager TTL→快照轮转→liveCMs 扇出）；私有 CM 经 `hotOverlayConfig` 播种（派生 triggerBudget 同代重算）。`m34_subcall_hotthread` 四测（新调用构造期即有效/在途边界应用/LiveCMCount 有界/reloader 同点轮转）；变异红＝还原 `eff := cfg` 或删扇出循环分别 FAIL。（**现行注记**：此 push 模型即 6.4 再重开要反转为消费边界拉取的对象，见 §5.24。）

### 6.16 轮二十九：6.5 缺口闭合（投影经调用上下文）
选 ctx 携带而非克隆 wrapper（克隆＝第二可执行实例/第二路由真源）。红＝`TestW1_ConcurrentCallsIsolateTheirProjections` 改前 A 读到 B 投影（RED_EXIT=1）；修＝`withCallProjection` ctx 载体＋RunFlow 同站点绑定＋删 buildExecutor 原地重绑；发布点唯一（构造期 SetToolParentProjection）。

### 6.17 轮三十：6.3 R1+R2（用户授权「打 tag 并切 replace」）
基线裁决：PR 分支已 rebase main（103 无关提交）→ 新建 `release/v1.11.2-tagent`＝官方 v1.11.2（`5a0030b62`）＋4 修复提交（零冲突）；fork 侧发布前门禁：修复分支 vs 纯基线对照——失败 6 vs 7（基线多 1）、race 栈签名集 diff 完全相同（13 条）＝零新增失败/零新增竞争形状；tag `v1.11.2-tagent.1` push fork（exit 0，ls-remote 实证）；两模块 replace 切 tag＋tidy＋`go list -m` 实证；tagent 侧对 tag 重验全绿。上游 PR 合并/type 标签待维护者。

### 6.18 轮三十一：3.2+4.1 联合（execBinding/ExecLease）
红＝`TestLease_UnrelatedGenerationReclaimedIndependently`（每 cm 全局聚合计数扣住无关代）＋BeginTurn/RunFlow 双重登记。交付：`execBinding`（retired∧refs==0 由最后 release 恰一次自关）、逐代注册表（退役＝发布内在步骤、同对象重复发布不新一代）、停止凭证 `SettleDetector.Stopped()`、有界 Close（未收敛持有＋`ErrExecUnconverged`）。自纠三缺陷（裸构造 cm 终态漏关/forceClose 违 spec/errors.Join）。压力测曾现同 runner 多次 Close→「删独立 retire 入口＋同对象不新一代＋release 单点」根治。

### 6.19 轮三十二：3.3 全路径矩阵（红→绿）
红①：`Config.Validate` 对所有 agent 引用要求本地定义（remote 引用被误拒）——摘 `!isRemoteRef()` FAIL；红②：A2A 传输重试对 `Response.Error` 形态是死码（一次 503 直落父调用）。修：单一谓词 `isRemoteRef()` 两域共用＋重试上移 `runAndCollect` 覆盖整次尝试。`a2a_delegation_test` 新建（httptest 真 A2A 端点）；七路径证点矩阵。边界：remote 跨发布在途归 3.4；工厂未注册 id 提前校验越界留记。

### 6.20 轮三十三：4.2 重入统一版本源＋组织 Close；退役半回退
R03 根因＝第二路由真源（relaunch/resume 闭包捕获 spawn 时 wrapper）。修：`ResolveReentryDelegation`（ctx 租约→该代面；无→effective；所选代不含目标即拒绝）；`execBinding.face` 入代；闭包改自由函数只捕常驻 owner cm＋纯数据；ctx 贯通 TaskSpec/TaskManager/TaskController。红＝三契约测改前 FAIL（`SERVED-BY-G1` witness）。自纠两回归（ctx.Done 判发起者错/裸 wrapper 测适配）；测抖如实归因（mock 取 tools[0] 假设错→namedDelegModel）。同轮：组织 Close 覆盖全部 owner（红→绿三测）；**退役半实现→7/7 绿→因六项既存测需逐条判读而完整回退**（三项实证发现留轮三十四）。CodeReview 三修复：TOCTOU（tryAcquireActive＋重读重钉）、relaunch 结算窗现读 dense、后台 producer `bgLease.WithContext`。

### 6.21 轮三十四：4.3 owner 排空/退役
三轴义务判据（代执行引用/LiveCMCount/看板活任务；终态任务不算）。`retirementLedger`：retireUnrouted 挂账/sweep 只对 Idle 调自身 Close/建前拒绝 closingIn 早于清扫且用当代 freshReach（次序缺陷修正）。顺带修真缺口：doRollback 借用不建→改名退役后回滚静默建到 entry store（径向重取缺失 owner，半成功全撤）。八测（含提交点停点 orgCommitBarrier）；红证＝摘 sweep 体 5 测 FAIL/摘 closingIn 拒绝测 FAIL。六项既存契约测显式迁移（逐条判读表）。

### 6.22 轮三十八：5.3 性能有界重验
发现 `executor_perf_boundedness_test.go` 不可编译（`publishFresh` 误用 PublishExecutor 返回值）→agent 测包从未过门，净零生产改动修复（探针租约读回代际号）。交付：`TestD53_CommitSeparateFromReclaim`（提交不携带回收）/`_HeldResourcesCountedNotForceClosedUnderChurn`（k 代有界＝活引用数）；红证＝变异回收门忽略引用两测 FAIL。既有五相位基准/屏障/有界复跑。

### 6.23 轮三十九：5.4「最终回归」（**已撤回**）
当时记录：23 跟踪生产文件＋4 未跟踪必要新文件（exec_lease/owner_obligation/owner_retirement/wf_facts）＋37 新测＋27 测改；全部受影响包零豁免 `-race` FINAL_RACE_EXIT=0（23 包）；跨发布矩阵 `-race -count=3` 0；wechat 三门 0。**未跑边界（仍有效）**：生产存储／掉电重启／72h 长稳／真实渠道——四项单列授权，本地 mock/临时存储/httptest 面不外推。**该轮「31/31 收口」声明已被 2026-09-25 修订撤回**；其门禁事实（当日工作树）仍为过程证据。

## 7. A 阶段证据（24 项计划，2026-09-22~23，轮一～十三）

### 7.1 轮一：0.3 基线＋§1 撤回＋版本收敛
基线：`dev@9c785ba`、v1.10.0、build 0；脏树含两外变更交付（不回滚）。§1 删 15 原型文件（org_exec/org_definition/org_generation/org_version/workflow DSL 等，删除前全仓引用扫描零生产调用方；4 组不可恢复已如实标注）；`orgCoordinator` 收敛双簿记（prevKeep+prevSnapshot→单一 prev；两处有意差异更优：回滚比较改实时值、序号仍前进）；arch 守卫重写＋`TestArch_NoSecondOrchestrationRepresentation`。

### 7.2 轮二：候选构造与发布分离（HEAD worktree 探针红）
`ExecutorConfig/buildExecutor/NewExecutorCandidate/PublishExecutor` 唯一装配与线性化；删 `RebuildExecutor` 十段零值合并；`Config.Clone` JSON 往返。红＝HEAD worktree `TestRED_StaleToolBindingSurvivesAtHead`（删工具后模型仍见旧声明）。审计测非空转实证（首跑即红于排除表字段名笔误）。

### 7.3 轮三：turn 边界取版＋发现 U-1
`BeginTurn/RunFlowWithExecutor`；删 BeforeModel 内 reloader 调用。红＝HEAD 探针「turn 内检查次数 0」不成立。**发现上游竞态 U-1**（Session.Clone 读 vs UpdateUserSession 写，-count=2 一次红一次绿），记台账不引入伪机制；委派 race 门自此受阻（至轮十三）。

### 7.4 轮四：§4.4 回收轮次选代
`TestReclaimTurnTakesCurrentGeneration`（task_settled 回流落新代）；absence 证明＝grep 无 generation 标记写入持久形状。U-1 定位细化（本仓 AppendEventHook 改写还原共享字段为重入写）。

### 7.5 轮五：§5.1 诊断面（部分）
`OrgFailure/OrgStatus/orgCoordinator.status()`＋`SetOrgDiagnostics`（启动写一次运行只读）；两处自纠（D4→D9 误引；desired≠effective 语义补齐）；红＝注掉 `c.lastFail=nil` 端到端测红。**门禁新事实**：tests 模块非 `-short` 在 HEAD 即红（真实模型非确定），worktree 对照证实——`-short` 口径定形。

### 7.6 轮六：§4.3 子树热增删
`ResidentTopology`（atomic.Pointer[map] COW，消两份冻结集）；memory 先检精化逐 owner（`changedMemoryAgents`，兼顾第二 writer 拒绝）；移除侧免新机制（Add 不覆盖）。迁移发现：旧拓扑拒绝测是假通过（拒绝实来自 memory 规则）→重写真红。§3.4 随之改写真实发布形态。

### 7.7 轮七：§5.1 闭合＋静默重参数化修复
数据流审查发现 §4.3 引入回归：applyHotAll 遍历常驻表对定义已删者取零值回落默认→只对本代 routable 下发、其余 draining 不碰（红＝注守卫 7→2）。诊断六点全落（desired/effective/lastFailure/逐 agent 回执/ExecutorRefs/失败不称生效）。门禁新事实：全包 `-count>1` panic（RegisterPlainTool）。

### 7.8 轮八：§5.3 基准＋§5.4 文档
BeginTurn 128ns/CheckOrgReload 9.2µs/候选 6.7–10.3µs/发布 5.4µs/整 turn 426µs；`TestOrgGenerationsStructuresStayBounded`（25 代容器不涨）。wiki §六·A 整节重写（原文档已错）；wechat 首次跑测 ok。

### 7.9 轮九：§5.2 六场景＋自纠＋发现 U-2
执行器层＋配置形状层六场景（三处 fail-before 真红）。**自纠**：2.3「acquire 后立即登记」从未实现→`BeginTurn` 双返回值（先登记后取 runner，release 折进 endTurn）；BeginTurn 128→183ns/2allocs。**发现 U-2**（steer.Close vs Invocation.View，4/20 命中）→设计事实：事件通道关闭≠goroutine 全退，`runnerInFlight==0` 非安全关闭充分条件；轮七 race 门判为运气。

### 7.10 轮十：CodeReview 整改（推翻两条自报）
**B-1：干净 HEAD 不可编译**（未跟踪 wf_facts.go 断链，九轮门禁掩盖——方法失误，新增干净 HEAD worktree vet 门）。M-1：「type:memory 不落盘」失实＋hottest-* lock/journal 已被跟踪。M-3 两点确认（壳实例/默认分叉）。已修：H-1（判定域求交 `ownerFP∩routable`，故障注入真红）、L-1（unRegisterStoreOwner）、L-2（orgRollback 改 atomic.Pointer）、M-1（testStore pid 隔离＋10 处改道）、M-4/L-6/D7 回写。自我修正：「必现」措辞过重。

### 7.11 轮十一：§2.3 测量结案
生产形状 20 代真发布：整次热更 1.93ms（-race 4.6–5.6ms），其中重解析 51%——**成本中心是解析不是构建，不为它引入异步**（语义退步换测不到的问题）；守卫断言 <10ms＋触发条件留档。过程教训：benchmark N=1 预热使守卫误红，改测试形态。

### 7.12 轮十二：§4.2 悬红收尾＋发现 W-1
发现被中断的悬红轮次（码/测已写从未跑门）→收尾记账。根因：生产把 wrapper 包进 OutputLimitTool，裸断言永不命中→`Unwrap()` 剥壳按 DeclaredAgentName 匹配；死断言（`*0`）修为本文案。**新台账 W-1**：两处 SetParentProjection 生产调用点同样看不见装饰层→parentProjection 恒 nil→auto-inject 从未触发（探针实证；修法极小但激活休眠路径属语义决策，只报不修→轮二十一闭合）。

### 7.13 轮十三：上游 v1.11.2 升级（U-1/U-2 结案）
用户裁决「立案修复；先看远端；完成后打 tag」。远端 v1.11.2 已修（U-1：EventMu 范围；U-2：#1926/#2165/#2462）→fork 路线短路，两模块 require 升 v1.11.2、撤实验 replace。行为判据累计 50/60 迭代全绿（未修概率 ~0.5%/0.001%）。连带：race 豁免版本守卫按设计触发并**反转**（已修复竞态零容忍，不续豁免）。委派族 race 门首次可跑。

## 8. 范围重定向与实施基线（2026-09-22）

### 8.1 两次重定向
- **现行范围修订**：编排与修复两线分离；Graph 目的收缩为组织配置与请求级版本发布；撤销内部 durable engine/facts/灰度；关键校正六条（开始执行绑整份快照／指纹含组织参数／送达不确定不自动重发等）。原 24/46 口径停用。
- **第二轮重定向**：核心为编排热更新，沿用 YAML 与动态委派；版本须约束真实执行；撤销旧 1.1–1.4/2.1/3.1-core 勾选（原型零生产调用方）；F1–F10 转后续台账（回链表保留于原文档历史，准出要点已并入各任务）。

### 8.2 实施基线（tasks.md 0.3 引用的「历史基线命令与 resident 契约清单」）
- 基线：`dev@9c785ba`（ahead 2，两笔基线隔离提交 b281149/b0e053f 系经授权按 change 提交）、go1.24.1 darwin/arm64、v1.10.0、`go build ./...` 0；脏树两外变更交付（resident-review-fixes 17/17、complete-resident-reliability §9）不属本变更不回滚；`.pre-isolation-backup/` 可逆快照。
- **resident 契约测清单（§1 保持或显式迁移对象）**：org_hotreload_test（指纹 5 测）、org_hotreload_e2e（2）、hotreload_multiagent（3）、agent/swap_executor＋context_manager_recycle、tests/upgrade_rollback_drill、arch_layers_test（重写）。
- **0.3 生产入口基线（task 7 清理后仍须独立可运行）**：编排热更 `computeOrgFingerprint/CheckOrgReload/ApplyOrgHotParams/SwapExecutor/RetireRunner`；输入接收 `InjectMessageContext/runEventLoop/submitDurableBatch/finishDurableBatch/ReconcileOutstanding`；任务 `TaskManager Spawn/Resume/Cancel + RebuildTaskRegistry[FromWAL]`；投影 `RebuildProjectionFromWAL/recomputePartition`；资源 `NewRuntimeResources/acquire/acquireDirLock/closeResource/poison`。不变量：`WorkflowInputs` 分派删后 runEventLoop 唯一内联、ReconcileOutstanding 唯一恢复入口。

### 8.3 阶段1（A：组织配置与编译，1.1–1.4）——**已随 §1 撤回**
历史交付（原型层）：DSL 去耐久化＋`definitionFromConfig`；`validateGraphShape`（拒环/孤儿）；`org_exec.go` 受信节点＋runOrgGraph（6 真实 Graph 测）；`ReleaseHash`（结构/参数两指纹分工，参数变更开新代）；两轮 CodeReview 整改（F-1~F-12：SetFinishPoint/Params YAML/UseNumber 防大数折叠/AgentGates 跨 Registry 等）。**全部随第二轮重定向撤回删除**（org_definition/org_exec/org_generation/org_version 于轮一删除，workflow/definition 部分保留至阶段7清理）；记录仅作历史证据。

## 9. 原耐久方案历史台账（非现行准出，索引）

阶段0（上游语义钉测 P1–P3）/阶段1（wf.* 事实模型＋facts store/saver）/阶段2（DurableEngine 三段式门/Claim/Signal）/阶段3（workflow/v1 严格 schema＋Registry）/阶段4（输入工作流灰度装配；两真缺陷修复：mint 键走 SnowflakeEventKey、claim 身份含 writer）/阶段5上（InboxV2 导入＋事实驱动恢复＋子进程崩溃四窗矩阵）——**均为被撤销的偏离范围试验**，其「全绿」描述未接入生产（`ta.inputEngine` 字段从未声明，清理前 agent 包即不可编译）。**阶段7 清理（2026-09-22，D14/E1）**：整删试验文件、精确回退灰度分派与反向边、保留 `event/wf_facts.go` 被动排除与 workflow/definition 中性编译层；引用核验（试验自符号 grep 空）；分层守卫转绿。上游调研结论存 `upstream-research.md`（v1.10.0 graph 三弱语义钉测、无 YAML 装载器）。| 剩项（6） | 3.3(重开)、4.2(重开)、5.1(重开)、5.2、5.3、5.4 —— **3.4 已于轮九十八转勾（§5.52/§5.53/§5.54）**；3.3 仅余「交代失监视」的 org 级端到端锚（需真 tmux，§5.51）；4.2 已完成重估（§5.50），仅余 WAL 重建入口一腿；P3 矩阵 5.1→5.2→5.3→5.4 |
