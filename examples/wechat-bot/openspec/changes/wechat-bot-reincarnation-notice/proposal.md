# wechat-bot 自替换换装通报（REINCARNATION NOTICE）钩子

## Why

2026-09-11 13:58 保险链（restart-maintenance.sh cron）自替换换装：旧进程（pid=2061791）死前正在向用户承诺"等结算唤醒"，换装后新进程（pid=2185305）对这句承诺零感知——事件流里既没有旧进程的临终遗言，也没有新进程的"我是谁、我从哪来"通报，用户三连追问才暴露此缺口。本计划补上自替换的"换装通报"钩子：新进程启动时若检测到刚发生过保险链换装，主动生成一份转世通报注入自己的事件流，让转世后的 agent 至少知道"上世怎么死的、何时换的装、详细档案在哪"。

## What Changes

- `main.go` 新增启动期检测：进程启动时读 `run/restart.done` 的 mtime（新鲜度阈值 10 分钟），且 restart.done 中 PID ≠ 自身 PID；命中则读 `run/REINCARNATION_NOTICE` 详情文件生成通报文本，经框架现有内部事件通道（`InjectMessageWithSource`，非 user source，不 arm meditation novelty gate）注入事件流
- `restart-maintenance.sh` SUCCESS 分支（healthz 200 后）同步写 `run/REINCARNATION_NOTICE`：换装时间、旧/新 PID、触发原因（sentinel 失活/进程不存在）、指向 restart.log 与构建产物的指针
- 走完整门禁（`go build ./...` + `go vet ./...` + `go test ./...`）后提交推送 dev 分支
- 用保险链脚本本身完成自替换部署（dogfood）：把新二进制落到磁盘（mv 原子替换或 kill 旧进程触发保险链），验证新进程上线后通报真实抵达事件流（日志可见 reincarnation notice 事件被消费）

## Impact

**涉及文件：**
- `tagent/examples/wechat-bot/main.go` — 新增 startup reincarnation-notice 检测与注入（新增函数，不动现有 consumer 路由逻辑）
- `tagent/examples/wechat-bot/restart-maintenance.sh` — SUCCESS 分支新增写 REINCARNATION_NOTICE
- `tagent/examples/wechat-bot/run/REINCARNATION_NOTICE` — 运行时产物（换装档案文件，SUCCESS 时生成）
- 框架 `tagent/agent/inject.go` 的 `InjectMessageWithSource` — **只读复用，不修改**（本计划零框架侵入）

**明确不涉及：**
- 不改框架 tagent 仓库本体（main.go 调用现有 API，无 rl 包/agent 包变更）
- 不改 run.sh / crontab / 锁路径 / 哨兵机制（保险链 v2 现状维持）
- 不做旧进程临终遗言捕获（那是另一个问题：旧进程被 SIGTERM 时无法保证落盘，本次只解决新进程侧的知情权）
- 不做跨文件系统的重启/恢复模拟（本地 go test 单测覆盖检测逻辑即可）

**最终产物：** 下一次保险链自替换后，新进程启动即知自己"转世"了：事件流里出现一条带时间/PID/触发原因/档案指针的转世通报，agent 可据此向用户销假或至少知道去找 restart.log；不再发生"上世承诺蒸发、转世一无所知"的空窗。
> 2026-09-11 升级（D8）：通报正文含 WAL 尾现场块——新进程醒来即持死前现场与断点标记，零探针续作；降级阶梯见 design D8。
