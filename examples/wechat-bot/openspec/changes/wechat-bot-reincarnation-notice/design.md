## Context

部署现状（读回核实，2026-09-11）：
- `restart-maintenance.sh`（保险链 v2）SUCCESS 路径：healthz 200 → `echo $NEW_PID > run/restart.done` → 归档 env.snapshot → exit 0。SUCCESS 时**只写了 PID，没写任何"上一世"信息**
- 新进程由 python posix_spawn 拉起（fds 3+ 关闭），启动路径与冷启动完全无区别——**重启后新进程无任何方式知道自己刚被换装**
- `main.go` 启动序：LoadConfig → … → `ta.StartLoop` → HTTPAPI → bot.Login → consumer goroutine → bot.Run。事件注入 API 已存在：`ta.InjectMessageWithSource(source, msg)`（agent/inject.go，消息恒进 persistentBus，非 user source 不 arm meditation novelty gate）
- 13:58 事故证据（logs/restart.log L485-492）：sentinel pid=2061791 失活 → 12s 完成换装（new_pid=2185305）→ 旧进程死前对用户的"等结算唤醒"承诺随进程消失而蒸发，主日志 `grep 结算唤醒` 零命中
- `run/restart.done` 现存内容：单行 PID（`2185305`），无时间戳字段

框架约束：本计划零框架侵入（proposal Impact），main.go 只调用既有 `InjectMessageWithSource`。

## Goals / Non-Goals

**Goals:**
- 新进程启动即判定"我是否刚被保险链换装"，命中则生成结构化转世通报并注入事件流（复用内部事件通道，不新开通道）
- 保险链 SUCCESS 分支同步落盘换装档案（REINCARNATION_NOTICE），作为通报的详情数据源
- 全链路可观测：检测命中/未命中、通报文本、注入结果均落日志
- dogfood 验证：用保险链自身完成本次部署，实测通报抵达事件流

**Non-Goals:**
- 不捕获旧进程临终遗言（SIGTERM 不可靠，见 proposal"明确不涉及"）
- 不改框架、不改 crontab/锁/哨兵机制
- 不做通报的用户可见回复策略——注入后 agent 怎么用是运行时语义（与 meditation 输出同理）
- 不解决"多次换装"的档案轮转（NOTICE 文件每次 SUCCESS 覆写即可，重启频率低）

## Decisions

### D1: 检测信号 = restart.done mtime 新鲜度（<10min）+ PID 不等判定

**选择**：启动时 `os.Stat(run/restart.done)`，取 ModTime；`time.Since(mtime) < 10*time.Minute` 且文件内 PID ≠ `os.Getpid()` → 判定"刚被换装"。

**理由**：
1. restart.done 由保险链 SUCCESS 分支在 healthz 探活通过后写入，mtime 即换装完成时刻——语义精确（不是"启动时刻"）
2. restart.done 也被保险链"基线校准"路径写（bot 活着且健康时回写当前 PID，L85-87）——PID 不等判定排除掉"cron 校准自己"的假阳性；且校准写的是活进程自身 PID，新启动的 bot 读到 PID≠自己 恰说明写它的不是自己 → 是换装（或上一代）
3. 10min 阈值：换装到新进程真正起来通常 <60s（实测 12s）；阈值给 build 失败重试留余量（retry next minute），又不至于大到把"几小时前的换装"误判为"刚换装"

**备选否决**：
- 读 PID 文件变化：`.pid-default`（logs/ 下）由 run.sh 写，保险链也写，两处写同一文件，语义混乱
- 环境变量哨兵（保险链 spawn 时注入 REINCARNATED=1）：需要改 spawn 段 env 快照逻辑，且 env.snapshot 回退场景（/tmp 清空）会丢——文件信号更鲁棒

### D2: 通报注入通道 = InjectMessageWithSource("meditation", …)

**选择**：复用 meditation 触发源。`ta.InjectMessageWithSource("meditation", model.Message{Role: RoleUser, Content: noticeText})`。

**理由**：
1. 复用框架现有内部事件通道（proposal 要求"复用框架现有内部事件通道，如 task settled/冥想的投递路径"）——零新通道、零框架改动
2. source 非 "user" → 不 arm meditation novelty gate（inject.go L60-64），不产生认知噪音副作用
3. consumer 对 meditation 输出的既有语义是"internal, don't send to user"（main.go L379-381）——转世通报正是内部事件：agent 知情即可，是否向用户销假由 agent 运行时决定
4. 该事件会作为普通对话轮进入 agent 上下文——转世后的 agent 第一轮就能读到自己的档案

**备选否决**：
- source="task"：语义是"异步任务结算回收"，转世通报不是任务结算；且 task 输出会走"回退最近活跃会话"投递给用户（main.go L384-393），把内部通报硬塞给用户违反 Non-Goal
- source="user"：会 arm novelty gate 且污染用户输入谱系

