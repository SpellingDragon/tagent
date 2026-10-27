
---

## 共享面收口记录（2026-10-27，HEAD ab28851 之后）

- **W1 三域单零写入**（迁移主干已把域内注释清干净——三单以扫描证据+锚复跑交差，不为交差造改动）：telemetry 12 PASS+消费锚 12 PASS；rl 7/16 PASS+三消费锚；evolution 76 PASS+变异 11/11。
- **共享面五件由编排者补**：①golden/runtime/architecture.json 五包登记与全部 allow/forbidden 边精确迁移（ARCH_IMPL RESULT ok）；②5 个旧 docs/api 残件删除（gen_godoc 曾报 generator-no-longer-writes）；③机械替换误伤三处还原（OPERATION:199/215 框架包 `trpc-agent-go/telemetry/*` 被误加 features 前缀；wiki/README:38-39 docs 内部相对链接被当包路径；OPERATION:243 `./evolution/governance` 漏网）+agent-architecture:142「独立顶级包」措辞；④内核两处注释旧径（memory/skeleton_assert.go:6、api/event/compress_request.go:13）+docs/api 重生成 GEN=0；⑤CONTRIBUTING features 规约句+cli「对外装配面」短语（D2 成文）。
- **D1 判据张力（W1-E 发现，裁定采纳）**：runtime/agent→features/evolution/governance（WithCallPass）实证「严格可移除」不成立——判据改「能力从属」；端口化另案候选。
- **存量悬空登记（非本变更引入，不扩面）**：behavior-matrix:52 refine.go、agent-architecture:164 asset_drift.go、behavior-matrix:9 gate.go、:22 checkIndex、agent-architecture 第三节 20+ 条 3369af7 前旧树——属既有文档债，留档待文档批。
- **保持不变的「路径状」身份**（可观测性/持久化契约，迁移不改名）：OTel meter 名 `tagent/telemetry`（×2）、事件类型前缀 `tagent/governance/v1/*`。

---

## 环境事故记录（2026-10-27 夜，负结果入库）

- **本机文件数据蒸发事件**：磁盘 99% 满（余 16Gi/926Gi）期间，**615 个 tracked 文件读空（openspec 归档 600、scripts 14、examples 1）+ features/ 53 个 .go**——stat 尺寸完好而 read 返回空（APFS data-less 形态；git 因 stat 吻合而不判变，checkout 空转，须 rm+checkout 强制重写恢复）。已全量恢复（REMAIN_VOID=1，余一为会话前即存在的未跟踪 data-less 文件 `examples/wechat-bot/skills/interview/agent-interview-helper`——其诡异状态之谜至此揭晓）。**受害者含架构检查器与 lib（71 自测败之因）**；早前 verify_restart_prefix「负载下 flake」同族（冷读空）。**建议用户：磁盘清理与备份检查（机器健康级问题，非仓库问题）。**
- **门禁面四处陈旧随迁**（CI 红的完整清单）：runtime-contracts.yml PRODUCT_PACKAGES、ci.yml race 表（已有）、lint.sh 产品包目录清单、hygiene.go 顶层注册表、examples/contract 五目录名（S4 推导按包路径）、两个自测 fixture 字面量（R23 型：判据对、字面量随契约陈旧——test_check_runtime_architecture 的 evolution 两处、test_check_runtime_workflow 的 RACE_PACKAGES）。

---

## F 门禁归因（2026-10-27/28 夜）

- **short10 RC=1**：repeat-1/10 之 03.6 FoldControl negative 子测（synthetic key 跨重启不等）——同片受控重跑绿+`-count=3` 绿（4/4 复跑全绿）；定性环境时序 flake（本机当晚高压）。余 9/10 match。
- **race RC=1**：TestMCPFaceBindsTheDeclaredServers 确定性 DATA RACE（2/2）——**基线 8919acf 同红**（继承面实锤）；栈顶=trpc-mcp-go v0.0.10 streamableHTTPClientTransport.setSessionID/send（LEDGER U5 既有双签豁免）；高压下竞态显化为功能性失败（race 污染 session id→网关断言败），race_check 豁免只认 race 报告不认功能败——门红如实记录。**处置=WATCH：升级 trpc-mcp-go 至含上游修复版后删 U5 签名复跑（LEDGER 既定移除条件）。**
- **mutations/symptom/witnesses/三模块/selftests/lint/openspec/short-root 全 RC=0**；longs 六片进行中（结果入 evidence/F2）。
- **远端 CI**：push 型 workflow（runtime-contracts）于 44761b9 **SUCCESS**（provider-contract=手动、real-smoke=定时、CI=仅 dev/main 与 PR——next 推送无 CI 型 run，非跳过）。

---

## 终态 CodeReview 补记（2026-10-28，归档后追加勘误段）

独立 CodeReview（净变更 8afa6cfc..0066ca8）：Critical 0 / Major 1 / Minor 2——
- **M1 已修**：smoke_exec_test 之 Wave2Exec watchdog 子测残留断言恒空的 SessionHistory（单面契约下的迁移遗漏消费者；基线即红、tmux 缺失环境从未踩到=门禁盲区）。修复=改读单面真义：被受理两轮 exec 留痕于映射序列恰 2 条、被拒第三轮不留事实（回执轮内范围）。修后三连绿+smoke 全套中本会话面绿；死助手 smokeExecHistoryCarries 与失用 import 一并清除。
- **m1 已修**：single_history_path_test.go 文件头注释首句残缺（磁盘故障恢复期内容异常）——补全首句。
- **m2 证伪**：幻影 ADD replay_presentation_test.go 经磁盘核实不存在（索引恢复陈旧记录），无行动。
- **继承面登记（基线 8919acf 同红、不扩面）**：TestLocalSmokeWave2Data（call_subagent 装配）/TestLocalSmokeWave2Recall 为本会话前已破的 smoke 项（同静默失效家族——不在 F2 门禁清单）；FoldControl 为当晚已定性时序 flake。留档待修。
数值/不变量核验（评审出具）：noticeCard 边界、快照单源链路、EventBody 单格式、SameFact 排除面、提交单入口、迁移残留零、Observer 分层——全部通过。
