> **历史输入（2026-11-20）**：本目录不再作为当前派发或自动规格同步输入；下文旧勾选、方案与限制按原时点保留，不授权后续实施。当前只按[编排入口](../../../../ORCHESTRATION.md)派发三个语义单元，全部旧要求由[承接表](../../../../CARRYOVER.md)逐项接收，不因转历史而取消。

## ADDED Requirements

### Requirement: 主 spec 与代码同向

主 spec 是能力的事实来源，其现行条款 SHALL 与代码的实际行为同向：一条 SHALL/MUST 所断言的行为，SHALL 能在生产路径上找到承载它的现行实现与公共行为锚。当代码已改变而条款未随，该条款 SHALL 被显式改写为现行语义，MUST NOT 以「历史记载」为由留在现行 Requirements 中继续反向约束。

反向的判定 SHALL 以生产路径实测为据，MUST NOT 以符号名相近或条款措辞相似推定：同一 spec 内相邻两条条款可以一条仍成立、一条已反向，改写面 SHALL 逐条界定。已退役机制的历史备注属历史记录，其内对**现行行为**的断言若被推翻，SHALL 附勘误更正，退役事实本身不改写。

#### Scenario: 禁 LLM 条款与语义综述接线反向

- **WHEN** 主 spec 断言历史综述正文「MUST NOT 改写为 LLM 生成文本」，而压缩消费者对该产物类型调用会话模型的综述面并以有界结果取代正文，且该面在正式装配入口接线
- **THEN** 该条款 SHALL 改写为「机械读法为底、持模型时为有界语义综述、无模型/失败/答空降级回机械」，并以 `assembly/summary_gist_test.go:105`（TestCompressionSummaryRecordsTheGist）、`:135`（TestCompressionSummaryKeepsTheMechanicalForm）、`:400`（TestCompressionConsumerDegradesAFailedSummary）为公共行为锚
- **AND** 同 spec 对段折叠产物的同形禁令 SHALL 保留——消费者的类型闸（`assembly/compress_consumer.go:276`）只放历史综述，段折叠正文仍是机械读法

#### Scenario: 条款点名已无定义的机制

- **WHEN** 主 spec 的现行条款 SHALL 一个在 Go 树零定义的票据事实、压实器或生命周期管理器
- **THEN** 该条款 SHALL 退役或改写为现行 owner，且退役前 SHALL 确认全仓无 spec anchor 指向该 capability（`scripts/gates/check_concept_budget.py:926-927` 拒 superseded 作锚）

#### Scenario: 历史备注内的现行断言被推翻

- **WHEN** 已 superseded 的主 spec 备注断言「现行折叠零 LLM」，而生产装配已挂上综述面
- **THEN** SHALL 在该备注附勘误句更正现行行为，MUST NOT 改写其退役事实部分，也 MUST NOT 把整份历史备注重写

### Requirement: legacy 条款按语义单元退役

legacy 条款的处置 SHALL 逐条按语义单元选择手法，MUST NOT 整文件删：仍有现行 owner 的条款改写（MODIFIED），条款整体无现行 owner 的移除（REMOVED），整能力已退役的标 superseded。标 superseded 的主 spec SHALL 保持既有体例——Purpose 下的退役备注说明退役事实与现行 owner，Requirements 只留一条「历史条款已退役」与一个指向现行 spec 的 Scenario，其中 MUST NOT 保留现行 SHALL/MUST 命令。

退役面 SHALL 按符号的实际定义与调用面界定，MUST NOT 按名字形态批量扩大：名字同族而归属不同的符号（一个属现行分页契约、一个属已退役重建路径）SHALL 分别处置。已退役的存储引擎包路径与仍在用的同名档位实现 SHALL 区分，后者条款保留。

