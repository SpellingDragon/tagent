## Context

运行中的 wechat-bot（PID 702169）依赖 `examples/wechat-bot/tagent.yaml` 配置。昨天的补丁引入了 `unified_compress_target: true`（config 字段 `UnifiedCompressTarget`），但因缩进层级不符导致 yaml 解析失败。用户已注释该行恢复运行，现决定彻底放弃该配置开关。

## Goals / Non-Goals

**Goals:**
- tagent.yaml 恢复到打补丁前干净状态（对照备份核对）
- 压缩死区修复改为代码默认行为（buildCompressorOpts 分龄目标默认对齐触发线）
- 删除 UnifiedCompressTarget 字段及全部映射/消费代码
- go build 主框架 + wechat-bot 全绿
- 排查拉起机制缺失原因（只读排查）

**Non-Goals:**
- 不重启运行中的 bot（PID 702169），代码改动等下次计划内重启生效
- 不在本计划内修复 crontab/systemd/watchdog（只排查、给结论）

## Decisions

### 决策 1：行为内化优于配置开关

**背景**：`unified_compress_target` 作为 yaml 开关存在两层问题：a) 用户侧缩进错误导致启动失败，暴露出配置面脆弱；b) 死区空转本质是代码默认行为缺陷，不应由用户配置兜底。

**决策**：把"分龄压缩目标对齐触发线"作为 `buildCompressorOpts` 的默认行为（无分支、无条件），删除配置开关。做到"无配置即正确"。

**理由**：符合"最小配置面"原则——正确的默认行为不设开关；同时移除了配置错误的攻击面。

### 决策 2：yaml 恢复以备份为唯一基准

**背景**：抢救过程中 keep_recent_tasks: 4 可能被误注释。

**决策**：以 `/tmp/tagent.yaml.bak.*` 中**打补丁前**的最新备份为基准，逐行对照恢复；不以记忆或推断为基准。

**理由**：备份是唯一可信基准，避免"恢复"引入新偏差。若备份与当前文件存在其他差异（非补丁相关），需先向用户确认再动。

### 决策 3：排查只读、结论留档

**背景**：进程停止后未被自动拉起，说明 crontab/systemd/watchdog 存在缺口，但运行中的 bot 不能重启。

**决策**：排查限于读取 crontab -l、systemctl list-units / unit 文件、watchdog 相关配置/日志，输出结论清单写入本 change 目录；不做任何修复性改动。

**理由**：修复拉起机制属于独立工作，与本次"yaml 清理 + 行为内化"目标正交；且修复动作可能触发进程管理行为，违反"不重启 bot"约束。

## Risks / Trade-offs

- **运行中 bot 与磁盘代码漂移**：代码改动后、下次重启前，运行中进程仍是旧行为（已注释掉开关的旧代码）。属预期内（用户已接受"等下次计划内重启生效"），但在重启前不要误判"死区空转已消失"。
- **备份不可用风险**：若 /tmp/tagent.yaml.bak.* 已被清理（/tmp 重启即失），需回退到"手动逐行修复 + 用户确认"路径。
- **构建验证范围**：go build 需覆盖主框架与 examples/wechat-bot 两个模块路径，避免只验其一造成假绿。

## Migration Plan

1. 先恢复 yaml（bot 运行不受影响——运行中进程不重读配置），核对 keep_recent_tasks 状态
2. 再改代码（默认对齐触发线 + 删字段），go build 双模块验证
3. 拉起机制排查结论留档
4. 代码行为在下次计划内重启时生效；重启前运行中 bot 行为不变（其配置中开关已被注释，不受字段删除影响）
