# Tasks

## 1. P1 追踪卫生

- [x] 1.1 （实测：10 个残骸目录全为 0 字节、仅含 lock/journal、无 .md/.json 数据后删除；`.pre-isolation-backup/`、`.tagent-workspace/` 按约保留）`git rm` hottest-sub1/ hottest-sub2/（4 个零字节文件）；删孤儿 `go.work.sum`；清工作树遗物（own-*、hottest-sub3、hottest-drop-*；`.pre-isolation-backup/`、`.tagent-workspace/` 属运行/备份数据，默认保留）
- [x] 1.2 （实现为 `codetools tracked-hygiene` 子命令，纯函数 + 3 条单测；接入 `scripts/lint.sh` 与本地同命令；负路径双红留痕 `hygiene-probe.log`）新增 `scripts/check_tracked_hygiene`（空文件/运行期后缀/顶层白名单三查）并接入 validators job；负路径探针（空文件与 `x.lock` 各造一枚）双红留痕后撤除
- [x] 1.3 （`go test ./... -short` 31 包全绿零 FAIL、`GOMAXPROCS=1` 根包 59.6s ok、lint ok 含新门）全量 short + lint 绿；提交

## 2. P2 train/ 挪移

- [ ] 2.1 `git mv train/rl scripts/rl`；grep 仓库内路径引用（README/docs/tests）同步；提交

## 3. P3 世代族拆包 agent/org

- [ ] 3.1 前置：枚举五文件对 runtimeConfig 的 7 处读写语义，定 `ShellRuntime` 快照与 `ShellBuilder` 契约（若发现写回运行态等无法快照化的用法，停下上报）
- [ ] 3.2 `agent/org` 建包：`git mv` org_hotreload/org_candidate_txn/org_candidate_overlay/owner_retirement/partition_collision；asset_drift 留根包
- [ ] 3.3 根包注入点装配 + `Org*` 类型别名 + TagentAgent 薄委托；`go build ./...`、分层机械断言测试绿
- [ ] 3.4 `gen_godoc` 重生成；根包/agent plain+race、`GOMAXPROCS=1` 根包、lint、openspec strict 全绿；提交

## 4. P4 巨型测试按域拆分

- [ ] 4.1 org_candidate_test(2007)/cross_generation_test(1969)/org_hotreload_test(1715) 按域拆 ≤600 行文件，留根包；纯逻辑单测（如有）随 agent/org
- [ ] 4.2 `check_test_merge.sh` 对每个源文件证无损；同位门逐文件核对锚点与镜像
- [ ] 4.3 全量 short + race + P=1 + lint 绿；提交

## 5. 收口

- [ ] 5.1 README 布局说明与 docs/wiki 索引同步（新包、新目录、train 新址）
- [ ] 5.2 push 并确认 CI 四 job 绿；归档
