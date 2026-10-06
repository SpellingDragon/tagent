---
name: go-service-ops
description: >
  Go 服务运维通用技能：旧工具链隔离构建（quic-go 类哨兵文件、GOROOT 污染、离线
  GOPROXY=off、fork 模块 replace）、长驻 bot/服务启停与三层健康验证（进程≠健康）、
  safe_mode 类止血开关、重连风暴诊断、扫码登录轮换凭据（gen/verify 两段式）、
  配置密钥卫生。适用于：go build 哨兵报错、version does not match go tool、
  服务假活、崩溃无守护、重连失败刷屏、cookie/token 过期换新、密钥进 git。
---

# Go 服务运维技能（构建 · 守护 · 凭据轮换）

## 🎯 何时使用

- `go build` 报 `can't be built on Go 1.20 yet` / `version "goX" does not match go tool`
- 长驻服务需要启停/重启，或"进程活着但服务不健康"的假活问题
- 日志出现刷屏式 `重连失败`（典型根因：凭据过期后无限重试）
- 需要用户扫码更新 cookie/token 类凭据
- 配置文件里密钥的权限/版本控制卫生

## 模式一：旧工具链隔离构建

**症状→根因**：依赖树里 quic-go/imroc-req 类库带 `internal/qtls/go120.go` **哨兵文件**，
故意在 Go≥1.20 编译失败（逼你升级依赖）。老项目（go.mod 声明 1.18）→ 用旧工具链。

四个必须显式设的环境变量（缺一翻车）：

| 变量 | 要点 |
|------|------|
| `GOROOT` | 指向旧工具链根目录。⚠️ 全局 shell 若 export 了新 GOROOT 会污染旧二进制（报 version mismatch） |
| `GOPATH` | 确认模块缓存实际位置——不同 GOPATH = 不同缓存，离线构建拿不到包 |
| `GOCACHE` | 与新版 Go 的构建缓存分开 |
| `GOPROXY`/`GOSUMDB` | 内网/GFW 环境：`GOPROXY=off GOSUMDB=off` 纯离线（前提：缓存已预热）；国内在线用阿里云镜像 |

**离线加依赖的技巧**：新工具/子命令**放进主项目 `cmd/<name>/` 子包**构建，直接复用主项目
go.sum 的完整依赖图——另建 module 在离线下几乎必然 `missing go.sum entry`。
**fork 模块**：照抄主项目的 replace 方案（import 原路径 + `replace 原 => fork 版本`；
注意 fork 的 go.mod 若没改 module 声明，直接 import fork 路径会报 path mismatch）。

**长编译防误杀**：tmux 监控型 exec 会在静默期把长编译判成假死。改用
`setsid nohup bash -c 'build ...; echo $? > /tmp/marker' & `，产物+标记文件读回验证。

## 模式二：长驻服务三层健康验证 + 止血开关

**进程活 ≠ 服务健康**（WS 断了进程照样在）。验证三层，全部读回：
1. 进程存在（`ps -p <pid>`）
2. 外连建立（`ss -tnp | grep <pid>` 有 ESTAB）
3. **业务日志健康计数**：`grep -c '失败模式串'` 两次采样对比，增长 = 风暴进行中

- 启动失败即退的程序（`log.Fatalln` 类）不会带病运行 → 启动后 ps 读回是必须动作
- 重启前先轮转日志（若 cron 有 truncate 型轮转，旧日志会被清掉丢证据）
- **风暴止血**：找配置/代码里的 safe_mode 类开关（跳过出问题的注册逻辑），先止损再修根因；
  功能取舍（关掉某功能）需用户拍板
- 崩溃无守护时：cron 拉起段常被注释停用，检查 crontab 别想当然

## 模式三：扫码凭据轮换（gen/verify 两段式）

TV/设备扫码登录通用流程：`gen`（拿 URL+authCode → 渲染二维码 PNG → authCode 存 state
文件 0600）→ 用户手机 App 扫码 → `verify`（轮询 poll 接口 → 成功写凭据文件）。

坑与对策：
- 上游轮询循环常**没有超时** → 外面套 `timeout 120`，以凭据文件 **mtime+size 读回**为成功判据
- 二维码有效期短（约 3 分钟）→ 过期就重新 gen（旧 authCode 作废），**投递前现生成**
- 凭据文件落盘后 `chmod 600`
- 过期判据通常在启动日志：`expired after -N second`（N 为负即已过期）

## 模式四：配置密钥卫生

- 权限 644 → **600**（同机任意用户可读 = 泄露面）
- 查 git 跟踪：`git ls-files | grep <配置名>`；被跟踪则 `git rm --cached` + 进 `.gitignore`
  （工作区文件不动），并提醒**历史 commit 里的密钥仍需轮换**
- 文件含密钥时**不整读进上下文**：用 python yaml 只做非空校验，输出 `SET/EMPTY + 长度`
- 警惕代码把 DSN/密码打进日志（会被日志轮转备份扩散）

## 模式五：配置热加载边界（org-hotreload）

tagent.yaml 改动自动热加载，但**生效范围有边界**（2026-10-05 实证）：

- **热应用通道**：常驻拓扑 agent（entry + 声明式子 agent，如 wechat-bot 的 tagent/knowledge/plan）的数值参数——`max_tokens`、`compress_threshold`、`keep_recent_tasks`、`task_terminal_ttl`、`task_default_ttl`。文件保存后秒级生效，日志 `[org-hotreload] agent "X" hot params applied: threshold=… maxTokens=…` 为准。
- **不在通道内**：内建工具型 agent（recall/action，由 `tool/*_agent.go` factory 构造）——参数构建时定型，热应用不覆盖，**需换装重启生效**。
- **必须重启的变更**：memory 段、entry 身份、模型/provider、system_prompt、tools 清单（指纹内的结构性字段，命中即拒热更并提示 RESTART required）。
- **验证姿势**：改完等 ~40s，`grep 'hot params applied' logs/*.log | tail`；数值变更还会记入回滚环（`numeric-only full apply recorded`），改坏可还原。
- **凭据强度（2026-10-06 外部评审教训）**：注释级探针（改注释让 mtime 变）只触发"同值重应用"——`hot params applied` 里的值与旧值相同，证明管道在跑但**不证明变更值被消费**。值级验证必须真改数值（如 keep_recent_tasks 1→5），以日志出现新值（keepRecent=5）为凭据。
- **探针闭环**：验证用临时值**用完即还原**（改回原值 + git status 干净），配置面不留探针残留——曾因忘还原被用户抓到。
- **坑**：factory 构造不透传 `compress_threshold`（ToolAgentFactoryConfig 无此字段），即便重启，recall/action 的阈值也是框架默认——改它无效，属已知边界非配置错误。

## 故障速查（跨项目）

| 症状 | 根因 |
|------|------|
| `cannot use "The version of quic-go..." as int value` | Go≥1.20 撞哨兵文件 → 换旧工具链 |
| `version "goX" does not match go tool version "goY"` | shell 全局 GOROOT 污染旧工具链 |
| `missing go.sum entry`（离线新建 module） | 依赖图解析不了 → 并入主项目 cmd/ 子包 |
| tmux 长命令被"假死"误杀 | 编译静默无输出 → setsid nohup + 完成标记 |
| verify 永远不返回 | 上游轮询无超时 → 外套 timeout，查 authCode 是否过期 |

## 📌 实例档案

本机 QQchannelRobot 的具体路径/命令/本项目特有坑 → 见同目录 `qqchannel-robot-facts.md`。
