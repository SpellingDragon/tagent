# Tasks

> 执行纪律：探针先行、据实定案；禁改测试期望求绿；禁降 count。工作区文件写入有「报失败实为已写入/延迟回写」前科——**每次编辑后以锚点计数复核**，改完 `gofmt -e` + `go build` 双验。

## 1. 定位（探针，不改生产码）

- [x] 1.1 复现稳定红基线并落档：`go test . -run '^TestRollbackOfHotAddNumericWithInFlightTurn$' -count=12 -v` 记录首个失败轮次号与断言行；同命令跨进程重复 12 次确认全绿（证"进程内累积"性质）—— 验证：两条命令结果差被写入 change 目录 `fail-before.log`
- [x] 1.2 三方探针定案 M1/M2/M3：临时探针在同轮打印「提交点 receipt 值」「`coord` 源解析值（含 ok 标志）」「`ContextCompressor` 外层字段与 `liveNums()` 结果」，比对锁定唯一分叉点；探针删除、结论以 D2b 追加进 design.md —— 验证：`grep -c "M[123]" openspec/changes/fix-owner-apply-read-divergence/design.md` ≥ 1 且 `go build ./...` exit 0（探针已清）

## 2. 修复（按 1.2 结论择一，禁扩面）

- [x] 2.1 若 M1（双真源）—— **N/A：探针 40/40 否证 M1**：`keepRecent`（及同批 budget/threshold）读路径合一到源解析，构造值退为"源缺席兜底"；删除/收口第二写入位（对齐 4.7 未收口项，`resident-readiness-plan` 注记引用之） —— 验证：`go test . -run '^TestRollbackOfHotAddNumericWithInFlightTurn$' -count=12` exit 0
- [x] 2.2 若 M2（源/记录轮转次序）—— **编排者越域实施（方案①）**：把 owner 源与提交点记录收敛为同一视图（一次性闭包捕获或轮转次序修正），补一条次序回归测 —— 验证：同 2.1
- [x] 2.3 若 M3（owner 身份分叉）—— **N/A：40/40 sameInstance=true**：产品侧保证 resident 表与换代后句柄一致（或明确 owner 换代语义并在 2.4 契约测钉住）；禁以"测试重取句柄"绕过 —— 验证：同 2.1
- [x] 2.4 契约测入 CI 门：新增测例钉「applied ⇒ 消费读同值」并在 `-count=12` 下稳定绿（本缺陷永久回归门），spec delta 同步 —— 验证：`go test . -run 'AppliedAndConsumed|ApplyRead' -count=12` exit 0

