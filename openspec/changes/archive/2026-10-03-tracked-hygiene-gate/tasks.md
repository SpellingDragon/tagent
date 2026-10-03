# Tasks

## 1. fail-before（先证门能抓到现状）

- [x] 1.1 **由 `2026-10-03-root-structure-and-hygiene` 以更好落点交付**：判据做进 `codetools tracked-hygiene`（纯函数 + 单测 + 接入 lint.sh），非新 shell 脚本；该档已归档 写 `scripts/check_tracked_hygiene.sh`（非空判据 + `check-ignore --no-index` 判据 + 显式空白名单）；对未清理的 HEAD 运行 MUST 红，留痕输出（4 空文件 + `hottest-sub1/2`）

## 2. 清理

- [x] 2.1 同上交付（4 个零字节残骸 git rm、遗物目录与孤儿 go.work.sum 清理，`.pre-isolation-backup/` 等按约保留） `git rm -r hottest-sub1 hottest-sub2`；删工作树遗物 `own-sub1 own-sub2 own-a2okagent own-zzbadagent hottest-sub3 hottest-drop-main hottest-drop-sub1 hottest-drop-sub2` 与孤儿 `go.work.sum`
- [x] 2.2 复核扩充：空追踪文件 0；**本档新增第四判据** tracked-and-ignored 并纠正其取法（`git ls-files -ci --exclude-standard`，非 check-ignore——后者连否定规则也算命中，实测会误报 13 条）；`examples/wechat-bot/.gitignore` 补齐白名单后现库 exit 0，负样本 `git add -f probe_tracked.log` exit 1 复核：全库追踪空文件数 = 0；`git ls-files | git check-ignore --no-index --stdin` 输出为空

## 3. 接门

- [x] 3.1 lint.sh 已有该段（上一档接入），本档在同一子命令内扩判据并加单测 `scripts/lint.sh` 增一段（文案风格与既有各段一致）；本地 `bash scripts/lint.sh` 全绿

## 4. 验证与 CI

- [x] 4.1 验证墙：codetools 单测 ok、tracked-hygiene exit 0、lint ok、bot 模块 build+test ok、根包 short 58.3s ok、全量 33 包零 FAIL。门在本次落地过程中两次自证有效：抓到 check-ignore 陷阱之外的真债（13 个 tracked∧ignored），又抓到我用 rm -rf 撤旧 delta 造成的 tracked-path-missing（索引未清），故改判为正规 git rm `openspec validate --strict`、`go build ./...`（应无影响）；push 并确认 CI 四 job 绿
- [x] 4.2 范围外声明保持（`.pre-isolation-backup/`、`.tagent-workspace/`、`.agents/`、`.qoder-handover.log` 不属追踪卫生判断面） 范围外声明已在 proposal 落档（`.pre-isolation-backup` 等不动）

## 5. 本档暴露的门自身缺陷（追加）

- [x] 5.1 `commit-scope` 在 CI 浅克隆下误判全树为代码路径 → 无父提交时弃权 + test job `fetch-depth: 2`；复现与验证都在 `file://` 深度 1/2 克隆中完成（见 D5）
- [x] 5.2 tip 46829c1 CI 四 job 全绿；日志中 abstaining 出现 0 次 ⇒ 门在 CI 上真判定并通过。已归档
