# design：spec-conformance

## D1 三龄段语义（裁决）
段龄轴：新段=L0（投影直接对应上下文文本）；中段=L1/L2（user input 与 agent output 之间过程信息折叠产出新事件→投影）；老段=L3（历史摘要）。产生关系：体积阈值触发、对上一临近龄段作用（新→中、中+老→老）。复用 `deterministic-compress-level` 的 age 规则（`age=totalSegs-1-segIdx`）。
被否方案：按轮次身份分三代（无阈值产生关系，与规格"体积阈值触发、对上一临近龄段作用产生"不符）。

## D2 子域表
| # | 子域 | spec 能力 | 一句话边界 | 外部依赖 |
|---|---|---|---|---|
| W1 | context-assembly | context-assembly | 映射序列记录＋三龄段＋动态环境块置底 | memory/replay、run_request、api/config |
| W2 | task-lifecycle | task-lifecycle | 逻辑状态＋TTL＋progress 回调 | runtime/task、runtime/agent |
| W3 | plugin-phases | plugin-phases | 摘要触发/环境注入相位 | api/plugin、evolution/meditation |
| W4 | sample-features | sample-features | subagent tool 形态＋recall 两段＋knowledge 工具面 | tool/subagent、tool/recall、tool/knowledge、examples/wechat-bot |
| W5 | observability | observability | span 去重＋六机制观测 | api/model、runtime/agent、evolution、memory |
| W6 | conformance-golden | conformance-golden | S13-S15＋S10 测试＋f·g 语义 | golden、tests、api/plugin、runtime/agent |

## D3 波次表
W1-W2 并发（触碰集不相交）→ W3-W4 并发 → W5 → W6 → F。波次仅为汇报分组，解锁=依赖入 main。

## D4 合并规则
契约零改动（golden 键冻结＝新场景申报）；同文件簇合单；共享面（golden/scripts/doc_budget）编排者单点。

## D5 三级验收
孙任务 exit 0（点名测试＋负锚＋消费者锚）；子域 scenarios（golden match）；变更级 DoD（lint 全绿＋全测＋golden 11+match）。