> **实施 agent 注记（1.2 定案 ⇒ 适用 2.2；2.2 修复位点越界，停手交裁决）**
>
> - **判证结果**：M1 否、M3 否，**M2 家族成立**（读数见 design.md D2b 与 fail-before.log）。
>   精确机理：`reload()`（tagent.go:568 起）把 `lastSeenMtime` 落在**提交之前**，而 §十四
>   （docs/wiki/platform/org-hot-reload.md#trigger-timing）规定该戳语义是「处理完成的标志是记下的
>   mtime」；同进程第二个认领者（实测证明"先落戳且提交在飞"，身份据代码闭合为回合驱动的
>   懒检测 `orgReloader → requestCheck → go reload()`）先落戳并进长临界区，运维同步入口随即在**锁外**按戳判定"已处理"、不取互斥量、不等提交即返回
>   （实测 6–11µs，正常 reload 为 468µs–1.9ms）。读落在这个返回与那次提交之间 ⇒ `OrgKeepRecent()=2`
>   而 receipt 说 7。TTL 轴同窗（`5m` 仍读 `1m0s`）。
> - **2.1（M1）不适用**：40/40 + 12/12 轮 `OrgKeepRecent()` 恒等于 `HotSnapshot().KeepRecentTasks`
>   且提交落地即读到新值 ⇒ 压缩器读侧已合一，无第二权威；notes.md 4.7 所述形状在当前代码上不成立，
>   SeedKeepRecent 的回滚不必重启，也**不得**按"再加一条写入通道"修（D5 否决区）。
> - **2.3（M3）不适用**：40/40 轮捕获句柄与 resident 表实例指针相同，且两者读数逐位一致。
> - **2.2 待裁决的方案（两处次序都在 `tagent.go`，白名单不含该文件；`applyHotAll` 另列禁区）**：
>   1. **首选（最小、恢复成文契约）**：`reload()` 去掉**锁外**的 `if mt == lastSeenMtime { return }`
>      快路径（tagent.go:573–576），让每个进入者先取 `mu`，锁内再按戳判定——认领者整程持 `mu`
>      直至提交完成，故并发同步入口自然满足 §十四「等整次构建与发布完成」；懒检测仍经
>      `requestCheck` 的 `building` 单飞 + goroutine，业务回合不排队（§十四「检测不等待构建」不变）。
>   2. **备选**：把 `lastSeenMtime` 改为**完成时才落**（提交成功/无需变更后），飞行中以独立
>      `inFlightMtime`/单飞标志做合并；语义与 §十四 字面一致，但改动面更大且需处理失败路径的戳复位。
>   3. **同批次要窗（receipt 先于视图轮转，属 `applyHotAll` 禁区，须单独裁定）**：`applyHotAll`
>      在循环内逐 agent 打 `hot params applied` 并装源，而 `appliedView` 要到 `recordHotApply/
>      swap/recordRollback` 才轮转 ⇒ "日志声称 applied"早于提交。若要"日志即提交"，需把该 receipt
>      日志挪到发布之后（仍在同一临界区），或改成提交后统一打。本 agent 未动、也未新增任何 push 写入。
> - **2.4 状态**：契约测已落 `owner_apply_read_divergence_test.go`
>   （`TestApplyReadDivergence_AppliedAndConsumedSameCommit`，用既有测试专用 `orgCommitBarrier`
>   把提交**结构性停在提交点**后观测，非耗时/重试门），未修形状下 12/12 轮确定性判出
>   `keepAtEntryReturn=2`；因 2.2 尚未并入，断言前加了**显式带读数的 Skipf**（不勾 2.4），
>   修复并入后该门自动生效并会在同一位置转红。

### 2.x 实施记录（编排者）

- 2.2 修法：删除 `reload()` **锁外**按戳早退（`tagent.go` 原 569-576），判定只留在锁内。后进者经 `mu` 自然加入在飞的那一次，返回时提交已完成 ⇒ §十四「mtime 落下即完成」恢复为真。方案②（`inFlightMtime` 第二变量）被否：多一份状态即多一份漂移。轮询路径 `requestCheck` 已有自建 `changed` 判定与 `building` 单飞，无新增争用。
- 2.4：修复生效后撤去 Skip 分支，门为硬断言；`-count=12` 与 `-count=40` 均绿，观测恒为 `entryWaited=true keepAtEntryReturn=7 sourceKeep=7 keepAfterCommit=7`。

## 3. 收口

- [x] 3.1 全量本地：根包 `-short`、`-race . ./agent ./rl`、`./scripts/race_check.sh`（CI 同形）、`bash scripts/lint.sh`、`openspec validate --strict` —— 验证：各 exit 0
- [ ] 3.2 push 并确认 dev CI 四 job 全绿（race job 不再偶发红即为收口证据）；随后开 PR 并 main —— 验证：`gh run list --branch dev --limit 1` conclusion=success
- [ ] 3.3 知会远端：此前告知「拉 dev 换装」的指引仍有效，但需补一句 race 门修复已并入、their 自检四连的 `race_check.sh` 应绿 —— 验证：sent 反查 msg_id
- [ ] 3.4 `openspec archive` —— 验证：`openspec list --json` 无该 change