主 spec 的结构 SHALL 保持 `# <capability> Specification`、`## Purpose`、`## Requirements`，每个 Requirement 含 SHALL/MUST 与至少一个 Scenario；主 spec MUST NOT 出现 change delta 的 `## ADDED/MODIFIED/REMOVED Requirements` 语法。机制细节（源文件名、调用形态、数字常量）SHALL 落在 Scenario 或长期文档，MUST NOT 落进 requirement 正文。

#### Scenario: 整能力退役后仍留现行命令

- **WHEN** 一份主 spec 标了 superseded，但其 Requirements 内仍含现行 SHALL/MUST
- **THEN** `python3 scripts/gates/check_concept_budget.py` SHALL 判红（superseded_spec_findings，`:815-839`），该批不得准出

#### Scenario: 同族符号被批量误删

- **WHEN** 退役重建路径时把名字同族的分页错误一并删除，而该错误仍被现行链分页与其契约示例消费
- **THEN** 该删除 SHALL 被拒——`memory/replay/projection.go:200` 与 `examples/contract/memory_replay/EventChain_example.go:19/:61` 是其独立消费面；只退役重建专属的错误与选项/报告类型

#### Scenario: 改写把机制细节写进 requirement 正文

- **WHEN** 一次转正把现行权威写成带调用括号的函数形态、源文件名或字节界数字，落进 requirement 正文
- **THEN** `python3 scripts/gates/check_spec_hygiene.py` SHALL 报新增违例；作者 SHALL 把细节移入 Scenario 或 `docs/wiki`，MUST NOT 为消红先改 `golden/runtime/spec_hygiene.json` 基线

#### Scenario: 仍在用的档位实现被顺手退役

- **WHEN** 退役已不存在的存储引擎包路径时，把同名但现行在用的分段存储实现的关闭/加载条款一并删除
- **THEN** 该删除 SHALL 被拒；只去条款中已零定义的分句子句，主体保留（`memory/ledger/segment_store.go:244` 是现行实现）

### Requirement: golden 记载与代码同步

golden 注册表中的说明性记载 SHALL 与其所登记的代码同向，包括键形、身份构成与机制归属的描述。当门禁只比对字面量而不比对说明文本时，说明文本的同步 SHALL 另有可复跑的负锚（残留检索），MUST NOT 以「门绿」声称记载已同步。

棘轮台账的历史抬升记录 SHALL 只追加勘误，MUST NOT 改写既有数值与历史条目；只有当导出面实测发生变化时，才按实测另抬并记理由。

#### Scenario: 键形说明落后于实现

- **WHEN** 注册表说明仍写单段槽位键形，而实现的键已含分区与槽位两段
- **THEN** 说明 SHALL 改写为两段键形（`golden/runtime/durable_truth.json:300` 对 `api/event/checkpoint.go:75-76`），前缀字面量登记值不动
- **AND** 负锚 SHALL 是残留检索——`scripts/gates/check_durable_truth_registry.py:460` 只比字面量，改前改后均绿，不足以证明同步；消费者锚为 `go test ./api/event -run '^TestCheckpointKeyNamesThePartition$' -count=1`（`api/event/checkpoint_test.go:43`）

#### Scenario: 兼容读法结论落台账

- **WHEN** 审计判定六端口常量仍是活的分派键、而 material 的兼容派生生产零消费
- **THEN** `golden/runtime/architecture.json:6` SHALL 以既有勘误体例追加该结论，辨明两者的主语差别，三个棘轮数与历史抬升记录逐字未变

### Requirement: 死生产者清零且覆盖不丢

无生产调用方的生产者 SHALL 删除，其编译期断言与只服务于它的伴生类型、错误与判定函数 SHALL 同批回卷。删除 SHALL 与退役其主 spec 条款同批原子落地，MUST NOT 先删实现后改 spec，也 MUST NOT 先改 spec 后留实现。

死生产者的测试锚 SHALL 先定性后处置：判据已由现行路径的孪生锚承载的，随机制退役并记明孪生锚；判据仍现行而立在被删函数上的，先迁移到现行路径再删函数，断言判据逐字保留。MUST NOT 整文件删而连带移走同文件内仍现行的锚，MUST NOT 削弱断言以迁就删除。活的操作员工具与现行档位实现 MUST NOT 列入死码。

