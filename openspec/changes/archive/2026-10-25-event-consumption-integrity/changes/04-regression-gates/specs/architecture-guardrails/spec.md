## ADDED Requirements

### Requirement: 事件完整性显式测试清单与共用执行引擎

规划/回归资产 SHALL 新增 `golden/runtime/event_integrity_scenarios.json`，以 `schema_version: 2` 声明八类机制（E1—E8）、**全部产品测试及其全部必跑子场景**、模块/包/build tags/race/档位与六组合矩阵。计划回写④后 graph 显式清单 SHALL 为 28 个绿测产品测试 + 1 个诊断测试（00.2）+ 137 个必跑绿具名子场景 + 4 个诊断锚；最终验收共 29 个父测试 / 141 个具名锚（`event-short` 28 父/138 锚，`event-long` 1 父/3 锚）。`subtests[]` SHALL 以 `(parent,name)` 为唯一键，每个键仅登记一条；`roles[]` MAY 同时含多个角色；`tests[].consumer_anchor` MUST 引用同父测试下 roles 含 `consumer` 的已登记行（可兼具 positive 角色，也可为独立消费者场景），MUST NOT 复制 consumer 行。137/141 等计数 SHALL 按唯一键计，MUST NOT 按 positive/negative/consumer 角色引用求和。重复实体注册 MUST 被拒绝；合法多角色引用（如 `positive`+`consumer`）MUST NOT 被当作重复拒绝。00.2 修复前红证 SHALL 单列，修复后四锚 SHALL 纳入最终 short。数量 SHALL 由 graph 清单长度导出，MUST NOT 写死“十个 EI 场景”。`EI01—EI10` SHALL 仅作为 03 的 `whole_chain` 整链组（`aggregate_only: true`），MUST NOT 替代 00/01/02/CLI 的独立测试与负锚，MUST NOT 计入测试/子场景数量。非产品叶（00.1/00.3/00.4、03.10、04.1—04.5、05.1—05.4）SHALL 通过 `non_product_leaves[]` 映射 checker 或文档证据，MUST NOT 被要求提供虚构产品测试。旧 `golden/runtime/scenarios.json` 的 S01—S15 及其 deterministic 归属 MUST 逐字保留，MUST NOT 因新合同改写、删除或重算。

新旧合同 MUST 共用现有 `run_runtime_scenarios.py` 与 `check_runtime_evidence.py` 执行/对账引擎，MUST NOT 新建平行评估系统或第二份合同解析器。新合同 SHALL 支持独立模块、build tags、`-race`、`--repeat N` 独立重复与 `--seed`/`--window` 矩阵；每个派生命令 MUST 固定 `-count=1`，每次独立日志、UUID run-id 与证据，单轮内出现重复 run 仍判失败。runner SHALL 提供 `--test-id <leaf>/<TestName>` 精确子集用于开发/生产验收：合同 `tests[].id` 固定 `leaf/TestName`（父测试级）并与 graph `test_registry[].contract_id` 同形，未登记或传子场景名即用法错误。CI 的 MR 面 MUST 使用完整 scope，MUST NOT 以 `--test-id` 缩小必跑面。证据 SHALL 携带源码树与运行配置指纹（受影响第一方源码、必要未跟踪测试、合成配置/fixture、合同、各模块 `go.mod/go.sum`），MUST NOT 只记 HEAD。skip、缺终局、包级失败、缺场景、截断、零匹配、伪造期望记录，以及把同一执行凭据冒充不同独立轮次/不同 matrix 组合的“证据复用” MUST 判失败。

#### Scenario: 八类机制由必跑子场景覆盖

- **WHEN** 合同检查器读取新合同
- **THEN** E1—E8 每族至少被一个 `mandatory: true` 子场景覆盖；每个产品测试至少 1 个 `positive` 与 1 个 `negative` 角色，且 `consumer_anchor` 指向同父测试下 roles 含 `consumer` 的已登记行（可兼具 positive 角色，也可为独立消费者场景）（不另增重复行）；缺族、某族只被 `whole_chain` 覆盖、缺负锚、空 selector、产品测试缺 consumer anchor 或 tier/scope 不一致均非零退出

#### Scenario: 显式清单不因固定十场景漏项

- **WHEN** 合同与 graph 冻结清单（`test_registry` + `leaves[].verification`）、源码 `TestEventIntegrity*` 父测试/`t.Run` 发现清单及执行日志三方交叉对齐
- **THEN** 28 个绿测、1 个诊断测试、137 个必跑绿子场景与 4 个诊断锚按唯一 `(parent,name)` 键逐项对齐且 141 锚/29 父可导出（不按角色引用求和）；缺一条、改名、改 roles、把 mandatory 降为 false、重复实体注册、增加未登记条目或把 EI 当独立测试计数均失败；合法多角色引用不被拒绝；不另造第二份 `EXPECTED_EVENT_MANIFEST` 元组真源，计数由 graph/发现清单长度导出而非手填常数

