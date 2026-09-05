# recall-hybrid-fusion Specification (Delta)

## ADDED Requirements

### Requirement: query 模式 RRF 融合
recall 统一入口的 query 模式在向量可用时 SHALL 执行 hybrid 检索:向量 topK 与分词关键词 topK 并行取回,按 RRF(rank 倒数和,k=60)融合重排,截断至调用 limit。向量不可用(未配置/重建中/该分区无向量)时 SHALL 自动退化为纯关键词路径,行为与现状一致。

#### Scenario: 同义改写可召回
- **WHEN** 历史事件内容为"部署失败回滚",查询词为"上线出问题撤回了"(无共同关键词)且 embedding 已配置
- **THEN** 融合结果包含该事件的 EventReference

#### Scenario: 无向量时退化
- **WHEN** embedding 未配置,执行相同 query
- **THEN** 结果与现状纯关键词行为逐字节一致

### Requirement: 两段式召回哲学保持
融合结果 SHALL 保持 EventReference 票据形态;全文取回仍经既有 GetEvent/recall items 路径。语义检索 MUST NOT 直接返回事件全文。

#### Scenario: 融合结果为票据
- **WHEN** hybrid 检索命中
- **THEN** 返回含 EventKey/EventSummary 的引用列表,全文需经票据二次取回

### Requirement: recall 工具声明恒定
hybrid 升级 MUST NOT 改变 recall 工具的 Declaration(名称/描述/InputSchema);融合逻辑全部位于工具实现内部。

#### Scenario: 声明区零变化
- **WHEN** 对比 embedding 配置开启前后 recall 工具的 Declaration JSON
- **THEN** 完全一致(prefix-cache 不变量)
