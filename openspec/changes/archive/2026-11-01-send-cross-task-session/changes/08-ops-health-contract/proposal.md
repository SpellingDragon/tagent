# 08 ops-health-contract：健康契约一致与五态探测（R6）

父变更：send-cross-task-session（依赖：无——与 01/06 并发）。

## Why

R6（✅ 实证，条件性静态推断已标注）：`ops-deployment-integrity` 主 spec 钉"`/healthz` 保持鉴权不豁免"，但 `host_face.go` 的 healthz 直接 200 不经 `authorize`——**实现对既有 spec 的欠账**；维护脚本注释称 healthz 受 token 保护，与实现矛盾；脚本前置探测只认 200，401/403 专门判断在重启后健康检查——鉴权失败可能先走重建/杀进程路径。

## What Changes

- healthz 契约对齐 `ops-deployment-integrity`（鉴权不豁免），或显式修订该条款（二选一在本域首叶定夺，被否方案入 design 记录）。
- 运维脚本探测区分五态（200/401/403/超时/拒连）：401/403→AUTH_FAIL 告警**不杀进程**、拒连/超时→等待窗口内正常状态、200→健康继续。
- 脚本注释与实现一致（文档不撒谎）。

## 边界与依赖

- **依赖**：无。
- **被依赖方**：F 收尾（门禁）。
- **接口面**：`hostFace.Handler` healthz、`restart-tagent.sh`/`restart-maintenance.sh` 探测段。
- **禁止事项**：不动 `/task`、`/reload` 鉴权语义；不动保险链换装逻辑；不改 healthz 响应体（200 "ok"）。