#### Scenario: 删函数留下无覆盖窗口

- **WHEN** 一个被列为必须保留的现行幂等锚靠被删的重建函数建投影，而删除先于锚迁移
- **THEN** 该次序 SHALL 被拒；先改用现行恢复路径建投影（`assembly/run.go:2562` seedAnchoredReplay，`:2633/:2676` 调 RestoreProducts），保留「同链同窗口折出同组内容寻址产物键」断言，再删函数

#### Scenario: 孪生锚已存在仍整文件删

- **WHEN** 死锚的判据已由现行生产面孪生锚承载，作者据此整文件删除
- **THEN** 该删除 SHALL 被拒——同文件内仍有测量存在性锚（`memory/replay/fold_test.go:118`）；且死锚独有的 retained-ref 子断言随判定函数退役一事 SHALL 显式记明，MUST NOT 声称判据全等价（孪生锚见 `memory/replay/replay_budget_test.go:402`、`memory/replay/age_segments_test.go:506`）

#### Scenario: 活工具被误列死码

- **WHEN** 清理把分片根迁移函数列入死生产者，而它有正式的命令行入口
- **THEN** 该删除 SHALL 被拒（`memory/ledger/migrate.go:76`，入口 `scripts/migrate-kv/main.go:24`）

#### Scenario: 主 spec 仍 SHALL 已删函数

- **WHEN** 重建函数已删而主 spec 的冷启动只读重建条款仍在
- **THEN** 残留检索 SHALL 判红；该条款须与删除同批 REMOVED，其幂等条款须同批去掉被删函数分句（`openspec/specs/event-sourced-projection/spec.md:47-51`、`:156`）

### Requirement: 兼容读法退役须先审计消费者

兼容读法（为尚未迁移的声明方保留的派生路径）SHALL 先枚举全部消费者再裁退役，消费者按生产、公共契约示例、测试三分登记。测试 SHALL NOT 计入独立第二使用方；公共契约示例是否构成独立使用方，SHALL 按其演示的是公共 API 的合法可选声明形态还是仅为兼容分支自证来判定。

审计未结 SHALL NOT 先删。仍承担分派职责的声明键 MUST NOT 因「兼容」字样被整体退役。退役与保留两种处置属设计级分叉时，SHALL 上报裁定后再动手，并记明替代方案与未采用理由。

#### Scenario: 生产面已全部迁移而兼容分支仍在

- **WHEN** 全部生产 face 都同时实现了端口声明与 material 声明，兼容派生分支在生产路径永不进入，而公共契约示例仍只声明端口
- **THEN** SHALL 先出三分消费者表（生产 face 逐个带声明位置、契约示例逐个带门约束、测试 face 单列），再裁「把 material 声明升为必选并同批补示例」或「保留兼容读法并落勘误」；两案均 MUST NOT 退役仍用于分派的端口常量（`api/plugin/material.go:25-45`、`api/plugin/phases.go:214`）

#### Scenario: 把测试计为第二使用方

- **WHEN** 审计表以测试 face 的存在论证兼容分支有独立消费者
- **THEN** 该论证 SHALL 判无效；独立性判定只采生产与公共契约示例两类消费面

### Requirement: 判据与完成账 SHALL 双向对账

每条判据与它的测量、锚、反例、执行作业 SHALL 双向对齐：判据的每条子要求 SHALL 有承载叶与可运行的锚；锚 SHALL 由实际执行它的作业运行；反例 SHALL 由判据点名的断言杀死。对账 SHALL 以生产路径与运行产物为据，MUST NOT 以登记表自述、符号名相近或条款措辞推定。

