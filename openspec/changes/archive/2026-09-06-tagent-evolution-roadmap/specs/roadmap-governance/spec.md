# roadmap-governance Specification

## ADDED Requirements

### Requirement: 阶段依赖与准入条件
路线图阶段 SHALL 按 P0 → P1 → (P2 → P3) 与 P0 → P4 的依赖序执行;任一阶段子变更开工前 MUST 满足:其全部依赖阶段的子变更已 archive;父路线图 design.md D4 中归属该阶段的预留确认项(C1-C11)已逐项核对(形成决议,或显式采用登记的默认降级路径)。

#### Scenario: 依赖未满足禁止开工
- **WHEN** P2 子变更创建时 P1 子变更尚未 archive
- **THEN** 执行者 MUST NOT 开始 P2 实施,并记录阻塞原因

#### Scenario: 确认项降级放行
- **WHEN** 某阶段开工时其 CONFIRM 项无用户决议
- **THEN** 按 D4 登记的默认降级路径执行,且子变更 tasks.md 对应任务标注 DEGRADED

### Requirement: 阶段准出门禁
每个阶段子变更 archive 前 SHALL 依次通过三道门禁:①`go build ./...` + `go vet ./...` + `go test -race`(新增代码必须有配套测试);②真实集成抽查(LLM/网络级测试,遵循 tests/ 惯例:无 key 自动 Skip、-short 跳过);③CodeReview sub-agent fresh-eyes 评审且"必须修复"级问题清零。同一阶段门禁失败的自修上限为 2 轮,超限 MUST 升级(ESCALATE)而非继续。

#### Scenario: 门禁全过后方可归档
- **WHEN** 子变更三道门禁全部通过且 tasks 完成率 100%(DEGRADED/BLOCKED 项已显式标注)
- **THEN** 执行 commit、archive 子变更、同步主 specs、更新父路线图检查点

#### Scenario: 自修超限升级
- **WHEN** 同一阶段门禁失败修复已达 2 轮仍未通过
- **THEN** 在子变更 tasks.md 标记 BLOCKED 与阻塞原因,跳过非依赖任务,向用户汇报,不强行推进

### Requirement: 真实 LLM 测试失败的定性义务
门禁②中 real-LLM/网络测试失败时,执行者 MUST 先经 Debug 流程定性(pre-existing flaky vs 本变更回归),定性证据至少包含:变更文件与测试依赖面的代码交集分析、跨运行失败点漂移对比、相关文件的 git 历史。判定为 pre-existing flaky 的失败不阻塞准出,但 MUST 记录在案。

#### Scenario: flaky 定性后放行
- **WHEN** 门禁②某 real-LLM 测试失败且 Debug 定性为 pre-existing flaky(三重证据齐备)
- **THEN** 该失败不阻塞准出,定性结论记入子变更 tasks.md 或提交说明

### Requirement: 不变量声明义务
每个阶段子变更的 design.md MUST 显式声明对四项全局不变量的遵守方式:prefix-cache 稳定性(agent 工具声明区恒定,动态能力经内容渗透)、Engine/Policy 分离(有状态引擎与配置派生策略解耦)、事件不可变(FullEvent 只增不改)、失败以 result 渗透(工具失败返回自纠材料而非 panic/中断)。

#### Scenario: 缺失不变量声明的 design 不得进入实施
- **WHEN** 子变更 design.md 未包含不变量遵守声明
- **THEN** 执行者 MUST 先补齐声明再开始 apply

### Requirement: sub-agent 分工惯例
自驱执行 SHALL 遵循已验证的分工:CodeReview sub-agent 承担评审门禁;Search sub-agent 承担事实核查与一致性审计;Debug sub-agent 承担失败定性;实施、测试执行与 openspec 工件维护由主执行者承担;可并行的重活(全量测试、多域调研)SHALL 分批异步派发(后台执行 + 日志轮询,规避交互式终端阻塞)。

#### Scenario: 评审门禁由独立 sub-agent 执行
- **WHEN** 子变更进入门禁③
- **THEN** 由 CodeReview sub-agent(而非实施者自查)产出评审结论

### Requirement: 归档义务与父路线图状态同步
子变更 archive 时 SHALL 同步其 delta specs 至主 specs,并在父路线图(tagent-evolution-roadmap)tasks.md 勾选对应阶段检查点、追加一行阶段完成记录(commit 号 + 日期)。父路线图 SHALL 在全部阶段(P0-P4)子变更 archive 后方可自身 archive;中途终止时以"部分完成"归档并记录终止点与已完成阶段清单。

#### Scenario: 阶段完成回写父路线图
- **WHEN** 某阶段子变更完成 archive
- **THEN** 父路线图 tasks.md 该阶段检查点被勾选且附 commit 号与日期

#### Scenario: 部分完成归档
- **WHEN** 用户终止路线图且已有阶段完成
- **THEN** 父路线图归档时记录终止点、已完成阶段及其成果不受影响
