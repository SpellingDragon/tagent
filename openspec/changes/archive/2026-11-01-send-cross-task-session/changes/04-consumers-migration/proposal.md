# 04 consumers-migration：消费面迁移

父变更：send-cross-task-session（依赖：01/02/03 的接口面已合并——实现依赖）。

## Why

模型面 BREAKING（send 职责重定义、`deliver_summary`/`resume_task` 退役）牵动全部消费面：prompt 资产、wiki 文档、smoke/集成测试、CHANGELOG。不迁移=文档撒谎+测试假绿（R34 消费者枚举）。

## What Changes

- `resources/prompts/*.md`（`plan_tool_desc.md`/`action_tool_desc.md`/`plan_agent.md`、wechat-bot 副本）：`resume_task` 引用改写为 `send{target:{task}}`；`deliver_summary`/出网 send 提法清理。
- wiki（`docs/wiki/examples/wechat-bot-runtime.md`、`plugin/plugin-architecture.md`、`platform/platform-subsystems.md` 等）：send 职责口径统一。
- 测试迁移：`examples/wechat-bot` smoke（`smokeApprovalDocument` 的 critical 练刀对象改 send 新形态；`TestLocalSmokeSymptomNoSendTool` 场景随新语义更新）、`tests/integration/host` 既有 send 出网场景改写。
- CHANGELOG 四要素（内容/影响/触发/自查）+ 导览文档。

## 边界与依赖

- **依赖**：01（声明文案）、02（重入语义）、03（对外语义）——实现依赖，全部合并后启动。
- **被依赖方**：F 收尾（DoD 文档项）。
- **接口面**：无代码接口（文档与测试面）。
- **禁止事项**：不改产品代码；不动 `docs/.dev/`；CHANGELOG 已发布版本段不可改写。
