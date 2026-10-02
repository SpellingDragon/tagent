# QQchannelRobot 本机事实档案

> 由 go-service-ops 技能引用。本文件只记**本机特定**的路径、命令、坑；
> 通用方法论见 go-service-ops/skill.md。

## 项目事实

- 项目根：`/home/lighthouse/QQchannelRobot`（git repo，远端 SpellingDragon/QQchannelRobot）
- 语言/工具链：Go 1.18 项目；**必须用 `/usr/local/go.bak`（go1.18.10）构建**，
  系统 Go 1.24 触发 quic-go v0.28.1 哨兵失败；模块缓存在 `GOPATH=/home/lighthouse/go`
- 平台：QQ 频道官方 API（botgo）+ Discord（可选）+ B 站直播监控/录制
- 配置：`config.yaml`（权限已收紧 600，已移出 git 跟踪；历史 commit 仍含密钥待轮换）
- B 站凭据：`cookie.json`（未跟踪；过期判据见启动日志 `expired after -N second`）
- 数据库：本机 MySQL 127.0.0.1:3306；`initState()` 会把 DSN 打进日志（敏感，轮转备份含密码）

## 常用命令（本机版）

```bash
# 构建（哨兵文件铁律：旧工具链 + 显式 GOROOT）
cd /home/lighthouse/QQchannelRobot && \
GOROOT=/usr/local/go.bak GOPATH=/home/lighthouse/go GOCACHE=/tmp/go118cache \
GOPROXY=off GOSUMDB=off timeout 600 /usr/local/go.bak/bin/go build -o bin/qqrobot .

# 扫码登录工具（gen 生成二维码 / verify 写 cookie）
GOROOT=... /usr/local/go.bak/bin/go build -o /tmp/bili-login ./cmd/bili-login
/tmp/bili-login gen -qr /tmp/bili_qr.png
timeout 120 /tmp/bili-login verify -cookie /home/lighthouse/QQchannelRobot/cookie.json

# safe_mode 开关位置：config.yaml channel_robot: 段（python 定点插行，勿整读密钥文件）
```

## 已知行为与坑（本机特有）

- 启动日志首行 `stat robot.go: no such file or directory` = 无害残留，非致命
- `CheckErr` = `log.Fatalln`：初始化失败即退，启动后必须 ps 读回确认
- crontab：`resources/check.shell` 每小时跑——日志轮转（保留7天）+ bilibili 媒体清理；
  自动拉起段 2026-09-08 起已重写激活（原为注释停用，修复见 commit cd15de6 与下节）
- 历史：2026-09-08 cookie 过期 32 天导致 B 站重连风暴（5 分钟 429 次失败），
  以 safe_mode: true 止血；cookie 更新流程经 `cmd/bili-login` 工具
- skill 沉淀时间点：安全提交 c30acbc（untrack config.yaml）

## 2026-09-08 变更落地后的事实更新

- 守护已激活：check.shell 守护段重写并实测（commit cd15de6）。判活用 `pgrep -x qqrobot`（进程名精确匹配）。
- **pgrep -f 自匹配陷阱（重要教训）**：锚定模式 `pgrep -f 'bin/qqrobot$'` 会匹配到**触发守护的命令链自身的 cmdline**（命令串里含同字样），导致"进程在→跳过拉起"误判。`pgrep -x` 按进程名精确等于匹配，从机制上杜绝。
- 构建固化：`bash build.sh`（项目根），GOROOT=/usr/local/go.bak + GOPROXY=off 全显式，产物 bin/qqrobot。
- 守护拉起命令：`setsid nohup bin/qqrobot >> robot.log 2>&1 &`；拉起前最后 200 行日志转存 logs/log.error.$DATE。
- 残留归档：robot.log.pre_safeboot / robot.log.safeboot / robot.log.bak / cookie.json.bak → logs/history/；根目录仅保留 cookie.json.expired.bak（稳定性观察后处理）。
- git 提交 cd15de6（本地，未 push；远端权限待用户定）。

