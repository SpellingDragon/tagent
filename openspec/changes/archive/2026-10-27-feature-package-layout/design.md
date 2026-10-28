# 设计决策

## D1 判据与成员（编排者裁，显式）

features/ = 能力从属域（实现内核端口或策略面的可选能力）：telemetry、rl、evolution。tool/ 不入（内核执行面，host 直接消费执行器）；logs/resources 非 Go 域不动；api/runtime/memory/cli/根包=内核与装配。**判据张力如实登记（W1-E 实证）**：runtime/agent 实依赖 features/evolution/governance（WithCallPass）——「整体移除不毁内核」对 evolution 不严格成立，故判据取「能力从属」而非「严格可移除」；governance 的 WithCallPass 端口化列为后续候选（另案）。被否：全收 tool/logs——反事实不过。

## D2 cli 留顶层成文

有正式契约面（HostBridge/DeliveryReceipt+contract example+3 集成测试）；移 example=契约降级（example 引 example 语义循环）；删除需先并面（无此需求）。定位句入 EXTENSIONS/CONTRIBUTING。

## D3 主干迁移=编排者单写（共享面）

三域 import 横切同批消费文件（rl×39 尤甚），并发派发必互踩——R21 同文件合单原则：git mv+import 重写+ci.yml race 表（./evolution/... ./rl/... → ./features/...）+概念预算 11 键前缀+变异注册表路径+doc/api 重生成，一次完成至全仓 build 绿。

## D4 W1 三域单并发（域内零相交）

每域单：包内 -short 锚+消费锚复跑+docs 该域句（13 处按域分）。负锚（全局，编排者收口）：旧路径 grep=0 + `go list` 无旧包。

## D5 CI 终态（用户指令）

本地全套（lint/selftests/check-openspec/三模块/witnesses/short10/mutations/symptom/race/long 六片）→ push next → 远端 CI 全绿确认（用户「确保所有 CI 通过」=push 授权）。
