# Design: 02-read-failure-honesty

## 技术要点

- read.go Event/Reference/Text：GetEvent 错误区分 not-held 与 corruption/IO，后者具名传播（与 QueryEvents 的 ErrSegmentBodyMissing 对齐），get.go 据此答具名错误而非 missing。
- get.go：Found=true 时 Reason 必空；parent 失败若 Found=true 则不设 relation_error（或改 Found=false），注释与实现一致。
- localfile.go kvCommit：flush 失败时 resident 不保留未落盘 put（回滚 resident 或标记不可读），读不 serve 未持久值。

## 文件清单

assembly/read.go、tool/recall/get.go、memory/ledger/localfile.go、对应测试。

## 风险与回退

- 风险：corruption 具名化可能使原先"missing"调用方改收到错误；属契约收紧，调用方按具名错误处理（负锚钉住）。
- 回退：单域回滚；missing/expired 语义对真缺失保持不变。