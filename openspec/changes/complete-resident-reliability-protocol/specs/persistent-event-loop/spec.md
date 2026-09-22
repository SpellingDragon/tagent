## MODIFIED Requirements

### Requirement: 批量 drain mailbox

Loop SHALL 阻塞等待首个有效事件，在一次有限 Pull 中按接收序收集并冻结批次；可靠模式每次最多 32 个 envelope，消息槽顺序不可压紧。多个输入合并为一个业务 turn，现有传输/退化重试 SHALL 复用该批次，不增加业务 turn。Loop SHALL 是常驻框架唯一 bus 消费者，BeforeModel MUST NOT 认领执行中新到事件；同步工具结果照常参与当前 ReAct。

原始接收集合 SHALL 不可被过滤覆盖；selected/skipped 及原因须另行记录。mixed meditation 让路且不重新发布；全 skipped 批次无需模型但仍形成逐槽完成证据。确定格式/身份冲突 SHALL 保留隔离原件、报告并取消该 agent 自动消费，不跳过故障后继续执行不完整上下文。

#### Scenario: 单事件 drain
- **WHEN** mailbox 只有一条有效消息
- **THEN** 当前批次只含该消息并触发一个 turn

#### Scenario: 批量 drain 多事件
- **WHEN** msg1、msg2、msg3 在本次有限 Pull 中被选中
- **THEN** 按序合并成一个 turn，尚未进入本批的消息保持 pending

#### Scenario: 等待第一个事件
- **WHEN** mailbox 为空
- **THEN** Pull 阻塞直到有效事件到达或 context 取消

#### Scenario: A+B 执行期间 C 到达
- **WHEN** A+B 已冻结，C 在两次模型调用之间或重试等待期间到达
- **THEN** 本轮所有实际请求只使用当前批次；C 不被中途认领，在下一轮消费
- **AND** volatile/durable 采用相同批次边界

#### Scenario: 过滤不丢确认责任
- **WHEN** 同一或不同 envelope 中同时存在用户输入和 meditation
- **THEN** 原始 claims 完整保留，每个槽有 processed/skipped 处置，过滤项不进入模型投影且不被重新发布

### Requirement: mergeBatch 合并批量消息

Loop SHALL 将冻结的有效消息按序合并为一条 user-role 驱动消息；文本 Content 使用 "\n\n---\n\n" 连接，单条 user 消息保持原内容。可靠输入的完整规范化 Message SHALL 保存在 canonical fact 并由投影供实际请求消费；多模态等非文本有效载荷 MUST NOT 因驱动文本为空被判为无输入。源消息和原始 role SHALL 不被原地修改。

#### Scenario: 多消息合并
- **WHEN** 冻结 system "tmux completed" 与 user "构建结果如何？"
- **THEN** 驱动消息为 user-role，文本为 "tmux completed\n\n---\n\n构建结果如何？"，源 role 不变

#### Scenario: 单消息不合并
- **WHEN** 冻结单条 user "你好"
- **THEN** 内容保持 "你好"，作为一个 turn 输入

#### Scenario: 非文本消息不是空输入
- **WHEN** 合法消息只有图片等非文本载荷
- **THEN** 该消息被选中并保留到实际请求，不记为 skipped empty

### Requirement: 可判定的接收结果

系统 SHALL 提供带 context 和稳定请求身份的当前接收入口，区分 volatile/durable accepted 与错误；随载 HTTP/宿主和内部调用方统一使用当前契约，删除仅用于兼容旧签名的 void 包装，不保留静默失败旧分支。关闭、满额、超时、非法载荷和存储失败 MUST NOT 表示为 accepted；202 只表示接收，不表示处理或送达。

接收结果不确定 SHALL 与确定未发布失败区分：rename 后 dir sync 失败时保留原件/序号/未决容量，暂停该信箱的新接收至核对完成，返回明确不确定错误。分配后的序号 SHALL 永不回退复用；同身份、同载荷重试完成所欠屏障，不得覆盖另一输入。关闭检查与接收发布 MUST 在同一生命周期协调内完成。

#### Scenario: volatile 模式满队列
- **WHEN** 未配置可靠 inbox 且队列满至超时
- **THEN** 返回背压错误并计数，不承诺持久成功

#### Scenario: 接收发布后同步失败
- **WHEN** 文件 rename 成功但目录同步失败
- **THEN** 不报告 durable accepted，不回退序号；保留该项等待核对，后续输入不能覆写其路径

#### Scenario: 接收与关闭并发
- **WHEN** 关闭与发布并发
- **THEN** 该项要么在关闭前完整接收，要么明确拒绝，不出现关闭返回后新增未登记项

### Requirement: 可靠输入全序持久化

