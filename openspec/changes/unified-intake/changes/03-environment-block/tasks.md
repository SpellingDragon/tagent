# 孙任务

- [x] E1 症状锚红测：`TestTheEnvironmentBlockTrailsTheContext`——真 Invoke 捕获请求：最后一条消息=环境块且含 `<environment>` 标记与 tools/skills/mcp 清单（**规划期红**：现状最后一条是 transcript/环境在前部）；负锚：环境块不出现在前缀区（前 N-1 条无 `## Environment`/`<environment>`）；负锚：清单含全部 VisibleSkills 名+`prompt_version` 行。消费者锚=真 Host.Invoke 端点捕获 —— 验证：`go test ./tests/integration/execution -run '^TestTheEnvironmentBlockTrailsTheContext$' -count=1`（先红后绿）
- [x] E2 环境块文本归位：`<environment>` 环绕+`prompt_version:` 行+段表移除 env 支+BoundaryView 扩字段承载（既有结构扩字段） —— 验证：E1 正锚转绿；`go test ./assembly ./runtime/agent -count=1` 既有全绿（负锚：EnvironmentBlock=false 时无末尾消息=基线）
- [x] E3 loop 末尾拼装：`Messages=前缀+transcript+环境消息`（RoleUser） —— 验证：E1 负锚（前缀区无环境块）绿；`go test ./tests/integration/execution -count=1` 不回归
- [x] E4 规格 delta+锚页：framework-prompt MODIFIED（环境块=动态能力清单、末尾追加、标记环绕）+ agent/event-flow 锚页随迁 —— 验证：`openspec validate unified-intake --strict`；anchor 保全
