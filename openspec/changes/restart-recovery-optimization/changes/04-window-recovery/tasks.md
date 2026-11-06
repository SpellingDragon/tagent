# 04-window-recovery 孙任务

## A. 窗口类界收口

- [ ] 4.0a 窗口类界收口：`max_tokens` 唯一输入；删 `provider_context_window`/`output_reserve`/`long_context_tokens`/`capture_budget_mb`/`vector_top_k`/`keyword_top_k`/`rrf_k`；provider 窗=回放窗、输出余量=窗/4、`long_context` 由装配剩余量算、捕获与缓存按窗口渲染字节派生、页界一个数、RRF K 常量；schema/decode/reload/示例/文档同批；旧键→具名拒绝 —— 验证：旧键在代码与示例 0 命中；schema 门反例（旧键⇒红）；示例配置启动绿
- [x] 4.0b 常驻界派生：删 `factWindow`/`replayResidentWindow`；带窗会话链以投影最老原文为界（`liveWindowCutLocked`），无窗链兜底 `noWindowResidentFacts=2048`（登记为机制常量）；投影裁剪只在无窗链生效（有窗链由折叠决定） —— 验证：`TestWindowedFactsFollowTheProjection`（界前释放/界后+pinned 保留/无窗兜底）+ 既有窗口测试全绿
- [x] 4.0d 折叠规则 golden 与 mutation：`golden/runtime/fold_contract.json`（段龄阶梯/带→动作/产物形状/体积因子）+ `golden/runtime/fold_plan_golden.json` 向量 + `TestFoldPlanGolden`（六段闭环 + 一个进行中段，逐字段比对）；四条 mutation（改阶梯窗、改中带层级、折进行中段、换产物类型）各以该测试为锚，`check_event_integrity_mutations.py` 注册并实测全 killed—— 验证：`go test ./memory/replay -run '^TestFoldPlanGolden$'` PASS；四条 mutation 各 `killed (named_assertion)`
- [x] 4.0c 词表与门禁：`golden/runtime/window_bounds.json` 四类登记（回放窗/预算面/策略/机制常量，20 条）；`check_window_bounds.py` 四规则 + 7 例正反自测，接入 lint；GLOSSARY「请求预算」行内收四类词（不新增行，预算持平）—— 验证：门 exit 0；反例（未登记窗口类键⇒红、第二个窗⇒红、常量当声明键⇒红、常量漂移⇒红、登记键从声明面消失⇒红）全过

## B. recall 与窗口解耦

- [ ] 4.1 观察表退役为查询缓存：启动零 land；查询按过滤条件读持久层、分页；缓存可丢 —— 验证：同一查询结果与今天逐条一致；启动 land 条数=0；缓存丢弃后结果仍一致
- [ ] 4.2 语义召回按查询候选现算：内存无 runtime 窗口向量、无全史向量表；启动 Submit=0 —— 验证：同一查询 Top-K 与今天逐条一致（候选集口径写明）；启动 Submit=0

## C. 窗口内重建

- [ ] 4.3 投影窗口内重建：窗口起点=产物覆盖边界+最近未闭合段；只折窗口内的段并挂产物 —— 验证：与「全量折叠后裁窗」逐条 DeepEqual；启动 `EachEvent` 条数=窗口条数；产物/边界不可得⇒退全量
- [ ] 4.4 固定成本：`landedSeq` 读索引尾；tier 打开懒校验 —— 验证：启动不读正文；坏分片首次读报错并退全量
- [ ] 4.5 消费者锚：真实 120 重启 —— 验证：boot 在 gen1/20/120 同量级（0.1–0.5s）+ 数据准确性对账绿

## verify 三锚（随任务行冻结，R29）

正锚：界登记门 + 窗口内重建 ≡ 全量折叠后裁窗 + 查询结果不变。负锚：产物/边界不可得⇒退全量；坏分片首次读失败⇒退全量。消费者锚：真实 120 重启 boot 与历史长度无关。