登记表自述「尚未绑定锚」MUST NOT 充当已达成；未达的判据 SHALL 保持未达并登记到叶，MUST NOT 以删除要求、改写判据或放宽阈值换取门绿。绑定作业的判据 SHALL 核该作业是否执行该锚的包与选择器，MUST NOT 只核作业名存在。反例被判为未杀死、或由判据点名之外的断言先响时，该判据 SHALL 记为未证。

#### Scenario: 作业名存在但从不执行该锚

- **WHEN** 一条判据的作业列在一个不运行该锚所在包与选择器的作业上
- **THEN** 该绑定 SHALL 判错；判据须改绑到真正执行该选择器的作业，门不得因作业名存在而放行

#### Scenario: 判据无锚而登记表自述未绑定

- **WHEN** 一条判据的锚为空，登记表注记为「在锚出现前不受守护」
- **THEN** 该判据 SHALL 记为未达并进入待处置清单，MUST NOT 计入已达成

#### Scenario: 反例由别的断言先响

- **WHEN** 一个反例被杀死，但触发失败的是判据未点名的另一条断言
- **THEN** 该判据 SHALL 记为未证；杀死它 SHALL 是判据点名的断言

### Requirement: 计划余项 SHALL 逐条处置并给残留零证

计划中每一条未完成项 SHALL 有明确处置（删除、接线、改写、退役、上报）与它的现状证据；「待外部解锁」的项 SHALL 标为阻塞，MUST NOT 计入完成。删除行 SHALL 同时证明名字同族但归属不同的现行符号仍在位；接线行 SHALL 给出非测试、非编译期断言的生产调用方。测试对某能力的唯一构造 SHALL NOT 被计为该能力已接线。

#### Scenario: 以同族符号连带删除

- **WHEN** 退役一条已死路径时把同名族的现行符号一并删除
- **THEN** 该删除 SHALL 被拒；残留检索须证明现行符号仍在位

#### Scenario: 只有测试构造未接线能力

- **WHEN** 一份处置台账把某能力标为已接线，而它的构造只出现在测试中、装配路径零处
- **THEN** 该行 SHALL 判为未接线，MUST NOT 勾选

#### Scenario: 阻塞项被计为完成

- **WHEN** 一项因外部变更占用而无法处置，却被记为完成
- **THEN** 该记账 SHALL 被拒；阻塞项须单列并注明解锁条件

### Requirement: 交接与归档前验证 SHALL 闭证

跨变更的共用判据 SHALL 有唯一 owner：同一失配 MUST NOT 同时被两处认领，也 MUST NOT 两处都不认。合提的变更 SHALL 各自绑定自己的终态证据，MUST NOT 混用两个时点的结论。

归档 SHALL 在消费完成证据后进行；归档命令或直接移动目录绕过预检时，实际归档文件树变化 SHALL 仍由提交/服务端必需检查核验，无匹配证据的变化不得发布。领域delta SHALL 按能力和Requirement合并并拒绝冲突，主规格 SHALL 保持正式规格结构，不保留ADDED/MODIFIED等delta段；MUST NOT 跳过同步或校验。合并侧的必需检查属外部权限，未配置时 SHALL 如实报告为未生效，MUST NOT 声称已拦截。

#### Scenario: 归档跳过完成判据

- **WHEN** 归档只跳过确认提示，而完成判据的检查未运行或以非零退出
- **THEN** 无有效证据的归档变化 SHALL 在受控入口或实际提交/服务端合并处被拒；直接移动目录也需拒绝，只有归档前自觉运行一次检查不算完成接线

#### Scenario: 同一失配无人认领

- **WHEN** 一处呈现侧失配同时落在两个变更的边界上，两处都不登记归属
- **THEN** 该状态 SHALL 判为交接缺口，须在启动门前定出唯一 owner

#### Scenario: 必需检查未配置

- **WHEN** 合并侧的必需检查尚未配置，而汇报声称已验证拦截
- **THEN** 该汇报 SHALL 判为不实；须报告「未生效」并注明所需授权