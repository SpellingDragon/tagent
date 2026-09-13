# Decision Log — fix-reincarnation-runtime-expectations

## D-2026-09-13-01 主模型供应商回退（坑4 / tasks 5.3）
- **背景**：cc21241（18:08，标题纯 refactor）夹带 zhipu→deepseek 全量切换（主模型 glm-5.3-flash→deepseek-flash），与用户 2026-09-11 指令「机器人模型配置全部改 glm-5.3-flash」冲突。
- **决策**：用户 2026-09-13 19:4x 拍板「回退为 glm 模型，优先回退」。选 A：恢复 zhipu/glm-5.3-flash。
- **落地**：`65aff87 revert(config): restore glm-5.3-flash as bot primary model`（tagent.yaml:18-19 provider: zhipu / model: glm-5.3-flash）。
- **生效验证（20:02 换装后）**：新进程 pid=887174（旧 662911 20:02:04 SIGTERM）；`go version -m` vcs.revision=764f6a8（=HEAD）、modified=false；healthz ok；启动横幅 `wiring.go:156 resolved direct model "glm-5.3-flash" via provider "zhipu"`。
- **遗留**：热更不覆盖全局默认模型（orgSubset 无该字段）+ wechat-bot 未武装 WithConfigPath —— 归入 tasks 5.1。