#### Scenario: 审查删除登记项被拒

- **WHEN** 有人只从合同删除一个测试/子场景以换取通过
- **THEN** 合同与 graph/源码发现不再对齐，检查器非零；同时删除合同与 graph 条目的 diff 无法由机器判定为任意两处篡改，MUST 由独立 diff 审查拒收，不得以“数量减少后仍自洽”放行，也不得声称机器可防双删

#### Scenario: 旧 15 场景不减少

- **WHEN** 新旧合同同时存在并分别检查
- **THEN** 旧合同仍为 15 项、旧命令形态与 deterministic 行为不变；新合同不得以减少旧项换取通过

#### Scenario: 独立重复与单轮唯一

- **WHEN** `event-short` 以 `--repeat 10` 执行，或 `event-long` 以六组合执行
- **THEN** 产生 10 份/6 份独立日志、UUID run-id 与 `repeat_index`，全部成功才通过；同一日志内重复 run/pass、五组合冒充六组合、9 次冒充 10 次或任何 `-count>1` 均失败

### Requirement: 直接调用归属检查不冒充完整调用图

架构门 SHALL 保留 `golden/runtime/architecture.json` 与 `scripts/check_runtime_architecture.py --implementation` 的包依赖方向判据；事件写入/消费归属 SHALL 以人工冻结的 `event_ownership` 表为输入，由 `scripts/codetools` 使用 Go 标准库（`go list`、`go/parser`、`go/types` 与类型信息）核对冻结所有者的**可解析直接调用点**。允许调用者放行，禁止调用者命中 MUST 非零；owner 无法解析、类型信息缺失、接口/反射/间接调用 MUST 计入 `uncovered` 并如实输出（`file:line` + 调用者 + 原因），MUST NOT 计为已覆盖，MUST NOT 用名字相似或字符串相邻补洞。`uncovered` SHALL 由独立复核记录逐条判定可达性，MUST NOT 静默放行。政策表 MUST NOT 从现存代码自动学习。该检查 MUST NOT 声称构建了完整调用图，也 MUST NOT 声称可静态识别任意“第二状态仓”语义；语义级重复状态判断留给 00.3 与独立评审。该检查 MUST NOT 替代包依赖门；`scripts/lint.sh` SHALL 调用该检查作为正式消费者。

#### Scenario: 合法直接调用通过

- **WHEN** 冻结 owner 的直接调用者均在 `allowed_callers` 内
- **THEN** 归属检查与包边检查均通过，退出码为 0；名字相似但非目标的调用不被误判

#### Scenario: 可解析的禁止直接调用被拒

- **WHEN** 出现对冻结 owner 的、类型信息可解析的 `forbidden_callers` 直接调用
- **THEN** 归属检查非零并具名该调用者与 owner，包边通过不能覆盖失败；检查器不把该调用描述为“第二状态仓”语义结论

#### Scenario: 无法解析的调用如实暴露

- **WHEN** 某调用点的类型信息不可得，或经接口/反射间接调用
- **THEN** 该调用进入 `uncovered` 列表并附 `file:line`、调用者与原因；检查器不得把它计为已验证；冻结 owner 本身无法解析时直接失败；`uncovered` 逐条独立复核，不以“工具没报”冒充覆盖

### Requirement: 故障变异必须由具名业务断言杀死

故障变异门 SHALL 在隔离树或 Go overlay 中恢复**已被 00.3 或后续复现证实**的缺陷形态；五类候选（双路正文、忙时忽略、身份破坏、提前确认、错误路由）MUST NOT 在未证实前启用。变异清单 SHALL 随 `scripts/check_event_integrity_mutations.py` 保存，MUST NOT 依赖进行中 change 目录存活。每个 00.3 `confirmed` 缺陷 MUST 恰有一个 mutation `basis` 指向；00.3 新证实的实际缺陷 MUST 登记，MUST NOT 漏项；未证实候选 SHALL 标为 `not_evidence_backed`，MUST NOT 计入 killed。检查 SHALL 先证明控制树在正式 selector 下全绿，再逐个应用变异并运行同一正式测试。只有进程非零且输出命中预先登记的具名业务断言 `expect_assertion` 时，才可记为 killed；编译失败、超时、零测试命中、panic 但无该断言、测试被改绿 MUST 判该变异验证失败，MUST NOT 计为成功杀死。未知/失效变异、清单与源码锚点失配、控制树不绿 MUST 使总门失败。原始证据 SHALL 记录基线源树摘要、实际补丁/overlay 摘要、实际命令与原始退出码，并写入 `--out` 指定的仓库外临时目录；证据摘要与 `log_ref` MAY 记入根 `ORCHESTRATION.md` 台账。

