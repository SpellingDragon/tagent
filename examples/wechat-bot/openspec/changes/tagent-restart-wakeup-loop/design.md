## Context

部署现状（读回核实）：
- tagent 实例部署于 `/home/lighthouse/tagent/examples/wechat-bot/`，二进制 `wechat-bot`（run.sh `ensure_binary` 经 `go build -o wechat-bot .` 构建），已构建在目录内
- `main.go` 已有 `signal.NotifyContext(..., SIGINT, SIGTERM)`（§6）→ SIGTERM 优雅停机钩子存在；HTTPAPI 经 `rl.NewHTTPAPI(ta)` + `http.ListenAndServe(":"+TAGENT_HTTP_PORT, httpAPI)` 监听 `:8089`（0.0.0.0），**中间件包装点是 examples 层**（main.go 中 httpAPI 作为 http.Handler 传入），不改 tagent 框架
- `run.sh` 已有 do_stop（TERM→10s 循环→KILL 升级）与 do_start（nohup + PID_FILE）；日志 `${LOG_DIR}/wechat-bot-${INSTANCE_NAME}.log`；PID 文件在 LOG_DIR 下
- 微信登录态持久化于 `.wechat-config/token.json` → 重启后免扫码依赖此文件（实测点）
- QQchannelRobot 的 `resources/check.shell`（每小时 cron）已有 flock 单实例守护段 + 日志轮转段；心跳段追加在其末尾
- `.env` 由 run.sh 启动加载 → `TAGENT_API_TOKEN` 从此注入

## Goals / Non-Goals

**Goals:**
- 遗嘱执行人脚本在独立 tmux 会话中独立于 agent 进程树存活，agent 死后接管一切
- /task 加 token 鉴权，healthz 保持无鉴权
- 重启实测：自杀→转世全链路证据落盘 restart.log
- check.shell 每小时心跳闭环两套系统
- skill 文档沉淀（/task 用法、重启 runbook、空窗行为规则）

**Non-Goals:**
- 不改 tagent 框架（rl 包）——middleware 在 examples 层包装，0 框架侵入
- 不做 systemd 化部署（run.sh nohup 模式保持现状；systemd 路线在 run.sh `systemd` 子命令已有指引，属未来选项）
- 不做双实例热备（盲窗 10-20s 接受）
- 不做心跳消息的内容语义/回复策略（仅机制化投递，语义由 tagent 运行时决定）

## Decisions

### D1: middleware 在 examples 层包装，不改 rl 包

**选择**：main.go 中不直接 `http.ListenAndServe(":"+port, httpAPI)`，改为包一层 authMiddleware 后再 ListenAndServe。

**理由**：
1. 零框架侵入：rl.HTTPAPI 的 ServeHTTP 是框架代码，examples 层包装不动 `go.mod` 依赖，上游同步无冲突
2. 用户明确说"main.go examples 层包 middleware"
3. 备选（给 rl 包加 WithAuth 选项）侵入框架、需同步上游 PR、且失败半径大（影响所有 examples 用户）——放弃

**实现**：authMiddleware 读 `os.Getenv("TAGENT_API_TOKEN")`；空则原样放行（兼容）；非空则 /healthz 与 /task 均查 `Authorization: Bearer <token>` 或 `X-API-Token: <token>` 头。错误返回 401 + JSON。**注意**：仅 /task 鉴权，/healthz 放行（探针无需凭据）。

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

### D3: 心跳闭环 = check.shell 末尾追加投递段

**选择**：QQchannelRobot/resources/check.shell 末尾追加：
```sh
# ── hourly heartbeat to tagent via /task (token-guarded) ──────────────
TAGENT_TOKEN="${TAGENT_API_TOKEN}" # QQchannelRobot 与 tagent 共享 token，从其 .env 或环境注入
curl -fsS -m 10 -X POST "http://守护机IP:8089/task" \
  -H "Authorization: Bearer $TAGENT_TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{\"type\":\"task\",\"message\":\"[heartbeat] qqrobot hourly guard done: qqrobot=$(pgrep -x qqrobot >/dev/null && echo alive || echo DEAD), logs rotated, media cleaned. $(date '+%F %T')\"}" \
  >> /home/lichage/QQchannelRobot/cron.log 2>&1 || log_evt "heartbeat to tagent FAILED"
```
（守护机 IP：与 tagent 同机则 127.0.0.1；跨机则真实 IP。执行时以实际为准。）

**理由**：把两套系统闭成互检环——qqrobot 每小时向 tagent 报到，tagent 死了心跳落空（下一小时重试+tagent 转世后首个心跳唤醒销假），tagent 死转世后 qqrobot 心跳把它拉回对话。复用既有 flock 单实例、复用 cron.log 计次。**不做**双向对等心跳（tagent → qqrobot），因为 tagent 运行时无需依赖 qqrobot 状态（Non-Goal：心跳语义）。

