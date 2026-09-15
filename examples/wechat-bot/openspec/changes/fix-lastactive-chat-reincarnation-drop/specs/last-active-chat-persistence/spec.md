## Purpose

保证 task 输出的兜底路由锚点（最近活跃会话 chat_id）在进程重启/热换装（"转世"）后仍然可用，使无 meta_chat_id 的 task 输出在转世后首条也能正确送达用户，而非静默丢弃。

## ADDED Requirements

### Requirement: 锚点持久化写入

系统 SHALL 在 lastActiveChat 锚点更新时（用户消息到达并 Store "latest" -> chatID 处），将 chatID 持久化到运行态目录 `run/` 下的锚点文件（last_active_chat）。写入 MUST 采用原子写（先写临时文件再 rename），避免进程被 kill 时留下半写文件。写入失败 MUST 仅记录 WARN 日志，不得影响主流程或导致进程退出。

#### Scenario: 正常持久化

- **WHEN** 用户消息到达，锚点 Store 为 chatID，且 run/ 目录可写
- **THEN** run/last_active_chat 被原子更新为新 chatID，主流程不受影响

#### Scenario: 持久化失败不致命

- **WHEN** 锚点写入 run/last_active_chat 失败（如磁盘错误、权限问题）
- **THEN** 进程记录 WARN 日志并继续正常运行，消息处理不中断

### Requirement: 启动回种

系统 SHALL 在进程启动时读取 run/last_active_chat，若存在且非空则将其回种到内存中的 lastActiveChat（"latest" -> chatID），使转世后首条无 meta_chat_id 的 task 输出能路由到转世前最后活跃的用户会话。文件不存在或读取失败（损坏/空文件）时 MUST 仅 WARN 并以空锚点继续启动，不得阻断启动。

#### Scenario: 转世后首条 task 输出送达

- **WHEN** 进程换装重启后，某 task 输出到达且其 payload 无 meta_chat_id
- **THEN** 该输出被路由到 run/last_active_chat 记录的最后活跃 chat_id，成功送达用户微信，不出现静默丢弃

#### Scenario: 锚点文件缺失或损坏

- **WHEN** 进程启动时 run/last_active_chat 不存在、为空或内容损坏
- **THEN** 进程记录 WARN（文件不存在时可静默或 DEBUG 级），以空锚点继续启动，不崩溃

### Requirement: 转世验证可观测

部署换装后的新进程 MUST 产出可验证的回种日志行（seed 日志），证明锚点已从磁盘回种。该日志行 SHALL 包含回种的 chat_id（或明确的"已回种"语义），作为部署后验证依据。

#### Scenario: 部署后验证回种成功

- **WHEN** 自杀换装部署完成，检查新进程日志
- **THEN** 能找到 seed/回种相关日志行，且销假报告成功送达用户微信
