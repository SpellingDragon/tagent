# recall-protocol Specification（MODIFIED delta）

## Purpose

读面对下层失败诚实：corruption/IO 错误具名传播不伪装 missing，Reason 与 Found 语义一致，barrier 失败不 serve 未落盘值。

## MODIFIED Requirements

### Requirement: corruption 不伪装 missing

读面 SHALL 区分真缺失（missing）与 corruption/IO 错误；后者 SHALL 具名上报，与 QueryEvents 的 ErrSegmentBodyMissing 对齐。

#### Scenario: corruption 具名

- WHEN 底层事件读取因 corruption 失败
- THEN recall_get 答具名错误而非 missing（锚）

#### Scenario: 真缺失仍 missing

- WHEN 键真不存在
- THEN 仍答 missing

### Requirement: Reason 与 Found 一致

Found=true 时 Reason SHALL 为空；二者 SHALL NOT 矛盾并存。

#### Scenario: parent 失败语义

- WHEN parent 回溯失败
- THEN Found 与 Reason 组合自洽（Found=true 则 Reason 空）

### Requirement: barrier 失败不 serve 未落盘

提交屏障 flush 失败后，内存驻留 SHALL NOT 返回未落盘值。

#### Scenario: flush 失败读

- WHEN put 后 flush 失败再读同键
- THEN 不返回未落盘值（具名错误或回退持久态）