# Tasks

## 1. fail-before（先证门能抓到现状）

- [ ] 1.1 写 `scripts/check_tracked_hygiene.sh`（非空判据 + `check-ignore --no-index` 判据 + 显式空白名单）；对未清理的 HEAD 运行 MUST 红，留痕输出（4 空文件 + `hottest-sub1/2`）

## 2. 清理

- [ ] 2.1 `git rm -r hottest-sub1 hottest-sub2`；删工作树遗物 `own-sub1 own-sub2 own-a2okagent own-zzbadagent hottest-sub3 hottest-drop-main hottest-drop-sub1 hottest-drop-sub2` 与孤儿 `go.work.sum`
- [ ] 2.2 复核：全库追踪空文件数 = 0；`git ls-files | git check-ignore --no-index --stdin` 输出为空

## 3. 接门

- [ ] 3.1 `scripts/lint.sh` 增一段（文案风格与既有各段一致）；本地 `bash scripts/lint.sh` 全绿

## 4. 验证与 CI

- [ ] 4.1 `openspec validate --strict`、`go build ./...`（应无影响）；push 并确认 CI 四 job 绿
- [ ] 4.2 范围外声明已在 proposal 落档（`.pre-isolation-backup` 等不动）