**风险注意**：meditation source 的输出在 consumer 走 `log.Infof("[Agent][meditation] 冥想输出: …")`——验证点即 grep 此行。若注入发生在 StartLoop 之后、agent 空闲，persistentBus 的 Publish 在 agent 处于 idle 时由 event loop 的 BeforeModel 消费——需确认 idle 期注入能否触发一轮（见 D4 时序）。

### D3: REINCARNATION_NOTICE 文件格式 = 人类可读 key: value 档案

**选择**：保险链 SUCCESS 分支新增：

```sh
cat > "$DONEDIR/REINCARNATION_NOTICE" <<EOF
reincarnated_at: $(date '+%F %T')
old_pid: ${OLD_PID:-unknown}
new_pid: ${NEW_PID:-unknown}
reason: ${REASON:-sentinel-dead}
build_sha: $(sha256sum "$BASE/wechat-bot" | cut -c1-12)
binary_size: $(stat -c%s "$BASE/wechat-bot")
log_pointer: $BASE/logs/restart.log
note: insurance chain swap completed; see log_pointer for full session
EOF
```

main.go 读该文件作为**元数据段**；注入时最终通报正文 = 元数据段 + WAL 尾现场块（D8，main.go 侧组装，shell 不碰事件存储）+ 断点标记。

**理由**：key: value 人类可读、grep 友好，agent 可直接读档；shell 侧零依赖（date/stat/sha256sum 均基础工具）；失败容忍——文件缺失时 main.go 生成降级通报（仅时间+PID 来自 restart.done，并注明 NOTICE 缺失）。

**备选否决**：JSON——shell 侧手拼 JSON 易错，且此文件是给人/agent 读的档案而非程序接口。

### D4: 注入时机 = StartLoop 之后、bot.Run 之前，goroutine 延迟投递

**选择**：main.go 在 `ta.StartLoop` 成功后、HTTPAPI 启动前，起一个 goroutine：sleep 数秒（等 event loop 就绪）→ 检测 → 命中则注入。

**理由**：StartLoop 前 persistentBus 可能未就绪（InjectMessageWithSource 有 nil-bus fallback 到 activeBus，冷启动时可能双输或丢弃）；sleep 数秒给 loop 稳态留缓冲。具体秒数执行时以实测为准（如 5s）。

**备选否决**：
- 在 HTTPAPI /healthz 里被动检测：违背"注入事件流"目标
- bot.Run 后在 OnMessage 里顺带检测：依赖用户先发消息，恰好复刻 13:58 空窗

### D5: 幂等与防重复通报

**选择**：检测命中并成功注入后，将 `run/restart.done` 原子改写为消费标记（如 rename 为 `restart.done.notified` 或在文件内容前缀加 `notified:`）——防同进程重启多次注入。不删 restart.done（保险链幂等门靠它判断"哨兵健康则静默退出"）。

**理由**：rename 后保险链 `kill -0 $BPID` 仍以文件存在性+健康检查为准，改写内容不影响其逻辑（它读 PID 后做健康检查，PID 没变）。若用 `notified:` 前缀方案，保险链读 PID 需容错——rename 方案对保险链零影响，选 rename。

**备选否决**：内存 flag 防重——进程重启即失效，起不到跨重启幂等作用。

### D6: 门禁与提交流程

**选择**：`go build ./... && go vet ./... && go test ./...` 全绿 → git commit → push dev。

**理由**：仓库现行门禁（同 run.sh ensure_binary 逻辑）；dogfood 部署前的硬闸。新增单测覆盖检测函数（新鲜度判定、PID 判定、降级通报生成），走标准 `_test.go` 文件。

### D7: dogfood 部署路径 = 保险链自身换装

**选择**：门禁过+push dev 后，模拟真实触发：`kill -TERM <当前 bot PID>` → cron 保险链下一分钟接手 → build（git 里新代码）→ 换装 → 新进程启动 → 检测 restart.done 新鲜 → 注入通报。

**理由**：proposal 明确要求"用保险链脚本本身再部署（自替换 dogfood）"——只有真实走一遍保险链，才能验证 REINCARNATION_NOTICE 写入 + 检测 + 注入全链。备选（手动 go build + mv + 手动拉起）绕过了保险链，验证不了脚本侧变更。

### D8: WAL 尾回放 = 现场还原（2026-09-11 用户拍板升级）

**选择**：注入前由新进程只读查询自身事件存储（trajectories）尾部 N 条，组装“现场块”并入通报正文：最后事件 key/类型/时间/摘要 + 断点标记（最后事件止于 thinking_plan 且无收尾 agent_output = 该回合中断于此）。续作语义 = 从最后检查点继续 + 已知缺口，非完美时间旅行。

