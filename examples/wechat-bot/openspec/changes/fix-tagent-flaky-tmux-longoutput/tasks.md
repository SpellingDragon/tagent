## 1. 复现与基线

- [ ] 1.1 连跑 5 次 `go test -run TestActionTool_TmuxLongOutput -count=1 -v ./tagent/tool/action/`（或仓库等效路径）统计失败率，留存输出（复现脚本可参照 scripts/ab_longoutput.sh 的做法，日志落 /tmp）
- [ ] 1.2 结合 tmux_monitor.go detectSessionState（L504-558）与 settle.go OnStateChange（L156-181）的时序链，实证确认"Completed 判定同次的 GetSessionOutput 早于终态行渲染"这一根因假设（可加临时日志：结算时打印 capture 长度 vs 落盘文件长度）
- [ ] 1.3 核验 scrollback 假设：测试输出 ~101 行远小于 capture-pane -S -1000 上限，排除"scrollback 不足导致截断"的替代解释

## 2. 修复落地（按 design.md D1：候选 1 优先）

- [ ] 2.1 在 Completed/Error 结算路径实现重 capture：detectSessionState 判定 Completed 时重读一次 GetSessionOutput，与新值比较后采用信息量较新的一次（MD5 一致即止，不引入固定 sleep）
- [ ] 2.2 若候选 1 实证不可行（如 monitor 回调时 session 已不可读），改用候选 2：在 buildResultFromSignal/settle 链路重 capture，同时把 settle.go 的 reap 调整到重 capture 之后，保持 reapOnce 语义不变
- [ ] 2.3 确认 Stable/TUI TimedOut 分支零改动（diff 只触及 Completed/Error 路径），并把"reap 后置于输出最终确定"的验证写入实现说明

## 3. 测试锁定与回归

- [ ] 3.1 修复后 `go test -run TestActionTool_TmuxLongOutput -count=10 -v` 连跑 10 次全绿，留存输出作为锁定证据
- [ ] 3.2 `go test ./tagent/tool/action/...` 包级全量回归通过（含 tmux_monitor_test.go 中 SettleSignal 相关 mock 测试）
- [ ] 3.3 若仓库有 CI 门禁脚本（lint/build），一并过一遍确保无风格/编译回归

## 4. 挂账回填与收尾

- [ ] 4.1 回填 quiet_timeout 已归档计划 5.2 条目：根因（结算 capture 早于终态渲染 + Completed 即 reap 无补救时机）、修复 commit、10 连跑证据摘要
- [ ] 4.2 确认未重启 tagent 进程（修复仅改代码与测试），全量绿后按流程归档本计划
