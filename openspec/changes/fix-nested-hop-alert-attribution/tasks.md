# Tasks

## 1. fail-before

- [ ] 1.1 `GOMAXPROCS=1 go test . -run '^TestOrgDelegation_NestedHopKeepsTheInitiatingGenerationTarget$' -count=25` 三批留痕（当前约 3 红/批），并附修复前提交 `40dd3ad` 的对照数（同为 3/25）证明非本会话引入

## 2. 身份锚定

- [ ] 2.1 `delegModel` record 增发起轮次标识（offer 参数回带请求指纹或自增 turn id），使"被钉跳的答案"可判定
- [ ] 2.2 断言改按身份取该轮答案；断言前先确认被钉跳答案已出现（不放宽为"任一条含 G1"）
- [ ] 2.3 编排改用 park 直接观测确认被挂起轮在场（与 fix-rollback-test-attribution 同法）

## 3. 验证与 CI 固化

- [ ] 3.1 `GOMAXPROCS=1 -count=25` 连续三批零红；默认 GOMAXPROCS 与 `-race` 根包绿
- [ ] 3.2 lint/openspec strict 绿；push 并确认 CI 四 job 绿
- [ ] 3.3 评估并（若通过）落地：`GOMAXPROCS=1 go test . -short` 进入门禁（此前因 offenders 搁置，现三枚已清）
