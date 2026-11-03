# 地址消费与答案归属收敛

父变更：runtime-architecture-convergence。W0-A01 报告已由编排者逐项裁定。

## Why

同一地址仍有 Consumer 与 Loop.Drive 两类生产驱动；进程内答案窗口/TTL 能改变晚到等待者能否取得原答案；失败重试又被永久 read/taken 标记阻断。这些问题来自并行权威，不是缺少新的辅助类型。

## What Changes

- **BREAKING**：Host/send/Run.Invoke/Run.InjectEvent 统一为提交事实、确保并唤醒地址 Consumer、按输入身份等待其答案；删除生产直驱例外。
- send 的成功回执只表示目标事实已持久提交，不等待目标回答。
- WaitAnswer 在事实保留范围内始终能取得输入所属闭合段的结果；答案缓存只允许加速，不得决定可答性。
- 同段合批的并发 Host.Invoke 等待者共享同一个不可变 Turn，撤回“第二个只回收据”的特殊契约。
- segment 保持私有执行态，Turn 保持不可变结果；不新增 Round、AnswerLedger 或第三套执行概念。
- consumer-loop-convergence 成为地址消费唯一现行规范，旧 persistent-event-loop 的冲突条款由 04 域统一退出。

## 边界与依赖

- **依赖**：无前置实现；答案链上关联使用现有 input key、InvocationID、agent_output 元数据。02 域负责通知何时可重试的语义裁定。
- **被依赖方**：02 的消息路径、03 的关闭路径都依赖本域唯一驱动。
- **接口面**：正式 Host.Invoke、模型可见 send、Run.Invoke/InjectEvent、Consumer.WaitAnswer；不新增公共接口。
- **禁止事项**：禁止保留 idle/busy 双驱动分支；禁止用更大缓存或更长 TTL 掩盖问题；禁止把 segment 提升为第二事实账；禁止修改压缩语义。

## 症状直译锚

用户原话“问题越改越多”在本域直译为：一次空闲 send 只能产生目标地址 Consumer 的一个输出，不能因调用时忙闲切换执行框架；同一输入的晚到调用方仍能读到属于它的答案，而不是在缓存淘汰后永久等待。

旧实现预期红，修复后绿；最终还需完整回滚本变更证明症状锚重新变红。
