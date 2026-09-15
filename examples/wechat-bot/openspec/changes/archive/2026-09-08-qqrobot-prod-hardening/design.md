## Context

QQchannelRobot 当前生产运行方式：cron 每小时触发 `resources/check.shell`，该脚本 L21-L40 的自动拉起守护段整体被注释停用，且原判定 `pgrep -f robot.go` 与线上以编译产物运行的事实不匹配（进程以 `bin/qqrobot` 名义运行，`robot.go` 永不匹配→守护段即使启用也会误判进程不存在、反复重启进程）。前置工作已完成（cookie 续期、safe_mode 关闭、观测端点 + qqops + bili-login 已提交 a433be1），bin/qqrobot 为 45MB 编译产物。仓库缺可复现构建脚本；工作区残留 kill 测试证据文件。约束见 proposal 的 Impact。

## Goals / Non-Goals

**Goals:**

- 守护段真正生效：正确判定进程存活、进程死时自动拉起、cron 并发不重复拉起
- 拉起与线上二进制一致（bin/qqrobot），杜绝 go run 编译竞态与污染
- 构建可复现：任何人/任何时刻可用 build.sh 重建等价产物
- 仓库与运维知识（skill 事实文档）与生产实况一致

**Non-Goals:**

- 不改 crontab 本身（路径未变）
- 不引入 systemd/supervisor 等新守护方案
- 不处理 cookie.json.expired.bak（保留观察，属后续稳定性确认）
- 不 push 远端（权限在用户）

## Decisions

**D1 守护判定 `pgrep -f 'bin/qqrobot'`**

按二进制命令行匹配进程。备选 `pgrep -x qqrobot`（按进程名精确匹配）因 -x 不看完整命令行，与历史多种启动方式（./QQchannelRobot、go run、bin/qqrobot）兼容性最稳的是 -f 'bin/qqrobot'——路径段唯一标识本部署。

**D2 拉起 `bin/qqrobot` 替代 `go run`**

go run 每次触发编译、竞态风险高（cron 并发时双编译）、错误处理复杂（子进程 PID 是 go run 的）。直接跑编译产物：进程即目标进程，kill 验证用精确 pid 无歧义。备选 systemd 被否——侵入式改造，超出本次范围。

**D3 flock 单实例**

`flock -n 锁文件 -c 命令` 或脚本头 `exec 9>lock; flock -n 9 || exit 0`。cron 最小间隔 1h，重叠概率低但非零（网络盘卡顿、系统时间跳变）；flock 零成本兜底。备选 mkdir 原子锁需处理死锁清理，复杂度高。

**D4 启动前转存 log.error.$DATE**

拉起前 `tail -n 200 robot.log > logs/log.error.$DATE` 保留崩溃现场。备选全量备份（robot.log.bak 方式）体积大、信息密度低。

**D5 build.sh 全显式环境变量**

GOROOT/GOPATH/GOCACHE/GOPROXY/GOSUMDB 全显式指向 go1.18.10 隔离目录，避免依赖用户 shell 环境（cron 环境与交互 shell 环境差异是历史上 .bashrc 导出失效的常见根因）。备选 docker 构建被否——单机部署，增加依赖。

**D6 git 不 push**

本地 commit 记录变更，远端 push 权限在用户。备选 push 被否——权限边界。

## Risks / Trade-offs

- [Risk] `pgrep -f 'bin/qqrobot'` 匹配过宽（如编辑器打开同路径文件、grep 自身匹配）
  → Mitigation: pgrep -f 匹配完整命令行，编辑器路径通常带 vim/nano 前缀不构成命令行命中；验证时用 `pgrep -af 'bin/qqrobot'` 打印实际匹配进程复核
- [Risk] flock 锁文件残留导致守护永久失效
  → Mitigation: flock 在进程退出时自动释放内核锁，锁文件残留不影响（只要不是 NFS 半连接状态）；验证时模拟并发触发确认后到实例静默退出
- [Cautious] bin/qqrobot 二进制重启拉起后，新进程可能因旧 config/cookie 状态异常退出
  → Mitigation: 拉起后 healthz/qqops 双层验证（任务 4.5.4）；若异常,robot.log 崩溃现场已被转存到 log.error.$DATE
- [Risk] 归档移动文件时 robot.log.bak 等大文件误入 git 提交
  → Mitigation: .gitignore 已含 robot.log*，归档到 logs/history/（若 logs/ 被 ignore 则天然不入库）；提交前 `git status` 逐项复核（任务 4.3）
- [Risk] 手动触发 check.shell 验证时，进程在运行却走到拉起分支（判定 bug 未修复干净）
  → Mitigation: 任务 4.5.1 专门验证"进程在→跳过拉起"路径；cron.log 跳过记录可审计

## Migration Plan

部署即生效：check.shell 修复后下一次 cron 触发即按新逻辑运行。回滚 = `git revert` 本次提交或注释回守护段（单文件变更，回滚原子）。验证顺序严格遵循 tasks 4.5 组：先"进程在"路径、再停服自愈路径，最后双层验证。

## Open Questions

（无）
