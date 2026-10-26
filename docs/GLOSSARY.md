# 术语表（GLOSSARY）

一句话：本页把仓库里的机制名翻成通用词，每行"一句话定义＋领域类比＋在哪看"，读完一页即可读文档与代码注释。**术语不改名**——它们是包名、常量与契约的一部分；改的只是第一次读到它的门槛。表里每行的位置都可 `ls`/`grep` 到。

## 一、机制名

| 术语 | 一句话定义 | 领域类比 | 在哪看 |
|---|---|---|---|
| 冥想（meditation） | 一个按节奏自己跑的后台回合：读已提交的事实、让模型反思，再产出一条新输入把主循环往前推；同时给上下文卡片定去留、超预算时浓缩。全程不训练模型。 | 值夜班的同事：趁没人找时理清账本，留一张"下一步做什么"的便条在你桌上。 | `evolution/meditation/`；[FACT-VOCABULARY](FACT-VOCABULARY.md) 族 7 |
| 转世（reincarnation） | 进程被自己换掉（重启／换装）后，新进程启动时把上一进程的现场组织成一条消息注入自己的事件流；整个进程生命周期只投一次，任何一步失败只记日志、不阻断启动。 | 换班交接单：接班的人第一眼看到"上一班做到哪、断在哪"。 | `examples/wechat-bot/wechat_notice.go`（`maybeInjectReincarnationNotice`） |
| 结算（settle） | 回放装配为"这一条该怎么呈现"做出的决定，按决定内容取身份留一条事实（`replay.settle`）：同一决定只落一条，预算跳过的条目不结算。任务状态里的 `settled` 是同名另一处用法，指任务已进入终态。 | 报销单：每笔决定留一张有编号的单据，重复提交不再生成第二张。 | `memory/replay/replay_settle.go`；族 6 |
| 血统（lineage） | 一轮对话的触发来源戳（用户、冥想、http、回执…），按回合盖一次、回合进行中不改；投递侧据此决定这轮产出能不能直接送到用户。 | 快递面单上的寄件人：收件规则看寄件人，不看包裹里装了什么。 | `api/event/meta.go`（`trigger_source`）；`evolution/meditation/judgment.go`（`DeliverableLineage` 白名单）；`cli/host_face.go`（`DeliveryReceipt`） |
| 义务（obligation） | 任务尚未兑现的承诺，按"在等什么"分类（等工具、等投递、已移交），带归属方与推进方；队列空不等于义务已清。 | 欠条：写下才算数，划掉要有回执。 | `runtime/task/registry.go`（`Obligations`）；`memory/query/reading.go`（`Reading.Obligations`） |
| 退位（retire） | 一个部件或条目不再担任主职责，但记录保留、不删除也不改写：`SummaryPlugin` 退位为元数据标注、压缩请求被消费时 retire 一次、模型版本被换下后标记 retired 但仍服务已持有的流。 | 退居二线：岗位交出去，档案留在册。 | [memory-curation 契约](../openspec/specs/memory-curation/spec.md)；`api/event/compress_request.go`；`rl/replacement.go` |
| 事件门（event integrity gate） | 每 MR 必跑的事件消费完整性三档：short 十次独立进程、long 六个 (seed,window) 组合各一次、变异门按冻结清单逐个具名杀死（未命中的算 uncovered 直接红）；required 只汇总三档，不替代旧检查。 | 出厂全检：三道不同工位都过才放行，少一道不许出厂。 | `scripts/preflight.sh`；[OPERATION](OPERATION.md#event-gates) |

## 二、事实七族

框架里"值得登记的事实"只有七族。族名是登记表里的权威名，下面只给一句话读法；身份、类型与锁定机制以 [FACT-VOCABULARY](FACT-VOCABULARY.md) 第二节为准。

| # | 族 | 一句话 | 在哪看 |
|---|---|---|---|
| 1 | 核心事件 | 一条事件本身：存储键即身份，类型随事实冻结。 | `api/event/` |
| 2 | 通知 | 任务对外的一条通知：`TaskID:Round:Kind:Seq` 四元组定身份，重复投递不添第二条。 | `runtime/task/fact.go` |
| 3 | 训练记录 | 一次决策尝试成一条记录：同 ID 幂等，重复派生不改写既有记录。 | `rl/` |
| 4 | 声明 | 一份组织声明：规范指纹定身份，同内容重载仍是同一身份。 | `api/config/fact.go` |
| 5 | 引用票据 | 指向原文的票据：同一份原文可有多张票据，共用一个内容键。 | `memory/ledger/reference.go` |
| 6 | 回放结算 | 一次呈现决定的留痕：同决定同身份，预算跳过的条目不落。 | `memory/replay/replay_settle.go` |
| 7 | 压缩请求 | 一次"该压了"的判定：同判定同身份，消费后单次退位。 | `api/event/compress_request.go` |

## 三、读代码前的五个通用词

| 词 | 一句话 | 在哪看 |
|---|---|---|
| 事件 | agent 收到或产出的每条记录（消息、工具调用、告警），带类型进总线。 | `api/event/` |
| 事实 | 有身份、可被引用的产出：产出即锁定，之后只追加。 | `api/event/fact.go` |
| 投影 | 由事实链装配出的有界读视图：只读，不写回事实链。 | `memory/query/` |
| 回合 | 一次从输入到产出终态的完整处理；血统按回合盖。 | `runtime/agent/loop.go`（`Drive`） |
| 装配根 | 把组织声明变成活动运行时的唯一入口。 | `run.go`（`Assemble`） |

## 四、新增词的判据
机制名就是学习成本。判据一句话：**一句话说不清（且不含本页未出现的词）就不加词，把它写成既有能力的字段**——完整判据见 [CONTRIBUTING](../CONTRIBUTING.md)「七、文档要求」。自检形式：把这句定义念给没参与讨论的人，他能复述，才写进文档。
| 终结转轮 | settle-driver：任务终结事实主动驱动随动回合——委派结果不经用户第二句即送达 | host.go SettleDriver |
| 风险规则 | risk_rules：声明式工具风险分级（精确名/前缀→级），critical 自动 Hold 并登记审批簿 | governance.risk_rules |
| 轨迹封印 | trajectory capture：全部模型调用（含委派 worker）入 attempts.jsonl 并 Seal 对账 | rl/capture_wiring |
