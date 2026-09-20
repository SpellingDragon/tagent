## MODIFIED Requirements

### Requirement: 投影是事实链的纯回放（一等不变量）

投影 SHALL 为事实链的派生映射，正常路径精确、退化恢复最终一致；全文只在事实链，不另存投影 checkpoint。可投影事实提交成功后同点增量追加，失败不得追加；压缩折叠本身作为一等 compaction 事实保存。内部处理回执、任务注册、常驻会话记录及既有快照/内联排除项 SHALL 不作为普通消息投影；正常写、在线 spill 恢复、冷启动使用同一类型/元数据判定，不能只在重启过滤。

恢复/提交下游 SHALL 使用后端返回的 canonical fact 构建引用，不以当前时间、重新生成摘要或原调用对象替代。已有普通历史不因重放重复展开；尚未完成且当前选中的输入，必须在执行前核对并补齐必要引用，即使其 key 早于 snapshot/tail 边界。该补齐只覆盖原始 outstanding 选择集合，不能将所有旧 key 重新追加；批次内重试使用已投影集合，防止压缩后重新插入。本变更不修改普通已完成历史的压缩/回放算法，既有普通 spill 晚于 compaction 补写的最终一致边界继续明确保留。

#### Scenario: 运行期投影随事实链增量回放
- **WHEN** 一条可投影事实经成功提交或必要修复
- **THEN** 同点追加 canonical 引用，未提交失败不能向模型暴露新引用

#### Scenario: 无游离 checkpoint
- **WHEN** 检查重建所需状态
- **THEN** 全文及压缩事实来自事实链，不存在独立投影快照作为第二真源

#### Scenario: 三路径均排除内部回执
- **WHEN** 同一内部 receipt 分别经正常提交、在线 spill 回补、冷启动扫描
- **THEN** 均不进入普通投影、模型历史、召回或 embedding

#### Scenario: 已有事实不等于当前请求已包含
- **WHEN** 当前 outstanding 输入事实已存在，但未被冷启动 snapshot/tail 复原
- **THEN** 执行前核对 canonical 并补齐当前输入引用，实际请求可见，不因 already 分类跳过输入

#### Scenario: 压缩后同批重试
- **WHEN** 当前输入已投影并经历压缩，随后同业务 turn 进行传输重试
- **THEN** 不重插同一输入引用，不复制事实，使用本批已有派生视图

### Requirement: 恢复观测覆盖全误差面

重建 MUST 保存 RecoveryResult，包含 mode/status、扫描/投影/截断数、missing_keys、pages_failed、batch_errors、payload_errors、耗时。水合按请求与返回 key 对账；空结果伴随 error 不得解释为空库。只有完整扫描、解析、水合且无截断时 status=full。partial/failed SHALL 在 diagnostics 持续可查，并仅在首次实际模型调用前追加一次短运行态提示，不成为历史第二真源。

提示消费点 MUST 在历史、压缩、live board 及所有装配回调之后，进入被包装模型调用的边界；不是任一 BeforeModel 回调结束。尚未调用底层的取消/短路/输入提交失败/空批跳过不消费；进入底层后 provider 失败不要求重发，也不证明远端收到或理解。正常 full/空库无提示。消息切片复制追加，system/历史/工具声明原有内容不变，提示为 user-role 临时运行材料。

同一常驻恢复状态在 volatile/durable、one-shot、热更及并发调用下最多消费一次。模型装饰器 SHALL 保留底层 Model/IterModel 的真实能力、Info、流/错误和关闭所有权；惰性 iterator 实际开始迭代前不消费，不能隐藏能力或伪造不支持的接口。

#### Scenario: tail 分页部分失败
- **WHEN** tail 某页查询失败
- **THEN** pages_failed 增加，status=partial/failed，diagnostics 与首次实际请求提示一致

#### Scenario: 批量读静默漏键
- **WHEN** 批量读取不报错但少返回请求 key
- **THEN** missing_keys 列出差集，不报告 full

#### Scenario: 空结果伴随错误
- **WHEN** 首次扫描失败且结果为空
- **THEN** status=failed，不记录正常空库恢复

#### Scenario: payload 无法解析
- **WHEN** compaction 载荷无法解析
- **THEN** payload_errors 增加、状态 failed，原始数据保留且对宿主可见

#### Scenario: 装配后短路
- **WHEN** 请求完成装配但后续回调短路或取消，未调用底层模型
- **THEN** 提示保持待显示，下次真正调用携带；历史和投影均不包含该提示

#### Scenario: 模型收到调用后失败
- **WHEN** 带提示请求已进入被包装模型，模型返回错误
- **THEN** 提示已消费、diagnostics 仍可读，下次调用不重复提示

#### Scenario: iterator 未被消费
- **WHEN** 创建惰性模型 iterator 后未开始迭代即取消
- **THEN** 不消费恢复提示，不把创建 iterator 当作模型已调用

#### Scenario: 热更与并发调用
- **WHEN** 执行器更换且同一恢复状态被多个调用竞争
- **THEN** 常驻提示状态不重置、不重复消费，实际请求最多一次携带，正常调用路径能力不退化
