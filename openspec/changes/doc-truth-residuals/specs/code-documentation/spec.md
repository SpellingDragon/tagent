## ADDED Requirements

### Requirement: 等价对账仪器的判据完整性

注释/测试合并的等价判定仪器（`scripts/codetools` merge-check 与 comment-check）SHALL 满足：空白折叠 MUST NOT 作用于字符串字面量内部（反引号原始字符串的内容差异不得被归一吞掉）；注释剥离 SHALL 覆盖行尾注释（含类型声明行的行尾注释，其删除不得被判为 body-changed）；`--map`/`--explain` 的单值语义与"每包一次调用配自己的 map＋explain"的正确形态 MUST 写进用法行自述；explain 豁免只作用于非 Test 声明的边界 MUST 在台账中随 Test 类残留一并声明处置路径（补 map 行／换基线口径／另案定性），MUST NOT 以"登记 explain"冒充 Test 类缺口的修复。

#### Scenario: 原始字符串内容变化不被归一吞掉

- **WHEN** 合并前后的两个测试在某反引号原始字符串内仅有空白差异
- **THEN** 等价判定 MUST 报告该差异，MUST NOT 因空白折叠把它判为相同

#### Scenario: 行尾注释删除不误报为正文变化

- **WHEN** 某类型声明仅删除行尾注释（如 `latencies []float64 // milliseconds` → `latencies []float64`）
- **THEN** merge-check MUST 判该声明 comment-only，MUST NOT 报 body-changed
