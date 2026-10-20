# Design：化归收敛的三决策

## D1 事实类别=自证契约（不搬包）

- event 包声明 `Fact` 类别接口：`FactKey() event.EventID`＋`FactType() string`（身份与类型两问，正好是"产出即锁定"的最小自证）。
- 七族各自实现（执行裁决：新增 config.Declaration 与 memory.OriginalRef；workspace.Ticket 不存在）（包内加方法，零搬迁）：`event.Event`、`task.Notice`、`rl` 记录族（选 AttemptRecord 为代表？——记录族多，契约以"单条记录"粒度：`rl.AttemptRecord`/`rl.FeedbackRecord`/`rl.RewardRecord` 各实现）、`config.Declaration`、`workspace.Ticket`。
- **被否**：(a) 全部事实类型搬进 event 包——域边界破坏（任务/训练/声明词汇互不依赖）；(b) 注册表（全局登记事实类型）——运行期可变结构无消费者。
- 词汇表 `docs/FACT-VOCABULARY.md`：一行一族（族/身份/类型/归属包/锁定机制），扩展者只读一页。

## D2 消费器=多实例同构（不改 Tick）

- 声明消费器协议（文档级契约＋协议测试）：消费器＝读事实（EventID/Key）→消费（可调用执行器）→产出新事实；不修改事实、不绕过持久。
- 三实例：`Loop.Drive`（回合驱动）、`Reflection.Tick`（时间驱动）、宿主循环（外部驱动，边界）。
- **被否**：Tick 收编为 Loop 相位——Tick 无 Invocation/决策语义，强收编造伪相；保持两个消费器+协议声明=诚实。
- 测试：`^TestConsumersShareTheProtocol$`（三实例各消费一条事实并产出新事实，断言"读→产"闭环与不修改源事实）。

## D3 呈现视图迁 view 包

- 新包 `view`：`Presentation{Fact event.EventID, Gaps []Gap, Views []View}`、`Gap`、`View`（自 plugin/present.go 迁出，类型与语义逐字保留）。
- plugin：`FinalPresenter.Present` 返回 `view.Presentation`；After 的 gap 收集返回 view 类型；停止在 plugin 声明视图类型。
- 引用面：run 族、tests、examples/contract 的 presentation 引用改 `view.`。
- **被否**：(a) 视图放 event 包——违背"事件与投影区分"（用户判据）；(b) 放 query——混读面投影与产出呈现（query=回放时态，view=呈现时态）。
- 依赖边：view→event（只引用事实身份）；plugin→view；run→view。view 无其他依赖（纯词汇）。

## 验收

- 01：五族各实现 Fact；`go test ./event ./task ./rl ./config ./workspace` 绿＋词汇表 doc-refs 绿。
- 02：`^TestConsumersShareTheProtocol$` -count=10 绿＋拓扑文档。
- 03：`go build ./...` 零残留（plugin 内无 Presentation 声明）＋全回归绿。
- 变更级：`bash scripts/lint.sh` exit 0；行为零变化（golden 12 场景全 match）。

## 风险与回退

类型迁移=compile 门兜底（引用未改即红）；每域独立提交粒度；回退=单域还原（view 包删除不影响行为）。