# Tasks

## 1. fail-before（先证缺陷）

- [x] 1.1（同目录两实例的并发形状改由"对端残骸不得堵死合法写入"表达（flock 后第二实例已不可能存在），实测 RED） `agent/reliability` 单测：同目录两 `Inbox` 并发对同一最终路径写信封——断言旧实现出现 tmp 撞名/互删（`file exists` 或对端 `no such file`）
- [x] 1.2（实测 RED：`An error is expected but got nil`） `agent/reliability` 单测：对已持有目录再 `NewInbox`——断言当前实现静默成功（事故门敞开）

## 2. 产品

- [x] 2.1（writerID=pid+crypto/rand，tmp 名 `path.writerID.seq.tmp`；错误路径只删自己的 tmp（实测 7 处调用点全部改经属主方法）） `Inbox` 铸造 writerID（pid+随机+原子序号源）；`writeEnvelopeFile` 走写者标识的 tmp 名；错误路径只删自己的 tmp
- [x] 2.2（`acquireDirLock` 非阻塞 flock（`//go:build unix` + 非 unix no-op）；`ErrInboxOwned` 具名拒绝；`Close` 交还；**实现期发现并修掉自己引入的锁泄漏**：打开成功后若后续步骤失败必须交还声明（deferred on retErr）） `NewInbox` 非阻塞 flock 单属主（`//go:build unix` 承载，非 unix no-op 并 Godoc 标注）；`Close` 释放；第二个打开者具名错误
- [x] 2.3（两条转绿；`go test ./agent/reliability/ -race` 16.5s ok） 1.1/1.2 转绿；`go test ./agent/reliability/ -race` 全绿

## 3. 测试探针去写副作用

- [x] 3.1（探针移至 `ta.Close()` 之后；`TestResidentDurableE2E_FiveSurfaceReconciliation` 实测 PASS(0.24s) 非跳过） `tests/resident_e2e_test.go` 的 `Outstanding()` 探针移到 `ta.Close()` 之后（唯一属主，盘上状态不变）；`./tests/ -short` 绿
- [x] 3.2（两处 reopen 改为先 `Close` 再重开） `agent/reliability/inbox_test.go` 两处 reopen（`CorruptItemQuarantined`／`UnknownVersionQuarantined`）改为先 `Close` 再重开
- [x] 3.3（`execution_gate:841`/`restart_matrix:141` 改经 `durableInbox()`（包内属主句柄，未新增导出面）；父进程每轮检视后 `CloseDurable` 交还——这是 round 2 子进程打不开目录的真因） `agent/restart_matrix_test.go:141` 与 `agent/execution_gate_test.go:841`：借第二实例写收据处改为"先释放前一属主再打开"，崩溃语义不变
- [x] 3.4（已写入 design D6：生产 `build_agent.go` 仅常驻 owner 领 `BusSpillDir`；`seedUnackedEnvelope` 无并发属主，实测根包全绿佐证） 普查结论入档：生产 `build_agent.go` 仅常驻 owner 领 `BusSpillDir`，flock 不改生产行为；`org_candidate.seedUnackedEnvelope` 无并发属主，不动

## 4. CI 固化

- [x] 4.1（`./scripts/race_check.sh .` 本地通过（74.4s，race_check: OK）） 本地 `./scripts/race_check.sh .` 通过（或按纪律处置：第一方修/上游登记 waiver）
- [x] 4.2（ci.yml race job 包集合加入 `.`；lint ok（门禁期间修掉自己写的 8 处游离注释与 1 处测试 doc 形状，未放宽规则）；`gen_godoc --check` 匹配） `.github/workflows/ci.yml` race job 包集合加 `.`，命令与本地同参；lint/openspec strict 全绿
- [x] 4.2a 门结构修正：`race_check.sh` 加 `-p 1`（跨包并发在同一 tmux 服务器上互相收割活会话，见 D7）；`scripts/test_race_check.sh` ALL PASS；CI 同参本地全量 `race_check: OK`
- [x] 4.2b 纠正 README 的失真判据（"会话型 tmux 测不进 CI"只对 test job 成立，race 门不带 `-short`）
- [x] 4.3 tip 0e4fb95 CI 四 job 全绿（test/validators/race/openspec），race job 首次覆盖根包并在 -p 1 下稳定
