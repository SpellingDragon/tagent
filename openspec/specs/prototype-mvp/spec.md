# prototype-mvp Specification

## Purpose
TBD - created by archiving change make-prototype-runnable-mvp. Update Purpose after archive.
## Requirements
### Requirement: 可运行的事件循环与协作式退出

prototype SHALL 通过同一 BaseTAgent 事件循环完成输入到最终答复；首条受理事件不得丢失，空闲时等待，不因最终答复或模型中间输出形成自触发循环。发布内部工具、ACK、终态和压缩相关信息 SHALL 不依赖当前消费者先结束才能完成。循环及其模型/工具/Reply 接缝 SHALL 传播 context，并在合作的生产者停止后返回；关闭后输入 SHALL 显式拒绝。

#### Scenario: 首条与连续输入
- **WHEN** 启动原型并输入第一条及随后多条消息
- **THEN** 每条消息进入真实处理路径，模型看到原文，最终答复送达，循环继续等待后续输入

#### Scenario: 超过唤醒容量的内部事件
- **WHEN** 一次模型响应产生多个工具调用或多条事件在消费前发布
- **THEN** 数据不因唤醒 channel 容量丢失，也不在循环内部同步发送处死锁

#### Scenario: 取消与停止后输入
- **WHEN** 取消空闲循环或正在等待可取消 mock 工具/子调用的循环
- **THEN** Run 与其合作生产者退出，重复启动及停止后 Input 返回明确错误，不关闭仍在被发送的数据通道造成 panic

### Requirement: 工具任务、错误与调用归属

任务 SHALL 由所属 agent 的唯一 taskManager 登记；ACK、终态、最终答复 SHALL 有不同语义，ACK 不触发假完成。工具参数和结果 SHALL 原样传递；任务 SHALL 保留发起调用的归属与执行函数，终态只结算一次。未知工具、工具错误、模型错误、空终答和治理拒绝 SHALL 可见且不被报告为成功。

#### Scenario: 真正经过工具的答复
- **WHEN** mock 模型返回具名工具调用及参数，工具产生结果
- **THEN** 真实工具函数收到原参数，模型随后读到对应终态结果并产生最终答复；仅返回固定 mock 字符串的旁路不满足此场景

#### Scenario: 多任务顺序稳定
- **WHEN** 同一模型响应派发两个工具，完成次序被测试屏障反转
- **THEN** 后续模型输入按派发次序装配已收齐的结果，结果不交换归属且没有重复终态

#### Scenario: 父子同构与停止
- **WHEN** 父模型通过注册工具调用子 BaseTAgent，子模型又调用自己的工具
- **THEN** 子任务留在子任务表，子最终结果回到父的原始调用；完成或取消后子循环被回收，不依赖另一套执行引擎

### Requirement: state 的单一运行时权威

state SHALL 持有生效 config、治理记录、任务实例和反思水位；config SHALL 只描述和装配，task_manager SHALL 只推进任务生命周期，memory SHALL 保存历史事实而非第二份当前配置或工作投影。治理规则 SHALL 来自所选配置快照而非独立可变副本。共享数据访问 SHALL 不产生 race，用户函数不在状态锁内执行。

#### Scenario: 拒绝而无副作用
- **WHEN** 已注册工具被当前快照的治理规则拒绝
- **THEN** 工具函数执行次数为零，state 留下一条可查询的拒绝记录，调用方收到拒绝结果

#### Scenario: 状态不随执行配置替换
- **WHEN** 发布新的模型、prompt、工具或压缩参数
- **THEN** 原有 memory、投影内容、任务表和拒绝记录保留，不因装配重新创建一套同名状态

### Requirement: 批次级热更新快照

publish SHALL 先验证并复制候选配置后一次换入；非法候选 SHALL 保持原配置。一次模型步骤及它产出的工具选择和治理判断 SHALL 使用同一快照；已经派发的执行函数不被后续 publish 改道。新的事件处理步骤 SHALL 可见新配置。本能力 SHALL 明确限定为 MVP 批次语义，不冒称生产组织级租约。

