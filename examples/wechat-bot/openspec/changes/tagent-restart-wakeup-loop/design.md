## Context

部署现状（读回核实）：
- tagent 实例部署于 `/home/lighthouse/tagent/examples/wechat-bot/`，二进制 `wechat-bot`（run.sh `ensure_binary` 经 `go build -o wechat-bot .` 构建），已构建在目录内
- `main.go` 已有 `signal.NotifyContext(..., SIGINT, SIGTERM)`（§6）→ SIGTERM 优雅停机钩子存在；HTTPAPI 经 `rl.NewHTTPAPI(ta)` + `http.ListenAndServe(":"+TAGENT_HTTP_PORT, httpAPI)` 监听 `:8089`（loopback 使用，用户知情接受维持无鉴权，不做 middleware 改动）
- `run.sh` 已有 do_stop（TERM→10s 循环→KILL 升级）与 do_start（nohup + PID_FILE）；日志 `${LOG_DIR}/wechat-bot-${INSTANCE_NAME}.log`；PID 文件在 LOG_DIR 下
- 微信登录态持久化于 `.wechat-config/token.json` → 重启后免扫码依赖此文件（实测点）
- QQchannelRobot 的 `resources/check.shell`（每小时 cron）已有 flock 单实例守护段 + 日志轮转段；事件驱动通知段追加在其末尾

## Goals / Non-Goals

**Goals:**
- 遗嘱执行人脚本在独立 tmux 会话中独立于 agent 进程树存活，agent 死后接管一切
- 重启实测：自杀→转世全链路证据落盘 restart.log
- QQchannelRobot 纯事件驱动联动：仅在真实事件（守护拉起动作/风暴级异常）时经 /task 通知 tagent（有事才叫）
- skill 文档沉淀（/task 用法、重启 runbook、空窗行为规则、有事才叫规则）

**Non-Goals:**
- 不做 /task token 鉴权（8089 loopback 无鉴权现状维持，用户知情接受）
- 不做每小时机械心跳投递（已裁定作废，避免认知噪音）
- 不改 tagent 框架（rl 包）——零框架侵入，无 main.go middleware 变更
- 不做 systemd 化部署（run.sh nohup 模式保持现状；systemd 路线在 run.sh `systemd` 子命令已有指引，属未来选项）
- 不做双实例热备（盲窗 10-20s 接受）
- 不做通知消息的内容语义/回复策略（仅机制化投递，语义由 tagent 运行时决定）

## Decisions

### D1: 8089 维持 loopback 无鉴权现状（原 middleware + token 方案已裁撤）

**选择**：不修改 main.go，不引入 TAGENT_API_TOKEN，8089 维持现状（loopback 无鉴权）。

**理由**：
1. 用户裁定砍掉 token 鉴权：/task 仅在 127.0.0.1 loopback 场景使用，外部攻击面不成立；QQchannelRobot 同机时经 127.0.0.1 访问
2. 裁撤后 main.go 无代码变更 → 重启链路验证不依赖代码差异，二进制替换仅是机制本身的验证
3. 备选（middleware + token）失败半径包括既有调用方 401、token 泄漏面管理、.env 变更联动——均已被裁定排除

### D2: 遗嘱执行人 = restart-tagent.sh，tmux 会话独立于 agent 进程树

**选择**：脚本启动方式 `tmux new-session -d -s tagent-restart`，脚本逻辑顺序：
1. sleep 缓冲期（等 agent 事件落盘）——按 tagent 单回合事件写盘节奏取 10s
2. `pgrep -f wechat-bot` 定位 agent 进程 → `kill -TERM` → 循环等退出（超时 30s）→ 升级 `kill -9`
3. `go build -o /tmp/wechat-bot.new .` → 校验产物 → `mv /tmp/wechat-bot.new wechat-bot`（mv 原子替换，同文件系统内 rename(2)）
4. 启动：`setsid nohup ./wechat-bot >> $LOG_FILE 2>&1 &`（run.sh start 语义）
5. 探活循环：`curl -fsS 127.0.0.1:8089/healthz` 直至 200 或超时（如 120s），结果+耗时写 restart.log
6. 全程 `log_evt()` 时间戳日志：脚本开始/停机方式/升级原因/构建开始/替换/启动/探活结果/盲窗耗时