### D3 补充：心跳唤醒语义

心跳消息以 `[heartbeat]` 前缀进入 agent 对话流，trigger_source=task。tagent 按运行时行为处理（skill 文档中沉淀规则：见 D5）。销假消息语义：agent 转世后读 restart.log 提取证据 → 实测 token/healthz → 微信送达用户。销假汇报不依赖心跳（转世后首轮用户消息或注入任务即可触发），心跳是兜底唤醒。

### D4: 二进制替换的原子性与失败半径

**选择**：新二进制构建到 /tmp，`mv` 到部署目录。**约束**：/tmp 与部署目录须同文件系统，否则 mv 跨文件系统退化为 copy+rename，原子性丧失。**执行时验证**：`df /tmp /home/lighthouse/tagent/examples/wechat-bot | awk 'NR>1{print $1}' | sort -u | wc -l` == 1 确认同盘；否则改用部署目录内临时文件 `.wechat-bot.new` → mv 同目录原子替换。

**理由**：同目录临时文件方案虽多一步，但确保 rename(2) 原子性；/tmp 同盘时两方案等价，不同盘时同目录方案仍原子。

**失败半径控制**：若 agent 已死、脚本在步骤 3 前崩溃，服务处于停机——属可接受（自杀→转世本就是有计划停机）。若构建失败（编译错），脚本 MUST 回滚：不替换、以旧二进制拉起、restart.log 记 FAIL。**旧二进制备份**：mv 替换前 `cp wechat-bot wechat-bot.prev`（同目录备份，非原子必要但便于回滚与对照版本）。

构建耗时风险：go build 全量构建可能远超 10-20s（首次无缓存）。缓解：重启前预检 `go build -o /dev/null .` 确认可构建，或接受较长盲窗（restart.log 记录实际耗时）。**建议执行时先跑一次构建确认耗时基线**。

耗时预算总览：缓冲 10s + 停机（TERM→退出，秒级）+ 构建（变数大，预检后控制在 60s 内）+ 启动+探活（秒级）+ 微信重登（若需扫码则盲窗失控，见风险 R2）。计划盲窗 10-20s 为乐观值，实测记录真实值。

### D5: skill 文档沉淀

**选择**：新增 skill 目录 `skills/tagent-self-restart/`（SKILL.md + resources/），内容包括：
1. /task 通道用法（端点、token 头格式、payload 格式、心跳示例）
2. 重启 runbook（何时自杀、怎么调 restart-tagent.sh、restart.log 位置、销假流程）
3. 稽核规则：销假汇报必须引用 restart.log 关键行（PID 变化、探活耗时、盲窗耗时）
4. 空窗行为规则：agent 在回合边界若预感停机/重启（如更新二进制后），先交代去向再自杀；转世后首轮对话中主动销假

**理由**：skill 是 tagent 运行时可读的知识沉淀位置（tagent 主进程读 `./skills`），把机制化经验固化到 tagent 自身的知识库，转世后的 agent 直接可用（不依赖用户转述）。备选（README.md 或 openspec 文档）不进入运行时上下文。

## Risks / Trade-offs

