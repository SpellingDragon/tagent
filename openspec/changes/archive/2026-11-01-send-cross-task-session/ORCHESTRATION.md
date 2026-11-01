# send 跨 task、会话通信收敛 编排文档

## 一、结论

- **抵达 DoD 的唯一路径**：W0（S2 红→绿最小可证伪路径）→ W1（01 常数冻结 + 06/08 并发）→ W2（02 重激活 + 03 对外）→ W3（07/09）→ W4（04 迁移 + 05 条件 + F1–F4）。
- **当前最大风险**：02 域的 settled 重激活状态机（终态翻转 + 新 Round 通知身份 + 恰好一次驱动）是最大技术未知数，且按 D7 要翻转既有 spec 的 failed 可重入条款——首波探针单先行。

## 二、编排四问

### 2.1 次序（就绪孙任务表）

| 派发单 | 域/孙任务 | 前置 | 状态 | 触碰集 | 单号 |
|---|---|---|---|---|---|
| W0-A | 01/0.x S2 红锚+短测 | 无 | ready | tests/integration/execution, tool/subagent | W0-A |
| W1-A | 01/1.1–1.5 | W0-A | blocked | assembly/host_tools.go, host_document.go, host.go | W1-A |
| W1-B | 06/6.1–6.5 | 无 | ready | examples/wechat-bot/wechat_notice.go, wechat.go(inbox) | W1-B |
| W1-C | 08/8.1–8.3 | 无 | ready | assembly/host_face.go, examples/wechat-bot/restart-*.sh | W1-C |
| W2-A | 02/2.1–2.5 | W1-A（常数冻结可先开发） | blocked | runtime/task/*, runtime/agent/delegate.go, tool/subagent | W2-A |
| W2-B | 03/3.1–3.5 | W1-A | blocked | assembly/host_tools.go(合单 W1-A 串行), features/evolution/meditation | W2-B |
| W3-A | 07/7.1–7.4 | W1-B | blocked | wechat_notice.go(与 W1-B 同簇→串行/合单) | W3-A |
| W3-B | 09/9.1–9.5 | 9.1 实证后展开；02 消费面 | blocked | runtime/agent/loop.go, assembly（消费面） | W3-B |
| W4-A | 04/4.1–4.4 | W2-A, W2-B | blocked | resources/prompts, docs/wiki, examples/wechat-bot tests, CHANGELOG | W4-A |
| W4-B | 05/5.1–5.3（条件） | W2-A | 候选 | tool/exec, task 层 | W4-B |

- **并发准入**：触碰集不相交（实测闭包：`go list -deps ./<pkg>`）；`host_tools.go` 簇（W1-A/W2-B）与 `wechat*.go` 簇（W1-B/W3-A）分别合单或串行。
- **关键路径**（红标）：W0-A → W1-A → W2-A → W4-A → F。
- **本轮做不了**：05 域交互式命令重入（条件解锁）；R7 的 9.2+ 在 9.1 实证为假时全部降为登记。

### 2.2 授权（派发白名单）

- W1-A：`assembly/host_tools.go`、`host_document.go`、`host.go`、`assembly/*_test.go`。01 域独占；02/03 只读其常数。
- W1-B/W3-A：`examples/wechat-bot/wechat_notice.go`、`wechat.go`、`wechat_test.go`（同簇串行）。
- W1-C：`assembly/host_face.go`、`examples/wechat-bot/restart-tagent.sh`、`restart-maintenance.sh`。
- 共享面单写：`event.MetaKey*`（06 域）、`task` 状态迁移（02 域）——编排者或 R22 冻结签名后派发。
- **删除面（D15 清除式归并）**：DEL-1–6 的删除与级联回卷归各域派发单白名单内执行；删除对象以 design D15 表为准，伴生引用降级检查记录随单入台账；残留 grep 门（F1.5）是收尾硬门。
- 禁：git commit/checkout 由编排者执行；过程件落 `/tmp/send-cts-<单号>/`（R26）。

### 2.3 凭据（完成度五查落点）

- 查1 勾选真实性：每叶验证命令复跑（`go test -run '^TestName$' -count=1` 逐字符）。
- 查2 产物盘点：白名单文件 vs `git status`；spec scenarios↔测试映射。
- 查3 数字溯源：passed 数/用例数逐条核对。
- 查4 遗漏：孙任务勾选 vs 域清单。
- 查5 生产消费者实测：每域至少一条经 `assembly.Boot`/真实入口的正锚+负锚；测试手工补接线不算。
- 必亲验数字：S1–S3+N1–N6 红因行、非空转用例数>0、`go build ./...` exit 0。

### 2.4 释放（锁/等待表）

| 锁 | 占用者 | 释放条件 | 等待表（释放即消费） |
|---|---|---|---|
| host_tools.go 簇 | W1-A | 01 合并 | 补派 W2-B 的 send 对外形态接线 |
| wechat*.go 簇 | W1-B | 06 合并 | 补派 W3-A（07 运维通报） |
| task 状态机 | W2-A | 02 合并 | 补派 W3-B 消费面、W4-B（05 条件） |

## 三、明细

- **派发粒度**：孙任务级（R28）；单均 2–5 叶；跨域合单仅在触碰集不相交时。
- **预算**：W0 ≤0.5 天；W1 ≤2 天（3 单并发）；W2 ≤3 天（02 为长跑单，R32 标注墙钟）；W3 ≤2 天；W4 ≤1.5 天。
- **真实路径欠账**：R7（9.1）先实证；R6 五态需脚本级模拟（200/401/403/超时/拒连）。
- **训练类**：无。

## 四、台账

| 轮次 | 五查结论 | 集成验证 | 跨域裁决 | 勾选凭据 | 下轮情报 | 锁释放 |
|---|---|---|---|---|---|---|
| R1（W0-A/W1-C1） | W0-A 5/7 绿（S2 红锚红因命中语义缺失）；W1-C1 8.2 绿（变异双向） | build 绿 | W0-A 2.2/2.6 授权边界停→批准最小扩权；红项归属：host 9 条=1.2 级联、Attention=第三方 | /tmp/send-cts-W0A、W1C1 | host.go/run.go 被第三方工作流+IDE 挂起锁持有 | 补派 W0-A2 |
| R2（W0-A2/W3-C/W4-D） | W0-A2 影子全绿但 IDE 锁阻落盘→编排者 patch 落地；W3-C 判 R7 为假；W4-D 4.1/4.2 绿（180 对保全零漂移） | S2 红→绿双证据入账 | 9.2/9.4 转登记、9.3 抽补强叶；mergeBatch 符号修正 | /tmp/send-cts-W0A2、W3C、W4D | resume_task 从未存在 Go 代码（仅 spec+prompt） | W0 启动门开，全量解锁 |
| R3（W1-A2/W4-E） | W1-A2 5/7（1.3/1.4 二次 IDE 锁）→编排者 patch 落地复验绿；W4-E 4.4+残留清零 | host 回归 50.4s 绿、残留 grep 零 | doc_budget +43 先拒后具名落锁 | /tmp/send-cts-W1A2、W4E | doc_budget 棘轮通道=先拒后具名落锁 | 补派 W2-B/W1-B |
| R4（W2-B/W1-B） | W2-B 5/5（S1/S3 变异红证据）；W1-B 4/5（N1 四窗口绿含咬证） | build+竞态绿 | 6.5 共享面 R22→冻结 InputMeta 签名；execution 10 条慢工具族红=DEL-1 级联（非外部，W1-A2 误判）→4.3 扩围 | /tmp/send-cts-W2B、W1B | 冥想目标会址注入裁决已落地（03 design 补记） | 补派 W1-B2/W4-F |
| R5（W1-B2/W4-F） | W1-B2 6.5 绿（nil 兼容 65.9s）；W4-F 4.3 四子项绿（断言面 183→185 不减） | 全绿 | 逐消息 trigger_source 丢弃策略接受 | /tmp/send-cts-W1B2、W4F | — | 补派 W3-A/W1-C2/W3-D |
| R6（W3-A/W1-C2/W3-D） | 07 4/4（N2/N4 变异红）；08 3/3（D12 路径 A）；9.3b 15/15 | 绿 | notice_crash DEL-4 级联补丁→编排者落；5.1 降档登记（op=send 前提失真） | /tmp/send-cts-W3A、W1C2、W3D | docker-compose healthcheck/wizard 文案登记 | 补派 W5-A |
| R7（W5-A/F 收尾） | 5.2/5.3 绿；F1/F1.5/F2/F3 全过（journey DEL-1 级联→W4-G 迁移后绿） | 全量门禁绿（外部红四组登记） | journey 红归属纠正（HEAD 净树取证） | /tmp/send-cts-W4G、F2 | 九锚双证据齐；停 F4 | F4 待用户明示 |