显式可靠模式 SHALL 将所有外部输入先写有界 inbox-v2，version=2，完成文件和目录屏障后返回 durable；消费者按单一串行化点分配的序列处理，channel 仅唤醒。默认未确认上限 2560，满额/不可写/初始化失败明确拒绝，不回退 volatile。批量接收 SHALL 为单 envelope，每槽无损保存源 ID/Type/Source/Timestamp、完整 Message、可 JSON 表示的业务 Metadata，原始槽不可压紧。

编码与比较 MUST 保留精确整数身份，不经 float64；JSON 不可编码、必要身份缺失、nil Message、非法状态在接收前拒绝。有效空输入与非法输入 SHALL 区分。运行时 claim 路径/确认状态不经业务 Metadata、Origin、模型或宿主投递字段传播。

#### Scenario: 低负载可靠输入仍可恢复
- **WHEN** 单输入取得 durable receipt 后子进程未 Close 即终止
- **THEN** 新进程恢复完整消息/来源/时间/Metadata，不依赖 channel 满载

#### Scenario: 并发与背压不超车
- **WHEN** 多生产者在积压临界点并发提交
- **THEN** 成功项有全序，超额明确拒绝，新输入不越过旧项

#### Scenario: 来源保真到宿主
- **WHEN** 带 chat_id 与内部任务世系的输入经过接收、重启和模型处理
- **THEN** 模拟渠道目标与扣留决定符合原始来源，不以最近聊天补偿丢失信息

#### Scenario: 大整数相邻身份不被合并
- **WHEN** 准备材料含超过 2^53 且相差 1 的两个合法事件 key
- **THEN** 编解码和幂等比较保持不同身份，同 key 异内容明确冲突

### Requirement: 输入处理确认与幂等

claim SHALL 保留原件。首次处理 MUST 在任何本批事实写入前，为整个冻结批次的各 envelope 耐久保存完整准备材料及 prepared_version=1：固定槽 canonical fact、完整规范化消息/来源/首次归因、预留 receipt key。原始 system role 保留，规范化副本为外部输入。准备已存在时 SHALL 精确校验并复用；缺标记的半成品冻结材料、解码失败、身份/分区/来源冲突不得重新生成身份继续。

所有 selected 输入经显式重放提交成功后 SHALL 才调用模型；成功结果包含实际 canonical fact。I/O 或不确定屏障失败时原批次以 100/200/400ms、400ms 封顶可取消退避，不从下一批取输入，不让插件另写合并事实。取消保留 claim，确定冲突隔离并停止自动消费。

合并输入回显的跳过 SHALL 仅作用于当前 runner 尝试的精确事件身份；核对 request token、根 invocation、agent/session、author/消息后绑定 Event.ID，判定发生在任何持久化前。不允许整 turn user-role 跳过、跨 invocation 继承跳过或依赖晚于插件的会话钩子。未绑定/不匹配的执行凭据 MUST 在实际模型入口阻断。

turn 终态 SHALL 区分 completed、failed 和取消未完成；必须观察响应内错误及重试耗尽，不仅看调用返回 nil。completion_version=1 冻结 request ID、receipt key、时间、归因、完整 receipt 事实及逐槽 processed/skipped（含事实 key 或原因）。系统 SHALL 依次耐久保存 completion、显式提交同一 receipt、RecordReceipt、Ack。任一步失败同进程只重试该阶段；completion 已耐久的重启不得重新执行模型。

删除与目录同步 SHALL 完成后才释放未确认容量及材料保留；unlink 成功而同步失败必须留下独立清理账目，文件不存在的重试仍完成目录屏障。冷启动先同步目录并清点，文件在则核对后清理，不在则无需恢复旧清理账目。nextClaimable 不绕过协议直接删除 receipted 项。

未确认输入事实和 receipt SHALL 受恢复租约保护，保护从已有文件重建、早于遗忘工作启动，清理成功才释放；之后回归原类型 TTL，receipt 为原 30 天年龄窗口。历史去重不新增永久表，超过 30 天客户端重提交不承诺幂等。模型、工具副作用及渠道发送 SHALL 明确非 exactly-once。

#### Scenario: 取出后入库前崩溃
- **WHEN** claim 后、事实提交前进程终止
- **THEN** 恢复原件并耐久准备或复用已有准备身份后重试

#### Scenario: 入库后执行前崩溃
- **WHEN** 部分或全部输入已提交但无 completion
- **THEN** 核对原身份补齐后执行；事实不重复，当前选中输入在实际请求中可见

#### Scenario: receipt 后 ack 失败
- **WHEN** receipt 已耐久但删除或目录同步失败
- **THEN** 同进程及重启只补清理，不重新执行，容量及保护只释放一次

#### Scenario: 旧格式或损坏项
- **WHEN** 当前准备材料损坏/身份错配，或过渡材料不满足当前版本
- **THEN** 正常运行不读取旧协议、不重新盖章；当前格式故障保留报错，不兼容旧运行数据可在明确受管重置流程中丢弃

