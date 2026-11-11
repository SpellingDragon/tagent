# orchestration Specification

## Purpose
编排契约：防护启动门、波次解锁、契约守护与三级验收的可验收条款。

归档溯源：`send-cross-task-session`、`agent-instances-and-delivery` 与 `attention-lifecycle` 的一级编排契约手工并入本条。
## Requirements
### Requirement: 隔离并发与新语义优先
编排 SHALL 使用 runtime-rebuild 的新任务及依赖，不继承旧勾选或恢复已删除源码。派发原子 MUST 是就绪叶任务集合；同树并发须满足 W_A∩W_B、W_A∩R_B、W_B∩R_A 均为空，测试包全部源文件纳入读取闭包。波次只用于汇报，解锁以实际前置集成和设计闸成立为准，不等待整域。

#### Scenario: 文件不同但验证闭包冲突
- **WHEN** 两单写不同文件但一单写入另一单测试所编译的包
- **THEN** 编排者合单、串行或隔离工作树，合入后重新集成验证；不得以追加兼容放行

#### Scenario: 释放资源后存在就绪叶任务
- **WHEN** 一单交回资源且等待表中的叶任务前置和授权齐备
- **THEN** 编排者重新计算触碰集并补派该叶任务，不等待其域内其它阻塞项

### Requirement: 只有集成路径作为行为证据
产品行为验收 SHALL 经正式根包入口、state、原生模型和工具、任务提交及实际消费者产生观测。确定性点名用例 MUST 有真实 run/pass；零匹配、子测试 Skip、包级失败及缺终局不得被退出码零或人工 PASS 覆盖。检查器合成 fixture 仅证明检查器，不是产品证据。MUST NOT 为新规划删除旧测试；适用测试迁移须先取得授权。

#### Scenario: 检查器自测通过但没有产品运行
- **WHEN** 规划、政策和金样合同及负控检查通过
- **THEN** 只报告规划与检查资产有效，产品任务保持未完成，G1 签名探针及 G2 设计推演/设计验收不冒称通过（产品观测属 09 证据，**不属 G2**）

#### Scenario: 原生工具往返
- **WHEN** 正式入口完成受理及后台结果回流
- **THEN** 严格协议服务核对第二轮实际 Request 中的消息配对、一次 Receipt 和真实正文，不以忽略输入的常量 final 作为闭环

### Requirement: 设计闸与授权分离
实施 MUST 先通过 G0 现场/资源盘点、G1 共享责任和原生接口探针、G2 正常/失败/并发/重启推演、G3 单级精确文件授权。设计合同先于消费者实现；无真实消费者的签名不得预建。重大方向变更 MUST 阻塞受影响单并交用户裁定。

#### Scenario: 规划已齐但签名未验证
- **WHEN** OpenSpec 报告四件套齐全而共享提交或绑定接口仍未编译取证
- **THEN** 设计待核与实施就绪分别登记，不自动执行 apply；本轮只允许规划和检查资产

### Requirement: 关键契约三层守护
golden/runtime 下的架构与场景合同 MUST 由集成者单写。修改 SHALL 同步能力 delta、消费者任务和负控，合并时核对合同差异，收尾再次按基线核查；不得复制期望充当运行观测或提高 baseline 掩盖失败。

#### Scenario: 新增未登记子包或金样错值
- **WHEN** 出现未登记第一方子包、越界依赖或运行观测值不同
- **THEN** 对应检查器返回非零且具名定位，格式检查不得替代此结果

### Requirement: 编排凭据与资源台账
ORCHESTRATION.md SHALL 按结论、编排四问、明细、台账四层组织；每单保存前置、精确写集、读闭包、资源锁和等待者、命令原始退出码及产物。完成前 MUST 核查勾选真实性、产物、数字来源和遗漏。真实资源 SHALL 逐项核对启用门、授权、预算和实际非空调用；未启用可 Skip，必验项不得以 Skip 结项。

#### Scenario: SFT 或微信未授权
- **WHEN** tokenizer/制品或真实测试接收者尚未获得授权
- **THEN** 对应实际消费或发送任务保持阻塞，独立确定性任务可继续，不打印凭据或自动安装依赖

#### Scenario: 收尾仍有真实路径欠账
- **WHEN** 09.1 确定性场景已通过但 09.5 的必验资源项未完成
- **THEN** 顶层 DoD 保持未完成，不归档，不称正式版交付；归档须另行授权并标准同步嵌套 delta

### Requirement: 两阶段交付与骨架评审门
交付 SHALL 分两阶段。阶段一 = 24 个骨架叶任务（编号 `NN.P0.M`）**仅含**数据/接口定义与 CI 合规注释文档，stub 方法体**仅**返回哨兵错误 `errors.New("phase 2: not implemented")`（无返回值方法仅 `// TODO(phase2)` 空体）、**禁止 panic**，**零业务逻辑**；阶段二 = 既有 65 个实施叶任务（`NN.M`）。阶段二 MUST 以骨架评审门 `00.P0.3`（全部域 P0 done 且评审通过）为前置；该前置 MUST 落为 `task-graph.json` 硬边并由规划检查器拒收缺边。

#### Scenario: 骨架含业务逻辑或 panic
- **WHEN** 某 `NN.P0.M` 任务含业务逻辑、`panic` 占位或非哨兵错误
- **THEN** 骨架评审门 `00.P0.3` 拒收，阶段二不解锁，该任务退回重写

