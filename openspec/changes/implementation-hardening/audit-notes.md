# 实施期审计笔记（audit-notes）

> WP1.5 模式级审计（2026-09-14）——清单将并入 LEDGER（任务 8.5）。

## close( 通道裸调点清点（生产码）

| 点 | 定性 | 依据 |
|----|------|------|
| lifecycle.go:180 `close(ta.outputCh)` | **🔴 V15 活裂缝** → 1.6 修复 | StartLoop 复用成员不重建：重启后消费者读已关通道；二次 Stop 二次 close → panic 逃逸 recover |
| task_manager.go:924 `close(task.watchDone)` | ✅ 已修（1.2） | RestoreTask/Spawn 均在创建点初始化；Resume 换代判定 nil 感知 |
| task_manager.go:85 | ✅ 安全 | `ch := make(...)` 局部 |
| tool_agent.go:738/750 | ✅ 安全 | wrapped 局部通道 |
| fixture.go:50/51 | ✅ 安全 | 构造器 make + once（测试件） |
| local_file_kv.go:301 / compaction.go:127 / lifecycle.go:101 / tmux_monitor.go:218 / trajectory_recorder.go:169 | ✅ 安全 | 同构模式：构造器 make + closed/running Swap/once 守卫 |
| engine_inmemory.go:165/169 / plan_agent.go:140 / settle.go:200 | ✅ 安全 | 局部或 close 守卫 |

## 接口方法裸调点清点

| 点 | 定性 | 依据 |
|----|------|------|
| task_manager.go Spawn/Resume select `.Detached()`、watch 启动 | ✅ 已修（1.3） | nil 通道变量式守卫；nil-interface vs nil-channel 两态已注释 |
| action_tool.go:415 `.Settled()` | ✅ 安全 | 自建具体 detector（会话绑定，非 nil） |
| engine_bridge.go:173 `b.engine.Close()` | ✅ 安全 | `if b.engine != nil` 守卫在 |

## 时序 flaky 发现（race 同族待深钻）

- 2026-09-14：`go test ./agent/task/` 首跑 FAIL、复跑 3/3 绿（新测 TestResume_RestoredTaskNilWatchDone 在列亦过）——task 包存在时序敏感测试，未定位到具体测试名；与审计 F-5（agent 包 race flaky）同族。留待 8.1 分类处置时一并检视。

## fsync 开销量测（2.3，2026-09-14，darwin/arm64，benchtime=1000x）

| 模式 | ns/op（KVPut+Sync 每次屏障） |
|------|------------------------------|
| FSyncOn | 5,640,724 ≈ 5.6ms |
| FSyncOff | 811,326 ≈ 0.81ms |

- 屏障路径 on≈7x off——但这是**最坏情形**（每写一次屏障）；生产管线 fsync 发生在批级（flushThreshold=50 或 flushInterval=2s 摊销），按每 turn 数事件的真实速率，摊销影响可忽略。默认开维持不变，数据支持该裁决。
