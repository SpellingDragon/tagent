# Design：包结构重排的四决策

## D1 归位表（含被否）

| 现位置 | 新位置 | 裁决 |
|---|---|---|
| `memory/curation.go`＋compress_request/prefold（认知整理+判定） | `evolution/meditation/`（或 `evolution/` 直下子包——按 import 面定，倾向子包） | 冥想=自进化引擎（非训练自进化），归 evolution 轴 |
| `memory/replay_budget.go`＋`replay_settle.go` | `memory/replay/` | 读出预算=独立语义单元 |
| `memory/search/` | `memory/recall/`（或保持 search 名——**保名**，改动最小；裁决：保名只挪语义归属文档） | 不折腾命名 |
| `memory/reference.go`＋localfile/rustviking＋lifecycle/retention/guard | `memory/ledger/` | 事实存储单元 |
| `workspace/externalize` 的票据面 | 保留在 workspace（外置=副本域的判定，票=产物）；仅文档登记票语义归属 ledger 的 OriginalRef | 不合并（合并=跨域搬动，收益低） |
| `memory/projection.go` | 留 memory（与 search 同层的读出面） | 拆到 query 会引入 memory→query 依赖，不拆 |

**被否**：(a) 全部归位按化归轴重排 16 包——动静过大，用户判据"简单才是美"；(b) 冥想留 memory 改名——放错轴的问题不解决。

## D2 正名与改名

- `curation`→`meditation`（正名恢复，wiki 命名沿革对齐）；`Curate`/`Curation` 类型名随之（或包内保留导出名加注释——**裁决：包名/类型名同步改**，pre-release 无兼容承诺，一次改净）。
- `replay_budget.meditation` 标记集→`consumed`（它本来就不是冥想）；compaction payload `MeditationKeys`→`ConsumedKeys`（golden S08 若引用同步）。
- Summary 语义注释修正（"不是内容总结，是原文/工具行"——wiki 退位语义入代码注释）。

## D3 机制子包三类文件挂法（判据）

```
<机制包>/：
  *event 词汇*   —— 事实/事件类型与身份（Fact 实现、常量）
  *消费接线*     —— 谁驱动（Tick/loop 接入点/协议实现）
  *扩展策略*     —— plugin 形态的可插拔面（策略/Condenser/判定）
  （第四类=业务逻辑 → 属领域包，不属机制包）
```
新包准入三问：业务还是机制？机制挂哪三类文件？事实/消费/扩展各是哪个文件？——写入 CONTRIBUTING。

## D4 不复活清单（登记即止）

mem_spill/error_tracking：wiki 边界句"设计已放弃（2026-10-19）"；snooze/min_source_events：consolidation 现状够用，不补；Summary：只改注释不改名。

## 验收

- 01 归位：包搬迁后全编译＋`grep` 旧路径零残留＋golden 12 场景全 match（行为零变化）＋事实词汇表更新（meditation 正名/consumed）。
- 02 拆包：ledger/recall/replay 子包建立＋引用更新＋architecture/hygiene/gen_godoc 登记同步＋全回归绿。
- 03 判据：CONTRIBUTING 节＋wiki 边界句＋doc-refs/lint 全绿。
- 变更级：行为等价（golden 全 match）＋分包判据自检（机制子包只含三类文件）。

## 风险与回退

类型搬迁=compile 门兜底；每域独立提交；回退=单域还原。