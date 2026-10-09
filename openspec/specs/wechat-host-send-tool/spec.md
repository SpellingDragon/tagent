# wechat-host-send-tool Specification

## Purpose
TBD - created by archiving change host-send-tool. Update Purpose after archive.
## Requirements
### Requirement: send 工具是宿主侧显式送达通道

`send` 工具 SHALL 仅在 agent 于配置中显式声明（ToolRef）时挂载（未声明则模型不可见）；参数 SHALL 仅含 `{content}`——发送通道与目标解析由装配闭包固定（宿主 bot 实例 + meta_chat_id>lastActive 三级规则），MUST NOT 可被 LLM 指定。content SHALL 有 8KiB 上界，超限具名拒绝。一切失败 SHALL 以 `[send_denied] <具名原因>` 结果文本返回且回合继续；成功 SHALL 回执送达目标。发送 SHALL 复用宿主既有唯一通路（typing 清理/长文拆分/附件/失败回执），MUST NOT 新开第二条到 IM 的路径。

#### Scenario: 非用户回合显式送达

- **WHEN** 模型在任意谱系的回合内调用 send 且目标可解析
- **THEN** 内容经既有发送通路送达用户，成功回执目标

#### Scenario: 无可解析目标拒绝

- **WHEN** 目标三级解析全部落空
- **THEN** 回合继续，`[send_denied]` 具名文本返回，无发送发生

#### Scenario: 超上界拒绝

- **WHEN** content 超过 8KiB
- **THEN** 具名拒绝文本返回，无发送发生

### Requirement: 被动通道语义零变化

本工具的引入 MUST NOT 改变谱系白名单驱动的被动投递（user/task/reincarnation/system_alert 的默认送达、housekeeping 谱系的扣留+回执）——两通道并存：被动=通知语义（触发源决定），主动=表达意图（显式声明决定）。

#### Scenario: 被动通道回归

- **WHEN** 未声明 send 工具的既有部署运行
- **THEN** 投递行为与本变更前逐字节一致

#### Scenario: 双通道并存

- **WHEN** task 回合的通知照常被动送达，同进程某 agent 又显式 send
- **THEN** 两者互不干扰，各自走同一发送实现

