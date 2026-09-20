## MODIFIED Requirements

### Requirement: 验证工具必须对底层失败保真

测试包装器 SHALL 保留底层命令状态与失败输出；编译失败、非法包、超时、普通断言失败 MUST 非零。上游 race 豁免 SHALL 只适用于登记的具体签名、版本和测试，不因栈位于依赖包就一律放行。没有 race 文本 SHALL NOT 被视为测试成功。

测量装饰器 SHALL 保留本次被测路径依赖的底层能力与错误。包装 LocalFileKV 的 countingKV MUST 暴露并实际转发 Sync，不能因只嵌入 KVStore 而跳过事件提交屏障，也不能用空实现伪造能力。编译期能力断言、屏障失败测试及包装后的独立进程读回测试 SHALL 共同约束该行为。

#### Scenario: 非法包
- **WHEN** race wrapper 收到不存在的 Go 包
- **THEN** 返回非零并显示失败，不输出成功 verdict

#### Scenario: race 豁免混合普通失败
- **WHEN** 输出含已知上游 race 和普通断言失败
- **THEN** 总结果失败，不被 race 豁免覆盖

#### Scenario: benchmark 包装器屏障失败
- **WHEN** 被包装后端 Sync 返回错误
- **THEN** FileSegmentStore 的事件提交返回同类错误，不能报告成功或记录有效提交延迟

#### Scenario: 包装器与生产耐久语义一致
- **WHEN** 通过 countingKV 写入单事件后子进程不经 Close 退出
- **THEN** 独立进程能按 key 读回原文和索引，Sync spy 证明走过事件屏障；报告仍区分进程终止与掉电

## ADDED Requirements

### Requirement: 离线性能基线必须使用同语义测量

storage 基准 SHALL 标明事件提交的 Sync/Flush/fsync 语义、源码 commit 与 dirty 状态、环境、配置、请求规模、实际 written、采样和命令。省略生产事件屏障的历史结果 MUST NOT 用作生产耐久吞吐/延迟基线，修复后 SHALL 重新生成数据并说明旧结论适用范围；旧原始记录保留，不伪造历史重测。

基准 SHALL 分别报告 GetEvent 与 QueryEvents 延迟；只采集 Go HeapInuse/Sys 时 MUST 使用准确指标名，不称为 OS RSS。100k 档仅写 20k 时 SHALL 标注实际规模，不外推完整 100k 表现。同语义 20% 回归阈值 SHALL 从修复后的可比较数据建立，不以屏障补齐引入的必要开销冒充同语义回归。压缩和 tokenizer 测量 SHALL 与 storage 独立解释，不推导未经实测的线上收益。

#### Scenario: 历史屏障缺失数据
- **WHEN** 对比修复前跳过事件 Sync 的结果与修复后 fsync-on 数据
- **THEN** 报告标明语义不可直接比较，重建正确基线，不直接套用旧 20% 阈值

#### Scenario: 采样与指标命名
- **WHEN** 请求规模为 100k、实际写入 20k，且只采集 runtime.MemStats
- **THEN** 报告显示 written=20k 与 sampled=true，内存字段明确为 Go 统计，不声称完整 100k 或真实 RSS

### Requirement: 可靠性修复验收到达最终消费者

本变更 SHALL 对来源、提交、确认、重放、计数、资源关闭、恢复提示和屏障包装建立 fail-before/pass-after 或明确不可复现说明。集成测试 MUST 实际启用 durable inbox 与 localfile，经过真实 runner/插件和模拟渠道投递接口，按 accepted ID、消息槽位、EventKey、projection、completion、receipt、实际模型请求和投递决定对账。不得用独立辅助函数测试、写盘成功或配置名替代该链路。

#### Scenario: 两个输入合批及下一批
- **WHEN** A+B 进入一个冻结批次，C 在执行中到达，且测试经历受控进程终止与重启
- **THEN** 终止前同次运行中 A+B 只计一个业务 turn，C 留下一批；重启按接收序重新合并未完成项，每条输入身份与完成证据可对账，不丢失来源或串配事实
- **AND** 不把输入事实幂等当作跨崩溃模型调用 exactly-once 或原批组合永久不变

#### Scenario: 共享关闭与渠道决定
- **WHEN** 一个共享 owner 重复关闭，另一个继续消费带 chat_id 的 durable 输入，随后处理 meditation 派生任务
- **THEN** 存活实例的 backend/engine 正常，用户输出发往预期模拟 chat，内部任务不发往用户

#### Scenario: 验证范围不足
- **WHEN** 仅完成本地短程/编译检查，没有完整基准、长跑、真实模型或部署数据
- **THEN** 分项标注已验与待验，不把提案 apply-ready 或测试编译通过当作可靠性发布完成
