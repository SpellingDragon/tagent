# Tasks

## 1. fail-before（先证缺陷）

- [ ] 1.1 `agent/reliability` 单测：同目录两 `Inbox` 并发对同一最终路径写信封——断言旧实现出现 tmp 撞名/互删（`file exists` 或对端 `no such file`）
- [ ] 1.2 `agent/reliability` 单测：对已持有目录再 `NewInbox`——断言当前实现静默成功（事故门敞开）

## 2. 产品

- [ ] 2.1 `Inbox` 铸造 writerID（pid+随机+原子序号源）；`writeEnvelopeFile` 走写者标识的 tmp 名；错误路径只删自己的 tmp
- [ ] 2.2 `NewInbox` 非阻塞 flock 单属主（`//go:build unix` 承载，非 unix no-op 并 Godoc 标注）；`Close` 释放；第二个打开者具名错误
- [ ] 2.3 1.1/1.2 转绿；`go test ./agent/reliability/ -race` 全绿

## 3. 测试探针去写副作用

- [ ] 3.1 `tests/resident_e2e_test.go` 以 `DurablePending()`（或补只读导出）替换第二个 `NewInbox` 探针；`./tests/ -short` 绿

## 4. CI 固化

- [ ] 4.1 本地 `./scripts/race_check.sh .` 通过（或按纪律处置：第一方修/上游登记 waiver）
- [ ] 4.2 `.github/workflows/ci.yml` race job 包集合加 `.`，命令与本地同参；lint/openspec strict 全绿
- [ ] 4.3 push 并确认 CI 四 job 绿
