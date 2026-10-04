## ADDED Requirements

### Requirement: 重入的执行视图与目标解析同源

存储任务的重入（`relaunch_task` / `resume_task`）MUST 在**解析所选的那张执行视图**上运行：解析出目标 wrapper 后，重入 MUST arms 该 wrapper 所声明的子代绑定（与一次正常 `Call` 同源），MUST NOT 仅携带属主面的租约直连子 agent 运行。

判据：一次重入实际使用的提示词/模型/工具配置 MUST 等于解析那一代所声明的子代配置。由 `ResolveReentryDelegation` 的两条分支各自保证——有发起者时按发起代解析并 arms 发起代声明的子绑定（重入留在自己那一代）；无发起者时按生效面解析并 arms 生效面声明的子绑定（重入到达新发布面）。

声明代不可用（该代已收敛关闭）时，重入 MUST 在产生任何模型调用之前具名拒绝，并释放已取的属主租约，不留悬挂引用。

#### Scenario: 无发起者的重入落到新发布面

- **WHEN** 子 agent 任务被派生后，新一代修改了该目标（仍被路由），随后在无在途发起调用的上下文里 relaunch
- **THEN** 该次重入 MUST 由新面的子 agent 服务（产物携带新代标识），MUST NOT 回落到出生代配置
- **AND** 断言的满足 MUST 只能由重入本身产生：不存在与重入无关的告警轮替它达标

#### Scenario: 有发起者的重入留在发起代

- **WHEN** 一次 relaunch 骑在仍持租约的在途发起调用上，而新一代已停止路由该目标
- **THEN** 重入 MUST 按发起代解析并 arms 发起代声明的子绑定，MUST NOT 改投当前生效代

#### Scenario: 目标声明代已关闭时具名拒绝

- **WHEN** 重入解析到的 wrapper 其声明代已收敛关闭
- **THEN** 重入 MUST 在启动任何模型调用之前返回具名错误，且属主租约 MUST 被释放（义务计数可归零，退役排空不挂死）
