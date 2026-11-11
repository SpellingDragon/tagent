# 真实任务对照（live-benchmark）

父变更：boundary-closure（W3；依赖 02 入 main——预算语义稳定后对照才公平）。

## Why

评审结论：快结果先推进等机制的净收益从未测过——"是否更高效，需要固定模型、工具和任务后的对照实验"。README 吞吐数据来自 11 月 1 日另一开发快照的 scripted HTTP 台架，不能代表当前 main。不测收益，"边界闭合"就只是成本项。

## What Changes

1. 对照 harness：固定任务集（12 个真实任务：问答×3、文件操作×3、exec 执行×3、多轮依赖×3）、固定模型（zhipu coding 端点 glm-5.3，ZAI_API_KEY）、固定工具面与预算；三方 runner——最小基线（纯 trpc-agent-go 循环，无 tagent 机制）、tagent（经 `examples/wechat-bot/cmd/bench-bridge` 子进程驱动 wechat-bot 配置面）、DeepSeek Harness（本机 checkout /Users/pengweiye/Documents/codes/deepseek-harness/，适配器跑同任务集）。
2. 五指标采集：任务成功率、结果送达率、响应延迟（首包/完成；冷启动与热路径分列）、token 成本、人工介入次数（**=审批入口调用数＋blocked 上报数，jq 字段含 breakdown 分项**）；**送达率口径统一为"发送方渠道确认"**（tagent=桥接内渠道 Send 回执；DSH=发送 API 成功；无渠道基线=任务终态并注明口径差）；run 记录含 status 字段。
3. run 落盘：每次对照一个 run-id，metrics JSON + 逐任务明细可回读；报告表自动生成；run 记录含 `status`（completed/skipped/blocked）。
4. R24 三态门（**双层不混用**）：**run 级（运行级）**——无 ZAI_API_KEY → Skip 记账（不阻断 W3 其余与 harness/DSH 适配交付）；有键缺配 → Fatal 上报；跑通 → 非空转证明（真实 API 调用计数入账）。**change 级（变更级）收尾**——仍无凭据 → blocked 法定停点上报（不结项）。

## 既有能力裁决（5.9）

- `evals-harness` 不重合：那是组件级 mock 评估（票据召回率/工具白名单，CI 可跑）；本域是**真实模型三方对照实验**（run-id+五指标），目录独立（bench/ 不入 evals/）。

## 范围边界

- 对照结果的系统消费不在本变更范围；本域只产出 run+报告+可回读明细，不建消费面、不改产品运行时。

## 边界与依赖

- **依赖**：02 入 main（接口常数：预算语义）；DSH 对照需其本机 checkout 可运行（只读使用，不改 DSH 仓库）。
- **被依赖方**：07 复用本域任务集子集作训练样本夹具；评审下一轮"收益验证"结论以本域 run 为凭。
- **接口面**：新增 bench/ 目录（harness、任务夹具、指标 schema）与 **examples/wechat-bot/cmd/bench-bridge/** 微型 main（复用 wechat-bot 模块既有装配与工具面；行式 stdin 协议交付任务/回传结果与指标）；`bench/`、`bench/tasks/`、`bench/runners/`、`bench/metrics/`、`bench/report/` 登记 `golden/runtime/architecture.json`（packages+edges；无 bench/bench）；不改 api/**；不改产品运行时行为。
- **禁止事项**：不动 assembly/、tool/exec、memory/（对照实验不得顺手改被测对象）；DSH 仓库零写入。
