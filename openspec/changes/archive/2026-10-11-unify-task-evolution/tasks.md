## 1. 原型实现

- [x] 1.1 Governance→Evolution 全量改名；EventModelDelta 恢复与 Reply final 分流；投递任务化与管线关系单点（TaskManager 持 owner、Deliver 改 ToolFunc 形态）—— 验证：`GOPROXY=off go vet ./prototype`。
- [x] 1.2 TaskWindow 条数聚合窗（编译自 AgentConfig，Close flush）；DenylistPlugin 治理拦截示例 —— 验证：`GOPROXY=off go test ./prototype -run TestAgent -count=10 -timeout=90s`。
- [x] 1.3 集成场景：投递失败收敛、中间输出不收口、拦截零执行、窗口攒批、Evolution 拒绝零改动回归、恢复/热更不回退；README 同步 —— 验证：`GOPROXY=off go test ./prototype -count=10 -timeout=90s`。

## 2. 文档与复验

- [x] 2.1 wiki README 顶部标注历史设计与分叉点（输入折叠、治理范围、批次栅栏/窗口、ModelDelta 删除与恢复）—— 验证：读回 docs/wiki/README.md。
- [x] 2.2 主代理复跑全量与 race、vet、Governance 残留 grep 清零、strict 校验 —— 验证：`GOPROXY=off go test -race ./prototype -count=10 -timeout=180s`；`grep -rn "Governance" prototype/` 为空；`openspec validate unify-task-evolution --strict`。

## 验证记录（主代理复验）

- 子代理 race count=3 未暴露的两处间歇失败，均由全量 race count=10 复现并当场修复：
  1. TestAgentPipelineAsyncDispatch：投递完成通知与触发词同批时 mock 的 lastInput 收敛读到空通知丢失触发语义——测试骨架错位，改"已见集合"收敛（修 mock 不改产品）。
  2. TestAgentTaskWindowBatching/window2：flush 逐条 Publish 允许消费端在批中间 pull，违反 spec"一次消费批次同时可见"——产品缺陷，新增 AgentState.PublishBatch（单锁整批入队+一次唤醒），flush 改走批量原子路径。
- 修复后连续两轮：`go test ./prototype -count=10` ok、`go test -race ./prototype -count=10 -timeout=180s` ok（约 22s/轮）；vet/gofmt/diff --check 无输出；`grep -rn "Governance" prototype/` 退出 1（清零）。
- 子代理申报的自行决策经复核接受：present 呈现插件（denied 事实经 Data 呈现给模型，符合"核心不拼接事件属性"既定裁决）；全部通知（running+result+unknown）入同一攒批；"治理插件"中文词保留、旧管道改称演进。
- wiki 声明块 +11 行仅插不改，四条分叉点与 README/prototype 现状一致。

