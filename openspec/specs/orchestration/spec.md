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

### Requirement: 启动门

批 1 SHALL 以 fidelity-and-recovery 全部完成为前置（G4/3.2/1.2）。

#### Scenario: 前置未齐不派发

- **WHEN** fidelity-and-recovery 尚有未勾叶
- **THEN** 批 1 SHALL NOT 派发

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

波次仅为汇报分组；子域 SHALL 按 design D2 外部依赖解锁，同文件簇（host_tools/host_document、wechat*.go）跨域改动 MUST 合单或串行。

#### Scenario: 同文件簇不合单即冲突

- **WHEN** 两单触碰集相交且未合单
- **THEN** 编排者 MUST 拦截派发（触碰集=改写文件 ∪ 构建闭包 ∪ 验证读取包）

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

### Requirement: 契约守护三层闭环

主 `openspec/specs/` 与 `api/` 的改动 SHALL 限于 design D2 表声明的 delta；F1 diff 断言、合并规则硬拦、spec 场景映射三层齐备。settle 恰好一次与会话占位语义 MUST NOT 在本变更中放宽。

#### Scenario: 越界契约改动被拦

- **WHEN** 实现引入 D2 表外的主 spec 改动或放宽红线语义
- **THEN** F1 断言失败或红线测试红并定位到文件

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

`api/event` 的类型与 meta 键 SHALL 由 01 域单写并在其入 next 后冻结；此后任何
二级域对冻结文件的改动 MUST NOT 入链，除非编排者以新派发单显式解冻。

#### Scenario: 冻结后的越界改动被拒

- **WHEN** 01 域已入 next 且某 W2 派发单白名单不含 `api/event`
- **THEN** 该域对 `api/event` 的任何改动 SHALL 不入链，违规单 SHALL 重开

### Requirement: 波次解锁以入 next 为准

波次 SHALL 仅作汇报分组；一个二级域的解锁条件 SHALL 是其全部前置域已合入
next，MUST NOT 以"前置域本地绿"替代。

#### Scenario: 前置仅本地绿不解锁

- **WHEN** 01 域在派发分支上全绿但未合入 next
- **THEN** 02 与 03 域的实施派发 SHALL 不开始

### Requirement: 变更级 DoD 以两证与全量门为准

变更级完成 SHALL 同时满足：两条症状锚红转绿各两证（回滚态必红、落地态必绿）、
全量项目门 exit 0、`openspec archive` 完成；缺任一项 SHALL 视为未完成。

#### Scenario: 缺红锚两证的完成主张被拒

- **WHEN** 某症状锚只有落地态绿证而无回滚态红证
- **THEN** F3 SHALL 不勾选，变更 MUST NOT 归档

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

