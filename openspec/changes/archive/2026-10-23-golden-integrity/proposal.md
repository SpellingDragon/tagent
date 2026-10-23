# golden-integrity：golden 特性默认生效——三龄段接线＋窗口驱动＋replica 裁决执行

## 标准（用户裁定）
golden 特性必须默认实现——不存在"要不要接线"的问题；零生产调用＝缺陷。

## 缺陷清单（同标准全量复盘）

| # | 缺陷 | 证据 | golden 承诺 |
|---|---|---|---|
| D1 | 三龄段折叠零生产调用 | FoldByAge 仅测试调用；recordMapped 不触发 | S14（age_fold_products/history_summary_leads/under_threshold_untouched）＋S13（folded_summary_leads_projection） |
| D2 | replay_budget 人造中间概念 | 独立于模型窗口，未配置＝失忆 | 已裁定删——窗口驱动（max_tokens 唯一声明） |
| D3 | memory/replica 半删状态 | lean-sweep design 裁定硬删但 7 文件在；生产消费者 0；S07 场景测试是唯一"消费者"（测试冒充生产） | S07 需改写（original store owner isolation 保留，replica 特定观察删） |

## 修法

### G1 三龄段接线＋窗口驱动（核心单）
- 删 AgentSpec.ReplayBudget（config/decode/fingerprint 全链）
- 投影注册条件：max_tokens>0 即注册（声明记忆即记得）
- 新配置（术语对齐旧版 f300a25）：compress_threshold（缺省 0.8，占窗口比例）＋keep_recent_tasks（缺省 2）——零配置全默认生效
- 接线（插件入流——易理解可维护）：AgeFoldPlugin 实现 plugin.Instance＋PostCommitObserver（与冥想 Driver 同端口同模式）——ObserveCommit → 检查投影体积（窗口余量×compress_threshold）→ 超阈 FoldByAge → compress.request 事实落链。recordMapped 保持纯净（只 Append）；所有响应事实提交的行为走 pipeline.After 统一分派
- 装配预算＝窗口余量推导（prompt/环境块/transcript 外的余量）
- 两道防线固定序：先折叠（材料层）后降级（呈现层）

### G2 replica 裁决执行＋golden 对齐
- 删 memory/replica 包（7 文件）＋memory/compress_filing.go＋contract 范例
- S07 场景改写：保留 original store 的 owner isolation/same-bytes（ledger 能力），删 replica 特定观察（副本恢复/清副本召回）
- golden architecture.json 同步删包

## 验收（三点锚）
- 正锚：S14 attestation 走生产路径——recordMapped 提交足够事实后投影自动出现 fold products（非测试直调 FoldByAge）
- 负锚：under_threshold_untouched——体积不达阈投影原样（不误折）
- 自愈锚：折叠后 Rebuild 重建→再触发→幂等同 key
- 38 包绿＋golden deterministic match＋lint ok
