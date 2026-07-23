## 1. 配置修复（examples/wechat-bot/tagent.yaml）

- [x] 1.1 `tagent`（entry，主 agent）在 `thinking_enabled: true` 旁补 `reasoning_effort: "high"`
- [x] 1.2 `knowledge` agent 补 `reasoning_effort: "high"`
- [x] 1.3 `action` agent 补 `reasoning_effort: "high"`
- [x] 1.4 更新 thinking 配置区注释：说明 `reasoning_effort` 是 hy3/hunyuan 的思考开关，`thinking_enabled`/`thinking_tokens` 对 hy3 无效（纠正原注释"reasoning_effort 仅 OpenAI o-series"的误导）
- [x] 1.5 确认 `plan`（glm-5.2）等非 hy3 agent 不动

## 2. 验证

- [x] 2.1 真实 API：`TENCENT_API_KEY=xxx go test -v -run TestHy3ThinkingMode_RealAPI ./tests/ -timeout 200s` 通过，矩阵确认 `reasoning_effort` 触发思考、`thinking_enabled` 单独不触发
- [x] 2.2 `go build ./... && go vet ./tests/` 通过；`go test ./tests/ -short -count=1` 通过
- [x] 2.3 配置解析 sanity：`TestLoadConfig_ExampleYAML` 通过（YAML 合法、tagent 加载器可解析修改后的 tagent.yaml）

## 3. 收尾

- [x] 3.1 `openspec validate hy3-thinking-activation --strict` 通过
- [x] 3.2 按 Conventional Commits 规范提交（scope: wechat-bot），说明 hy3 只认 reasoning_effort 的根因与配置修复
