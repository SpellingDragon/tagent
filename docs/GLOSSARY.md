# 术语表（GLOSSARY）

一句话：本页把仓库里的机制名翻成通用词，每行"一句话定义＋领域类比＋在哪看"，读完一页即可读文档与代码注释。**术语不改名**——它们是包名、常量与契约的一部分；改的只是第一次读到它的门槛。表里每行的位置都可 `ls`/`grep` 到。

## 一、机制名

| 术语 | 一句话定义 | 领域类比 | 在哪看 |
|---|---|---|---|
| 冥想（meditation） | 一个按节奏自己跑的后台回合：读已提交的事实、让模型反思，再产出一条新输入把主循环往前推；同时给上下文卡片定去留、超预算时浓缩。全程不训练模型。 | 值夜班的同事：趁没人找时理清账本，留一张"下一步做什么"的便条在你桌上。 | `features/evolution/meditation/`（自管谱系触发源，不登记事实族；族 7 是压缩请求） |
| 转世（reincarnation） | 进程被自己换掉（重启／换装）后，新进程启动时把上一进程的现场组织成一条消息注入自己的事件流；整个进程生命周期只投一次，任何一步失败只记日志、不阻断启动。 | 换班交接单：接班的人第一眼看到"上一班做到哪、断在哪"。 | `examples/wechat-bot/wechat_notice.go`（`maybeInjectReincarnationNotice`） |
| 结算（settle） | 任务进入终态时在通知义务上盖的回执：一次结算一份回执，重启后凭账本恢复未消费的结算义务。回放呈现不再落账——视图属于那一次装配。 | 值班交接：交完班签一次字，签过的不签第二遍。 | `assembly/settle_turn.go`；`runtime/state` mailbox |
| 血统（lineage） | 一轮对话的触发来源戳（用户、冥想、http、回执…），按回合盖一次、回合进行中不改；投递侧据此决定这轮产出能不能直接送到用户。 | 快递面单上的寄件人：收件规则看寄件人，不看包裹里装了什么。 | `api/event/meta.go`（`trigger_source`）；`features/evolution/meditation/judgment.go`（`DeliverableLineage` 白名单）；`cli/host_face.go`（`DeliveryReceipt`） |
| 义务（obligation） | 任务尚未兑现的承诺，按"在等什么"分类（等工具、等投递、已移交），带归属方与推进方；队列空不等于义务已清。 | 欠条：写下才算数，划掉要有回执。 | `runtime/task/registry.go`（`Obligations`）；`memory/query/reading.go`（`TaskReading.Obligations`） |
| 退位（retire） | 一个部件或条目不再担任主职责，但记录保留、不删除也不改写：观测记录退位为只读读数（`SummaryPlugin` 已不存在）、压缩请求被消费时 retire 一次、模型版本被换下后标记 retired 但仍服务已持有的流。 | 退居二线：岗位交出去，档案留在册。 | [memory-curation 契约](../openspec/specs/memory-curation/spec.md)；`api/event/compress_request.go`；`features/rl/replacement.go` |
| 事件门（event integrity gate） | 每 MR 必跑的事件消费完整性三档：short 十次独立进程、long 六个 (seed,window) 组合各一次、变异门按冻结清单逐个具名杀死（未杀死的算 not_killed 直接红）；required 只汇总三档，不替代旧检查。 | 出厂全检：三道不同工位都过才放行，少一道不许出厂。 | `scripts/preflight.sh`；[OPERATION](OPERATION.md#event-gates) |
| 委派（delegation） | 父代理调用委托工具这一个动作：框架原子地投一条任务事件（正文=任务、属性=岗位说明与血统）和一批带着源键血统的材料事实进子代理的库，点名唤醒。失败即委派整体失败；子代理的消费循环零特殊逻辑，材料先到先消费。父子只是两段链视图间的引用关系——代理同构，无子角色。 | 把一件事连同它的背景档案一起移交：交接单一张，背景各自归档，收件人照常开工。 | `runtime/agent/loop.go`（`delegatedInputOf`）；`tool/subagent/tool.go` |
| 配置变更（config change） | 一次发布对某代理某维度的判定事实（model/prompt/binding/file/org.*），是控制面的唯一变更流水。三层语义：**源文件是编辑界面**（单向流入，只声明哪些维度可被外部控制）；**链上 `config.declaration` 声明事实是配置真相**（身份=规范指纹、正文=规范声明序列化，可回读）；**唯一发布入口**是生效路径。没有第四条路（内存台账、裸文件读、旁路 setter 均不生效），请求边界从事实投影、在途请求保持旧视图；每次发布尝试另落一条带 result 的发布事实（`published`/`unchanged`/`load-failed`/`governance-rejected`/`install-failed`），回执读账如实、安装失败不回成功。 | 合同正本存档案室（链上声明），桌上的草稿只是编辑界面；每次变更走同一台盖章机（唯一入口），回执以档案室登记为准，草稿在客厅改得再像也不作数。 | `api/config/fact.go`；`assembly/commit.go`（配置链与发布事实）；`openspec/specs/config-hot-reload` |
| 消费循环（consumption loop） | 一个 `(agent, session)` 地址一条常驻 goroutine：拉取已到达的事件、按类型分派、有驱动事件时调一次模型，然后回到拉取，不等派发的工具返回；入口只提交事实并唤醒它。实例按地址装配（`Consumers.Ensure`），入口 agent 之外也挂（委派的会址也有自己的一个），一个地址绝不跑两个驱动者。 | 值班员守着收件箱：来一件处理一件，不站在传送带前等某一台机器。 | `runtime/agent/consumer.go`；`assembly/host.go`（`invokeConsumed`） |
| 链权威答案（answer on the chain） | 答案的事实形态：`agent_output` 携带 `input_events`（本段消费了哪些输入）与终态元数据（terminal/failure/final_round/trigger/decision_count）。进程内缓存只是链的读透视图——淘汰后按输入键从链重建，不可重建的答案豁免淘汰。 | 账本是正本，白板只是抄写板：抄写擦了就重抄，正本永远在。 | `runtime/agent/loop.go`（`commitOutput`）；`runtime/agent/consumer.go`（`answerOfChain`） |
| 失败观察（failure observation） | 失败终态落成链上事实（正文=原始失败原因，meta=终态/任务/调用身份），并按唤醒法则驱动目标地址的后续消费——失败可见、可续、不无限重驱。 | 坏消息也记在案并递到该看的人手里，而不是塞抽屉里反复重试。 | `assembly/host.go`（`noteFailureObservation`）；`runtime/agent/delegate.go`（`DrainReport.Failed`） |
| 段（segment） | 消费循环的一次连续处理：从驱动事件开段（范围身份 `<session>#cN`），跨工具结果与新输入续轮，到模型无工具调用的最终回复落 `agent_output` 时闭合；它就是投影折叠与 turn 的单位。 | 一张工单：从受理到结论，中途补材料不换单，结案才归档。 | `runtime/agent/consumer.go`（`openSegment`/`closeSegment`）；`assembly/facts.go`（`recordMapped`） |
| 投递账目（delivery ledger） | 进程内投递的回读面：按源事件、目标与投递身份列出本 run 提交过的投递事实及回执；两族投递（agent 主动发送 / 框架结算通知）共用一套五键，所以不按来路分两种读法；查询不解析正文。 | 快递底单存根：谁寄的、寄给谁、对方签收没，一查就知道，不用重读包裹。 | `assembly/host_tools.go`（`DeliveryLedger`／`Records`／`BySource`／`ByTarget`）；`api/event/meta.go`（五个 `delivery.*` 键） |
| 风暴熔断（storm fuse） | 一个模型响应发出过多调用、或请求提示已过长时，整批调用不截断也不降级，而是停成一条人审请求（摘要只列工具与参数指纹）；等待发生在原本要派发它的那次派发内，批准=整批按序执行、拒绝/超期/取消=零执行，没有审批簿就整批拒绝。 | 关口安检：一件行李也不拆，整车排队等签字；没盖章的整车原路退回。 | `runtime/agent/fuse.go`（`FuseLimits`/`parkedBatchOf`/`awaitBatch`）；`runtime/task/approval/approval_store.go`（审批簿） |
| 交付义务事实（delivery obligation fact） | 随答案**同一笔**落链的对外发送承诺：**owed 声明+四结算态+迁移专用 owed 态**（held/sent/refused/headless-retired；held 的 reason 含 approval/retrying/retry-exhausted/unknown）都在链上销账；重启照链补送，收方按身份去重。状态变化必达模型（感知是构造保证，投影侧有类型→角色映射），对用户怎么说由模型自主。 | 出库单钉在货物交接单上：单货同笔，货在单必在；缺单的货重启时对账补开。 | `openspec/specs/delivery-closure/spec.md`；`assembly/host_delivery.go`（结算事实读写与旧账迁移） |
| 症状直译锚（symptom anchor） | 把用户原话直接变成的第一行验收测试：修复前必失败、修复后必通过；工程指标全绿替代不了它。 | 病情自述本身就是复查项目：化验单全部正常，不能替病人说"没事"。 | `openspec/specs/symptom-anchors/spec.md`；`TestSymptomAnchor*` 测试家族（assembly／tool/exec／memory/replay） |
| 请求预算（request budget） | 发给模型前对整包内容的总量守恒：固定说明、技能、环境材料、历史与工具说明一起计量；装不下按次序舍可舍的，舍不动就拒发并记账。 | 装箱发货：箱子尺寸固定，先保单据，塞不下抽填充物，实在装不下就拒发并告警。 | `openspec/specs/request-budget-accounting/spec.md`；`assembly/request.go` |
| 记录世代（recorder generation） | 一条事实由哪个进程实例写下的标识：跨重启回放时，读者分辨"这是前任写的"，而不是"这是陈年化石"。 | 交接班记录上的值班员工号。 | 事件信封 `instance_id`；`memory/replay/age_segments.go`（折叠行 `gen:` 前缀） |
| 会话世代（session generation） | 一条命令回执属于终端会话第几任的标识：只有本任的输出与退出码才算数，前任旧屏绝不当成本次结果。 | 客房号不变、每换一任住客就换房卡：旧房卡开不了这次的门。 | `tool/exec`（世代令牌）；`tool/exec/tmux_generation_test.go` |
| 独立验收（independent acceptance） | 训练样本的合格判定只看任务产物本身（文件在不在、内容对不对），不采信执行者自述"我做完了"。 | 阅卷只看答卷，不听考生自我感觉。 | `openspec/changes/archive/2026-11-05-07-training-sample-loop/`；`openspec/specs/offline-training-export/spec.md`（不自动映射奖励的边界） |

## 二、事实族登记表（已登记十族，编号 6 留空）

框架里"值得登记的事实"共十族（1–5、7–11；编号 6「回放结算」已退役留空、不重排）。族名是登记表里的权威名，下面只给一句话读法；身份、类型与锁定机制以 [FACT-VOCABULARY](FACT-VOCABULARY.md) 第二节为准。

| # | 族 | 一句话 | 在哪看 |
|---|---|---|---|
| 1 | 核心事件 | 一条事件本身：存储键即身份，类型随事实冻结。 | `api/event/` |
| 2 | 通知 | 任务对外的一条通知：`TaskID:Round:Kind:Seq` 四元组定身份，重复投递不添第二条。 | `runtime/task/fact.go` |
| 3 | 训练记录 | 一次决策尝试成一条记录：同 ID 幂等，重复派生不改写既有记录。 | `features/rl/` |
| 4 | 声明 | 一份组织声明：规范指纹定身份，同内容重载仍是同一身份。 | `api/config/fact.go` |
| 5 | 引用票据 | 指向原文的票据：同一份原文可有多张票据，共用一个内容键。 | `memory/ledger/reference.go` |
| 7 | 压缩请求 | 一次"该压了"的判定：同判定同身份，消费后单次退位。 | `api/event/compress_request.go` |
| 8 | 压缩产物 | 一条压缩请求至多一条的产物：同身份重试不分叉，键与正文是投影恢复的唯一来源。 | `api/event/compress_product.go` |
| 9 | 收尾判定 | 一次"该收尾了"的判定：同一 range 同原因至多一条。 | `api/event/final_round_request.go` |
| 10 | 注意力 | 一条在场问题／回答／清除记录：同键拒二落，answer/clearance 具名拒绝。 | `runtime/task/attention.go` |
| 11 | 交付结算 | 一条对外发送承诺的状态：随答案同笔声明 owed、按身份+状态追加落链，重启照链对账。 | `assembly/host_delivery.go`（`deliverySettlementKey`） |

## 三、读代码前的五个通用词

| 词 | 一句话 | 在哪看 |
|---|---|---|
| 事件 | agent 收到或产出的每条记录（消息、工具调用、告警），带类型进总线。 | `api/event/` |
| 事实 | 有身份、可被引用的产出：产出即锁定，之后只追加。 | `api/event/fact.go` |
| 投影 | 由事实链装配出的有界读视图：只读，不写回事实链。 | `memory/query/` |
| 回合 | 一次从输入到产出终态的完整处理；血统按回合盖。 | `runtime/agent/consumer.go`（常驻消费循环开段/闭段） |
| 装配根 | 把组织声明变成活动运行时的唯一入口。 | `assembly/run.go`（`Assemble`） |

## 四、新增词的判据
机制名就是学习成本。判据一句话：**一句话说不清（且不含本页未出现的词）就不加词，把它写成既有能力的字段**——完整判据见 [CONTRIBUTING](../CONTRIBUTING.md)「七、文档要求」。自检形式：把这句定义念给没参与讨论的人，他能复述，才写进文档。
