# tests/ — 跨包集成与端到端测试

本目录只收**跨包黑盒**测试（`package tagent_test`，仅经导出 API）：集成、e2e（真实 LLM）、不变量对账、回归场景。

**单元/白盒测试跟随被测包**（Go 惯例）：机制中间态（压缩滚动合并、任务占坑窗口、settle 时序等）依赖包内私有符号，只能在同包 `_test.go` 中测——不要迁入本目录，也不要为迁移而导出内部符号。

| 测试类型 | 位置 |
|---|---|
| 白盒单元（私有函数/状态机中间态） | 各包内 `*_test.go` |
| 跨包测试基建（fixture/ManualDetector） | `agent/task/fixture.go` |
| 集成/e2e/不变量 | 本目录 |

运行：`go test ./tests/`（e2e 需 `TENCENT_API_KEY`；`-short` 跳过真实 LLM 用例）。
