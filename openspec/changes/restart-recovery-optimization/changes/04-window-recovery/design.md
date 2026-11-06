# 04-window-recovery 设计

## D1 机制界不可配，策略可配

- **机制界**（内存、缓存、页界、输出余量、provider 窗）：由回放窗派生或降为不可配常量。
- **策略**（压缩时机与保留深度）：`compress_threshold` 与 `keep_recent_tasks` 保留可调——它们决定压缩程度，是产品策略，不是界。

## D2 窗口类界收口表

| 现名 | 处置 |
|---|---|
| `max_tokens` | **保留**，唯一窗口输入 |
| `compress_threshold` / `keep_recent_tasks` | **保留可调**（策略） |
| `DefaultCompressThreshold` / `DefaultKeepRecentTasks` | **保留**（策略默认值） |
| `provider_context_window` | 删，provider 窗=回放窗 |
| `output_reserve` / `DefaultOutputReserveDivisor` | 删键，输出余量=窗/4 机制常量 |
| `long_context_tokens` / `DefaultLongContextTokens` | 删键，降为策略：值由装配剩余量算出 |
| `capture_budget_mb` | 删键，派生为窗口渲染字节×固定倍数 |
| `vector_top_k` / `keyword_top_k` | 删键，收敛为一个页界 |
| `rrf_k` / `RRFRankConstant` | 删键，RRF K 为算法常量 |
| `factWindow` / `replayResidentWindow` | 删，常驻=窗口渲染集 |
| `BudgetCharsPerToken`=4 | 保留，唯一换算常量 |
| `DefaultAgeVolumeThreshold` / `DefaultAgeKeepRecent` | **保留**（属于压缩策略的兜底） |

## D3 压缩策略零改动（明确不动）

以段为单位处理；`keep_recent_tasks` 开关决定最新段有多少原样保留；更老的段由分龄推断层级（段产物 / 历史摘要）；阈值决定何时开始压；提交后判卷落 `compress_request`、消费者落 `compress_product`、`ApplyProducts` 替换投影、原文不删。以上代码路径、默认值、spec 条款都不改。

## D4 recall 与窗口解耦

观察表（`memory/store_query.go`）退役为查询缓存：启动零 land；查询把过滤条件翻成持久层的键范围（分区 + 时间），按范围读正文解码、过滤、排序、分页；缓存 LRU 可丢，不是真源。语义召回不持有 runtime 窗口向量，也不建全史向量表：查询时对本次候选集现场算向量并重排。

## D5 窗口内重建

窗口起点=链上压缩产物的覆盖边界 + 最近未闭合段；只折窗口内的段并挂产物。失败退回：产物或边界不可得、版本不符、坏分片首次读失败→退全量重建（今天的路径原样保留），具名告警，结果与窗口路径逐面一致。

## D6 词表与门禁

四类词：**回放窗（max_tokens）/ 段窗（WindowTS）/ 上限（budget，不可配）/ 策略（policy，可配）**。F0 新增一条规则：任何新增「上限类」可配项即红；界必须登记来源与派生关系；策略项必须显式登记为策略。现有界与策略登记为基线。

## D7 不改的东西

事件契约、事实族、插件管线、交付/冥想语义、压缩策略；「分区位图/Init 游标」预案按实测删除。
