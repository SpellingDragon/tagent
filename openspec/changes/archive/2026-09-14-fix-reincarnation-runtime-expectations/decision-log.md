# Decision Log — fix-reincarnation-runtime-expectations

## D-2026-09-13-01 主模型供应商回退（坑4 / tasks 5.3）
- **背景**：cc21241（18:08，标题纯 refactor）夹带 zhipu→deepseek 全量切换（主模型 glm-5.3-flash→deepseek-flash），与用户 2026-09-11 指令「机器人模型配置全部改 glm-5.3-flash」冲突。
- **决策**：用户 2026-09-13 19:4x 拍板「回退为 glm 模型，优先回退」。选 A：恢复 zhipu/glm-5.3-flash。
- **落地**：`65aff87 revert(config): restore glm-5.3-flash as bot primary model`（tagent.yaml:18-19 provider: zhipu / model: glm-5.3-flash）。
- **生效验证（20:02 换装后）**：新进程 pid=887174（旧 662911 20:02:04 SIGTERM）；`go version -m` vcs.revision=764f6a8（=HEAD）、modified=false；healthz ok；启动横幅 `wiring.go:156 resolved direct model "glm-5.3-flash" via provider "zhipu"`。
- **遗留**：热更不覆盖全局默认模型（orgSubset 无该字段）+ wechat-bot 未武装 WithConfigPath —— 归入 tasks 5.1。

## D-2026-09-13-02 双链路保留的评审结论（tasks 5.1 子项）
- **裁定**：entry agent 的 `main.go openai.New()+SwappableModel` 链路**保留**，不迁入 provider 工厂。理由：① `TAGENT_API_ENDPOINT` 环境变量覆盖（AReaL RL 训练代理）是部署期语义，工厂解析不含；② SwappableModel 的运行时 Swap（HTTP API 动态换端点）要求持有具体实例引用；③ 除此之外的**全局 fallback 链路已统一**（resolveGlobalDefaultModel），观测盲区已修（override 路径补包装）。
- **风险清点**：TrajectoryRecorder 出错透传（recordGenerateContent L228-251 先 record 后 return err）；无 *SwappableModel 类型断言（全仓扫描 0 命中）；包装外置保证 swap 后仍被记录。
