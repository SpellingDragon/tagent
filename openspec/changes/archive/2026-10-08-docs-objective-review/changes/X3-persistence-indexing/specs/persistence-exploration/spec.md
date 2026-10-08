# persistence-exploration Specification

## ADDED Requirements

### Requirement: 写入账表交付

报告 SHALL 交付"每 turn 写入账表"：沿一次 turn 的完整链路列出每个写入调用点（存储键/生产者/同步性/触发频率），标注静态推演置信级。此表为 E4 提交边界与批量化的决策地基。

#### Scenario: 账表可稽

- **WHEN** 编排者四查本报告
- **THEN** 写入账表 ≥8 行且每行有 文件:符号 级调用点

### Requirement: 查询面与索引缺口清单交付

报告 SHALL 盘点全部 Query* API 的访问模式（点查/范围/过滤维度）与当前满足方式（索引/扫描/窗口模拟），并给出一二级索引缺口清单（如 Metadata 过滤、trace_id 反查）——此清单同时服务 X1 的 join 可行性结论。

#### Scenario: 缺口与 X1 交叉

- **WHEN** X1 核验三跳 join 中间键能力
- **THEN** 本报告查询面清单可被引用（无硬依赖，结论交叉印证）
