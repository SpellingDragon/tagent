# Tasks

## 1. P0 止血：pruneTerminal nil detector（已完成）
- [x] 1.1 根因定位：完整 panic 栈 + `RestoreTask` 不设 detector 的赋值点全量审计
- [x] 1.2 修复 `agent/task/task_manager.go` L769：持 `t.mu` 读 detector + nil 判空
- [x] 1.3 回归测试 `agent/task/task_prune_nil_detector_test.go`（真 RestoreTask 路径 + List/Spawn 双入口）
- [x] 1.4 `gofmt` / `go build ./...` / `go vet` / `go test ./agent/...` 全绿
- [x] 1.5 commit `4aedeef`
- [ ] 1.6 换装部署到运行进程，确认新二进制携带该修复

## 2. P1 plan 工具 provider 配置（已完成待部署）
- [x] 2.1 根因：`wiring.go:37-40` 空 agent provider 回退全局 deepseek
- [x] 2.2 修复 `tagent.yaml` plan 段补 `provider: zhipu`
- [x] 2.3 commit `d438666`
- [ ] 2.4 换装后实测 `plan` 工具恢复可用

## 3. 转世运行预期（设计）
- [ ] 3.1 成文《转世运行预期》：三类状态 × 预期行为 × 降级策略 × 可观测证据
- [ ] 3.2 projection 无 compaction 快照时的 fallback 重建设计
- [ ] 3.3 nil-probe 任务（plan/subagent）的回收策略设计
- [ ] 3.4 验收标准与演练脚本

## 4. 部署与验证
- [ ] 4.1 换装部署
- [ ] 4.2 验证 panic 消失（连续多轮消息 + 工具调用无 panic）
- [ ] 4.3 验证 plan 工具可用
