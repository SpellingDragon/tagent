# exec 回执完整性：技术要点

## 文件清单

- tool/exec/tmux_executor.go：Start/Probe/Capture/Stop 的世代校验与死 pane 处置
- tool/exec/tmux_command.go / tmux_session.go：会话世代令牌（如需要）
- tool/exec/exec.go / state.go：RunTool 回执组装处的一致性证明
- **tool/exec/tool.go（held 短路 :228）**：复用前 Probe，不再跳过探活
- tool/exec/tmux_executor_test.go：fake-tmux 锚⑥与负锚
- tool/exec/tmux_integration_test.go（新增或扩展）：真实 tmux 集成锚（本地+CI 双跑；**runner 无 tmux=配置错误判红，不是 silent skip**）
- examples/wechat-bot 或 docs 中 exec 纪律说明更新

## 根因更正（grill r1 F19）

假成功的执行路径=**held/复用短路**：`RunTool.Call` 见 manager.held(key) 为真时跳过 Probe/Query（tool.go:228）；`Manager.Acquire` 命中 resources 后无条件复用、从不 Probe pane（exec.go:224-233）；`Start` 只在新资源路径被调且遇已存在会话返回 errSessionExists。修复必须落在 held 短路与 Acquire 命中处（复用前 Probe；pane_dead 先清理再 Start），改 Start 无效。

## 关键决策

- 世代令牌：会话创建时生成短随机 token，注入命令前置 echo 标记或环境变量；Capture 结果必须包含本次 token 标记才算“本次输出”；无标记=陈旧 scrollback → 重建会话重试一次，仍无=显式失败（非零语义回执）。**世代令牌校验失败归类＝明确失败（陈旧世代不猜测）：Release 旧会话后以全新会话重建执行（F12）；MUST NOT 把陈旧世代留作未知或当作成功。**
- 死 pane 复用：Probe 显示 pane_dead 时 Start 路径 MUST NOT 复用（当前 remain-on-exit 保留是为可观察，但下一次 Start 同名会话必须先清理或换名）；**默认走重建执行**（一次性会话模式，远端实证可用）；**显式失败仅当重建不可行**——不再把“探活后直接报错”当作等价分支。验收断言落在重建分支：`TestRebuiltSessionReallyRuns`（重建后新命令真实执行、探针落盘、退出码真实）钉住，“探活后直接报错”的实现过不了该测试。
- 退出码可信度：exit 记录与会话世代绑定（记录文件名带世代）；世代不匹配的记录不作为本次退出码。
- fake-tmux：以可执行脚本伪装 tmux 子命令（new-session/display-message/capture-pane 等），脚本内建“死 pane+旧缓冲”剧本；不修改生产代码的测试分支。
- CI：集成锚进既有 workflow（真实 tmux 下跑死 pane 剧本 + 真实 touch 落盘读回）；**`.github/workflows/ci.yml:44-47` 已含 Install tmux（本域 1.5a 只核验；无 diff 时以核验凭据收口），CI 日志必须含锚 PASS 行**——无 tmux 的 runner=配置错误判红。
- 无实验开关（r22 纯化）：世代校验无条件；缺世代标记一律按显式陈旧世代拒收并在重建后重试一次（`TestGenerationCheckIsUnconditional` 负锚：任何 `TAGENT_EXPERIMENT_*` 值均无效）。

## 风险与回退

- token 注入对输出的污染：标记用独立行+前后缀，回执组装时剥离；用户可见输出保持干净（负锚断言剥离）。
- 重建会话的成本：仅在上限一次重试；重试事实进回执元数据（可观测；`TestRebuildRetryMetadataRecorded` 锁定）。
- 回退：域独立 revert（无实验开关；r22 纯化）。
