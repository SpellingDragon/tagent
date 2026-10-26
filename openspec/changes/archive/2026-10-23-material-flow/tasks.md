# Tasks：material-flow（两相位统一＋投影三用＋通知归链）

> 诸事皆允（未正式发布）。task 级并发：文件簇零相交。波次：解锁=依赖入 main。

## 波1（P1-P4 完全并发）

- [x] P1 api/plugin 两相位重构：4相位(call/risk/observe/present)＋compress-trigger/environment-inject → **生产(f)/消费(g)两相位＋输入类型路由派发**；核心消费器（Drive/Tick）与插件统一为相位实例 —— 验证：`go test ./api/plugin/... -count=1`＋负锚（未声明读写类型拒绝）
- [x] P2 evolution/meditation 血统降位：novelty/投递白名单判断出移为域插件逻辑（消费时自判，不依赖全局血统概念）；CompressPhase→Observer 适配 —— 验证：`go test ./evolution/meditation/... -count=1`＋负锚（novelty 判定不依赖血统轴）
- [x] P3 tool/subagent refs 详情：ExternalContext() 渲染 refs 走 AssembleBudgetPrefix 同一预算（full→ticket→summary）——子 agent 拿到内容不再是裸 ID —— 验证：`go test ./tool/subagent/... ./tests/integration/execution -count=1`＋refs 详情断言＋预算降级断言
- [x] P4 delegation 血统落委派准入记录（裁决A：agent.Delegation 加 TriggerSource→task 记录；常量 event.LineageDelegation） —— 验证：`go test ./... -count=1`＋委派链血统戳断言

## 波2（P5-P7 完全并发，依赖波1 接口）

- [x] P5 loop.go＋facts.go 合单：通知统一（拆 intake 车道 notifie/arrived→投影增量读，位点=ended）＋observe 挂点适配新两相位接口 —— 验证：`go test ./runtime/agent/... ./tests/integration/state -count=1`＋崩溃重建通知不丢不重负锚
- [x] P6 request.go＋prepare.go 合单：环境块入投影装配（PhaseEnvironmentInject→装配段）＋transform 适配＋投影三用统一（主上下文/环境块/委派refs 一套装配器） —— 验证：`go test ./tests/integration/host -count=1`＋三用共用预算断言
- [x] P7 插件适配扫尾：memory/evolution/governance 插件实现改生产/消费声明（读X产Y） —— 验证：`go test ./memory/... ./evolution/... -count=1`

## 波3

- [x] P8 golden/S4/EXTENSIONS/lint 收口 —— 验证：golden match＋lint ok
- [x] F1 契约零改动（golden match）＋F2 全量门禁（lint ok/40包）＋F3 DoD
- [x] F4 archive（用户明示归档）