**理由**：
1. 用户指定"独立 tmux 会话脱离我的进程树"——tmux server 是 agent 的兄弟进程，agent 死后 tmux 会话存活，脚本继续执行
2. 备选一（systemd）：重部署形态，超出本次范围（Non-Goal）
3. tmux 会话本身无需长期存活，任务完成即退出（会话 `remain-on-exit off` 默认）——避免又一个需要守护的守护者

**tmux 会话名**：`tagent-restart`（探活完成、agent 转世后正常退出；异常时保留现场供 grep restart.log）

### D2b: tmux 会话脱离进程树的加固

tmux 的 `new-session -d` 启动的进程父进程是 tmux server（PID≠agent），agent 死后 tmux server（常驻）继续拥有该会话。但若 tmux server 本身由 agent 进程启动且 agent 死时 server 也死，会话即亡。**加固**：agent 调用 `setsid tmux new-session -d -s tagent-restart 'bash restart-tagent.sh'`——setsid 使 tmux client 立即脱组，tmux server 若不存在则首次连接时被拉起（与 agent 无父子关系，父为 init）。脚本内部无需再 setsid。

### D3: QQchannelRobot 联动 = 纯事件驱动通知（有事才叫，原机械心跳已裁撤）

**选择**：QQchannelRobot/resources/check.shell 末尾追加事件检测段，仅两类真实事件才经 /task 通知 tagent：
1. **守护拉起动作**：check.shell 的守护段本轮真的执行了拉起（restart/restartall 等动作被触发）
2. **风暴级异常**：守护日志中检出风暴级异常（如短时间内大量 ERROR / 连续重启 / OOM 等模式）

```sh
# ── event-driven notify to tagent via /task (loopback, no auth) ──────
# 有事才叫：本轮守护发生拉起动作 或 检出风暴级异常 → 发一条通知
if [ "${RESTARTED:-0}" = "1" ] || grep -qE 'storm|OOM|continuous-restart' "$GUARD_LOG"; then
  curl -fsS -m 10 -X POST "http://127.0.0.1:8089/task" \
    -H 'Content-Type: application/json' \
    -d "{\"type\":\"task\",\"message\":\"[event-notify] qqrobot guard: action=$([ \"${RESTARTED:-0}\" = \"1\" ] && echo restarted || echo storm-detected), $(date '+%F %T')\"}" \
    >> "$CRON_LOG" 2>&1 || log_evt "event-notify to tagent FAILED"
fi
```
（守护机地址：与 tagent 同机则 127.0.0.1；跨机则真实内网 IP。执行时以实际为准。事件检测的 grep 模式与变量名需在执行时按 check.shell 实际日志格式校准，此处为示意。）

**理由**：机械心跳已裁定作废——每小时一条心跳消息进入 agent 对话流属认知噪音，且 tagent 停机期间心跳落空重试机制复杂。事件驱动天然无噪音：无事时 check.shell 静默，tagent 收不到消息即为正常状态；有事时一条通知足以唤醒 tagent 关注 QQchannelRobot 侧异常。唤醒来源只保留设计内来源（task_settled + 外部输入事件），事件通知属于外部输入事件的一种。

### D3 补充：事件通知唤醒语义

通知消息以 `[event-notify]` 前缀进入 agent 对话流，trigger_source=task。tagent 按运行时行为处理（skill 文档中沉淀规则：见 D5）。销假消息语义：agent 转世后读 restart.log 提取证据 → 实测 healthz → 微信送达用户。销假不依赖事件通知（转世后首轮用户消息或注入任务即可触发），事件通知是"有事才叫"的异常通道。

