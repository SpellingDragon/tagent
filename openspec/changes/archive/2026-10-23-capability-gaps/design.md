# design：capability-gaps

## D1 exec 归 tool/exec（否决：宿主自实现）
执行是核心域：Executor（tmux 会话生命周期）＋MonitorConfig 三态（watch→stable→suspect）——宿主只注入。

## D2 审批登记在 riskCritical（否决：Hold 后异步登记）
Hold 判定处同步登记：Lookup 未命中→Request(参数摘要)→回 Hold 回执（回执带请求 ID）——批准后 Lookup 命中放行（既有语义）。

## D3 默认风险面声明式（否决：硬编码工具黑名单）
governance 段声明 risk_rules（工具名/前缀→级）；无声明零风险（现状兼容）——wechat 可声明 exec/delete 为 critical。

## D4 worker 轨迹走 CaptureAdapter（否决：独立 sink）
同一 TrajectoryCapture 的 Scope——worker binding 模型经 withCapture 包装；manifest/seal 全覆盖。

## D5 host.go 四接线合一单（R21 热点合单）
metrics 端点/exec 接线/风险面透传/call_subagent 收录——同一文件顺序四处，禁拆两单互踩。

## D6 冒烟转正为验收（R29 前置）
四负锚（exec ErrUnwired/skill GAP/data call_subagent 拒/审批 smokeGovernor）全部翻正＝六能力的生产消费者证据；锚在 W0 各单的验证面已写死，W2 只对账。
