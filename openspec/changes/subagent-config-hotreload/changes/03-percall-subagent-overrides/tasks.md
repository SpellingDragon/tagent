# Tasks: 03 per-call 子 agent 覆盖层

- [ ] 3.1 fail-before 双红：① 连续两次调用空白 agent，第二次不带覆盖——断言第二次视图=generation 默认（若现状共享态污染即红）；② 越域 tools_subset 断言结构化错误（现状无校验即红） —— 验证：`go test ./agent/ -run 'Override' -count=1` 两测红（红证据入档后转绿）
- [ ] 3.2 参数面与校验：委派工具 schema 四新字段（system_prompt_override/model_override/tools_subset/**context_refs**——上下文引用暴露，经既有 RuntimeState/ExternalContextEntry 通道注入，不自建传递面）；tools ⊆ max_tools fail-closed 校验；模型引用存在性校验 —— 验证：`go build ./... && go vet ./agent/` exit 0；覆盖测试经真实委派调用路径（非 mock 工具直调）
- [ ] 3.3 覆盖栈接线：invocation 携带 overrides → `Run` 装配期读取（prompt/model/tools = overrides ?? generation 定义）；invocation 消亡即弹出（结构性作用域） —— 验证：`go test ./agent/ -run 'Override' -count=1` exit 0（3.1 双测转绿）
- [ ] 3.4 Declarative 冻结：`Declarative.Overrides` 序列化 + relaunch 重放还原 + 跨重启 `RebuildTaskRegistry` 重放测 —— 验证：`go test ./agent/task/ -run 'Declarative|Rebuild' -count=1` exit 0
- [ ] 3.5 代际交互测：覆盖调用在途时文件热更发布新代——断言在途调用视图钉定（含覆盖不被新代冲掉）、下一次无覆盖调用用新代 —— 验证：`go test ./agent/ -run 'Override.*Generation|Generation.*Override' -count=1` exit 0
- [ ] 3.6 空白 agent org 样例 + wiki 增补 + spec delta + 全量回归：配置样例（max_tools 清单）入 examples；wiki 热更页补 per-call 节；全包净 —— 验证：`go test ./... -short -count=1` exit 0
