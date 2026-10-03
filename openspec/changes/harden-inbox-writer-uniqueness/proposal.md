# Proposal: inbox 写者唯一性与 race 门盲区（CI 固化）

## Why

CI run `36982781005` 在 `tests/resident_e2e` 上暴露了一组**真实并发数据破坏面**，不是测试噪声：

- `writeEnvelopeFile`（`agent/reliability/inbox.go:1035-1038`）的临时文件名唯一性只到 **pid 粒度**（`path + ".%d.tmp", os.Getpid()`），而它是**包级自由函数**，不在任何 `Inbox.mu` 覆盖之内。同进程两个实例、或多进程共享 spillDir，会撞同一个 tmp 名。
- 撞名的后果被错误路径放大：每条错误路径都 `os.Remove(tmp)`（`:1044/1048/1052/1059`），而名字是共享的——A 失败清理时删掉的是 **B 正在写的 tmp**，B 随后 `os.Rename` 得到 `no such file or directory`。这正是 CI 上「先 file exists、后同路径 no such file」两段式症状的机制。
- 两实例的 `seq` 各自计数，最终路径 `<020d>.json` 也会相同：即使 tmp 不撞，后者也会**静默覆盖**前者的信封。
- `NewInbox` 只 `MkdirAll` + sync，**没有目录独占声明**：一个目录一个属主只是隐含语义，没有任何东西强制它。

同一次诊断还确认：CI 的 race job 包集合**不含根包 `.`**（`.github/workflows/ci.yml:99`）——「race 全绿」对根包内的跨 goroutine 缺陷零证据力，而上一变更修的恰好就是根包用例里的交错。

## What Changes

- **产品**：tmp 名携带写者身份（pid + 实例随机标识 + 原子序号），错误路径只删除自己创建的 tmp；`NewInbox` 以非阻塞 flock 声明 spillDir 单属主，第二个打开者得到具名错误（非 unix 平台降级为 no-op 并文档化）。
- **测试**：`resident_e2e` 不再对活跃目录开第二个 `Inbox` 当只读探针（打开即写 requeue），改用总线暴露的只读诊断（`DurablePending`）。
- **CI 固化**：race job 的包集合加入根包 `.`；加入前先在本地以同一命令证明通过。

## Impact

- Affected specs: `persistent-event-loop`（ADDED：inbox 写者唯一性与目录单属主）、`race-gate-coverage`（新 capability：race 门覆盖第一方全包）
- Affected code: `agent/reliability/inbox.go`、`tests/resident_e2e_test.go`、`.github/workflows/ci.yml`
- 不改 envelope 的文件格式与目录布局；不引入新依赖
