# 06-learning — 任务清单（tasks.md）

> 二级子变更 `06-learning` 的三级孙任务。**全部未勾选**（`- [ ]`），**不继承**旧 change 完成数。
> 状态仅 `planned` / `design-blocked`；**本轮不声称 Code 实际 PASS**。
> 编号与只读计划 §7「06-learning」行一一对应，每原编号一次。
>
> **验证命令基线**（仓库根执行）：
> - 确定性（并发族带 `-race`，重复 10 次 + JSON）：
>   `go test -race ./tests/integration/learning -run '^<Test名>$' -count=10 -json`
> - **真实 SFT/反馈资源**：独立门控、单次 `-count=1`；缺凭据/模板不支持/`SKIP` **不能计完成**。
> - 判定口径：点名用例真实 `pass`；零匹配/`Skip`/包级 `Fail` 不算通过；不用 `grep/head/tail` 退出码替代原命令。
> - 产品实现单共同前置 G0–G3 不逐行重复。

## 前置与边界

- **上游**：01-state-binding（01.5）、02-execution（02.4 模型终局、02.5 装配）、03-history（03.3 持久提交）。本域消费，不改其装配。
- **写白名单（未来，本轮不写）**：`rl/` 采集/导出/消费相关产品包、真实消费脚本与 `tests/integration/learning/**`。**本轮只写本目录 md**。
- **禁止**：接 PPO/GRPO 引擎；默认 AReaL 新依赖；自造常量消费者结项；把 capture 当恢复真源；secrets 落盘。
- 状态标签：`planned` / `design-blocked`。

## 任务

- [ ] 06.1 接通**版本化记录**与 episode/decision/attempt **实际生产点**（字段解释由 00.3 审定）；旧格式**只读导入**、未知谱系显式；新格式**明确版本**不被旧 v2 结构绑架 —— 状态：planned —— 前置：02.5 —— 候选文件：`rl/record.go`、`rl/episode.go`、`tests/integration/learning/causal_records_test.go` —— 验证：`go test -race ./tests/integration/learning -run '^TestCausalTrainingRecords$' -count=10 -json`

- [ ] 06.2 实现**请求边界安全快照**、原生响应/logprobs 保留、统一终局观察；**Headers/URL secrets 不落盘**；logprobs **确实返回才 present**、`unsupported` 需支持声明、`unknown≠not_returned`；不重跑 provider options 造快照 —— 状态：planned —— 前置：02.4、06.1 —— 候选文件：`rl/capture.go`、`rl/record.go`、`tests/integration/learning/capture_fidelity_test.go` —— 验证：`go test -race ./tests/integration/learning -run '^TestCaptureFidelityAndRedaction$' -count=10 -json`

- [ ] 06.3 实现有界队列、字节预算、**丢失账**、发送/关闭准入与 **writer 封账**；采集与 model **共享终局**、发送与关闭**同一 protocol**、**no seal 记 partial** —— 状态：planned —— 前置：06.2 —— 候选文件：`rl/sink.go`、`tests/integration/learning/capture_admission_test.go` —— 验证：`go test -race ./tests/integration/learning -run '^TestCaptureAdmissionAndSeal$' -count=10 -json`

- [ ] 06.4 实现授权导出、延迟反馈、**版本化 reward 派生**、逐样本来源 manifest；**feedback/reward 分开** —— 状态：planned —— 前置：03.3、06.3 —— 候选文件：`rl/export.go`、`tests/integration/learning/authorized_export_test.go` —— 验证：`go test -race ./tests/integration/learning -run '^TestAuthorizedTrainingExport$' -count=10 -json`

- [ ] 06.5 实现严格转换、消息合法性、动作/观察 mask、用途 eligibility、**episode≠session** 及基于**因果依赖**的分组防泄漏；**既有转换器 v2 只读导入** —— 状态：planned —— 前置：06.4 —— 候选文件：`rl/sft.go`、`scripts/train/convert_trajectories.py`（只读导入）、`tests/integration/learning/sft_cleaning_test.go` —— 验证：`go test -race ./tests/integration/learning -run '^TestSFTCleaningContract$' -count=10 -json`

- [ ] 06.6 实现**真实 SFT 消费者**读取与 tokenize/collate，检查 labels/mask/非空样本与拒绝账（**真实 tokenizer/chat template，不以常量替身结项**） —— 状态：**design-blocked**（真实制品/入口待授权） —— 前置：06.5 及授权制品 —— 候选文件：`rl/sft_consumer.go`、`tests/integration/learning/sft_consumer_test.go` —— 验证（真实资源，独立单次门控）：`go test ./tests/integration/learning -run '^TestSFTConsumer$' -count=1 -json`（缺凭据/模板不支持/`SKIP` **不能计完成**）

- [ ] 06.7 实现 RL HTTP 鉴权、allowlist、**逐跳重定向**、大小限制与反馈归属 —— 状态：planned —— 前置：06.4 —— 候选文件：`rl/http.go`、`tests/integration/learning/feedback_http_test.go` —— 验证：`go test -race ./tests/integration/learning -run '^TestFeedbackHTTPContract$' -count=10 -json`；真实端点另列单次门控 `-count=1`

- [ ] 06.8 实现**模型替换租约**贯穿实际请求和流结束，重建失败**不发布错误绑定** —— 状态：planned —— 前置：01.5、06.7 —— 候选文件：`rl/replacement.go`、`tests/integration/learning/model_replacement_test.go` —— 验证：`go test -race ./tests/integration/learning -run '^TestModelReplacementLifecycle$' -count=10 -json`

## 缺口修复

（执行期缺口写在此节：场景 → 根源 → 影响面。契约/版本变化先回写本域 design 与顶层计划。本轮无。）
