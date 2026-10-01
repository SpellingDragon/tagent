## MODIFIED Requirements

### Requirement: 验证工具必须对底层失败保真

测试包装器 SHALL 保留真实退出状态及完整失败输出；编译错误、非法包、超时、普通断言失败 MUST 非零。race 豁免仅适用于已登记具体签名/版本/测试，不能因栈在依赖包一律放行，没有 race 文本不等于成功。管道尾部命令成功不得遮蔽测试退出码，失败后单次成功不能直接认定 flaky。

测量包装器 SHALL 以明确底层类型保留被测 Sync 与分区枚举能力、计数及错误，不提供无操作能力回退。测试替身 SHALL 截获实际新旧写入入口，并能分别注入输入、receipt、读取、写入及屏障失败；不能只覆写旧入口而让新重放默默成功。

#### Scenario: 非法包
- **WHEN** wrapper 收到不存在的 Go 包
- **THEN** 非零退出且保留失败，不输出成功 verdict

#### Scenario: race 豁免混合普通失败
- **WHEN** 已知 race 与普通断言失败同时出现
- **THEN** 总结果失败，普通失败不被豁免

#### Scenario: 新重放路径故障注入
- **WHEN** 测试声称输入提交失败而 receipt 可写
- **THEN** spy 证明实际重放入口被拦截、receipt 未提交且模型调用数为零，不接受只拦旧写入的测试

#### Scenario: 测量包装器屏障失败
- **WHEN** 底层 Sync 失败或不支持该能力
- **THEN** 错误透传或构造拒绝，不用 no-op 伪造成功，也不记录成功提交延迟

### Requirement: 证据分层与可复现性

每个工作包 SHALL 记录基线 commit、dirty diff 与未跟踪文件内容摘要、实际模块工具链/环境、配置、命令/退出码、完整日志、跳过与失败原因、源码到单元/组合/消费者测试映射。已有实现 SHALL 分类为已验证/部分/未验证，不因旧计划勾选继承完成。每任务全部验收条件成立才勾选；有失败则保留未决，不通过调整错误契约制造全绿。

真实 LLM、独立进程、掉电、长期运行证据 MUST 分开，不以 mock、优雅 Close、同进程 New 或旧内存/投影代替独立恢复。源码审阅推导的路径必须标待运行复现；工件 apply-ready 不等于实现完成。被 supersede 的计划 SHALL 标为废弃且不可执行，保留原记录，不按成功归档同步主规格。

#### Scenario: 只有 mock 与优雅关闭
- **WHEN** 只有 mock 或同进程 write→Close→New 测试
- **THEN** 报告限定证据，不宣称真实模型质量、掉电或多天稳定性通过

#### Scenario: 旧勾选与新验收不一致
- **WHEN** 旧任务已勾但当前代码缺少完整协议或测试只覆盖局部
- **THEN** 新任务仍未完成，证据说明实际缺口，不继承旧百分比

#### Scenario: 测试未知失败后重跑成功
- **WHEN** 首次失败没有完整原因，后一次成功
- **THEN** 记录未决失败，不宣称已确认 flaky 或全绿

### Requirement: 双模块与真实消费者验收

CI SHALL 分别覆盖根模块及 wechat-bot 独立模块 build/vet/short，并覆盖受影响 memory、agent、plugin、rl、evolution、event、tool 及组合根的定向 race。恢复/任务/热更测试 SHALL 到达实际模型请求、diagnostics、任务结算和宿主投递决定，不以写盘、日志或辅助函数结果为终点。

组合验收 MUST 使用同一实际 agent/context、inbox-v2、localfile、真实 runner/插件及 mock 模型/渠道；使用 channel/failpoint 固定批次时序。崩溃窗口通过独立子进程受控退出而非 Close 模拟，父进程新开存储与空投影。按 accepted ID、源消息槽、canonical key/原文、投影、completion、receipt、实际请求、模拟投递和逻辑计数逐项对账。

#### Scenario: 恢复后实际请求
- **WHEN** 新进程从 localfile 恢复并提交首个模型请求
- **THEN** 捕获完整请求、来源、票据与恢复状态，与死亡前对账

#### Scenario: A+B/C 可重复时序
- **WHEN** 测试屏障证明 A+B 已执行，随后发送 C，并注入一次传输重试
- **THEN** A+B 是一个业务 turn，C 下一 turn，重试不扩批；两种接收模式均覆盖

#### Scenario: 完整崩溃窗口矩阵
- **WHEN** 分别在接收 rename/dirsync、claim、prepare、任意输入事实、模型结束、completion、receipt、unlink/dirsync 处终止子进程
- **THEN** 所有 accepted 项均可解释为待处理/已处理待清理/已清理/隔离，无静默丢失或身份错配；无 durable completion 的外部副作用重复不冒称 exactly-once

#### Scenario: 路由与共享资源组合
- **WHEN** 一个共享 agent 并发关闭，存活 agent 恢复用户聊天输入及内部任务结果
- **THEN** 用户输出到正确模拟目标，内部结果扣留，共享 backend/engine 无提前关闭

## ADDED Requirements

### Requirement: 跨进程验收使用最小 localfile；生产耐久矩阵推迟至专用存储引擎

本变更的 `LocalFileKV`（`type: "localfile"`）仅为验证可靠性协议跨进程语义的**最小临时后端**，不对其做生产级耐久性认证。storage 验收 SHALL 通过包装后单事件不经 Close 退出的独立进程读回及 Sync spy，在 flush-only 语义下验证提交屏障确实原子落盘、新进程可读到原文/索引；不将进程终止称为真实掉电。完整 1k/10k/100k × fsync on/off × 并发矩阵及 fsync-on 模式认证随最小化移出本变更，推迟至接线 rustviking 等专用存储引擎的后续阶段。正确性组合门仍 MUST 全部通过，不以部分 cell 替代。

报告 SHALL 记录源码 commit/dirty 摘要、实际 Go/OS/文件系统/配置、命令与完成状态；Go 内存指标不冒称 OS RSS。历史省略生产屏障的旧基准数据保留但撤销耐久性能解释；压缩/tokenizer 证据独立解释。

#### Scenario: 包装后无 Close 读回
- **WHEN** 单事件写入（经提交屏障 Sync）后子进程直接退出
- **THEN** 父进程独立读回原文/索引，spy 证明 Sync 屏障执行了原子落盘；不宣称掉电耐久

#### Scenario: 采样与指标命名
- **WHEN** 100k 档实际仅写 20k 且只采集 runtime.MemStats
- **THEN** 标 written=20k、sampled=true、Go 内存指标，不虚构 100k 或 OS RSS

#### Scenario: 旧屏障语义不可比
- **WHEN** 新结果与旧无屏障结果对比
- **THEN** 标明不可直接比较并保留旧原始记录，不能套用旧阈值评价新耐久路径