#### Scenario: 控制树先绿

- **WHEN** 变异检查启动
- **THEN** 未变异的控制树先以正式 selector 跑通；控制树失败即总门失败，不继续用变异结果充数

#### Scenario: 具名断言杀死变异

- **WHEN** 应用一个已被 00.3 证实的变异并运行对应正式测试
- **THEN** 测试非零且输出包含该变异登记的 `expect_assertion`，才记为 killed

#### Scenario: 编译失败或超时不算杀死

- **WHEN** 变异导致编译失败、超时、零测试命中、仅 panic 或测试被改绿
- **THEN** 该变异判“验证失败”，总门非零，不把它报告为成功拦截

#### Scenario: 未证实形态不得凑数

- **WHEN** 00.3 未证实某候选形态，或新证实缺陷没有对应 mutation
- **THEN** 未证实候选不计 killed；新证实缺陷缺映射使自测/总门失败，不为凑五类而虚构变异

### Requirement: 叶—测试—子场景—证据映射与 lint 对象一致

回归门 SHALL 将每个叶任务、spec scenario、Golden 条目、真实 test selector、具名子场景、正式消费者与必跑档位逐项对齐：00—05 共 42 叶 MUST 各自被 `tests[].leaf` 或 `non_product_leaves[].leaf` 恰一次引用；产品叶 MUST 用测试映射，非产品叶 MUST 用 checker 或文档证据映射，不得互相冒充。每个 `tests[].leaf` MUST 存在于活动 `task-graph.json`；每个 `mandatory: true` 子场景 MUST 在所选测试文件中以真实 `t.Run("<name>"` 存在；每个产品测试 MUST 有指向正锚行的 consumer anchor 与非空 selector；`subtests[]` MUST 以 `(parent,name)` 为唯一键，`roles[]` 可多角色且不重复计数；`whole_chain[].test_id` MUST 存在且 `aggregate_only: true`。合同 MUST 与 graph 冻结清单、源码发现清单及执行日志三方对齐，改名、删除、空 selector、缺必跑子场景、缺非产品映射、证据复用均失败。同一 `(package,test)` 可服务多个整链组，但每个 run MUST 有独立 record；同一执行凭据（run_id/日志真实路径/artifact）MUST NOT 冒充不同独立轮次或不同 matrix 组合，同一轮内多个 case MAY 共享同一条日志，但每个 case MUST 按具名 selector 独立核验。`scripts/lint.sh` SHALL 在存在活动 `task-graph.json` 时检查该活动 graph，MUST NOT 固定检查归档 graph；无活动 graph 时 SHALL 明确 skip，MUST NOT 伪造通过。新检查 MUST 保留旧十域/旧 15 场景规则，MUST NOT 全局放宽编号、数量或场景约束。

#### Scenario: 改名、缺叶或缺必跑子场景失败

- **WHEN** 叶被改名/删除，或某测试的 negative 角色、consumer anchor 指向未登记的子场景、非产品叶映射缺失、selector 为空或在测试文件中不存在
- **THEN** 映射门非零并具名缺项，不允许以父测试通过或键名对齐替代

#### Scenario: 证据复用失败

- **WHEN** 同一执行凭据（run-id/日志真实路径/artifact）被用来冒充不同独立轮次或不同 matrix 组合
- **THEN** 证据检查判失败，后补 pass 不能覆盖先前失败；同轮多个 case 合法共享同一日志，但每 case 仍须按具名 selector 独立核验

#### Scenario: bundle 汇总同源

- **WHEN** `check_runtime_evidence.py --bundle` 读取 short/long/mutations 证据
- **THEN** short 恰好 10 份、long 六组合齐全、mutations 恰一份、基线源树摘要一致、同一执行凭据不得冒充不同独立轮次/不同 matrix 组合；缺件、空目录、截断、skip、重复 run 均失败

#### Scenario: lint 检查活动对象

- **WHEN** 仓库存在活动 `openspec/changes/*/task-graph.json`
- **THEN** `lint.sh` 按该活动的字符串 `domain_profile`（当前 `event-integrity`，指向固定六域规则；叶数/范围/阶段门一律读 graph，不在 profile 复制对象状态）检查计划/合同/证据；活动 graph 存在却检查归档 graph 即失败

#### Scenario: 旧规则不被放宽

- **WHEN** 新六域 profile 生效
- **THEN** 旧变更仍按冻结的十域、旧叶编号与阶段门校验；不得为新变更全局放宽或跳过旧负控
