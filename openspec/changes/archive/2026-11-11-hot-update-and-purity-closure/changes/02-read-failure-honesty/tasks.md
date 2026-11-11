# Tasks: 02-read-failure-honesty

- [x] 2.1 立读面症状锚红测试：corruption 不得伪装 missing —— 验证：`go test ./tool/recall/ -run TestRecallCorruptionNotMissing -count=1`（基线红：`reason:missing`；现 exit 0）
- [x] 2.2 GetEvent 错误具名传播（read.go Event/Reference/Text 不吞错） —— 验证：2.1 转绿 + 负 `go test ./assembly/ -run TestReadCorruptionNamedError -count=1`（exit 0）
- [x] 2.3 get.go Reason/Found 语义对齐 —— 验证：`go test ./tool/recall/ -run TestRecallReasonFoundConsistent -count=1`（exit 0）
- [x] 2.4 kvCommit barrier 失败 resident 不 serve 未落盘值 —— 验证：负硬门 `go test ./memory/ledger/ -run TestLocalFileFlushFailNotServed -count=1`（exit 0；回滚修复时红）
- [x] 2.5 消费者锚：recall_get 公共工具入口 corruption→具名错误负例 —— 验证：`go test ./tool/recall/ -run TestRecallGetPublicCorruption -count=1`（公共入口非 helper；exit 0）