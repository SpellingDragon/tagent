# 拉起机制排查结论（只读，2026-09-09）

## 现象
tagent wechat-bot 进程昨晚退出后未被自动拉起，需用户手动重启（18:11 手动拉起成功，PID 702169）。

## 排查证据（全程只读，未触碰运行中的 bot）
| 检查项 | 结果 |
|--------|------|
| `crontab -l`（完整读取） | **存在两条 tagent 条目**：每分钟 `flock` 跑 `restart-maintenance.sh`（marker `tagent_restart_marker`）+ 诊断探针。初版结论"无条目"系仅读了 crontab 前 10 行注释头所致，**已修正** |
| systemd / supervisor | 无相关 unit，supervisor 未安装（这两项维持原判） |
| 进程父链 | bot PID 702169 的 PPID=1（init），系看门狗 posix_spawn 分离启动，非"无守护者"的证据 |
| `logs/restart.log` 18:07-18:12 | 看门狗**每分钟都在尝试拉起**，全部死于构建失败：`go: downloading ollama v0.16.3 → zip: not a valid zip file`；脚本设计"构建失败不动服务"，故放弃 |

## 结论（修正版）
**"没被自动拉起"不是没有机制，而是拉起链条连续两个故障：**
1. **第一故障**：`tagent.yaml` 中 `unified_compress_target` 行缩进错误 → 用户手动启动时 yaml 解析失败、启动报错；
2. **第二故障**：看门狗（cron 每分钟）虽在拉起，但其构建环境（crontab 显式 `GOPATH=/home/lighthouse/go`）中 ollama v0.16.3 的模块 zip 损坏/缺失，且 aliyun GOPROXY 上该版本 zip 本身即损坏（重新下载仍报 not valid zip）→ `go build` 失败 → 放弃拉起；
3. **隐藏地雷**：双 GOPATH 分裂（cron 用 `/home/lighthouse/go`，shell 会话用 `/home/lighthouse/data/go`，后者缓存完好），解释了"用户手动构建/启动一次成功"。`/tmp/tagent_env.snapshot` 曾为空，看门狗拿不到正确环境快照。

## 已实施的修复（当日完成，零服务影响）
1. **看门狗环境快照**：`/tmp/tagent_env.snapshot` 与 `$BASE/run/env.snapshot` 已写入 `GOPATH=/home/lighthouse/data/go`（完好缓存侧），看门狗后续构建走好缓存；
2. **cron 侧坏缓存修复**：清掉 `/home/lighthouse/go` 下 ollama 残骸，从完好缓存离线移植 `v0.16.3.zip/ziphash/info/mod`（Go 按 ziphash 校验），cron 环境 `go build` 验证通过（CRON_ENV_BUILD_OK）；
3. **看门狗视角模拟构建**：`env -i` + snapshot 环境下构建通过（WD_SIM_BUILD_OK）；
4. 以上验证通过后，"bot 下次崩溃 → 看门狗拉起"链条恢复闭合。

## 附带发现：后台结算回复被静默丢弃（2026-09-09 当日观测）
- **现象**：`task settled` 事件触发的回合中，agent 产出的用户可见回复未送达微信（用户实际"没收到"）。
- **定位**：`main.go` L372-374——`triggerSource=task` 的回复依赖事件携带 `meta_chat_id` 路由回原会话；系统事件触发的回合若无此绑定，仅 `log.Warnf("无 meta_chat_id，无法发送")` 后 `continue` 丢弃，无兜底路由。当日 restart.log 同窗口观测到 ≥2 条被丢（18:49 状态汇总、18:50 findings 修正通报）。
- **建议修法**（单列，随下次计划内重启一并换装）：task 结算回合缺 `meta_chat_id` 时回退到"最近活跃会话"（内存维护 lastActiveChatID，由 user 触发回合刷新）；保守变体：仅投递含文件路径/告警标记的回复，其余降级为日志。

## 遗留建议（单列，不在本计划内执行）
1. **收敛双 GOPATH**：crontab 的 `GOPATH=/home/lighthouse/go` 与实际使用 `/home/lighthouse/data/go` 应统一（改 crontab 或迁移缓存），否则每个新 indirect 依赖都可能重演本次分裂；
2. GOPROXY 上 ollama v0.16.3 zip 损坏属上游问题，若未来需要全新拉取该版本，考虑 `GOPROXY=direct` 或换 proxy；
3. 长期仍建议评估 systemd unit 替代 cron 看门狗（Restart=on-failure + journald 日志管理）。

## 附带修复（本计划内已完成）
启动失败的直接根因是 `tagent.yaml` 中 `unified_compress_target` 行缩进错误导致解析失败；该配置项已彻底移除（内化为代码默认行为），yaml 恢复至与干净备份逐字节一致。
