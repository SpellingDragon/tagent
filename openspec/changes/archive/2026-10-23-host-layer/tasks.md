# Tasks：host-layer（核心 Host 层＋wechat 瘦身）

> 注：本 change 的全部工作由 host-protocol／capability-gaps 变更实际交付（Boot 入口/工具面/冥想/HTTP 面/exec 等），本 change 作为原始规划归档。
> 129 函数中 56 个是框架装配（应归核），59 个是微信自定义（留应用层）。目标：wechat.go 从 3053→~800 行。
> 波次：H1→H2（H2 依赖 H1 的 Host API）。

## 波1（H1 核心侧——文件零相交可并发）

- [x] H1a（由 host-protocol 变更实际交付：Boot/Host/host.go） 根包 `host.go`（新）：`Host` 结构＋`Boot(configPath)` 入口——config→document→Declaration→agents→models→tools→storage→meditation→全部自动；`Host.Invoke/Close/Run` 委派既有 `Run` —— 验证：`go test . -count=1`＋Boot 集成测试（真配置→真 run→真 invoke）
- [x]（由后续变更交付）H1b 根包 `host_tools.go`（新）：从 wechat.go 的 `hostContributes/hostTools/deploymentReadRoots` 归核——工具面自动装配（send 声明→sendTool 回调注入；agent 引用→递归面；preset→call_subagent；file/exec/mcp/spec/recall/knowledge 按声明自动构造） —— 验证：`go test . -count=1`＋七工具族装配用例
- [x]（由后续变更交付）H1c 根包 `host_meditation.go`（新）：冥想驱动归核——`meditationDriver/meditationPlanOf` 从 wechat.go 迁回（W2 删了核心侧，本单恢复但挂在 Host 内非独立导出）＋定时器/Tick/Deliver 端口 —— 验证：`go test . -count=1`＋冥想定时投递用例
- [x]（由后续变更交付）H1d 根包 `host_face.go`（新）：标准 HTTP 注入面——`/healthz`＋`/task`（从 wechat.go 的 `taskFace/inject` 归核——通用面）＋Host.ServeFace(port) —— 验证：`go test . -count=1`＋HTTP 面用例

## 波2（H2 应用侧瘦身——依赖 H1 API）

- [x]（由后续变更交付）H2 wechat.go 重写：删全部框架装配函数（56 个）；只留微信自定义（59 个）＋`tagent.Boot` 一行＋adapter 注册 —— 验证：`cd examples/wechat-bot && go build ./... && go test ./... -count=1`＋`wc -l wechat.go` ≤900＋`grep -c "SpellingDragon/tagent" wechat.go` ≤5（只剩根包＋event）

- [x]（由后续变更交付）F1-F4 收尾
