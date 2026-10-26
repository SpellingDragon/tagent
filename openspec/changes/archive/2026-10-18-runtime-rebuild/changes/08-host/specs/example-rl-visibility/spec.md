# example-rl-visibility delta（08-host）

> 本 delta 只承载**新增条款**；既有 Requirement（HTTPAPI 服务启动、健康检查端点、run.sh 可观测环境默认值）语义不变，**继承**（见 `../proposal.md` §继承）。`TAGENT_HTTP_PORT` 默认 8089、`/healthz` 鉴权不豁免等语义继续适用。

## ADDED Requirements

### Requirement: 宿主诊断可见性接实际开关

宿主 SHALL 通过实际开关（配置项/环境变量）暴露统一运行总图（Query）、反思/自演进状态与训练（RL）诊断的可见面；开启/关闭相关子系统 SHALL 真实反映在可见面上。关闭 OTel 等非 Query 子系统 MUST NOT 使 Query 不可用。诊断可见面 SHALL 复用运行时只读总图，MUST NOT 复制第二份状态。

#### Scenario: 开关驱动可见面

- **WHEN** 宿主以某组开关启动（如启用反射/RL 采集）
- **THEN** 对应诊断可见面出现且反映真实运行态；关闭该开关后可见面对应项消失或标注不可用

#### Scenario: OTel 关闭不影响 Query

- **WHEN** OTel 关闭而宿主运行中
- **THEN** Query 只读总图仍可查询（统计/列表/详情/订阅无缺口）

#### Scenario: 诊断不复制状态

- **WHEN** 审查诊断可见面实现
- **THEN** 其数据来自统一只读投影/事实源，不存在并行可写状态

### Requirement: 宿主生命周期（启动/关闭/重启）可验证

宿主 SHALL 在启动时建立并可被诊断观察，在收到终止信号时走优雅关闭（HTTP `Shutdown`、任务层排空义务），重启后经持久现场延续身份；MUST NOT 在 goroutine 内以无控制永久循环阻塞退出。

#### Scenario: 优雅关闭

- **WHEN** 宿主进程收到 SIGTERM 且仍有重试等待中的 HTTP 循环
- **THEN** 重试循环立即取消、走 `Shutdown` 优雅等待，进程不因 HTTP goroutine 阻塞退出

#### Scenario: 重启连续

- **WHEN** 宿主关闭后重启（同一持久的 agent/session/store）
- **THEN** 身份与现场经持久层延续，可见面如实呈现恢复结果，不凭空补出未持久记录