**理由**：只读查询、零写路径；把“通报后 agent 自己挖现场”的多轮 recall 往返（2026-09-11 实测 3 轮 recall + 6 探针）压成零探针；检测门（D1）保证回放只在转世场景发生，冷启动零成本。前置条件：memory store 须暴露时间倒序尾查询（recall 已证明该层存在，main.go 侧 API 形状见任务 1.2 核实）。

**降级阶梯**：现场块+元数据（正常）→ 纯元数据（WAL 查询失败，log 明示）→ 最简通报（NOTICE 缺失，D3 原降级）。禁静默降级。

**token 纪律**：现场块封顶 ~2-4K tokens，只带 key+摘要不带全文正文（防与上下文装配重复拉取；refs 去重行为见 OpenQ 4）。

## Risks / Trade-offs

- **[R1] 新鲜度检测的假阳性：手动重启（run.sh start）在换装后 10min 内发生** → 概率低；且后果只是多发一条内部通报，agent 可自行判断。接受
- **[R2] meditation source 注入后，若 event loop 处于 idle 且无 BeforeModel 轮次，通报滞留 bus** → persistentBus 消息不丢（下次任意轮次 BeforeModel TryPull 拾起）；最坏情形是通报延迟消费而非丢失。执行时验证 idle 注入是否立即触发轮次，不触发则记录实际消费时机
- **[R3] REINCARNATION_NOTICE 写失败（磁盘/权限）** → main.go 降级通报（D3），不阻塞启动；保险链 SUCCESS 分支写失败仅 log，不影响 restart.done 主流程
- **[R4] 通报注入导致 agent 首轮上下文膨胀** → 通报文本控制在 ~500 字节内（key:value 紧凑格式），可接受
- **[R5] rename restart.done → restart.done.notified 后，保险链幂等门读不到哨兵** → 读回确认保险链逻辑：哨兵缺失时走"目标自发现"→ 发现 bot 活着且健康 → 回写基线 + 静默退出（L78-87），行为安全；但需在任务中实测验证此路径
- **[R6] go test 触碰网络/微信 API** → 检测函数纯文件系统逻辑，无网络依赖；新增测试不引入外部依赖
- **[R7] dogfood kill -TERM 时 agent 正在处理用户消息** → 换装盲窗 10-60s，本就是保险链设计内行为；选低峰期执行
- **[R8] restart.done 的 mtime 在 cron 校准路径被频繁刷新** → 校准仅在哨兵 pid 失活时触发；PID≠self 判定（D1）覆盖此情形——校准写的是活 bot 自身 PID，新进程读到 PID≠self 且 mtime 新鲜的窗口极窄（校准后马上重启），接受
- **[R9] 多次快速换装（build 失败重试）** → 每次 SUCCESS 覆写 NOTICE，最后一份为准；中间失败不写（只在 SUCCESS 分支写），无半成品档案
- **[R10] 上下文双重复原**：通用装配与现场块可能重复引用同一批事件 → 现场块只带 key+摘要；执行期验证 refs 去重行为（OpenQ 4），重复显著再收敛为纯 key 列表

## Migration Plan

1. **阶段 1（代码）**：main.go 检测+注入函数（纯新增）、restart-maintenance.sh SUCCESS 分支写 NOTICE、单测 → 门禁（build/vet/test）
2. **阶段 2（提交）**：commit + push dev
3. **阶段 3（dogfood）**：kill -TERM 当前 bot → 保险链接管 → 验证：restart.log SUCCESS 行、run/REINCARNATION_NOTICE 存在、新进程日志可见通报注入与消费（grep "reincarnation" / meditation 行）
4. **阶段 4（观察）**：转世后 agent 首轮对话确认通报已进入上下文；微信侧用户消息正常收发

**回滚策略**：代码回滚 = git revert + 保险链再换装一次（自动走旧二进制 → wechat-bot.prev 回滚点）；NOTICE 文件与 restart.done.notified 均为运行时产物，无需清理。

## Open Questions

1. idle 期 meditation 注入是否立即触发一轮？→ 执行时实测（R2）；不改变设计（消息不丢是底线）
2. sleep 延迟具体取几秒？→ 执行时按 loop 就绪节奏定（5s 起步）
3. run.sh 冷启动路径（非保险链）是否会写 restart.done？→ ✅ 已核实（任务 1.1，2026-09-11）：**不会写**。`restart.done` 在 run.sh 全文零命中（grep -c = 0），`do_start()` L359 起、L383 仅 `nohup … &` 一行拉起，start/rl-start 入口均汇入 do_start → D1 检测门无需追加 insurance 标记判定，假阳性面结论成立
4. WAL 尾查询 API：main.go 可达的只读时间倒序接口形状？（任务 1.2 核实）+ 现场块与上下文装配的 refs 去重行为
