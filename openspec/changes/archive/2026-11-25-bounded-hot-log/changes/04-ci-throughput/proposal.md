# Proposal：CI 吞吐（ci-throughput）

## Why

用户 2026-11-23 裁定：CI 效率过低，且优化须从框架机制归因后实施。实测（bde7f394 两次 run 分解）：单 run 墙钟 22.6–25.6 分钟；关键路径＝race 单 job 13–15 分钟；资源大头＝event-integrity 家族 17 个 job（10 短×3.5m＋6 长×5–9m＋变异 6.5m）；wall clock 超出最慢 job 的 8–10 分钟为 24 job 的并发排队；全 workflow 无 actions/cache，每 job 冷 toolchain＋冷编译＋apt 安装。

机制归因（design D0）：耗时形状是**证明义务的镜像**——真实生命周期测试、`-race` 全覆盖、重复轮次对抗非确定性、变异咬合都是不可删的证明；纯浪费在**组织层**：无缓存、race 未分片、（双触发与变异分片为可选裁项）。本域只优化「证明的执行方式」，不删任何证明。

吞吐读数以真 CI 为权威面，而共享 runner 上的 consumer 风暴、restart 恢复通知等待与完成通知接受/认领竞态曾使同一 test/race job 非确定红；不先隔离或修复这些稳定性缺陷，前后读数不可比。故本域同时承接这三类稳定性前置修复：它们是读数可复现的前提，不是产品行为变更，也 MUST NOT 以放宽断言或扩窗换取绿。

## 边界与依赖

- 父变更 bounded-hot-log；本域不依赖 01 契约，可自启动门起与 01/02/03 并行。
- 依赖：`.github/workflows/ci.yml` 现行 job 结构、`scripts/gates/race_check.sh`（含 `test_race_check.sh` 自测）、consumer 与 restart 的既有测试入口、完成通知的接受/认领实现（`assembly/facts.go` 与 `runtime/state`）。
- 被依赖方：F2 全量门禁（按新 CI 形态取证）、03.302 的矩阵证据（同一冻结树时点取数）。
- 接口面：workflow 的 `setup-go` cache 配置、race 的 `--shard` 参数（包集唯一来源在 race_check.sh）、`race-aggregate` job 状态、consumer 独立执行 step、restart 夹具的等待谓词、完成通知的原子 handover。
- 禁止：以删测试/缩包集/跳过/放宽阈值换墙钟；让缓存参与任何判定；在本域内改 push/PR 触发策略（须单独裁）。稳定性修复同样 MUST NOT 扩窗、删除断言或标记跳过。
- 写面声明：本域 MUST NOT 被宣称只改 workflow 与 race_check；稳定性前置另触达测试夹具、`assembly/facts.go` 与 `runtime/state` 的完成通知面，逐项列在 What Changes 与 Impact。

## What Changes

- **Go 缓存接线**：`setup-go` 开启 module/build cache（键随 go.sum），跨 job 复用；缓存只加速编译，**不参与任何判定**。
- **race 分片**：包集定义收敛到 `scripts/gates/race_check.sh`（shard 参数为唯一来源）；workflow 改为 matrix（3 分片）＋聚合 job（needs 全绿；任一红则聚合红）；脚本内置「分片并集 ≡ 原全包集」自检（缺包/重包即红）。
- **稳定性前置修复**（读数可复现的前提，属本域）：consumer 风暴重路径移出包内并发，同 job 独立 step 串行复跑（不加时不改断言）；restart 夹具在恢复通知模型请求与答案完成后再发显式 invocation；mailbox（完成通知）的接受与认领走原子 handover，消除 AcceptNotice 与 Claim 之间的竞态。三项各自保留原判据与锚；证据来源为 ORCHESTRATION 台账的三条 2026-11-24 记录（consumer 风暴隔离、restart 夹具竞态修复、`b66cda5b` 原子 handover）。
- **读数与证据**：以真 CI 为权威面，记录变更前后 job 分解与墙钟；关键路径目标＝分片后 race 墙钟 < 原单 job 的 60%，run 墙钟同步下降；冷/热缓存两轮均须绿。
- **显式不做**：重复轮次矩阵（10 短 6 长）不动；变异矩阵不动（除非按组拆分被证明零语义变化，列为可选）；**双触发去重不做**（门面语义变更，须单独裁）。

## Capabilities

### New Capabilities

- `ci-throughput`：缓存「只加速不判定」、race 分片「并集保真＋聚合 fail-closed」、consumer/restart/mailbox 稳定性前置「保留原判据」、墙钟/吞吐读数前后对比。

## Symptom Anchors

**S1（第一天红）**：`check_race_shards` 的并集断言——当前无分片定义，断言「3 个 shard 的并集 ≡ CI 全包集」红；分片落地后转绿。
**S2**：分片缺包/重包的负场景——人为从 shard 定义删一个包，并集断言必红。

## Impact

吞吐优化面：`.github/workflows/ci.yml`（cache、race matrix、aggregate job、consumer 独立 step）、`scripts/gates/race_check.sh`（shard 定义与自检）、`scripts/gates/test_race_check.sh`（分片自测）。稳定性前置面：consumer 与 restart 相关测试夹具、`assembly/facts.go` 与 `runtime/state` 的完成通知接受/认领。吞吐优化本身不改产品行为与测试断言，不改重复轮次与变异矩阵；稳定性前置修复按各自锚变更实现与夹具，但 MUST NOT 改动断言值或放宽阈值。