### D4: 二进制替换的原子性与失败半径

**选择**：新二进制构建到 /tmp，`mv` 到部署目录。**约束**：/tmp 与部署目录须同文件系统，否则 mv 跨文件系统退化为 copy+rename，原子性丧失。**执行时验证**：`df /tmp /home/lighthouse/tagent/examples/wechat-bot | awk 'NR>1{print $1}' | sort -u | wc -l` == 1 确认同盘；否则改用部署目录内临时文件 `.wechat-bot.new` → mv 同目录原子替换。

**理由**：同目录临时文件方案虽多一步，但确保 rename(2) 原子性；/tmp 同盘时两方案等价，不同盘时同目录方案仍原子。

**失败半径控制**：若 agent 已死、脚本在步骤 3 前崩溃，服务处于停机——属可接受（自杀→转世本就是有计划停机）。若构建失败（编译错），脚本 MUST 回滚：不替换、以旧二进制拉起、restart.log 记 FAIL。**旧二进制备份**：mv 替换前 `cp wechat-bot wechat-bot.prev`（同目录备份，非原子必要但便于回滚与对照版本）。

构建耗时风险：go build 全量构建可能远超 10-20s（首次无缓存）。缓解：重启前预检 `go build -o /dev/null .` 确认可构建，或接受较长盲窗（restart.log 记录实际耗时）。**建议执行时先跑一次构建确认耗时基线**。

耗时预算总览：缓冲 10s + 停机（TERM→退出，秒级）+ 构建（变数大，预检后控制在 60s 内）+ 启动+探活（秒级）+ 微信重登（若需扫码则盲窗失控，见风险 R2）。计划盲窗 10-20s 为乐观值，实测记录真实值。

### D5: skill 文档沉淀

**选择**：新增 skill 目录 `skills/tagent-self-restart/`（SKILL.md + resources/），内容包括：
1. /task 通道用法（端点、payload 格式、事件通知示例）
2. 重启 runbook（何时自杀、怎么调 restart-tagent.sh、restart.log 位置、销假流程）
3. 稽核规则：销假汇报必须引用 restart.log 关键行（PID 变化、探活耗时、盲窗耗时）
4. 空窗行为规则：agent 在回合边界若预感停机/重启（如更新二进制后），先交代去向再自杀；转世后首轮对话中主动销假
5. 有事才叫规则：`[event-notify]` 消息仅在真实事件时到达，收到即意味着 QQchannelRobot 侧有拉起动作或风暴级异常，应核实 cron.log 并处置

**理由**：skill 是 tagent 运行时可读的知识沉淀位置（tagent 主进程读 `./skills`），把机制化经验固化到 tagent 自身的知识库，转世后的 agent 直接可用（不依赖用户转述）。备选（README.md 或 openspec 文档）不进入运行时上下文。

## Risks / Trade-offs

