# Tasks

## 1. fail-before

- [x] 1.1 `GOMAXPROCS=1 go test . -run '^TestOrgDelegation_NestedHopKeepsTheInitiatingGenerationTarget$' -count=25` 三批留痕（当前约 3 红/批），并附修复前提交 `40dd3ad` 的对照数（同为 3/25）证明非本会话引入

## 2. 身份锚定

- [x] 2.1 原设想"record 加发起轮次标识"被证伪并改写：`model.Request`（v1.11.2）不带调用身份，mock 边界无稳定回合 id；改用**答案来源的代际标记**做归属锚（通知回合按契约只能带新代标记），见 design D2
- [x] 2.2 断言改为"B 收到旧代标记答案"的计数增量，基线在 park 成立、发布之前取；同时删去按数组下标取答案与不可表达的 `NotContains(G2)` 聚合断言（§四 允许新代面执行通知回合）
- [x] 2.3 park 谓词改 `parkedNow("SUB-B-PROMPT") >= 1`（原 record 存在性可被前一回合同类记录满足）

## 3. 验证与 CI 固化

- [x] 3.1 `GOMAXPROCS=1 -count=25` 连续三批零红；默认 GOMAXPROCS 与 `-race` 根包绿
- [x] 3.2 lint ok、openspec strict valid、根包 -race 75.5s ok、fail-before/after 与变异检验留痕；push 后由 CI 裁决新步骤
- [x] 3.3 落地：`GOMAXPROCS=1 go test . -short -count=1` 成为 test job 独立步骤；准入证据为整包三次连绿 + 定向 75 次连绿 + 变异检验 15/15 红（见 D4/D5）
