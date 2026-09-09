## Why

QQchannelRobot 因 B站 cookie.json 过期 32 天陷入重连风暴，已停机并以 safe_mode 重启止损（调用方声称新日志重连失败计数=0）。需通过独立扫码登录工具完成 B站凭据续期，并在用户明确确认后关闭 safe_mode 恢复正常模式。

## What Changes

- 核验前置状态：读回 config.yaml 的 safe_mode 标志与重启后日志的重连失败计数，确立"无风暴"基线证据
- 构建 ~/qqlogin-tool（gen/verify 两子命令，复用 biliup-go login 包）：补 go.sum 后以 GOROOT=/usr/local/go.bak 显式隔离 + GOPROXY=off 离线构建
- gen 生成二维码 PNG → 微信投递用户 → 扫码后 verify 写入 cookie.json → 读回验证有效性
- 汇报结果并等用户点头后：关闭 safe_mode、正常模式重启、观察日志确认恢复且无风暴复发

## Impact

- 文件：config.yaml（safe_mode 开关）、cookie.json（B站凭据更新）、~/qqlogin-tool/（工具源码/go.sum/二进制/二维码产物）
- 运行时：机器人有一次停机→重启窗口；恢复后B站连接应正常
- 硬约束：构建必须 GOROOT=/usr/local/go.bak + GOPROXY=off（仅用已有模块缓存）；verify 外层必须套 timeout 防上游无限轮询；所有状态断言必须读回文件/日志证据，不接受口头声称