- **[R1] tmux server 未运行时首连拉起 server，与 agent 无父子关系（父为 init），agent 死后 server 存活** → 已由 setsid tmux 加固；另 restart-tagent.sh 开头 `tmux has-session` 预检（可选）
- **[ tmux 老化风险] tmux server 自身死掉** → 心跳落空会被下一小时守护重发；tagent 转世后由首轮心跳或用户消息唤醒，销假不依赖单点
- **[R2] 微信重登需扫码**（token.json 失效或被清）→ 微信 Bot 停机期间用户无微信渠道可达；缓解：重启实测点明确验证 token.json 允许免扫码重登；若实测需扫码则记录到 restart.log 并停止该轮重启（回滚到扫码流程）
- **[R3] /task 增加鉴权后既有调用方（如 qqrobot 心跳）未带 token 被 401** → 部署顺序：先给 check.shell 心跳段配 token（与 tagent .env 同源），再设置 TAGENT顺序_API_TOKEN 并重启；或反向：先设 token 再上心跳段。执行时同一变更内完成，避免窗口
- **[R3' 注意 token 泄漏面]** .env 已被 .gitignore 白名单忽略；restart-tagent.sh / check.shell 中不硬编码 token（从环境读取），避免泄漏
不阻
- **[R4] go build 失败导致服务长时间停机** → 脚本内预检 + 失败回滚（不替换旧二进制、旧二进制拉起、restart.log 记 FAIL）；旧二进制备份 wechat-bot.prev
- **[R4' /tmp 空间不足]** → 预检 df /tmp 可用空间
- **[R5] 盲窗期间心跳/用户消息丢失** → WeChat 登录态由 token.json 承接，转世后 WeChat poller 拉取离线消息（需实测确认）；心跳落空下一小时重试
- **消息丢失风险：Port 8089 已有进程占用] 端口冲突 → 预检 `ss -tlnp | grep 8089`；kill agent 前先确认监听进程即 agent 自身
- **[R6] check.shell 心跳段异常阻塞守护主体（flock 9min）** → 心跳段超时（curl -m 10）且失败仅记日志不阻塞（`|| log_evt`），守护主体（qqrobot 守护+日志轮转)不受影响
- **[R7] skills/ 目录被 go build 嵌入或干扰** → skills/ 不是 Go 源文件，仅 SKILL.md 起作用，不参与编译
- **[R8] restart.log 无轮转膨胀** → restart 每次追加，接受（重启频率低）；或按大小归档（非必要）
- **[R9] TAGENT_API_TOKEN 未设置时 /task 完全开放在 0.0.0.0:8089** → skill 文档与 .env.example 明确警示：生产环境必须设置
- **[R10] mv 原子替换时旧二进制仍被运行中进程引用** → 本场景不存在（替换发生在旧进程已死后）
- **[R11] restart-tagent.sh 自身错误导致服务永久停机** → 脚本遵循"每一步失败都落盘并尽力拉起旧二进制"原则（except-trap 记录失败点）；若彻底失败，用户人工 tmux attach 现场排障（日志在 restart.log）
- **[R11' 心跳消息进入 agent 对话流造成认知噪音]** → 心跳以 `[heartbeat]` 剃头前缀过滤：skill 文档沉淀规则"心跳非对话内容，除非状态异常（如 qqrobot DEAD）否则不展开处理"；agent 运行时若判定为噪音可静默（不回复用户），未来可加 /task payload 通道标记低优先级（Non-Go
al）

## Migration Plan

1. **阶段 1（步骤 1）**：写 restart-tagent.sh（阶段 2 前）：纯新增文件，无服务影响。 `bash -n` 语法检查 + `shellcheck`（若可用）
2. **阶段 2（步骤 2）**：main.go middleware + .env token → **需重启生效**。先不动服务，代码就绪后与阶段 3 合并重启一次
2'. **阶段 3（步骤  Plan 3）**：在 tmux 里跑 restart-tagent.sh 实测自杀→转世（此时 middleware 已生效，重启即验证鉴权）。预期盲窗 10-20s（若构建超时则更久），转世后步骤 4 验证
4. **阶段 4（步骤 .md/4）**：转世后销假：读 restart.log + token 拦截实测 + healthz + 微信销假汇报
5. **阶段 4'（步骤 5）**：check.shell 心跳段（tagent 已带 token 并运行，则心跳段即刻可验证）
6. **阶段 5（步骤 6）回滚**：skill 文档（无服务影响）

**回滚策略**：
- main.go middleware 出问题：回滚 = `git checkout <prev> -- main.go` + 重跑 restart-tagent.sh（或手动重跑构建替换）
- restart-tagent.sh 出问题：回滚 = 不触发重启（脚本仅在被显式调起时执行），服务运行不受影响；已引发的停机由 wechat-bot.prev 拉起
- check.shell 心跳段出问题：回滚 = 删掉心跳段；守护主体（守护+轮转+清理）不受影响（心跳段结构隔离、失败不阻塞）
- token 不设置即回滚到无鉴权状态（行为级回滚，改环境变量即可）

## Open Questions

1. 实测时 tagent 双方同机部署（tagent 与 QQchannelRobot 同在 127.0.0.1）还是跨机？→ 执行时确认（影响 check.shell 心跳段 URL）
2. QQchannelRobot 的 token 从哪注入？（同机）从 tagent .env 读，还是独立配置？→ 扁平处置：执行时同机时直接复用 tagent .env 的 token 值写入 check.shell 头部变量（或从其环境注入）
2'. 心跳消息里是否需包含 tagent 自身状态自检（如 tagent 进程存活）？→ 属心跳语义范畴，执行时按简明优先（仅 qqrobot 状态 + 时间戳），后续迭代可加
3. go build 全量构建耗时基线（无缓存时）？→ 执行时预检确认（影响盲窗预期）
3'. tagent 转世后 WeChat poller 能否拉到停机期间离线消息？→ 执行时实测记录（R5 验证点）
4. tmux 是否已安装/可用？→ 扷行时确认（若不可用，改用 `setsid nohup` 直接拉起脚本，逻辑不变）
4'. 是否有 `ss`/`lsof` 颗检工具可用？→ 执行时确认（影响端口预检命令选择）
5. 二进制替换是否需 go.mod 版本同步升级（tagent 框架本体是否有变更）？→ 本次仅 examples 层 main.go 变更，无 go.mod 变更，不涉及
