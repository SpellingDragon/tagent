# 07 本地门禁全量复跑凭据（2026-11-04）

采集者：逐模块核对会话。基线：dev == origin/dev == 3b727199（+ 12 个未提交的文档修正）。
口径：所有命令在仓库根执行；GOCACHE 指到工作区内 .tmp/gocache-eval 以避开沙箱对 ~/Library/Caches/go-build 的限制。
本文件只登记「本地确实跑过什么、退出码是什么」，不冒充远端 CI 结论。

| 命令 | 退出码 | 结果摘要 |
|---|---|---|
| python3 scripts/gates/check-openspec.sh | 0 | 144 passed / 1 failed（WIP change/post-training-data-loop，不阻断）；all main specs pass validate --strict |
| python3 scripts/gates/check_concept_budget.py --json | 0 | errors: []；domain_surface removed=10 / new=1 / renames=0；64 条 baseline 豁免 + 1 条 contract-surface 债务（tool/subagent/tool.go:ResultDelegation） |
| python3 scripts/gates/check_doc_budget.py | 0 | RESULT: ok；214 篇 / 21786 行 = 基线（零差额） |
| go run ./scripts/codetools doc-refs docs/wiki docs/*.md | 0 | no dangling file citations；2 citation(s) resolve at the commit that introduced them |
| bash scripts/gates/lint.sh --only fmt | 1 | checker selftests 257 tests OK；test_race_check.sh 的 2 个 real-run 例失败于 operation not permitted（沙箱拒绝 ~/Library/Caches/go-build），非文档回归 |
| go build ./...（GOCACHE 指到工作区内） | 0 | 仅一条非致命 warning：模块 stat cache 写到沙箱外被拒 |
| go test ./... -short -count=1 | 1 | 37 包 ok；3 包 FAIL（tests/integration/execution、tests/integration/host、tool/exec），24 个失败子用例全部源自 tmux new-session ... fork failed: Operation not permitted；逐条核对无产品回归断言 |
| git status --porcelain | 0 | 12 个 M（文档/规格/基线）+ 1 个 ignored untracked 目录 |

复现命令：

  python3 scripts/gates/check-openspec.sh
  python3 scripts/gates/check_concept_budget.py --json
  python3 scripts/gates/check_doc_budget.py --json
  go run ./scripts/codetools doc-refs docs/wiki docs/*.md
  GOCACHE="$PWD/.tmp/gocache-eval" go build ./...
  GOCACHE="$PWD/.tmp/gocache-eval" go test ./... -short -count=1

诚实边界：本文件不能证明 r162 所引的远端 run 41772779593；见 09-ci-run-attestation.md。
