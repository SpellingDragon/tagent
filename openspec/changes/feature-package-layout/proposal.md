# 特性包布局：features/ 分组 + cli 定位成文 + 新能力包规约

## 症状（验收矩阵第一行）

| 原话 | 直译测试 |
|---|---|
| 「telemetry、evolution、rl 这些领域放入 features 包作为子包…后续的能力迭代，都统一通过包管理」（用户，2026-10-27） | `go list ./...` 中三域位于 `tagent/features/{telemetry,evolution,rl}` 且旧路径零残留（`grep -rn 'tagent/telemetry"\|tagent/evolution"\|tagent/rl"' --include='*.go'`=0）；全仓 build/三模块/全套门禁绿 |

## Why

顶层十个域平级，内核（api/runtime/memory）与可选能力（telemetry/evolution/rl）无目录分界，新能力无统一落点（组合根持续增重的上游原因之一）。裁定（编排者，2026-10-27，承接用户取向指令）：**features/ = 能力从属域：实现内核声明端口或策略面的可选能力**——telemetry（纯 api/plugin 观察）、rl（纯 api capture 面）、evolution（反思+治理策略面）入选；tool/ 不入（内核执行面组成，被 host/runtime 直接消费）；api/runtime/memory/cli/根包=内核与装配面不动。

## What Changes

- 三域 `git mv` 入 features/，全仓 import/基线键/CI race 表/变异注册表路径/docs/api 同步（共享面编排者单写）。
- **cli 定位成文**：留顶层=「对外装配面」（嵌入者编程入口 NewCLI+HostBridge 投递审核契约）——不移 example（契约面降级禁止）、不删（有正式契约面与集成测试消费）；写入 EXTENSIONS/CONTRIBUTING 定位句。
- **新能力包规约**：新能力=features/ 下一包+一装配点+api/plugin 端口声明，成文入 CONTRIBUTING 包结构节（等行或锁步）。

## Impact

机械迁移零行为变更；同步面=import（9+5+39 文件）、ci.yml、概念预算 11 键、变异注册表、docs 13 处、doc/api 重生成。启动门=无（独立案）。

## 必答一问

「本案完成后，新能力还会有第二个落点吗？」——不会：规约成文+目录分界+门禁（架构检查器/概念预算路径键）钉住；旧路径零残留为结构负锚。
