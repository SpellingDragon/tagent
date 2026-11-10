# bounded-compression delta

## ADDED Requirements

### Requirement: 投影压缩有界且幂等

投影压缩 SHALL 把任意长历史压进固定上下文预算：投影渲染体积 SHALL ≤ 预算，老带摘要 Text SHALL ≤ 界，二者均 SHALL NOT 随历史长度增长。压缩 SHALL 有界且收敛：fold 迭代 SHALL 收敛到稳定不动点（存在 n 使 `fold^n(x) == fold^(n+1)(x)`），体积全程 SHALL NOT 超界；不要求严格即时幂等（世代晋升下首次折叠晋升材料、后续折叠到稳定态）。事实链 SHALL 是唯一真相，摘要 SHALL 只携有界表示与完整 Covered keys，原文 SHALL 可按 Covered key 回链查询。

#### Scenario: 极大历史投影有界
- **WHEN** 会话累积远超预算的 turn 后触发压缩
- **THEN** 投影渲染体积 SHALL ≤ 预算，老带摘要 Text SHALL ≤ 界，不随 turn 数增长

#### Scenario: 重复折叠收敛
- **WHEN** 对材料连续施加折叠
- **THEN** 体积 SHALL 全程不超界，且迭代 SHALL 收敛到稳定不动点（某次起 `fold^n == fold^(n+1)`），SHALL NOT 累积膨胀

#### Scenario: 原文回链可查
- **WHEN** 摘要覆盖了一批原文条目
- **THEN** Covered SHALL 列出全部被覆盖条目身份，原文 SHALL 可按身份从事实链读回

### Requirement: 三段龄按世代递推分职

三段龄 SHALL 按折叠世代递推定义：新 SHALL 为 recent 保留段（原样）；中 SHALL 为上一次折叠时在新带的段（这次折叠过程细节，留 input/output 骨架）；老 SHALL 为上一次折叠时已在中/老带的段（这次摘要）。每触发一次折叠 SHALL 使材料晋升一级（新→中→老）。band 归属 SHALL 由上一次折叠的归属决定，SHALL NOT 由绝对段数位置决定。折叠（中，结构重组）与摘要（老，有界压缩）SHALL 是两种分职操作。

#### Scenario: 逐级晋升
- **WHEN** 连续触发多次折叠
- **THEN** 同一批材料 SHALL 依次经历 新（原样）→中（折叠过程细节）→老（摘要），band 由上次折叠归属递推

#### Scenario: 新带不被折叠过程细节
- **WHEN** 一段处于新带
- **THEN** 它 SHALL 原样保留，SHALL NOT 被折叠过程细节

### Requirement: 老带摘要 LLM 语义优先且有界降级

老带摘要 SHALL 优先用 LLM 生成有界语义 gist 并作为摘要事实落链；恢复与回放 SHALL 读已存储的摘要事实（确定性），SHALL NOT 重新生成。无模型配置时 SHALL 降级为机械有界表示（recent-K 行加 Covered-key 引用，K 由预算派生）。LLM 摘要失败 SHALL 降级机械有界表示或记欠账，SHALL NOT 阻塞或累积。摘要上一摘要 SHALL 产出有界 gist，SHALL NOT 展开抄全文。

#### Scenario: 有配置走 LLM 摘要事实
- **WHEN** 配置了模型且老带需摘要
- **THEN** SHALL 由 LLM 生成有界 gist 落摘要事实，回放读该存储事实

#### Scenario: 无配置机械降级
- **WHEN** 未配置模型
- **THEN** SHALL 降级机械有界表示（recent-K 行＋Covered-key 引用），体积 SHALL ≤ 界

#### Scenario: 摘要不累积
- **WHEN** 老带含上一摘要再次摘要
- **THEN** 上一摘要 SHALL 贡献有界表示而非全文，摘要体积 SHALL 稳定

### Requirement: 压缩恢复 parity

锚定重建 SHALL 与全量走查逐项一致（二者施加同一有界折叠、读同一摘要事实）。压缩确定性 SHALL 保证崩溃恢复重现同一投影。

#### Scenario: 锚定与全量一致
- **WHEN** 同一持久根分别经锚定重建与全量走查恢复
- **THEN** 投影呈现、判决集、landed 序列 SHALL 逐项相等