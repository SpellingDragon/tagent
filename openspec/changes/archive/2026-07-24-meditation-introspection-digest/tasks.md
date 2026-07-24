## 1. digest 渲染（确定性纯函数）

- [x] 1.1 新增 `agent/meditation_digest.go`：`renderSelfStateDigest(tasks []*Task, idle time.Duration) string` —— 按状态计数 + suspect/dead/failed 简摘（desc/status/age）+ 空闲时长，有界（明细上限 + 超出汇总）
- [x] 1.2 空/降级路径：`tasks` 为空 → 返回空串或单行"无活跃任务"
- [x] 1.3 表驱动单测：多状态计数正确、需关注任务简摘、超上限截断为汇总、空输入降级

## 2. MeditationManager 接入 digest

- [x] 2.1 `MeditationManager` 增加可选 `taskController TaskController` 字段 + 构造期注入（可为 nil）
- [x] 2.2 `buildMeditationMessage` 前置 digest：`[meditation] 头 + digest + prompt`（digest 在 prompt 前）；`taskController==nil` 时跳过 digest 段
- [x] 2.3 idle 传入 `renderSelfStateDigest`：由 `now - lastEventTime` 计算（复用现有锚定 agent 输出的时钟）

## 3. 装配

- [x] 3.1 `agent.go` 装配处把 `taskManager`（TaskController）注入 `NewMeditationManager`
- [x] 3.2 确认无任务层配置下 MeditationManager 仍正常（nil 注入不 panic）

## 4. 验证

- [x] 4.1 `MeditationManager` 单测：注入含各状态任务的 fake TaskController → 冥想消息含 digest 段且位于 prompt 前；nil → 无 digest 段、行为等价
- [x] 4.2 回归：`go test ./agent/ -short -count=1` 全绿；冥想既有测试不回归
- [x] 4.3 `scripts/check-openspec.sh` 通过；`openspec validate meditation-introspection-digest --strict` 通过；按 Conventional Commits 提交
