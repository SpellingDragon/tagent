# ORCHESTRATION（派发唯一依据）

## 〇 结论

**抵达 DoD 的唯一路径**：W1 红锚 + 01 消费循环 → 02 会话入口 / 03 分段闭合（可并行）→ 04 派生读面 → 05 宿主 → F 收尾；每域独立可回滚，症状锚回滚必红作为回退证据。

**当前最大风险**：消费语义改动跨 `runtime/agent` 与 `assembly` 两条核心路径；若 01 的批次边界定义含混（哪些事实算「已到达」、工具结果何时进入批次），02/03 会建立在错误契约上。缓解：W1 只做 01 + 冻结签名，签名变更按跨域返工处理。

## 一 编排四问

### 1. 次序

- 原子派发单位 = 就绪孙任务集合；准入 = 依赖传递闭包不相交（到文件触碰集）。
- 01 → (02 ∥ 03) → 04 → 05 → F。02 与 03 触碰集不交（`assembly/*` vs `memory/replay/*`），可并行。
- 同文件簇必须合单或串行；`runtime/agent/loop.go` 只允许 01 触碰（02 若需改 loop，须改成冻结接口调用，否则合单）。
- **派发粒度 = 域级**：01–05 共五张派发单，每单一个域；域内孙任务由同一 agent 按 `tasks.md` 顺序连续完成，域未收尾前不向同域插入第二写者。

### 2. 授权（每单九要素）

每张派发单必须写明：目标 / 白名单文件 / 依赖前置 / 验证命令（正锚+负锚+消费者锚）/ 禁止事项 / 预算 / 交付物 / 回滚点 / 汇报格式。

写入白名单（按域）：

| 域 | 白名单 |
|---|---|
| 01 | `runtime/agent/loop.go`、`runtime/agent/dispatch*`、`runtime/agent/loop_test.go` 及新增 `runtime/agent/consumption_*`；`tests/integration/host/*` 与 `tests/integration/execution/*` 的**消费语义锚迁移**（编排者裁定：删 `await` 是本域冻结取向，旧锚按新语义重写，不删断言） |
| 02 | `assembly/host.go`、`assembly/invoke.go`、`assembly/run.go`、`assembly/host_*.go`、对应 tests |
| 03 | `memory/replay/age_*.go`、`memory/replay/projection*.go`、`api/event` 之外的段判据测试 |
| 04 | `features/rl/export_*.go`、`assembly/request.go`（快照面）、`features/rl/capture_*.go` |
| 05 | `examples/wechat-bot/*`、`examples/cli-host/*` |

共享面（`api/event`、`assembly/facts.go`、`design.md`）由编排者单写。本轮起，编排者另单写了 `assembly/{run,host}.go` 的**结果唤回**接线（见台账 W1-1）：这是「结果落链必须有人驱动下一次消费」的最小闭环，属阶段 1 的一部分；任何代理不得再改这三个文件的唤回路径。

### 3. 凭据

- 完成度五查：勾选真实性 / 产物盘点 / 数字溯源 / 遗漏检测 / **生产消费者实测**。
- 每单完成：`git diff --stat` 校对白名单、复跑验证命令、写后 grep 实证。
- 波次收尾派独立验证子代理，对照任务行验收面评估偏差（R30），返工止于波内。
- **零容忍门清偿归属（5.7）**：每单自清白名单内 finding（`comment_policy`、`doc-budget`、`proc-refs`、`package-layout`，具体命令随派发单下发）；共享面（`api/event`、`assembly/facts.go`、`docs/api`）归编排者单写并清偿；编排者终跑门是唯一终态判据——各域绿 ≠ 整体绿。
- **门控三态（R24）**：`event-short`/`event-long` 需 tmux——无 tmux 判 Skip 并在台账记原因；有 tmux 缺凭据判 Fatal；每次运行须报跑过的用例数 > 0（非空转证明）。

### 4. 释放与等待表

| 等待项 | 谁在等 | 释放条件 |
|---|---|---|
| D6 签名冻结 | 01 | 编排者写入 `design.md#D6` 并 commit |
| 01 入 main | 02、03 | W1 门禁全绿 + 独立审者放行 |
| 01+03 入 main | 04 | 两域各自入 main |
| 02 入 main | 05 | W2 门禁全绿 |
| 全部入 main | F | W1–W5 全绿 |

## 二 明细

### 派发单—孙任务矩阵

见各域 `tasks.md`；本文件只登记派发顺序：

| 单 | 域 | 孙任务 | 关键验证 |
|---|---|---|---|
| D1 | 01 | 症状红锚 + 批次取用 | 红锚先红；`-run 'TestSlowToolDoesNotBlockNextConsumption'` |
| D2 | 01 | 工具解耦 + 跨来源合批 | 正锚 + 负锚（同批不重复消费）|
| D3 | 02 | 提交即受理 + 单消费者推进 | 消费者锚：`Host.Invoke` 走正式入口 |
| D4 | 03 | 消费关系落链 + 段判据 | 回滚重建锚 |
| D5 | 04 | rl 导出按消费关系 | 导出样本配对锚 |
| D6 | 04 | 工具/环境/capture 同快照 | 热更窗口内快照一致锚 |
| D7 | 05 | 宿主适配 + 端到端症状锚 | wechat-bot 集成路径 |
| F | — | 收尾 | F1–F5 |

### 合单判据

- 同一文件簇 → 合单。
- 跨域但共享面 → 编排者先落共享面字段，再派各域。
- 任一单需要改 `design.md#D6` 签名 → 停，回到编排者。

### 预算

- 每单不超过 3 个文件簇；超出拆单。
- 每单完成必须能独立跑通其验证命令，不依赖未完成的后续单。

## 三 台账（逐轮回填）

| 轮 | 派发单 | 凭据 | 结果 | 计划回写 |
|---|---|---|---|---|
| — | — | 红锚 `go test ./tests/integration/host -run '^TestSlowToolDoesNotBlockNextConsumption$' -count=1` → EXIT=1（receipt unknown + 5s 无回答） | 启动门：红锚存在且红 ✅ / 四问齐备 ✅ / D6 冻结 ✅；待 W1 派发 | run-notes.md 记红证据 |
| W1-1 | 01 消费语义 + 结果唤回接线（编排者单写） | 新锚 `TestToolResultIsConsumedWithoutNewInput` 红：`go test ./tests/integration/host -run '^TestToolResultIsConsumedWithoutNewInput$' -count=1` → EXIT=1，20s `timed out waiting for the answer of the settled result`；`go build ./...` EXIT=0 | 已实现：`Drive` 初始批次合并、空批守卫、`Run.setResultWake` + `Host.consumeResults`（结果落链唤醒一次消费）；**新锚仍红，唤醒未生效，原因未定位** | 越界登记：改了 `assembly/{run,facts,host}.go` 共享面；新增导出 API 已降为未导出；负锚未落地；本单不得计完成 |
| 待裁定 | — | — | 新锚红的定位方向：①`Run.record` 是否被 send 工具调用；②`evt.Owner` 是否完整地址；③consumer 是否被 Boot 启动并被 Close 正确停止 | — |

## 四 法定停点

连续推进至以下停点必须停并列清单：

1. 资源门缺凭据（如需要真实 provider / 服务器）。
2. 提交或归档需明示。
3. 重大取向裁定（签名变更、范围增减）。
4. 出现 blocked（依赖不可满足）。
5. DoD 达成。
