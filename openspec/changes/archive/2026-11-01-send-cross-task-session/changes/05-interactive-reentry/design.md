# 05 技术要点（条件波次）

1. **类型分派**：重入通道按 `task` 记录的执行器类型分派——round 型（subagent）走 02 续行；tmux 型走会话注入（Rearm 基线+SendKeys），复用 `task-reentry` 特异出入口条款，不新开注入路径。
2. **TTL**：写入型重入重置 TTL 锚点为 now+ttl（`async-task-lifetime` 既有条款）；只读 peek 不刷新。
3. **降档预案**：tmux 侧接线若涉 detector/watch 复杂交互，本域降为"仅登记"（D3 W4 条件），02 主线不受阻；降档时在一级 tasks 勾"裁剪记录"。

文件清单：`runtime/task`（分派）、`tool/exec`（op=send 共通道确认）。
风险：IsTUI 拒绝与占坑单胜在注入路径上的并发窗口——负锚钉住。