#### Scenario: 00.P0.3 未 done 即派发 NN.M
- **WHEN** 骨架评审门 `00.P0.3` 尚未 done 而编排尝试派发任一 `NN.M`
- **THEN** 判定为编排违规（`task-graph.json` 硬边要求 `NN.M` 含前置 `00.P0.3`，`check_runtime_plan.py` 对缺边拒收并返回非零）

#### Scenario: 域 P0 申报 done
- **WHEN** 某域 P0 任务申报 `done`
- **THEN** MUST 附 `go build ./...`、`go vet ./...`、`gofmt -l <包>`（空输出）的**原始退出码**与每包 `skeleton_assert.go` 编译断言证据；无证据不得勾选

### Requirement: 全事件完整性规划与实施分门

`event-consumption-integrity` 的编排 SHALL 区分规划物化、域产物就绪、正式消费者验收与外部发布授权。一级四件套完整不表示已取得业务实施授权；各叶 SHALL 保留原始正锚、负锚及消费者锚，未验证者 MUST NOT 勾选。嵌套域的 delta SHALL 分别校验，归档前合并到标准 capability 路径再同步主规格。

#### Scenario: 只有规划物化授权

- **WHEN** 用户只要求写入 OpenSpec 规划
- **THEN** 写入范围限当前 change 的规划产物，产品源码、测试、CI、主规格与历史归档保持不变，实施叶全部未勾选

#### Scenario: 一级四件套完成而二级未验证

- **WHEN** 一级 status 报 artifacts 齐全而嵌套 delta 或验收映射未验证
- **THEN** 编排报告区分结构就绪与完整规划核验，不声称实施或归档条件满足

### Requirement: 域产物集成与最终验收无循环等待

编排 SHALL 以就绪叶产物派发，按写集及验证读取闭包仲裁并发。依赖根接线的业务域可交付经核验的域产物供根所有者集成，但 MUST 保持最终验收未完成；根集成 SHALL NOT 等待该域依赖集成后的最终勾选。正式集成后 SHALL 对照原验收面复验并由独立验证者检查偏差，不以组件绿代替正式消费者。

#### Scenario: 消费者域等待根接线

- **WHEN** 02 域产物已经具备集成条件而正式入口尚未接线
- **THEN** 01.5（根装配）/01.6（微信宿主装配）可消费该产物进行单写集成，02 对应叶保持未勾选；接线后补齐正式证据才勾选

#### Scenario: 不同测试包读取同一修改面

- **WHEN** 两单文件名不同但验证编译或读取闭包交叉
- **THEN** 合单、串行或隔离工作树，不能凭不同目录或不同 go.mod 允许共树并发

#### Scenario: 实际验收面比计划缩水

- **WHEN** 缺少原定负锚、消费者或档位
- **THEN** 重开受影响叶并核对下游回归，不通过修改完成口径追认结果

### Requirement: 全事件覆盖与重复证据逐项核销

本变更 SHALL 为当前生产事件机制的 E1—E8 分类登记生产、消费、身份、确认及恢复责任，并关联全部独立测试和具名子场景。短程 SHALL 每 MR 十次独立单轮执行，长程 SHALL 每 MR 执行三种子与两窗口六组合；证据 SHALL 保留每次原始退出码及真实生命周期，不因汇总隐藏失败。整链聚合记录 MUST NOT 替代独立负锚，诊断基线红证 MUST NOT 被当作最终绿证。

#### Scenario: 少一轮或少一个组合

- **WHEN** short 仅有九份证据，或 long 仅有五个组合
- **THEN** 事件汇总失败，不以已执行子集宣称完整

#### Scenario: 相同日志被重复登记

- **WHEN** 多次运行复用同一日志或 run-id，或日志缺少所选子测试终局
- **THEN** 检查拒绝该证据，父测试通过不能补齐缺项

#### Scenario: 长程资源统计

- **WHEN** 六组合长程运行完成
- **THEN** 每组合四会话共享 Host，1200 个不同业务输入与120次真实进程重启；总计7200输入和720重启，一次进程重启不按会话数重复计数

### Requirement: 确定性验收不扩张外部授权

编排 SHALL 保留既有 CI、Lint、Golden 与发布证据要求。新增事件汇总只覆盖新增三档，不替代既有必跑检查。成本超预算 SHALL 先优化组织或报告裁定，MUST NOT 自动缩减覆盖或关闭消费者；真实模型、微信、掉电、72h、训练制品、提交/推送及归档 SHALL 分别登记授权和实际证据。

#### Scenario: 本地模拟通过但远端未授权

- **WHEN** 本地正式装配模拟通过而提交、真实渠道或远端 MR 未获授权
- **THEN** 仅登记本地确定性完成，外部待验不伪报通过，不修改运行中的机器人或远端保护

#### Scenario: 成本超出候选预算

- **WHEN** 校准显示 short、long 或 mutations 超过候选超时
- **THEN** 保留测量与失败证据，优化分片或请求预算裁定，不删种子、窗口、输入、重启、冥想或持久化屏障

### Requirement: 症状锚启动门

本变更 SHALL 以三条症状直译测试的**先红**作为其余一切编码工作的启动门：任何一域的行为修改在 00 域红证产出之前不得开始。

#### Scenario: 未出红证即改行为被拒

- **WHEN** 00 域三测试任一尚未在基线树上产出红证
- **THEN** 01/02/03/04 域的行为修改 SHALL NOT 进入实施；交付物缺红证视为启动门未过

### Requirement: 麻雀闭环

01.1（受理回执→`ok`）SHALL 在 W0 内完成，使症状 2 的直译测试由红转绿，作为其余域的机制样板。

