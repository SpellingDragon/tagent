# orchestration Specification

## Purpose
TBD - created by archiving change simplify-prototype-event-pipeline. Update Purpose after archive.
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

