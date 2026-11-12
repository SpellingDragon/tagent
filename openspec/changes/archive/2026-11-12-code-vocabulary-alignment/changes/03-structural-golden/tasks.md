# Tasks: 03-structural-golden

> verify 锚冻结于一级 `tasks.md`；白名单见一级 `ORCHESTRATION.md` 2.2 D-03。立案即跑（与 02 并发）。

- [x] 03.1 评估 `runtime/task/checkpoint.go` SnapshotCheckpoint(:93)/RestoreCheckpoint(:118) vs 接缝 SnapshotState/RestoreState（判据见本域 design；唯一生产调用方 assembly/task_checkpoint.go:34,43）—— 验证：读码裁定产出（保留理由 or 改名旧→新映射）＋级联调用方全清单 `grep -rn "SnapshotCheckpoint\|RestoreCheckpoint" --include=*.go .`；若改名 `go build ./... && go vet ./... && go test ./runtime/task/ ./assembly/` exit 0；裁定上报编排者
- [x] 03.2 删除 `assembly/checkpoint.go:71` checkpointSliceMailbox 重复常量（逐一核 checkpointSlice* 系列值，只删与 StateSliceMailbox 同值同义者）—— 验证：负锚＝删除前 `grep -rn "checkpointSliceMailbox" --include=*.go .` 实证唯一消费者 assembly/checkpoint_test.go:1151；正锚 `go build ./... && go vet ./... && go test ./assembly/ ./runtime/state/` exit 0；消费者锚＝checkpoint_test.go:1151 改用 StateSliceMailbox 后真跑绿
- [x] 03.3 `golden/runtime/window_bounds.json:218,227` 检查点义「载体」→checkpoint（泛义不动）—— 验证：`python3 scripts/gates/check_window_bounds.py` exit 0；词汇门对该文件绿（02 入 main 后复核）

## 完成定义

常量去重后 build/vet/测试绿＋零生产消费者实证＋golden 门绿＋方法名裁定产出。runtime/task/checkpoint.go 方法名裁定先于 01 注释改名（共享面串行）。