#### Scenario: W0 未闭环不进 W1

- **WHEN** 01.1 未使 00.2 转绿
- **THEN** W1 三单 SHALL NOT 派发

### Requirement: 触碰集不相交与热点合单

同文件簇的孙任务 SHALL 合单或严格串行（01 与 02 同动 host.go 即串行）；仅文件簇零相交的任务可并发。

#### Scenario: 同文件并发被拒

- **WHEN** 两笔派发单的写集同时含 host.go
- **THEN** 两单 SHALL NOT 并发；编排文档记录合单或串行理由

### Requirement: 症状红转绿双证据（F3）

F3 SHALL 含症状直译锚的回滚即红与修复后绿两次证据；工程化测试全绿不替代症状证据。

#### Scenario: 缺回滚证据不得勾选 F3

- **WHEN** F3 只有修复后绿证、无回滚即红证
- **THEN** F3 SHALL NOT 勾选，收口未完成

### Requirement: 契约偏移检查

实施期任何对骨架/契约（主规格、golden 合同）的修改 SHALL 有评审记录；本合同条款修订须显式声明。

#### Scenario: 无评审的契约改动被拒

- **WHEN** 实施单修改 `openspec/specs/**` 或 `golden/runtime/event_integrity_scenarios.json` 而无评审记录
- **THEN** 该单 SHALL 回退并补评审

### Requirement: 麻雀先行

投递 hold→批准补投→恰一条 的短闭环 SHALL 在其余实现之前完成，作为发送/审核衔接的机制样板。

#### Scenario: 麻雀未闭环不扩面

- **WHEN** 1.0 的短闭环未红转绿
- **THEN** 1.1/1.2/1.3 SHALL NOT 派发

### Requirement: 症状回滚即红

F3 SHALL 含 G-A/G-B 的回滚即红双证据：还原 entry 的 send 授权→G-A 必红；禁用投递审核→G-B 必红。

#### Scenario: 缺回滚证据不得收口

- **WHEN** F3 只有修复后绿证
- **THEN** F3 SHALL NOT 勾选

### Requirement: 同文件簇串行

`host.go` 与 `api/plugin/governance.go` 由 1.1/1.2 同时触碰的，SHALL 串行或合单；entry 配置与台架文档（1.3）在其后。

#### Scenario: 同文件并发被拒

- **WHEN** 两单写集同时含 host.go
- **THEN** 两单 SHALL NOT 并发

### Requirement: 协议保真麻雀先行

G1 协议保真红转绿 SHALL 先于其余实施；G10 测试去假设同批完成（根治 CI flaky）。

#### Scenario: 麻雀未闭环不扩面

- **WHEN** G1 三场景未红转绿
- **THEN** 2.1/2.2/3.x/4.x SHALL NOT 派发

### Requirement: 十锚回滚即红

F3 SHALL 含 G1–G10 各自的定点破坏红证（还原对应修复点→该 G 测试必红）。

#### Scenario: 缺回滚证据不得收口

- **WHEN** F3 只有修复后绿证
- **THEN** F3 SHALL NOT 勾选

### Requirement: 协议保真热点串行

`request.go` 触碰的叶（1.0、2.1、4.x）SHALL 串行；`facts.go`/`loop.go` 簇（2.2）与 `host.go` 簇（3.1/3.2）合单。

#### Scenario: 同文件并发被拒

- **WHEN** 两单写集同时含 request.go
- **THEN** 两单 SHALL NOT 并发

### Requirement: 零差额在检查面强制

check_doc_budget.py SHALL 在实测与基线不相等时（两向）以非零退出码拒绝；锁步（--tighten 或具名重锁）SHALL 与文档体量变更同提交。

#### Scenario: 下漂不锁即红

- **WHEN** 实测 < 基线且未先 --tighten
- **THEN** 检查 SHALL exit 1 且文案指向 --tighten

#### Scenario: 钩子守卫可达

