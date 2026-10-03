# Design

## D1 tmp 名携带写者身份，错误路径只删自己的

`writeEnvelopeFile` 从包级自由函数改为持有写者标识的调用：`Inbox` 在 `NewInbox` 时铸造一次 `writerID = pid + 随机 64 位 + 进程内原子序号源`，tmp 名为 `path + "." + writerID + "." + seq + ".tmp"`。此后：

- 同进程多实例、跨进程共享目录都不再撞名；
- 错误路径的 `os.Remove(tmp)` 删的必然是**自己刚创建**的那个文件——「互删在途数据」这一竞态被结构性消除，而不是被 try-and-hope 掩盖。

被否方案：改用 `os.CreateTemp`（随机后缀等价，但拿不到「同写者序号」这一可读性，且错误信息里失去稳定前缀，排查时无法区分是哪个写入者留下的残骸）。

## D2 目录单属主由 flock 声明，而不是由约定看守

「一个 spillDir 一个属主」是既有语义（恢复动作带写副作用决定了它）。本变更把它变成**机器强制**：`NewInbox` 在 `<dir>/.owner` 上取非阻塞 flock；拿不到即返回具名错误（含持有者 pid，若锁文件可读）。`Close` 释放锁。

- 实现：`//go:build unix` 文件承载 `syscall.Flock`；其余平台提供语义为「恒成功」的 no-op 并在 Godoc 标注——锁是防事故的栏杆，不是可移植性承诺的 hostage。
- 被否方案：锁文件 `O_CREATE|O_EXCL` + 心跳/陈旧判定——引入超时语义与孤儿锁回收问题，复杂度远超收益；flock 由内核在进程死亡时自动释放，正是这里的正确工具。

## D3 活跃目录上的访问一律经唯一属主

`resident_e2e` 原本在同目录开第二个 `Inbox` 读 `Outstanding()`，但 `NewInbox` 的打开即执行 claimed→pending 的 requeue 写——探针带着写副作用，还正是 CI 上撞名的两个写入者之一。flock 落地后这类用法一律改造，**不新增导出 API**：

- 只证"ack 后无残留"的探针：移到 `ta.Close()` 之后再打开（`Inbox.Close` 明确不动盘上未确认项，语义保真），此时它是唯一属主；
- 需要属主写面（写收据）的崩溃重启模拟：同样先释放前一属主再打开，落盘状态由前一段生命周期写好，被测语义不变；
- 中途观测计数：走总线已有的只读面 `DurablePending()`。

被否方案：把活跃 inbox 的句柄导出（`EventBus.DurableInbox()`）——那等于把属主写权发给任意调用方，与本变更要立的"一目录一属主"直接对冲。

## D6 实现期发现的迁移面（比立法时预估大）

立法只点到 `resident_e2e` 一处。落地前普查 `NewInbox` 全部调用点（25 处），确认生产侧本就单属主：`build_agent.go` 只在 `!mode.isExecutorShell()` 时下发 `BusSpillDir`，热更壳不领目录，故 flock 不改变生产行为。测试侧需要改造的是四处"在同目录上开第二个实例"的用法：`inbox_test` 两处 reopen（未 Close 就重开）、`restart_matrix:141` 与 `execution_gate:841`（借第二个实例写收据）、`resident_e2e:372`（探针）。另确认 `org_candidate.seedUnackedEnvelope` 播种时目录无活属主，不动。

## D4 fail-before 的构造

1. **互删竞态**（修复前红）：同一目录两个 `Inbox` 实例并发对同一 `path` 写信封——旧实现 tmp 撞名，断言必见 `file exists` 或对端 `no such file`；修复后 tmp 不撞、双方 rename 各自成功（D1 层）。
2. **单属主**（修复前红）：对已打开实例的目录再 `NewInbox`——修复前成功（正是事故门），修复后得到具名错误且原实例不受影响。
两条都以普通单测落 `agent/reliability`，不依赖真机时序。

## D5 race 门先本地证明、再上 CI

加根包 `.` 前必须本地跑通 `./scripts/race_check.sh .`（exit-code fidelity 是合同）。若根包出现**上游签名**的 race，走既有 waiver 机制（`raceEnabled` build-tag 逐测排除 + 登记台账），不得因加包而放水；出现第一方 race 则先修后上。CI 的 race job 与本地命令必须同参。
