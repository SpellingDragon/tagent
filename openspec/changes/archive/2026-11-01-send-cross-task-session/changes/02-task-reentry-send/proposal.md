# 02 task-reentry-send：对内重入与 settled 重激活

父变更：send-cross-task-session（依赖：01 参数面常数——接口常数依赖，开发可并行，集成需 01 合并）。

## Why

用户裁定："main agent 重入 task（包括 agent 和交互式工具）"、"settled 可以被重新激活，修改状态"、"仅 settled 可重激活"。现状：subagent task 一轮即 settle 不可再驱动；`resume_task` 是第二通信工具面且其合法源状态含 failed（与裁定冲突）。

## What Changes

- `send{target:{task}, content}` 对 task 注入输入使其继续：存活类 task（轮次不在飞）直接喂输入；**settled task 重激活**（状态 settled→活跃、新 Round 续行、可再次 settle）；failed/cancelled/running/suspect 具名拒绝并引导。
- `resume_task` 模型工具删除归并（工具面消失，语义由 send 承载）；并发重入占坑单胜、陈旧信号不串轮等既有语义保真迁移。
- 重激活留审计事实（settled→reactivated）；新 Round settle 通知为新身份（不与旧 Round 合并），恰好一次驱动发起方。

## 边界与依赖

- **依赖**：01（`target.task` 字段常数、回执格式）；`runtime/task` registry/manager 为实现依赖（本域独占改写）。
- **被依赖方**：04（prompt 改写引用 send 重入）、05（交互式命令重入复用本域通道）、09（消费面）。
- **接口面**：`task` 状态迁移（reactivate）、`Notice` 新 Round 身份语义、`send{task}` 分派入口。
- **禁止事项**：不动 failed/cancelled 语义（拒绝即止）；不改 `Notice` 四段身份结构；不实现 tmux 重入（05 条件域）；不动 `Delegator.Complete` 的受理语义（只扩状态迁移）。
