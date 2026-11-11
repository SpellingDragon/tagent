# 配置活线：孙任务

> 验证约定：所有 `go test -run` 验证命令默认附加 `-v`；用例数=`--- PASS:` 行数；零匹配（ok [no tests to run]）视为空转不算通过。

- [x] 1.1 声明事实生产点与载荷：Boot 装载后提交 `config.declaration` 事实（常量驻 assembly；身份=规范指纹；Content=规范声明序列化；大正文走既有内容寻址引用）；同声明重载不双发（幂等）。`TestDeclarationFactCarriesPayload`（Boot 后链上有声明事实、Content 可还原规范声明、指纹身份）+ 同一测试内重载两次断言恰一条。 —— 验证：`go test ./assembly -v -run 'TestDeclarationFactCarriesPayload' -count=1` 1 PASS 行（幂等断言在同一测试内）。

- [x] 1.2 发布入口消费声明事实：Reload 从链上最新声明事实构造候选；治理/基线/发布记录/Hold 路径不变（断言走的是既有 `Run.Publish`，不是第二路径）。`TestPublishConsumesDeclarationFact`。 —— 验证：`go test ./assembly -v -run 'TestPublishConsumesDeclarationFact' -count=1` PASS。

- [x] 1.3 失败回执如实（r17 缺陷结构性修复）：安装失败→`install-failed` 事实落链带 result＋`/reload` 回执失败（当前红——探针 `TestReloadReportsSuccessWhenTheModelRebuildIsRefused` 已证实现状回成功）。`TestFailedInstallReceiptIsHonest`。 —— 验证：`go test ./assembly -v -run 'TestFailedInstallReceiptIsHonest' -count=1` PASS。

- [x] 1.3a 治理拒绝与装载失败（F1）：`TestGovernanceRejectionReceiptIsHonest`（治理拒绝→不发布+失败事实（result=governance-rejected）+回执如实）+`TestLoadFailureFactAuditable`（装载失败→运行中绑定不断+失败事实（result=load-failed）可回读）—— 验证：`go test ./assembly -v -run 'TestGovernanceRejectionReceiptIsHonest|TestLoadFailureFactAuditable' -count=1` 2 PASS 行。
- [x] 1.4 watcher：改文件→防抖→自动走唯一发布入口；内容不变（仅 mtime）不触发；坏 YAML 不断运行中绑定。`TestWatcherFileChangeReloads`+`TestWatcherIgnoresMtimeOnlyTouch`+`TestWatcherBadYamlKeepsRunningBinding`。 —— 验证：`cd examples/wechat-bot && go test -v -run 'TestWatcherFileChangeReloads|TestWatcherIgnoresMtimeOnlyTouch|TestWatcherBadYamlKeepsRunningBinding' -count=1` 3 PASS 行。

- [x] 1.5 重启对账：文件新→自动发布；一致→静默零发布；链新→以链为准不倒退。`TestRestartReconcilesFileWithChain`。 —— 验证：`go test ./assembly -v -run 'TestRestartReconcilesFileWithChain' -count=1` PASS。

- [x] 1.6 跨维窗口可审计：发布记录带各维生效结果（绑定代/模型版本/各维 applied|held|skipped），窗口内组合按代号可查询。`TestCrossDimWindowAuditable`。 —— 验证：`go test ./assembly -v -run 'TestCrossDimWindowAuditable' -count=1` PASS。

- [x] 1.7 发布记录上链：`PublicationRecorder` 接 durable 链（替换 `memoryPublications`），重启后可回读。`TestPublishRecordLandsOnChain`。 —— 验证：`go test ./assembly -v -run 'TestPublishRecordLandsOnChain' -count=1` PASS。

- [x] 1.8 规格与文档：`config-hot-reload` MODIFIED（链真词语义：声明事实＋唯一入口＋回执如实＋五不变量）；`config-liveness` ADDED 五不变量；GLOSSARY 配置变更词条链真词化；deploy README 热更说明。 —— 验证：`bash scripts/gates/check-openspec.sh` exit 0＋`python3 scripts/gates/check_doc_links.py` exit 0。

- [x] 1.9 三钉（golden 共享边由 lead r29 修复后 --implementation ok；本域 golden config_facts 登记＋check_config_liveness 门＋自测 11 例全过）：①golden 登记声明/发布事实类型与唯一入口（`check_runtime_architecture.py --implementation` exit 0）；②lint 拦第二发布路径/新增特性专属装配路径（带正反样例自测）；③两门接入 `scripts/gates/preflight.sh`（逐提交执行）。 —— 验证：golden 门＋lint 门＋preflight 配置链段三道命令 exit 0；反向样例（临时注入第二发布路径/新增特性专属解析路径）各判红，正反证据入台账。

- [x] 1.10 域门禁：

## scenario ↔ 测试映射

| spec scenario | 测试 |
|---|---|
| 声明落链带载荷 | TestDeclarationFactCarriesPayload |
| 发布经唯一入口 | TestPublishConsumesDeclarationFact / TestWatcherFileChangeReloads |
| 安装失败回执如实 | TestFailedInstallReceiptIsHonest |
| 治理拒绝即不生效 | TestGovernanceRejectionReceiptIsHonest |
| 装载失败不断线 | TestLoadFailureFactAuditable / TestWatcherBadYamlKeepsRunningBinding |
| mtime-only 不触发 | TestWatcherIgnoresMtimeOnlyTouch |
| 重启对账不漂移 | TestRestartReconcilesFileWithChain |
| 跨维窗口可审计 | TestCrossDimWindowAuditable |
| 发布记录上链 | TestPublishRecordLandsOnChain |`go test ./assembly -v -count=1`＋`cd examples/wechat-bot && go test -v -count=1 ./...`。 —— 验证：两命令 exit 0。

## 五不变量 ↔ 测试映射（r12 式）

| 不变量 | 测试 |
|---|---|
| 文件是界面链是账 | TestDeclarationFactCarriesPayload / TestPublishConsumesDeclarationFact / TestRestartReconcilesFileWithChain |
| 变更必经唯一发布入口（治理保留） | TestPublishConsumesDeclarationFact / TestWatcherFileChangeReloads |
| 回执如实（失败必达账面） | TestFailedInstallReceiptIsHonest |
| 声明事实带载荷可审计 | TestDeclarationFactCarriesPayload / TestCrossDimWindowAuditable / TestPublishRecordLandsOnChain |
| 重启对账不漂移 | TestRestartReconcilesFileWithChain / TestPublishRecordLandsOnChain |