#### Scenario: A 成功 B 失败但 receipt 可写
- **WHEN** A 提交成功、B 提交失败，回执存储本可成功
- **THEN** 模型调用数为零、completion/receipt/Ack 均未发生；恢复后仅用原身份补齐 B

#### Scenario: 准备屏障失败
- **WHEN** 本批任一 envelope 的准备文件或目录屏障失败
- **THEN** 本批尚不开始事实写入及模型执行，即使其他 envelope 已准备完成

#### Scenario: 精确回显隔离
- **WHEN** 当前合并输入回显、相同内容的后续 user、子调用、assistant/tool 事件依次经过真实插件
- **THEN** 只跳过当前已提交合并输入的精确事件，其他事件正常保存

#### Scenario: 确定失败和关闭取消
- **WHEN** 响应携带模型错误或重试耗尽
- **THEN** 终态为 failed 并保留错误摘要；关闭取消且无终态时不生成 completion

#### Scenario: 结果写失败但模型已结束
- **WHEN** 模型已结束，首次 completion 写入失败
- **THEN** 同进程保存该结果并只重试结果提交；崩溃前尚未形成耐久 completion 才允许重做

#### Scenario: 全 skipped 批次
- **WHEN** 原批次全部槽位按规则被跳过（mixed-batch meditation 让路、present-but-not-selected），无任何提交的输入事实
- **THEN** 不调用模型，仍形成逐槽 skipped（原因属闭合集 `{meditation_yield, not_selected}`）的 completion 并按协议清理
- **AND** 合法空输入不属此列——提交门为其落一条事实并标 processed，绝非 skipped（“empty_input”为死枚举，无任何槽位携带过它，已在实现中删除；resident-review-fixes 5.2 将措辞与实现语义收敛）

## ADDED Requirements

### Requirement: 可靠模式恢复能力与格式准入

可靠模式 SHALL 在接收及遗忘工作启动前递归核验后端及所有包装层的显式重放、材料保留和声明的提交屏障能力；不以外层满足接口代替内层能力。内存后端仅具有进程内事实语义，不得作为跨进程事实恢复验收证据；跨进程验收使用 localfile。localfile 仅为验证本能力的**最小临时后端**（提交屏障 Sync 原子落盘、重启读回），不提供生产级耐久/安全/可维护保障；生产耐久认证推迟至接线 rustviking 等专用存储引擎的后续阶段。不支持能力明确配置失败，未启用可靠模式的自定义后端不被强制扩张主接口。

无准备材料的 pending 属当前合法初始状态；已有冻结材料却不满足当前 prepared_version/必要字段的过渡数据 SHALL 不被自动读取或迁移。用户允许切换时丢弃不兼容旧运行数据，故采用受管目录的一致恢复单元重置，不要求排空旧协议，不维护 v1/spill 迁移/兼容入口。读取错误与当前格式损坏不得当作旧数据自动清空。运行期只使用当前实现和契约。

#### Scenario: 包装器伪装能力
- **WHEN** 外层声明重放而其内层不支持
- **THEN** 构造可靠模式失败，不等待输入处理时才发现，也不退化成普通写入

#### Scenario: 不兼容过渡状态重置后启动
- **WHEN** 明确受管目录中的旧格式或半成品已按授权重置，并以当前格式初始化
- **THEN** 启动只进入当前接收/准备/完成路径，不读取旧状态、不保留兼容分支，不将清除旧输入冒称已经处理

#### Scenario: 当前格式故障不可误清理
- **WHEN** 当前运行写入的准备材料损坏，或目录/存储读取失败
- **THEN** 保留故障依据并报错，不能套用过渡数据清理规则

### Requirement: 启动直接核对未确认完成证据

启动 SHALL 清点原始 outstanding envelope，以各自固定 receipt key 直接核对事实类型、request ID、准备身份、逐槽结果和完整 canonical 载荷，不依赖 projection snapshot/tail 顺带发现。只有 prepared 时继续输入；completion 有而 receipt 缺时只补回执；两者匹配只清理；读取 I/O 阻断，任一矛盾身份或缺 completion 的 receipt 隔离报告。保护登记和核对完成前不得启动可淘汰恢复材料的生产者。

#### Scenario: 回执 key 早于压缩边界
- **WHEN** outstanding receipt 的 key 不在 snapshot/tail 扫描范围
- **THEN** 直接查询仍找到并核对，只补确认，不再次调用模型

#### Scenario: completion 后 receipt 前崩溃
- **WHEN** completion 已耐久但 receipt 未入事实链
- **THEN** 复用冻结的完整 receipt（含原时间与归因）提交再清理，不重新运行

#### Scenario: 清理标记与事实矛盾
- **WHEN** envelope 标为 receipted 却没有匹配 completion/receipt，或查询错误
- **THEN** 保留原件并报错，不按状态字符串删除
