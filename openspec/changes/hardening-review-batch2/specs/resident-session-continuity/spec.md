# resident-session-continuity Delta

## ADDED Requirements

### Requirement: 长驻会话无年龄死刑
resident/interactive 会话 MUST NOT 仅因自最初 spawn 起超过某时长而被清理；清理判定 MUST 以「无人持有」为必要条件：先对全部候选尝试收养（任务引用或 owner 在册即收养并刷新 last_adopted_at），仅对收养后仍无人持有且超过独立 orphan grace（自最后收养时间起算）的会话执行清理。最初 SpawnedAt 仅作展示。

#### Scenario: 运行多日的正常会话重启
- **WHEN** 一个正常运行超过 24 小时、仍有任务引用的 resident 会话经历进程重启重挂
- **THEN** 该会话 MUST 被收养继续存活，MUST NOT 被当作孤儿清理

### Requirement: 重挂后信号链真实接通
重挂恢复的会话 MUST 接通完整信号消费链：重挂路径创建的 detector MUST 绑定到 TaskManager 对应任务并启动 monitor；跨重启 resume 在会话相同时 MUST 复用现有绑定、会话不同时 MUST 原子替换生产端与消费端。「会话存在 / 看板 running / tracked=true」MUST NOT 单独作为恢复成功证据——验收 MUST 观察到真实 watch/settle 信号到达 TaskManager。

#### Scenario: 重挂后后台任务完成
- **WHEN** 重挂的 resident 会话中恢复的任务随后真实完成并输出
- **THEN** 完成信号 MUST 经绑定 detector 到达 TaskManager 并结算该任务（看板与事件同步终态）