- **flock fd 继承陷阱（重要教训）**：守护脚本 `exec 9>lock` 后用 setsid/nohup 拉起 bot，fd 9（含锁）被子进程继承——bot 活多久锁就被占多久，后续 check.shell 全部 flock 短路，**轮转/清理静默停摆**（回归！）。修法：启动命令尾部 `9>&-` 显式关闭继承。验证手段：`lsof /tmp/qqrobot-check.lock | wc -l` 应为 0。
- 守护事件现在 tee -a 到 cron.log（绝对路径），crontab 重定向同步改为 `>>`（原 `>` 每次覆盖会抹掉追加记录）。二次运行实测：skip 记录 + 轮转记录同现（commit 见 git log）。

## 2026-09-08 控制链路优化（change qqrobot-control-link-optimization）

### 本地指令通道（agent → robot，双向链路的"下行"）
- **GET 127.0.0.1:9601/cmd**：列出全部注册指令（keyword+aliases，JSON）。实现：`service.ListCommands()`（ops_registry.go，只读枚举，注册表不导出）。
- **POST /cmd {"command":"help","args":[...]}**：以合成作者 `local-admin` 走 `handleCommonAtMessage` 分发——与 QQ @消息**完全同路径**（注册表→白名单闸）。返回 200 accepted / 404 未知 / 400 参数。实现：ops_cmd.go。
- **语义**：本地回环调用者=可信本机操作员。这是"绕过 QQ 传输"而非"绕过指令语义"——业务指令白名单闸仍在，local-admin 是内建放行项（见下）。
- **重要时序教训**：不能靠 hook RegisterCommand 做镜像——service 包 init()（注册 17 条指令）先于 main 包 init() 跑，hook 挂上时注册已结束。只读枚举函数才是正解。

### 白名单两态（service/whitelist.go，替换 engine.go 硬编码 return true）
- `channel_robot.white_list`（config.yaml，[]string）：**配置了→严格校验**（TrimSpace 后精确匹配）；**未配置→allow-all 兼容模式**+首次触发醒目 Warn（sync.Once 一次性）。
- `local-admin` 无条件放行（host 侧合成身份，QQ 消息伪造不出——作者 ID 来自网关 JSON，但 local-admin 非合法 QQ openid 格式）。实测 QQ 侧伪造面：网关消息 Author 字段来自平台，本地无法注入。
- 测试位置：whitelist_ext_test.go（**主包**而非 service 包——repo init() 用仓库根相对路径读 dict.txt，service 包测试二进制必死，教训见 jieba.go）。

### robot → agent 事件上报（双向链路的"上行"）
- 契约：`POST 127.0.0.1:8089/task`，body `{"messages":[{"role":"user","content":"[qqrobot-guard][level] msg"}],"user_id":"qqrobot-guard","session_id":"qqrobot-ops"}` → 202 注入 tagent 事件循环（tagent/rl/http_api.go handlePostTask）。
- check.shell notify_agent()：拉起成功→info；拉起失败→crit；healthz 无响应→warn；cookie >26h→warn；**健康→完全静默**（没事不吵）。
- 端到端自测已通：curl 发 payload→202→消息出现在 agent 会话。
- crontab 已从每小时改 `*/30 * * * *`。

### robot.log..gz 双点 bug（根因+修复）
- 根因：2026-09-08 上午重写 check.shell 时把 `DATE=` 赋值行弄丢（注释里还留着 $DATE 引用），cron 环境 DATE 空 → `robot.log.${DATE}` → `robot.log.` → gzip 出 `robot.log..gz`，且 gzip "already exists; not overwritten" 每小时刷 cron.log。
- 修复：行首补 `DATE="$(date +%Y%m%d_%H%M%S)"`；历史残留 `logs/robot.log..gz` 已改名归档。
- **教训**：重写脚本时 grep 一遍 `\$[A-Z_]*` 引用 vs 赋值行，防"幽灵变量"。

### 干跑验证方法论
- check.shell 类脚本五分支验证：进程在+健康（静默）/ healthz 死（warn）/ cookie 过龄（warn）/ 拉起成功（info）/ 拉起失败（crit）。
- 桩注入要点：脚本行首 `PATH=` 会**自重置 PATH**，环境变量注入无效——须 sed 改副本（PATH 行+BIN 行指向桩）。真实健康分支直接跑生产脚本即可（幂等：skip start）。