- **[R1] tmux server 未运行时首连拉起 server，与 agent 无父子关系（父为 init），agent 死后 server 存活** → 已由 setsid tmux 加固；另 restart-tagent.sh 开头 `tmux has-session` 预检（可选）
- **[R1' tmux server 自身死掉]** → 拉起动作检测依赖 check.shell 本身存活，事件通知落空仅在 tagent 停机 + qqrobot 异常双重故障时发生，需用户人工介入；tagent 转世后由首轮用户消息唤醒，销假不依赖单点
- **[R2] 微信重登需扫码**（token.json 失效或被清）→ 微信 Bot 停机期间用户无微信渠道可达；缓解：重启实测点明确验证 token.json 允许免扫码重登；若实测需扫码则记录到 restart.log 并停止该轮重启（回滚到扫码流程）
- **[R4] go build 失败导致服务长时间停机** → 脚本内预检 + 失败回滚（不替换旧二进制、旧二进制拉起、restart.log 记 FAIL）；旧二进制备份 wechat-bot.prev
- **[R4' /tmp 空间不足]** → 预检 df /tmp 可用空间
- **[R5] 盲窗期间用户消息丢失** → WeChat 登录态由 token.json 承接，转世后 WeChat poller 拉取离线消息（需实测确认）
- **[R5' 端口冲突] Port 8089 已有进程占用** → 预检 `ss -tlnp | grep 8089`；kill agent 前先确认监听进程即 agent 自身
- **[R6] check.shell 事件检测段异常阻塞守护主体（flock 9min）** → 通知段超时（curl -m 10）且失败仅记日志不阻塞（`|| log_evt`），守护主体（qqrobot 守护+日志轮转）不受影响
- **[R7] skills/ 目录被 go build 嵌入或干扰** → skills/ 不是 Go 源文件，仅 SKILL.md 起作用，不参与编译
- **[R8] restart.log 无轮转膨胀** → 每次重启追加，接受（重启频率低）；或按大小归档（非必要）
- **[R10] mv 原子替换时旧二进制仍被运行中进程引用** → 本场景不存在（替换发生在旧进程已死后）
- **[R11] restart-tagent.sh 自身错误导致服务永久停机** → 脚本遵循"每一步失败都落盘并尽力拉起旧二进制"原则（except-trap 记录失败点）；若彻底失败，用户人工 tmux attach 现场排障（日志在 restart.log）
- **[R12] 事件检测的 grep 模式与 QQchannelRobot 实际日志格式不匹配** → 检测段在执行时按实际日志校准；漏检风险接受（拉起动作是硬信号兜底）；误检风险通过日志格式校准控制
- **[R13] 8089 无鉴权暴露** → loopback-only 使用前提下风险可接受（用户知情接受）；若未来需外网访问，再引入鉴权（另行裁定）

## Migration Plan

1. **阶段 1（任务组 1）**：写 restart-tagent.sh：纯新增文件，无服务影响。`bash -n` 语法检查 + `shellcheck`（若可用）
2. **阶段 2（任务组 2）**：在 tmux 里跑 restart-tagent.sh 实测自杀→转世。预期盲窗 10-20s（若构建超时则更久），转世后任务组 3 验证
3. **阶段 3（任务组 3）**：转世后销假：读 restart.log + healthz 实测 + 微信销假汇报
4. **阶段 4（任务组 4）**：check.shell 事件驱动通知段（拉起动作检测 + 风暴级异常 grep 模式校准，tagent 已运行即可验证）
5. **阶段 5（任务组 5）**：skill 文档（无服务影响）

**回滚策略**：
- restart-tagent.sh 出问题：回滚 = 不触发重启（脚本仅在被显式调起时执行），服务运行不受影响；已引发的停机由 wechat-bot.prev 拉起
- check.shell 通知段出问题：回滚 = 删掉通知段；守护主体（守护+轮转+清理）不受影响（通知段结构隔离、失败不阻塞）
- 二进制替换出问题：回滚 = wechat-bot.prev 重命名回 wechat-bot + 重新拉起

## Open Questions

1. 实测时 tagent 与 QQchannelRobot 同机部署（127.0.0.1）还是跨机？→ 执行时确认（影响 check.shell 通知段 URL）
2. go build 全量构建耗时基线（无缓存时）？→ 执行时预检确认（影响盲窗预期）
3. tagent 转世后 WeChat poller 能否拉到停机期间离线消息？→ 执行时实测记录（R5 验证点）
4. tmux 是否已安装/可用？→ 执行时确认（若不可用，改用 `setsid nohup` 直接拉起脚本，逻辑不变）
5. 是否有 `ss`/`lsof` 预检工具可用？→ 执行时确认（影响端口预检命令选择）
6. 事件检测的"风暴级异常"具体 grep 模式需按 QQchannelRobot 实际日志格式校准？→ 执行时按 cron.log 实际格式定（拉起动作是硬信号兜底）
7. 二进制替换是否需 go.mod 版本同步升级？→ 本次无 main.go 变更（token 已裁撤），无 go.mod 变更，不涉及
