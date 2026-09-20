## MODIFIED Requirements

### Requirement: 恢复观测覆盖全误差面

重建 MUST 返回并保存 RecoveryResult，包含 mode/status、扫描/投影/截断数、missing_keys、pages_failed、batch_errors、payload_errors、耗时。水合 MUST 按请求 key 与返回 key 对账；底层返回空集合与 error 不得被空库分支吞掉。status=full 仅在所有相关扫描、载荷解析、水合均完整且未截断时成立。

partial/failed SHALL 进入持续可读的 diagnostics，并在首次实际提交模型的请求尾部追加一次简短运行态提示。提示 MUST 在 projection 历史渲染和 live task board 装配后加入，不拼入 runner 的原始输入，不进入 FullEvent、projection 或独立 checkpoint。durable、volatile、one-shot 与热更后首次请求 SHALL 共享相同规则；请求提交前的输入写失败、空批跳过或取消不得提前消费提示。provider 已收到的首次请求即计作提示交付，不宣称模型已理解；正常 full 结果 SHALL 不增加提示 token，也不改写 system、工具声明和历史前缀。

#### Scenario: tail 分页部分失败
- **WHEN** tail 某页查询失败
- **THEN** pages_failed≥1，status=partial/failed，diagnostics 与模型可见提示一致

#### Scenario: 批量读静默漏键
- **WHEN** GetEvents 未返回 error 但返回键集合少于请求集合
- **THEN** missing_keys 列出差集，不能报告 full

#### Scenario: 空结果伴随错误
- **WHEN** 首次扫描即错误且结果为空
- **THEN** status=failed，不记录空事实链正常恢复

#### Scenario: payload 无法解析
- **WHEN** compaction 存在而 payload 无法解析
- **THEN** payload_errors≥1，结果 failed，保留原始数据且对宿主可见

#### Scenario: durable 首次请求可见
- **WHEN** partial 恢复后的 durable 输入已提交并进入实际模型调用
- **THEN** 捕获的请求尾部含一次 recovery notice，输入原文与 projection 不含该提示

#### Scenario: volatile 与 one-shot 不污染历史
- **WHEN** volatile 或 one-shot 输入在 failed 恢复后触发模型请求
- **THEN** 提示仅出现在请求尾部，FullEvent、projection 及后续重放不把它当用户历史

#### Scenario: 请求前失败不提前消费
- **WHEN** 首批因输入写失败、空批跳过或装配前取消而未提交模型
- **THEN** 下次真正提交的模型请求仍携带一次提示，diagnostics 始终保留恢复状态

#### Scenario: 后续请求与正常恢复
- **WHEN** 同一冷启动的提示已提交，或恢复 status=full
- **THEN** 后续请求不重复提示；正常恢复无提示，原 system、历史与工具声明保持不变
