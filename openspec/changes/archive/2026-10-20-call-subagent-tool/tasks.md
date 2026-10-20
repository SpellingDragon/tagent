# Tasks：call_subagent 工具与预置能力装配

> 编排：叶级 verify 冻结（正锚/负锚/消费者锚逐字符）；派发原子=就绪孙任务；每叶完成即勾选。

## 1. 装配器（md 定义 + 显式参数）

- [x] 1.1 实现 frontmatter 解析器（agents 文件形态）＋拒绝矩阵 —— 验证：`go test ./tool/subagent -run '^TestAgentsFileParseMatrix$' -count=10`（正例字段全出/负例：双源同给/文件不可读/缺 name/类型错 各具名拒）
- [x] 1.2 实现显式参数装配器（prompt/model/skills/tools/mcp）＋继承语义（未声明取父） —— 验证：`go test ./tool/subagent -run '^TestExplicitParamsAssembly$' -count=10`（含继承覆盖负向）
- [x] 1.3 装配产物经 trpc agent 构建器落地（复用面） —— 验证：`go test ./tool/subagent -run '^TestAssembledAgentServes$'`（装配后可执行一次 turn，模型 mock）

## 2. call_subagent 工具与 task 化执行

- [x] 2.1 工具 desc 双装配源声明＋工具实现（desc 即契约：agents_file / 显式参数） —— 验证：`go test ./tool/subagent -run '^TestSubagentToolDeclaration$'`（desc 字段/拒绝形态）
- [x] 2.2 装配→`agent.Delegate` task 化执行链路＋通知回调（复用特性 3 语义，不重写） —— 验证：`go test ./tests/integration/execution -run '^TestSubagentCallAdmitsTask$' -count=10`（Receipt 即返/任务完成通知到发送者/零同步阻塞）
- [x] 2.3 配置面：AgentSpec.Tools 预置三类工具引用（knowledge/recall/call_subagent）装配 —— 验证：`go test ./tests/integration/config -run '^TestPresetToolsAssemble$'`（config 声明→装配出对应工具；knowledge 复用 trpc knowledge/tool；recall 复用 memory 检索）

## 3. plugin / event / event-loop 扩展面保真

- [x] 3.1 event-loop 扩展测试：自定义事件类型经宿主循环被消费→调用 LLM/tool→结果投递（最小形态，冥想/knowledge 作为扩展示例文档化不迁移） —— 验证：`go test ./tests/integration/execution -run '^TestCustomEventConsumedByLoop$' -count=10`
- [x] 3.2 扩展点文档：plugin 四相位/event 词汇/loop 消费点的"扩展者做什么、与什么有关系"（docs/ 页＋契约索引） —— 验证：`go run ./scripts/codetools doc-refs docs/wiki docs/*.md` exit 0＋`bash scripts/lint.sh`
- [x] 3.3 S11 golden：config 驱动 subagent＋pipeline 顺序（特性 4/5 端到端） —— 验证：`python3 scripts/run_runtime_scenarios.py --scope deterministic`（S11 attested）
- [x] 3.4 S12 golden：call_subagent 双装配源→task 执行→通知回调 —— 验证：同上（S12 attested）

## 4. 收尾（一级直管）

- [x] F1 契约零改动断言（本 change 自身 specs 目录）
- [x] F2 全量门禁：`bash scripts/lint.sh` exit 0＋`go test ./... -count=1` 健康
- [x] F3 DoD 逐项核验入 evidence
- [x] F4 archive（本次全量归档明示）
