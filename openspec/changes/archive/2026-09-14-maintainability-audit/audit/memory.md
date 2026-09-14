# memory 族评分卡（memory + engine + kv + embedder）

base: `cf006e1` | memory 16 文件/4577 行 + engine 4/1090 + kv 2/808 + embedder 3/372 | cover: memory 75.5% | 取证: segment_store/error_tracking/mem_spill/engine_bridge 全文 + 余者结构扫与定向核对

## memory（核心包）

| 维 | 级 | 依据 |
|----|----|------|
| 复杂度 | B | segment_store.go 969 行承担存储+分段+LRU+窗口 seq 恢复多责（实为存储引擎本体，宜再分层）；余文件（tombstone/key_schema/mem_spill）职责单一 |
| 耦合 | A | 叶子包；ErrorTrackingStore 经 DegradationSink 窄接口依赖倒置避免 import reliability（error_tracking.go:33-40）——设计上乘 |
| 测试 | A- | cover 75.5%；写路径不变量（seq 恢复 D12 防覆写 segment_store.go:212-218、重放幂等预检 mem_spill.go:16-20）皆有生产教训注释与测试 |
| 文档一致 | A- | 头注释带设计出处（报告 D3/行号）密度高但含过时行号引用（报告行号会漂移）；stub（SearchByEmbedding/StoreEventWithEmbedding）显式声明不冒充实现 |
| 演进风险 | B | MemoryStore 接口宽（可选能力方法靠 stub + SupportsVectorSearch 探测）；新后端须复刻 stub 语义——接口隔离不足是长期税（见 F-7） |

发现: F-6（rustviking CLI 无超时）、F-7（MemoryStore 接口宽 stub 模式）

## memory/engine（语义引擎）

| 维 | 级 | 依据 |
|----|----|------|
| 复杂度 | A- | engine_inmemory 652 行（hybrid RRF + KV 重建）最大件；rrfFuse 纯函数（engine_inmemory.go:572） |
| 耦合 | S | C6 解耦缝契约清晰（engine.go:1-8 设计意图注释）；Capabilities/Ready 门控完备（engine_inmemory.go:250-262 重建成前退化为关键词） |
| 测试 | A- | 889 测试行含 engine_bridge「消灭 stub」验证与未接线行为逐字节不变测试 |
| 文档一致 | A | 与 wiki/platform C6 契约一致 |
| 演进风险 | A | 新引擎接入指南居 kv.go/embedder.go 判例同构 |

发现: 无

## memory/kv（KV 后端）

| 维 | 级 | 依据 |
|----|----|------|
| 复杂度 | A | local_file_kv 413 行（JSON 文件 KV）/ rustviking_client 395 行（CLI fork）各单一后端 |
| 耦合 | S | KVStore 契约居核心 kv.go，后端零相互依赖 |
| 测试 | B+ | 643 测试行；CLI fork 的超时/挂起路径无测（与本处 F-6 直接相关） |
| 文档一致 | A | 与 specs/rustviking-client 的否定性要求一致 |
| 演进风险 | B | **CLI fork 无超时（F-6）**：exec.Command 裸调（rustviking_client.go:83/108），fork 挂起→同步写路径阻塞→ErrorTrackingStore 退化防线失效（其依赖错误返回，挂起不返回） |

发现: F-6

## memory/embedder（嵌入供应商）

| 维 | 级 | 依据 |
|----|----|------|
| 复杂度 | S | zhipu/mock/traced 三实现各 ~100-200 行 |
| 耦合 | S | Embedder 契约居核心 embedder.go |
| 测试 | A- | 285 测试行（10.6s race 通过——含真实 API 契约测试） |
| 文档一致 | A | 与 D2 设计的 provider/model/dimensions 配置一致 |
| 演进风险 | A | 新供应商判例同构 |

发现: 无
