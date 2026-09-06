# turn-tracing Specification (Delta)

## ADDED Requirements

### Requirement: turn 级 root span
事件循环 SHALL 为每个 turn(一次批量拉取→RunFlow→settle/退化重试的完整轮次)开启 root span(名 `tagent.turn`),属性 MUST 包含:批内 EventKey(canonical hex 列表)、trigger_source、agent 名;有会话元数据时 SHALL 附 chat_id/user_id;退化重试 SHALL 以属性标记而非另开 root span。RunFlow 及其下的框架自动 span SHALL 成为该 root 的子树。

#### Scenario: 一个 turn 一棵 trace 树
- **WHEN** OTLP 导出启用且一个用户消息 turn 完成(含 ≥1 次工具调用)
- **THEN** 导出的 trace 以 tagent.turn 为 root,框架 execute_tool/chat span 为其后代,root 属性含该 turn 的 EventKey 与 trigger_source

#### Scenario: 退化重试不分裂 turn
- **WHEN** turn 触发退化重试(无工具调用且空输出)
- **THEN** 仍为同一 root span,degenerate_retry 属性为 true

### Requirement: 异步任务 span link
工具经 TaskSpawner 派生后台任务时 SHALL 捕获当前 span context 随任务透传(task 层不解释,沿用 Origin baggage 模式);任务 settle 触发的新 turn 中,处理该 settle 的 span SHALL 以 span link 指向派生时的 span context(link 而非父子)。

#### Scenario: spawn 与 settle 双向可跳
- **WHEN** 一个 tmux 长任务在 turn A 派生、在 turn B settle
- **THEN** turn B 的处理 span 携带指向 turn A 派生 span 的 link,trace 后端可双向导航

### Requirement: noop 降级零影响
未配置 OTLP 导出(未设 OTEL_EXPORTER_OTLP_ENDPOINT)时,全部 span/metric 操作 MUST 为 noop:零导出、零可观测开销、事件循环/工具执行/轨迹记录行为与无观测时一致。span 埋点 MUST NOT 触碰任何工具的 Declaration。

#### Scenario: 默认态零行为变化
- **WHEN** 未设 OTLP endpoint 运行完整集成测试套件
- **THEN** 全部测试通过且与埋点前行为一致(既有断言零修改)

#### Scenario: 声明区零变化
- **WHEN** 对比埋点前后所有内置工具的 Declaration JSON
- **THEN** 完全一致(prefix-cache 不变量)

### Requirement: 内部路径轻量 span
memory 批量写入、L3 压缩折叠、recall 查询实现层、meditation 轮次 SHALL 各自产生轻量 span;属性仅含非敏感元数据(条数/耗时/分区 id),MUST NOT 包含事件内容或消息文本。

#### Scenario: 内容不入 span
- **WHEN** 导出启用且发生压缩折叠与 recall 查询
- **THEN** 相应 span 属性仅含计数/耗时/分区标识,不含任何事件 Content 片段
