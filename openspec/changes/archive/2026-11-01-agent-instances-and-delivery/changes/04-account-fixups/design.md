# 04 技术要点

零行为改动域。要点：

1. **账目修正**：勾选状态差异是流程瑕疵非代码缺陷——修正只动 checkbox 与凭据注记。
2. **注释保真**：`CommitToSession` 现服务多用途（attention 投影、host 交付、send 跨会话、meditation），docstring 按事实陈述用途集合。
3. **注记预留**（D12）：route 端口候选的注记格式="候选端口名 + 一句话职责 + 准入条件 + 不占位声明"；恰六契约的防御价值（`validatePort` 拒绝意外端口）保持。
4. **取舍归档**（D13）：等价性论证结论（判断权=发送方显式寻址+main-sub 编排；框架"不猜地址"红线是反向保护）、暂不触发 5 处局部语义清单、FinalRound 未来插件化→HITL——写入设计文档固定，防止后人重复探讨。

文件清单：`openspec/changes/send-cross-task-session/.../09-.../tasks.md`、`assembly/run.go`（注释）、`api/plugin/material.go`（注释）、本变更 design 附录。
风险：无（零行为）。
