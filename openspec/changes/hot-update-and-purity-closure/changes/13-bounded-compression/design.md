# 13-bounded-compression 设计（冻结契约）

> 合并门根因修复：长程校准压缩产物涨到 88MB 超帧上限。根因＝三段龄名存实亡（级别算了但 FoldByAge 只分新/非新，中+老塌进一条摘要）＋摘要累积全文（historySummaryLine 对上一产物返回 entry.Content 全文，每摘一次抄一遍）。用户裁决：世代递推三段＋老带 LLM 语义摘要优先（无配置才机械降级）＋有界幂等。

## D1 第一性原理与硬不变量
投影是模型读的有界派生视图；链是全量真相。压缩职责＝把任意长历史压进固定上下文预算，保住按需回链查原文。
1. **有界**：投影渲染体积 ≤ 预算，与历史长度无关；老带摘要 Text ≤ 界。
2. **有界＋收敛**（r49 裁定，取代严格 fold²==fold）：投影/摘要体积永不超界；fold 迭代收敛到稳定不动点（存在 n 使 `fold^n(x) == fold^(n+1)(x)`）。**不要求严格 `fold(fold(x))==fold(x)`**——世代晋升下首次折叠把材料晋一级（新→中），第二次再晋（中→老）才到稳定态，故 fold²≠fold¹ 但 fold³==fold²，体积全程有界。真正不变量是「不累积膨胀＋收敛」，不是即时幂等（88MB 根源是累积，已治）。
3. **链是真相**：摘要只携有界表示＋完整 Covered keys；原文按 key 回链查，摘要不囤全文。
4. **逐级保真**：越新越全、越老越简。

## D2 三段龄＝世代递推（用户定义，取代绝对段龄位置）
每次到达阈值触发折叠时：
- **新**＝recent 保留的那批（keepRecent 最新段），原样。
- **中**＝上一次折叠时还在「新」的（上次新 − 本次新），做**折叠**（foldProcessDetail：丢 thinking/tool 命令，留 input/output 骨架；结构性重组，内容基本保全）。
- **老**＝上一次折叠时已在「中/老」的，做**摘要**（summarization，减内容的有界压缩，与折叠是两种操作）。
晋升节奏 tied to 折叠事件：新→中（折叠）→老（摘要），每折一次晋升一级。实现需记录上一次折叠的 band 归属，本次据此晋升（非按绝对段数 LevelOfAge）。

## D3 老带摘要＝LLM 语义摘要优先（设计本意）
- **正常路径**：LLM 生成有界语义 gist，**作为摘要事实落链**。回放/恢复读已存储的摘要事实（确定）→ parity（锚定≡全量）天然成立（两路读同一条摘要事实，非重新生成）。摘要生成在持模型访问的层（assembly）触发，memory/replay 消费已落链摘要事实，保层纯度与恢复确定性。
- **降级路径**（无模型配置）：机械有界表示——recent-K 行（foldLineOf 的 `[time|gen] type: summary`）＋超界最老行退化为 Covered-key 引用（原文在链上）。K 由预算派生，登记 window_bounds。
- **失败语义**：LLM 摘要失败→降级机械有界表示或记欠账待补（不阻塞、不累积）。
- **有界幂等**：摘要上一摘要时产出有界 gist 而非展开抄全文；再摘一次体积稳定。

## D4 折叠/摘要分职（修正当前错位）
- 当前代码错：foldProcessDetail 作用在 BandNew（应保留），中+老全塞 historySummaryOf。
- 修正：FoldByAge 按 Band 分流——新保留 / 中 foldProcessDetail / 老 摘要。LevelOfAge 的绝对位置分级由 D2 世代递推取代。
- historySummaryLine 对上一产物**不再返回 entry.Content 全文**，改有界引用（LLM gist 或机械 recent-K＋key）。

## D5 配套
- 帧上限回退合理值（产物有界后不需 64MB 创可贴）。
- async filer 的 parity off-by-one（TestDefaultBootRebuildsFromTheAnchor 全走 93 vs 索引 92）一并修。
- fold golden（fold_plan_golden/fold_contract）随有界摘要重生成。

## D6 验收锚（红→绿）
- 有界：N 极大 turn 后投影体积 ≤ 预算、摘要 Text ≤ 界。
- 收敛：fold 迭代到稳定不动点（fold^n==fold^(n+1)），体积全程有界（r49：不要求严格 fold²==fold）。
- parity：锚定重建 ≡ 全量走查（同施加有界 fold，读同一摘要事实）。
- Covered 完整：所有原文按 key 可回链查。
- LongRun：产物有界、boot 平坦、跑完整代（121）。

## D7 行为变更（已裁决接受）
模型上下文从「逐字全史」变为「近期逐字（新）＋中段骨架（中）＋老段 LLM 有界摘要（按需回链查原文）」。符合有界派生视图哲学。