- **WHEN** 提交暂存任何 *.md/*.markdown
- **THEN** pre-commit SHALL 运行同一零差额检查

### Requirement: 每批三问

每批收尾 SHALL 回答：删了什么状态（diff）/责任归谁（报告）/原有验收仍成立（全量+定点破坏）。

#### Scenario: 三问缺一不勾批

- **WHEN** 任一问无证据
- **THEN** 该批 SHALL NOT 勾选

### Requirement: 不变量红即违例

D5 锚测试任一红 SHALL 视为对应不变量违例，阻断收口。

#### Scenario: 锚红不得收口

- **WHEN** 任一锚测试红
- **THEN** F3 SHALL NOT 勾选

### Requirement: 验收锚随任务行冻结

每叶验收面（正锚/负锚/消费者锚）SHALL 随任务行冻结于派发前（design D6）；执行期只对账，追认缩小验收面=该叶重开。

#### Scenario: 缩水即重开

- **WHEN** 完成时以替代测试或缩小的验收面勾选
- **THEN** 该叶 SHALL 重开

### Requirement: 波后独立偏差评估

每批执行完、任务勾选前 SHALL 派独立验证子代理（与实施者分离）对照原任务行验收面五面核查（产物/正式链路/负锚/勾选/目标覆盖），偏差分级处置，返工止于波内。

#### Scenario: 无偏差报告不得勾批

- **WHEN** 波后无独立偏差报告
- **THEN** 该批 SHALL NOT 勾选收口

### Requirement: 旧路径零残留

迁移 SHALL 使旧顶层包路径在 Go 源、CI、门禁基线与文档中零残留（结构负锚）。

#### Scenario: 旧路径残留即红

- **WHEN** 任一 `tagent/{telemetry,evolution,rl}"` import 或基线键残留
- **THEN** 结构断言 SHALL 红

### Requirement: CI 终态确认

收尾 SHALL 本地全套门禁绿后推送，并确认远端 CI 全绿后方可归档。

#### Scenario: 远端未绿不归档

- **WHEN** 远端 CI 任一 job 红
- **THEN** F6 SHALL NOT 勾选

### Requirement: 静默红清零

清理 SHALL 使 smoke 全套（tmux 真实执行）与 race（无 U5 豁免）、结构门全部转绿。

#### Scenario: 全套 smoke 绿

- **WHEN** `go -C examples/wechat-bot test -tags smoke . -count=1`
- **THEN** SHALL exit 0

### Requirement: 三段验收

孙任务 SHALL exit 0 且带正/负/消费者三锚；子域 Scenario 逐条映射测试；变更级 DoD SHALL 含症状锚红转绿与回滚即红双证据。

#### Scenario: 回滚即红

- **WHEN** 完整回滚本变更复跑症状锚
- **THEN** 两条直译测试 SHALL 红

### Requirement: 最小可证伪路径先行

W0 SHALL 在全量展开前完成症状锚 S2 的红→绿短测（从用户症状直通缺陷机制）；S2 未绿，W1 之外的实现单 MUST NOT 派发（R34 规模警戒：叶数>20）。

#### Scenario: S2 先行

- **WHEN** W0 结束
- **THEN** `TestSendSymptomReentersAgentTask` 由红转绿的双证据入台账，且其红因命中"重入不可达"而非编译失败

### Requirement: 波次解锁以依赖入 main 为准

波次 SHALL 仅作汇报分组；子域 SHALL 按 design D2 外部依赖解锁，同文件簇（host_tools/host_document、wechat*.go）跨域改动 MUST 合单或串行；解锁的集成线 SHALL 为 **main**（与「波次解锁以入 main 为准」同义），MUST NOT 以“前置域本地绿”替代，且 MUST NOT 等待整波。

#### Scenario: 同文件簇不合单即冲突

- **WHEN** 两单触碰集相交且未合单
- **THEN** 编排者 MUST 拦截派发（触碰集=改写文件 ∪ 构建闭包 ∪ 验证读取包）

#### Scenario: 前置仅本地绿不解锁

- **WHEN** 01 域在派发分支上全绿但未合入集成树（main）
- **THEN** 02 与 03 域的实施派发 SHALL 不开始

### Requirement: 防护启动门

G1 实现单派发前，settle 恰好一次、会话占位、白名单 fail-closed 三条红线的既有测试 SHALL 复跑绿且防护用例立起；W0 未完成，01 域实现单 MUST NOT 派发。改动期间任一红线测试红 SHALL 立即停线。

#### Scenario: 红线先绿后改

- **WHEN** W0 未收口而 01 域实现单就绪
- **THEN** 编排者 MUST 拦截派发，直至防护面凭据入台账

### Requirement: verify 三锚随任务行冻结

每叶 SHALL 携带正锚（测试名与 -run 正则逐字符一致）、负锚（具名错误路径）、消费者锚（正式入口/真实路径）；验收面缩水 SHALL 重开该叶（R29）。

#### Scenario: 空转拦截

- **WHEN** 正锚零匹配或用例数为 0
- **THEN** 该叶勾选无效

### Requirement: 症状锚红转绿双证据

变更完成 SHALL 携带 I1–I4 的红转绿双证据（完整回滚必红、修复后必绿）入 run notes；工程锚全绿而症状锚红时以症状为准。send-cross-task-session 的 S1–S3+N1–N6 九则直译锚同受本条约束（见 symptom-anchors）。

#### Scenario: 回滚即红

- **WHEN** 收尾 F3 执行
- **THEN** 回滚树上四则症状锚全红且红因命中各自语义缺失

### Requirement: 波后偏差评估与独立放行

每波收尾 SHALL 派独立验证子代理对照原任务行验收面五面核查（R30）；修复批次后 SHALL 过独立只读审者拷问至 frontier 空（R35），放行证据取自提交树。

#### Scenario: 追认缩水被拒

- **WHEN** 勾选按缩水后验收面追认
- **THEN** 偏差报告判"补差重开"，返工止于波内

### Requirement: 启动门未过不得派发

编排者在启动门核实通过前 MUST NOT 派发任何二级域实施单；门未过时的唯一动作是把门的状态写入 ORCHESTRATION.md 台账。

启动门的裁定背景：attention 与 PR #33 并行推进、在同一分支汇合后一并合并；实施基线 = 含 PR #33 内容的合并线。

#### Scenario: 门未过时派发被拒

- **WHEN** ORCHESTRATION.md 台账中启动门仍为未过
- **THEN** 任何二级域的实施派发 SHALL 不发生，且台账 SHALL 记录门的状态与核实命令

### Requirement: 共享面由冻结方单写

`api/event` 的类型与 meta 键 SHALL 由 01 域单写并在其入 main 后冻结；此后任何
二级域对冻结文件的改动 MUST NOT 入链，除非编排者以新派发单显式解冻。

#### Scenario: 冻结后的越界改动被拒

- **WHEN** 01 域已入 main 且某 W2 派发单白名单不含 `api/event`
- **THEN** 该域对 `api/event` 的任何改动 SHALL 不入链，违规单 SHALL 重开

### Requirement: 独立领域拷问先于实施授权

每个领域 SHALL 由不带前序审计结论的独立代理阅读当前文档、主规格与代码，输出具名场景、证据和待裁 frontier。编排者 MUST 对每项分别裁定成立、反证驳回或范围外登记；W0 未完成前 MUST NOT 派发产品实现。

#### Scenario: 六域报告到齐

- **WHEN** 六个领域各自提交文档—规格—代码证据及待裁问题
- **THEN** 编排者逐项记录裁定并回写对应二级子变更
- **AND** 任何未裁关键问题都阻止相关实施单进入就绪队列

### Requirement: 公共概念只减不增的实质判据

新增导出类型、接口、状态名或读面 MUST 对应独立领域事实并说明既有概念为何不能承载；缓存、锁、围栏、补偿事务和实现索引 SHALL 默认内化。仅改名、增加别名或缩小接口方法数 MUST NOT 计为概念收敛。

#### Scenario: 修复拟新增公共机制

- **WHEN** 一个修复需要新增导出机制类型
- **THEN** 该类型必须进入概念账本并证明独立领域必要性
- **AND** 无法证明时改为非导出实现或删除，不得以测试绿豁免

### Requirement: 公共症状锚与垂直红绿证据

实施 SHALL 从用户原话对应的公共入口症状锚开始，每个孙任务 MUST 冻结正锚、负锚和生产消费者锚。完成证据 MUST 包含非空转的红→绿过程；内部 helper 或私有状态断言 MUST NOT 替代公共行为。

#### Scenario: 地址消费最小探针

- **WHEN** 通过正式 Host/send 路径向空闲地址提交输入
- **THEN** 该输入只能由该地址的常驻 Consumer 驱动并得到其自身答案
- **AND** 该行为锚在旧实现上失败、在收敛实现上通过

### Requirement: 每批修复接受新鲜独立复审

每个修复批次完成后 SHALL 由不带上一轮结论的新只读审者重扫受影响面和同类路径。审者 MUST 报告扫描范围与逐项证据；只有显式 frontier 空才可声明该批放行。

#### Scenario: 修复后再次出现关键问题

- **WHEN** 新审者在修复面或同机制路径发现关键问题
- **THEN** 编排者重新裁定并派发最小修复，不得沿用上批自证结论
- **AND** 同一机制三次修复仍出现关键缺陷时停止打补丁并升级设计裁定

### Requirement: CI 分层不得后置或丢失完整证明

CI 优化 SHALL 区分快速 PR 反馈与 merge 前完整证明，保留独立进程、矩阵身份和缺号/重号/缺片拒收。完整长程测试 MUST 在进入 main 前通过；MUST NOT 以扩大超时、放大未实测矩阵或 push 后 nightly 冒充等价优化。

#### Scenario: 长测移到 merge queue

- **WHEN** 完整六组合改为 merge_group 执行
- **THEN** 相同输入、重启、seed、window 与证据校验全部保留
- **AND** 缺少任何必需组合的变更不得进入 main

### Requirement: 波次解锁以入 main 为准

波次 SHALL 仅作汇报分组；一个二级域的解锁条件 SHALL 是其全部前置域已合入集成树（main）并通过全量门核验，MUST NOT 以"前置域本地绿"替代，亦不等同于同波其他域完成。编排者 SHALL 在每次派发前重读 ORCHESTRATION.md 第二节并核对四问与实况一致。

#### Scenario: 前置先行解锁

- **WHEN** W1 中 01 已入 main 并过全量门，而 04 仍在实施
- **THEN** 02 的前置（W1 全入 main——01 的 loop.go 提交面与 05 的投影面已稳定，02 依赖其稳定）尚未满足，02 不可先派；若 04/05 提前完成则前置提前满足

#### Scenario: 前置仅本地绿不解锁

- **WHEN** 01 域在派发分支上全绿但未合入集成树（main）
- **THEN** 02 与 03 域的实施派发 SHALL 不开始

### Requirement: 启动门——症状锚先红

一级变更 SHALL 在任何实现派发前，把六条症状锚（快）测试落在基线树上并确认全部红着（锚①-④在 assembly、⑥在 tool/exec、⑦在 memory/replay），并以症状锚⑤（慢）（既有 TestLocalSmoke）的评审两次实测红凭据为计划期凭据（自有红证于 W2 首日、W1 合并增量（01+04+05）已入 main、02/03 未入树时补齐，见一级 tasks 0.1）。任一锚在基线树变绿 SHALL 视为误诊，先回写一级 design D1 再继续。

#### Scenario: 六条症状锚（快）命令

- **WHEN** W0 执行后从仓库根分别运行 `go test ./assembly -run 'TestSymptomAnchor' -count=1 -v`、`go test ./tool/exec -run 'TestSymptomAnchor' -count=1 -v`、`go test ./memory/replay -run 'TestSymptomAnchor' -count=1 -v`
- **THEN** 三包 FAIL 行数分别为 4/1/1 且均 >0（非空转证明），无 PASS/SKIP 行冒充红证

#### Scenario: 症状锚⑤（慢）独立命令

- **WHEN** 执行一级 tasks 0.1 的症状锚⑤（慢）命令 `cd examples/wechat-bot && go test -tags smoke -run '^TestLocalSmoke$' -count=1 -timeout 2h -v ./...`
- **THEN** 计划期红凭据=评审两次实测 175/159 KiB/轮 > 128 KiB/轮；W2 首日自有红证同命令重跑，日志含 rounds=300 与斜率数字（非空转），输出存 run notes

### Requirement: 契约守护

本变更 SHALL 保持 `api/**` 零改动，唯一例外=`api/config` 新增两个可选字段（02 的 provider 窗口与输出余量声明；**预算门开 ⟺ 显式 provider 窗口声明 ∨ AgentSpec.MaxTokens>0（缺省推导）；MaxTokens=0 且未显式声明 = 预算门不启用（记 debug 事实，不裁剪不拒发）**；`api/config/decode.go`、`api/config/fingerprint.go` 的配套行同属该例外清单）；交付记录 schema 演进 SHALL 仅发生在 01 且向后兼容旧记录读取；`tagent.record.v1` 既有字段语义 SHALL 不变；**08 的声明类型常量驻 assembly、09 保留既有 `meditation` 节，二者对 `api/**` 零改动**。收尾 SHALL 以双断言：`git diff <基线>..HEAD -- api/` 输出仅含列明的 api/config 新增可选字段行（含 decode/fingerprint 配套行），且 `git status --porcelain -- api/` 为空。

#### Scenario: 收尾契约断言

- **WHEN** F1 执行 `git diff 48d8eb93..HEAD -- api/` 与 `git status --porcelain -- api/`
- **THEN** 前者输出仅含列明的 api/config 新增可选字段行（含 decode/fingerprint 配套行）、后者输出为空，均 exit 0

### Requirement: 变更级 DoD

一级变更完成 SHALL 同时满足：七条症状锚全绿、全量门禁与 race 门通过、配置活线证据位（声明事实/发布记录/失败回执三链测试＋watcher 三例＋重启对账/跨维窗口）与冥想自主证据位（自主整理、自察四测试、激活因果、自管产出不计入状态积累）各有 PASS 行、对照 run（含任务成功率/送达率/延迟/token 成本/人工介入次数五指标，其中人工介入次数=审批入口调用数+blocked 上报数）落盘可回读、训练样本三要素（请求回读、独立验收、验收结论入 rl annex）齐备率以全部样本（含 incomplete）为分母且 MUST 为百分之百、收尾任务全部勾选且各附凭据；奖励向量派生不在本变更范围，不计入 DoD。

#### Scenario: DoD 核验

- **WHEN** F3 在回滚树与修复树各跑一轮七锚命令（全带 `-v`）
- **THEN** 回滚树全红（六条症状锚（快）FAIL 行数 4/1/1、症状锚（慢）红）、修复树全绿（六条症状锚（快）PASS 行数 4/1/1、症状锚（慢）绿）

#### Scenario: 08/09 证据位核验

- **WHEN** F3 收集 08/09 域的 DoD 证据
- **THEN** 08 的声明事实/发布记录/失败回执三链测试与 watcher 三例、09 的自察四测试/激活因果/自管产出不计入状态积累各有 PASS 行，两树各包 PASS/FAIL 行数均 >0 且两份输出均存入 run notes

### Requirement: 先交接后销账

生成结果的交付义务 SHALL 在发送前持久登记；生成义务 SHALL 在交付义务登记成功后才消费。

#### Scenario: 崩溃后不漏送

- **WHEN** Sender 未确认时进程被强杀并重启
- **THEN** 已生成结果 SHALL 被恢复发送，待交付账本销账

#### Scenario: 发送已落地而回执未落按至少一次容忍

- **WHEN** Sender 已接受正文而 sent 回执未落
- **THEN** 该义务 SHALL 保持未销，重启 SHALL 恰好重投一次（该次重投的 sent 回执落地时；回执未落按至少一次容忍），该重复投递 SHALL 被容忍为至少一次语义的一部分；回执落地 SHALL 是阻止再次投递的唯一凭据

理由 SHALL 与声明同处记载：发送与「发送已落」的持久回执是两个崩溃点，任何实现都只能在两者之间选一个顺序；先发送后回执必然留下「已送达但未销账」的窗口，把该窗口读成漏送会迫使实现选择先回执后发送，从而在相反窗口里真漏送。本契约因此把该窗口的重复投递定为可接受代价，而不是违约。

### Requirement: 交付义务只登记入口地址的正文

交付义务 SHALL 只为入口 agent（entry）地址生成的正文登记。其余地址 SHALL 按 headless 语义驱动：其正文由驱动它的调用方读取，SHALL NOT 进入消息渠道。登记面（settle 终态）、交接面（handoff）与恢复面（boot restore）SHALL 读同一地址门禁，与实时投递面的入口判定同源。

已登记的非入口地址正文义务（上一版或兄弟进程所留）SHALL 在恢复时被跳过投递，并以具名状态 `headless` 销账；该记录 SHALL 保留正文与目标，供读者审计，SHALL NOT 因此被投递。

#### Scenario: 非入口地址的正文不登记也不投递

- **WHEN** 非入口 agent 地址上的 follow-up turn 完成并生成正文
- **THEN** SHALL NOT 登记交付义务，重启后消息渠道 SHALL 收不到该正文

#### Scenario: 遗留的非入口义务恢复时具名销账

- **WHEN** 恢复读到 target 为非入口地址的 owed 记录
- **THEN** SHALL NOT 投递该正文，该记录状态 SHALL 转为 `headless`

### Requirement: 未销义务在进程内补投

消费回执尚未落地的未销交付义务（批量登记中途失败、兄弟进程登记后崩溃、通道暂不可用）SHALL NOT 滞留到下次重启：交接面 SHALL 在该次通知的终态落地时，对其所驱动的通知 invocation 生成的正文与记录仍处 owed 状态的正文一并补投，SHALL NOT 只发送本次新登记的正文。

补投面 SHALL 由该次交接的通知身份界定：交接面只读该通知 invocation 生成的正文，因此只有该通知的消费回执尚未落地的义务落在此面内。消费回执已经落地而发送未发生的义务 SHALL NOT 被读作本 Requirement 的滞留：该通知不再被唤醒，其实例的恢复面是重启（见「残余窗口显式声明」）。

补投 SHALL 以记录状态为准：状态已为 `held`/`retry`/`sent`/`refused`/`headless` 的正文 SHALL NOT 被交接面重投。补投与 boot restore 的并发 SHALL 由 sent 回执的幂等交付与上条的至少一次容忍界住。

#### Scenario: 已登记未发送的义务由下次交接补投

- **WHEN** 某正文的义务已登记为 owed 而发送未发生，同一进程内该通知的下一次交接读到该记录
- **THEN** 该正文 SHALL 被补投一次，其记录 SHALL 转为 `sent`（通道接受且 sent 回执落地时；裁决挂起转 `held`、通道拒收转 `retry`、回执未落按至少一次容忍）

#### Scenario: 已销账的义务不被补投

- **WHEN** 交接读到的记录状态不是 owed
- **THEN** 该正文 SHALL NOT 被重投

### Requirement: 状态提交 checked

交付状态的持久化失败 SHALL 以具名错误返回且不发布内存态变化。

#### Scenario: 持久化失败不吞错

- **WHEN** 交付状态写入失败
- **THEN** 接口 SHALL 返回错误且内存态保持

### Requirement: 旧格式具名拒绝

上一版 key/content/summary 格式字节 SHALL 被 DecodeEventBody 具名拒绝。

判据 SHALL 由原始 JSON 的顶层键存在性给出：顶层无 `message` 键即不是本 schema，覆盖 key/content/summary 与 event_key 两种旧形态。判据 SHALL NOT 由解码后的结构体零值给出——空 message 是本 schema 内的合法审计事实，零值判据会把它一并误拒。

#### Scenario: 不再静默丢正文

- **WHEN** 旧格式字节解码
- **THEN** SHALL 返回 ErrEventBody，正文不再静默为空

### Requirement: 残余窗口显式声明

用户 turn 的「invoke 返回→交付登记」微窗口（同一调用栈内）SHALL 被显式声明为残余窗口，本契约 SHALL NOT 据此作无条件的「不漏送」承诺。

理由 SHALL 与声明同处记载：结果只有在 invoke 返回之后才存在，而登记只能在拿到结果之后才可写；进程在该窗口内被杀时账上尚无这条义务、正文也尚无持久副本可读，重启能看见的只有登记完成之后的事实，所以该窗口不存在可恢复源。契约承诺的是**已登记义务**的恢复，不是窗口本身的覆盖。

两个「登记已完成、发送未发生」的窗口 SHALL 同处声明，其恢复面是重启，而不是进程内的补投面：

- 「消费回执已落但发送未发生」：通知的消费回执落地之后、正文发送之前进程停止，该通知不再被唤醒，义务记录仍为 owed，由重启的恢复面（boot restore）补投。
- 「用户轮登记后发送前」：用户 turn 的交付义务登记之后、发送之前进程停止，同一进程内没有会读到该义务的交接面，同样由重启的恢复面补投。

两者 SHALL 被读作已登记义务的恢复，措辞与 `settleTerminal` 的注释一致：登记与回执之间的停顿由重启投递的 owed 正文作答（"a stop between the two is answered by the owed body a restart delivers"）。

#### Scenario: 回执已落而发送未发生的义务由重启补投

- **WHEN** 通知的消费回执已落地而正文发送未发生，进程随后被强杀并重启
- **THEN** 该义务 SHALL 由重启的恢复面补投，本契约 SHALL NOT 把它读作进程内补投面的滞留

#### Scenario: 用户轮登记后发送前停止的义务由重启补投

- **WHEN** 用户 turn 的交付义务已登记而正文发送未发生，进程随后被强杀并重启
- **THEN** 该义务 SHALL 由重启的恢复面补投，进程内的交接面 SHALL NOT 被读作该义务的补投面

#### Scenario: 强杀后已登记义务恰一次投递

- **WHEN** Sender 未确认时进程被强杀并重启
- **THEN** 强杀前投递 SHALL 为 0 条，重启后该结果 SHALL 恰投递 1 条（该次重投的 sent 回执落地时；回执未落按至少一次容忍），持久账 `tagent/task/v1/delivery:` 前缀下的 owed 条目 SHALL 归零（该次重投的 sent 回执落地时），该结果的生成义务 SHALL 恰被消费一次

#### Scenario: 微窗口内被杀不作恢复承诺

- **WHEN** 进程在 invoke 已返回而交付义务尚未登记的微窗口内被杀
- **THEN** 该结果 SHALL 计为残余窗口内的丢失，本契约不作恢复承诺

### Requirement: final 跨重启不承诺

执行最终输出的 final 表 SHALL 被声明为进程内运行时状态：跨重启不承诺保留。重启后读取 final SHALL 返回 ErrFinalOutputUnrecorded，该错误 SHALL 被读作可解释错误而非成功语义的违约；final 的持久化登记 SHALL 归入下一轮实现，不在本契约的承诺面内。

#### Scenario: 重启后读 final 属可解释错误

- **WHEN** 进程重启后读取上一进程写下的 final
- **THEN** SHALL 返回 ErrFinalOutputUnrecorded，且该返回 SHALL NOT 被读作本契约的违约

#### Scenario: 进程内 final 照常可读

- **WHEN** 同一进程内 final 写入后读取
- **THEN** SHALL 返回已写入的最终输出

### Requirement: 无持久层装配的降级

未装配持久层（storage tier）的运行实例 SHALL 被声明为进程内降级：交付状态与待交付义务 SHALL NOT 承诺跨重启保留，重启后从进程内空态开始。本 Requirement SHALL 被读作上述各条的边界，SHALL NOT 被读作无条件承诺。

#### Scenario: 无 tier 装配下重启

- **WHEN** 未装配持久层的实例重启
- **THEN** 交付状态 SHALL 从进程内空态开始，重启前登记的交付义务不作恢复承诺

### Requirement: 独立写权状态可反事实删除

凡不改允许操作的状态 SHALL 通过「删掉后从已有记录无歧义算回来」检验；通过者 SHALL 删除写权改派生读数。

#### Scenario: progress 事实化

- **WHEN** 进度不改允许操作
- **THEN** 进度 SHALL 以事实记录，状态位写权 SHALL 删除

### Requirement: 执行核心零训练形态

核心（runtime/ 运行时核心）SHALL 只保留执行证据（调用时固定的实际请求、输入引用、工具调用、已归约结果、因果）；训练组织 SHALL 移训练模块。装配面（tool/ 等组合根）对训练模块的编译级依赖不在本约束，其运行时关闭路径 SHALL 有锚。

#### Scenario: 关闭训练后核心协议完整

- **WHEN** 训练/自演进关闭
- **THEN** 核心执行协议 SHALL 完整成立，实际输入 SHALL 仍调用时固定

### Requirement: 六端口闭集无组合词汇

Material×Product 组合词汇若不做真正组合推导 SHALL 删除；六具名端口窄接口 SHALL 保留且不合并。

#### Scenario: 扩展者少理解一层

- **WHEN** 实现插件
- **THEN** SHALL 只需声明一个具名端口并实现其窄接口

### Requirement: 热更新三档明示

模型/提示词 SHALL 走新代发布；工具与插件声明、存储根/持久化格式/资源所有权变更 SHALL 具名拒绝并要求重启（natives 随 Boot 绑定，无新代重绑路径）；固定配置 SHALL 一次发布。MCP 服务器声明除外：其随新代发布重绑（fingerprint 含 servers，真实改动必换代热生效）。

#### Scenario: 越档变更具名拒绝

- **WHEN** /reload 请求越档变更
- **THEN** SHALL 具名拒绝并指明重启要求

### Requirement: 新工具接入零补偿

接入新工具 SHALL 只需适配器提供执行、取消及恢复证据；宿主 SHALL NOT 另写通知补偿队列；核心 SHALL NOT 新增生命周期状态。

#### Scenario: 责任模型降低复杂度

- **WHEN** 一个新工具按此契约接入
- **THEN** 受理→结算→投递全链 SHALL 跑通且宿主零补偿代码

### Requirement: 阶段门禁与拷问闭环

一级变更 SHALL 把实施划分为七个阶段（W0–W6）与一个收尾（W7），每一阶段完成后 SHALL 依次执行「派独立只读子代理拷问 → 主代理逐项回答并附证据 → 再派子代理依回答完善 → frontier 空」，MUST NOT 在 frontier 非空时进入下一阶段。子代理拷问 SHALL 对照原任务行的验收面，而非追认缩水后的面。

#### Scenario: 阶段推进凭据

- **WHEN** 某阶段声称完成
- **THEN** ORCHESTRATION 台账 SHALL 含该阶段拷问子代理 id、frontier 项清单、主代理回答、完善凭据与复跑命令，缺一不得推进

### Requirement: 三挂点边界机械守护

全部改动 SHALL 落在「消费方向 g 的读链」内部，事实链保持唯一真相；MUST NOT 引入链外可写第二真相。外部状态 blob SHALL 由链上票据事实按内容寻址引用、带完整性校验、损坏即退回全量事件流。F0 SHALL 新增边界规则：任何链外持久物未经 R3 登记即红。

#### Scenario: 全量等价

- **WHEN** 存在一条恢复态票据
- **THEN** 「全量重放」与「加载 dump + 其后增量」SHALL 逐位一致；票据缺失/截断/校验不过/版本不认识时 SHALL 退回全量且结果不丢义务

### Requirement: 词汇一致

项目术语 `转世` SHALL 统一为 `重启恢复` 并同步简化代码标识符；MUST NOT 使用 `读透视图` / `链权威答案` 作为项目词汇；契约值（事件元数据、事实键、冻结类型）MUST NOT 随改名变更。

#### Scenario: 契约零改动

- **WHEN** 阶段 1 与收尾分别运行 F1
- **THEN** `python3 openspec/changes/archive/2026-11-08-restart-recovery-optimization/changes/01-vocabulary/verify_contracts.py` SHALL 输出 `VERDICT: PASS (line sets identical)` 与 `MISMATCH_FILES= 0`，即六个冻结字符串（四个契约拼写 + 两个测试名）在「基线 `1c4e165f` 的 10 个持有文件」与「工作树」中含该串的整行序列一致。MUST NOT 用 `git diff <基线>..HEAD`：阶段 1 时 HEAD 恰等于基线，该命令恒为空（vacuous），且契约串不在该 pathspec 内（见 r6）

