# wechat-host-send-tool Specification

## Purpose
send 是模型可见的**唯一通信工具**：跨 task、会话投递一条输入。目标由 `target` 参数显式选择，回执三态，拒绝不中断回合。
## Requirements
### Requirement: 被动通道语义零变化

本工具的引入 MUST NOT 改变谱系白名单驱动的被动投递（user/task/reincarnation/system_alert 的默认送达、housekeeping 谱系的扣留+回执）——两通道并存：被动=通知语义（触发源决定），主动=表达意图（显式声明决定）。

#### Scenario: 被动通道回归

- **WHEN** 未声明 send 工具的既有部署运行
- **THEN** 投递行为 SHALL 与未声明该工具时逐字节一致

#### Scenario: 双通道并存

- **WHEN** task 回合的通知照常被动送达，同进程某 agent 又显式 send
- **THEN** 两者互不干扰，各自走同一发送实现

### Requirement: send 是唯一跨 task、会话通信工具

agent 的模型工具面 SHALL 恰有一个通信工具 `send`；其 Declaration SHALL 只承诺跨 task、会话通信，MUST NOT 承诺送达用户渠道。`deliver_summary` 与 `resume_task` MUST NOT 出现在工具面。

#### Scenario: 工具面唯一

- **WHEN** 读取任一声明 send 的 agent 的模型请求工具面
- **THEN** 恰有一个通信工具 `send`，无 `deliver_summary`、无 `resume_task`

#### Scenario: 声明无出网措辞

- **WHEN** 读取 send 的 Declaration
- **THEN** 描述不含 "user address"/"deliver to the user" 类出网承诺

### Requirement: target 对象二选一

参数 SHALL 为 `{target, content}`；`target` SHALL 为对象二选一：`{"task": "<id>"}` 或 `{"agent": "<id>", "session": "<id>"}`。双目标同现、目标皆缺、agent/session 不成对、未知字段、空 content、content 超 8KiB SHALL 具名拒绝（`[send_denied] <reason>`），MUST NOT 抛错中断回合。

#### Scenario: 双目标同现被拒

- **WHEN** 模型传 `target` 同时含 task 与 agent+session
- **THEN** `[send_denied]` 具名文本返回，无任何投递发生

#### Scenario: 超上界被拒

- **WHEN** content 超 8KiB
- **THEN** 具名拒绝，无发送发生

### Requirement: 回执两态

成功 SHALL 回裸 `ok`（凭据非报告，不含目标身份、无可转述措辞）——且 SHALL 只在正文确已送达后给出：任务形态待该轮首个 commit 落地，会话形态待正文入链；未送达 SHALL 回 `[send_denied] <reason>`；error SHALL 仅保留给参数形状非法（JSON 不可解析）。

#### Scenario: hold 不冒充送达

- **WHEN** 治理规则把一次 send 挂起
- **THEN** 模型读到的是受理回执的 `task.HoldReceipt` 形状，send 方言只答 `ok` 或 `[send_denied]`，送达回执不出现

### Requirement: 出网不由 send 承担

send MUST NOT 经出网投递链（`MessageSender`/hostDeliverFunc）发送任何正文；声明 send 的宿主 MUST NOT 要求 sender 存在；用户会址回合的出网仍由回合正文自动投递承担（既有语义零改动）。

#### Scenario: 无 sender 仍可装配

- **WHEN** 文档声明 send 且宿主注入了 sender 缺省
- **THEN** Boot 成功（旧 `ErrHostSenderMissing` 的 send 分支不再触发）

#### Scenario: send 不出网

- **WHEN** 模型调 send 任一目标
- **THEN** 出网渠道零正文到达
