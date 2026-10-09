# meditation-agent-partition Specification（delta）

## MODIFIED Requirements

### Requirement: 反思事件注入本 agent 的循环 session

冥想管理器的触发动作 SHALL 恒为向本 agent 常驻循环的 session 注入冥想输入事件（`source=meditation`）——不跨 agent 注入、不依赖投递缝。session 安排约定：业务线 session 沿用宿主路由；独立策展 agent 的反思线使用保留 session 名（推荐 `meditation`（冥想语族一致；与谱系常量 `meditation` 属不同命名空间——session 是循环身份、trigger_source 是事件属性，亲和非冲突）），固定单线（思路连续、增长由自身阈值折叠管理），永不与用户路由 session 撞名。

#### Scenario: 注入目标恒为本循环

- **WHEN** 任一 agent 的反思触发
- **THEN** 冥想事件进入该 agent 自己的 mailbox/session；无跨 agent 写入路径

#### Scenario: 策展线固定 session

- **WHEN** 策展 agent 多次反思
- **THEN** 反思均发生在同一保留 session（如 `meditation`），跨重启经投影重建延续

