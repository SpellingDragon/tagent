# Tasks

> 执行纪律：探针先行、据实定案；禁改测试期望求绿；禁降 count。工作区文件写入有「报失败实为已写入/延迟回写」前科——**每次编辑后以锚点计数复核**，改完 `gofmt -e` + `go build` 双验。

## 1. 定位（探针，不改生产码）

- [ ] 1.1 复现稳定红基线并落档：`go test . -run '^TestRollbackOfHotAddNumericWithInFlightTurn$' -count=12 -v` 记录首个失败轮次号与断言行；同命令跨进程重复 12 次确认全绿（证"进程内累积"性质）—— 验证：两条命令结果差被写入 change 目录 `fail-before.log`
- [ ] 1.2 三方探针定案 M1/M2/M3：临时探针在同轮打印「提交点 receipt 值」「`coord` 源解析值（含 ok 标志）」「`ContextCompressor` 外层字段与 `liveNums()` 结果」，比对锁定唯一分叉点；探针删除、结论以 D2b 追加进 design.md —— 验证：`grep -c "M[123]" openspec/changes/fix-owner-apply-read-divergence/design.md` ≥ 1 且 `go build ./...` exit 0（探针已清）

## 2. 修复（按 1.2 结论择一，禁扩面）

- [ ] 2.1 若 M1（双真源）：`keepRecent`（及同批 budget/threshold）读路径合一到源解析，构造值退为"源缺席兜底"；删除/收口第二写入位（对齐 4.7 未收口项，`resident-readiness-plan` 注记引用之） —— 验证：`go test . -run '^TestRollbackOfHotAddNumericWithInFlightTurn$' -count=12` exit 0
- [ ] 2.2 若 M2（源/记录轮转次序）：把 owner 源与提交点记录收敛为同一视图（一次性闭包捕获或轮转次序修正），补一条次序回归测 —— 验证：同 2.1
- [ ] 2.3 若 M3（owner 身份分叉）：产品侧保证 resident 表与换代后句柄一致（或明确 owner 换代语义并在 2.4 契约测钉住）；禁以"测试重取句柄"绕过 —— 验证：同 2.1
- [ ] 2.4 契约测入 CI 门：新增测例钉「applied ⇒ 消费读同值」并在 `-count=12` 下稳定绿（本缺陷永久回归门），spec delta 同步 —— 验证：`go test . -run 'AppliedAndConsumed|ApplyRead' -count=12` exit 0

## 3. 收口

- [ ] 3.1 全量本地：根包 `-short`、`-race . ./agent ./rl`、`./scripts/race_check.sh`（CI 同形）、`bash scripts/lint.sh`、`openspec validate --strict` —— 验证：各 exit 0
- [ ] 3.2 push 并确认 dev CI 四 job 全绿（race job 不再偶发红即为收口证据）；随后开 PR 并 main —— 验证：`gh run list --branch dev --limit 1` conclusion=success
- [ ] 3.3 知会远端：此前告知「拉 dev 换装」的指引仍有效，但需补一句 race 门修复已并入、their 自检四连的 `race_check.sh` 应绿 —— 验证：sent 反查 msg_id
- [ ] 3.4 `openspec archive` —— 验证：`openspec list --json` 无该 change
