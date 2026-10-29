> **活契约页**：逐条对齐当前码面（`tool/exec/` 的 `stop.go`／`monitor.go`／`tmux.go`／`tmux_executor.go`／`tmux_command.go`／`state.go`），代码内 `// 契约:` 注释指向本页锚点。执行面其余契约（资源、租约、工具声明）见 [tool-architecture.md](tool-architecture.md#action-tool)。

# tmux 会话：怎么算活、怎么算完、怎么算停住了

本页只管一件事：一次外部执行在 tmux 里被**观测**成什么样。判据的失效表现不是报错，而是误杀健康任务，或把一次没确认的退出报成干净退出。

<a id="quiet-vs-dead"></a>
## 一、静默不等于假死

`Monitor.Poll` 每次取一帧（`Capture`）并探一次活（`Probe`），按"这帧已经安静多久"分类（`MonitorStatus`）：

| 观测 | 判定 | 为什么 |
|---|---|---|
| 帧无变化、进程仍活、安静时长未到稳定窗 | `watch` | 长编译/训练/下载的静默是常态，不是异常信号 |
| 帧无变化、进程仍活、安静时长到稳定窗 | `stable` | 只说明"它安静了"，**不说命令成功** |
| 进程仍活、安静时长越过 `FakeDeadDuration` | `suspect` | 诊断升级（可能无响应），**不是死亡裁决、不击杀** |
| 进程已不在（`ProbeDead`） | 不判 `suspect` | 已停的东西不能再被当成"假死"，终态帧仍按 `stable` 交付 |

三条机械规则：**首轮 `Poll` 只锚帧**（`changedAt` 设为当下）；**帧一变就重置静默起点**（静默按"最近变化"计，不按时间累计）；`Interval` 到期才轮一次，节奏属于这次获取。

<a id="probe-three-states"></a>
## 二、探活三态：判不了就报判不了

`ProbeState` 三值，`ProbeUnknown` 是零值：**没能判定的探测会是 unknown，绝不是 dead**。`TmuxExecutor.Probe` 的判据单源：

- 会话不在 `list-sessions` 名单里 ⇒ `ProbeDead`（附原因，供 `Explain` 读）；
- `#{pane_dead}` 为 1 ⇒ `ProbeDead`（命令已退出、pane 被 `remain-on-exit` 留住）；
- tmux 客户端不可用/输出不可辨 ⇒ `ProbeUnknown`，只升级诊断。

不向会话注入回显来判活：往正在编译的进程标准输入塞字符会**污染它的输入流**，而 pane 退出状态本身就是答案。

## 三、节奏是声明的一部分

`MonitorConfig` 四个值：`Interval`（轮询间隔）、`StableDuration`（普通会话稳定窗）、`InteractiveStableDuration`（TUI 稳定窗）、`FakeDeadDuration`（安静越此阈且仍活即 `suspect`）。`DefaultMonitorConfig` = 30s / 60s / 90s / 120s；`MonitorConfigFrom` 只覆盖声明给出的键，**未知键忽略**，零值字段各取自身默认。监控节奏在获取时随声明固定，租约按同一节奏续期（见 [tool-architecture.md](tool-architecture.md#tmux-monitor)）。

## 四、停止要给证据，不给请求

`Stop` 返回 `StopEvidence{Resource, Confirmed, ExitCode, Reason}`：

- **取消确认不是证据**。取消是执行可能还没观测到的信号，只拿到 ack 的执行器必须报"未确认"。
- `ExitCode` 只在观测到退出时才有意义；`Confirmed=false` 时恒为 `-1`，且 `Reason` 必须说明为什么没确认。
- tmux 侧实现：先读一次会话名单，不在名单里也**停 20ms 再读一次**（一次瞬时读不作证据）；在名单里则读 `#{pane_dead} #{pane_dead_status}`，退出码优先取 `pane_dead_status`，取不到回退到命令自己落盘的记录（`exec-exit` 目录，读走即删）；`kill-session` 后再确认一次会话确实缺席。
- `Confirmed=false` 时上层**保留资源并报告保留**，不报成结束（`Manager.Release` 的 `Released` 只在停止已确认时为真）。

## 五、建会话：不静默双开，不丢最后一屏

- 同名会话已存在 ⇒ `errSessionExists` 直接拒绝：两个开发服务器抢同一端口是典型故障，"看起来成功"的第二个进程比报错更难查。
- 先 `new-session -d` 建壳，再 `set-option remain-on-exit on`，**回读校验**该值确实为 `on`（失败即杀会话报错，不带着"退出就消失"的窗口干活），最后 `respawn-pane -k` 起真实命令。
- 命令包成子 shell：`( cmd ); code=$?; <写退出码记录>; sleep 0.1; exit $code`。那 100ms 的 drain 让**瞬时结束的命令**其输出也来得及落在 pane 屏上，`Capture` 才有东西可读。
- `Capture` 读的是运行期观测到的帧；中途的部分读取不覆盖它。

## 六、`exec-state` 只进审计，不进模型

`StatePhase` 是**对资源的观测，不是对工作的裁定**：`stable` 不是成功，`suspect` 不是死，半帧不是完成。宿主只把 `stable`／`suspect`／`exited` 三个相位写成**无载荷审计事实**（`exec-state: <phase>` 摘要 + resource／phase／exit_code 元数据），**不驱动回合、不进模型上下文**（`assembly/host.go` 的 `observeExecState`）。症状锚钉住两侧：三态各静默（正锚：`provider` 请求数与用户 outbound 都不增加），三态都必须留痕（负锚：不静默地什么都不记）——`examples/wechat-bot/smoke_test.go` 的 `TestLocalSmokeSymptomExecStateSilent`。

## 七、tmux 是硬依赖

`IsTmuxAvailable()` 每次调用做**真实 PATH 查找**，没有构造可以替代这次探测；不可用时获取以 `ErrTmuxUnavailable` 拒绝。缺依赖是启动/获取期就该看见的事，不是运行期悄悄换一种语义的事。

## 已知缺口与演进方向

| 缺口 | 现状与防线 | 候选方向 |
|---|---|---|
| 无重启接管的会话 | 会话在 tmux 里看似仍活，本进程不再监视；读取只发生在 `Poll` | 把跨进程接管作为独立机制登记，不在监控层偷补 |
| 退出码有两条来源 | `pane_dead_status` 优先，记录文件兜底；两者都缺即 `-1` | 一次实测说明"两条都缺"出现的条件 |
| 阈值缺实测标定 | 30/60/90/120 为默认 | 一次真实长任务标定，写清 TUI 与批处理的差别 |

延伸：[存储概念图](../../demos/storage-concept-map.html)（执行资源、账本与现算读数的分工）· [存储时光机](../../demos/storage-time-travel.html)