#### Scenario: 模型运行期间发布
- **WHEN** G1 模型已开始且被屏障暂停，发布 G2 更换同名工具和治理规则，然后释放 G1
- **THEN** G1 产出的工具调用按 G1 的函数和规则执行；后续独立步骤可使用 G2，状态内容没有被清空

#### Scenario: 非法候选
- **WHEN** 候选含缺失模型、nil 工具或非法压缩参数
- **THEN** publish 返回明确错误，原模型和工具继续可用，不出现部分发布

### Requirement: 事实记录、真实票据和有界投影

进入模型的事实 SHALL 先在 memory 中提交，最终输出 SHALL 记录后再投影与投递。memory SHALL 保留原文、支持并发安全查询，并剥离运行时回调/执行引用。压缩 SHALL 只改投影，且只使用真实系统生成的票据；recall SHALL 对坏键或缺失键如实报告 miss。

#### Scenario: 压缩后召回
- **WHEN** 多轮真实输入触发 mock 压缩，再通过已注册 recall 请求旧事件
- **THEN** 投影规模受配置限制，首尾票据仍指向真实事件，原文表不被截断，召回返回原文

#### Scenario: 正文伪造票据与未知键
- **WHEN** 输入正文包含形似票据的假键，或请求不存在/畸形键
- **THEN** 假键不被当成系统压缩导航锚；召回明确 miss，不伪造内容或接受任意非零键为已提交事实

#### Scenario: 同进程投影恢复
- **WHEN** 对已停止写入的实例获取投影快照，并以相同 memory 构造新实例恢复
- **THEN** 已恢复投影可继续对话与召回；文档不将其表述为跨进程耐久恢复

### Requirement: 手动反思与来源保真

手动反思的 pending/watermark SHALL 由 state 维护；反思输入、派生任务及输出 SHALL 保留 meditation 来源，新颖性检查 SHALL 排除该来源。反思 SHALL 有实际访问观察目标事实的接缝。注入或执行失败 SHALL 释放 pending 而不推进已消费水位。

#### Scenario: 无新事实不反复触发
- **WHEN** 业务新事实触发一次反思并完成，随后重复 tick 或仅产生反思自身事件
- **THEN** 不启动新的反思；新业务事实到达后才可再次触发，pending 阻止重入

#### Scenario: 失败不烧水位
- **WHEN** 反思注入失败或模型返回错误
- **THEN** pending 被释放，watermark 保持原值；下一次有效 tick 可重试相同未处理事实

### Requirement: 每文件边界说明与 MVP README

prototype 的每个 Go 文件 SHALL 在头部用中文说明职责、未实现能力和实际模块接线位置；README SHALL 包含 Mermaid 架构图与请求时序图、模块所有权表、预期特性及已实现/mock/未实现状态、可复跑命令和限制。文档 SHALL 不把固定 mock 答案、内存快照或简单赋值描述成真实资源能力。

#### Scenario: 新读者按文档执行
- **WHEN** 在仓库根执行 README 提供的 demo 与测试命令
- **THEN** 无需凭据或外部服务即可实际运行并退出；文档列出的文件、函数和接线点与实现一致

### Requirement: 可复跑的行为验证

MVP SHALL 包含真实 Run/Input 路径上的确定性测试和 Example，覆盖主流程、错误、取消、热更、父子调用、压缩召回与反思。测试 SHALL 用可控制的同步屏障证明先后关系，不用 sleep 作为正确性条件。所有新增用例 SHALL 经至少十次重复与 race 检查。

#### Scenario: 验证门禁
- **WHEN** 执行 `GOPROXY=off go test ./prototype -count=10`、`GOPROXY=off go test -race ./prototype -count=10` 和 `GOPROXY=off go test ./prototype -run '^Example$' -v`
- **THEN** 命令退出码均为零，确有测试/示例执行，不是 `[no test files]` 或仅编译成功

