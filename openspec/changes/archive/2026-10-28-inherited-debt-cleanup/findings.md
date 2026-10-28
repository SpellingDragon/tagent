
# findings（inherited-debt-cleanup，2026-10-28 执行）

- **叶1 Wave2Data（R23 骨架随契约）**：受理回执最小化（AdmissionReceipt{CallID,TaskID,Accepted,Kind}）后断言仍盼 "ok"——改形状断言（{callID 前缀+" true "），denied 拒收断言保留；`-count=3` 绿。
- **叶2 Wave2Recall（R23 骨架随语义）**：recall_recent 返回链上最近事实，运行时自产事件（thinking_plan/task_settled/action_command…）与夹具同链——恒 count=2/恒 truncated=false/items 恰两条系"链上仅夹具"的旧世界假设。改下界断言（count>=2+两条夹具键可见且 child 新于 parent），hook limit 5→12 让夹具入窗；truncated/items 精确形状断言移除（分页语义归 recall 包单测）。`-count=3` 绿。
- **叶3 mcp 升级=负结果**：v0.0.10→v0.0.18 实测（1）setSessionID/send 同栈竞态**仍在**（streamable_client.go:607）；（2）另现功能性退化——TestMCPFaceBinds 在 v0.0.18 下 connection refused（server 会话处理变更）。按 D3 回落条款回退 v0.0.10；U5 双签名豁免恢复并注明「18 试过并回落」；race 门回落后 RC=0（含 capabilities 包）。分类器自测全程 ALL PASS（fixture 随豁免表演进：v0.0.18 路径行实测更新后又随回落复原）。
- **叶4 tmux 拆分（C-拆分代理）**：547→357/90/129 三文件（executor/session/command 簇），纯移动逐字节证明（24 函数计齐、三区间 diff RC=0），包内 9 个 tmux 测试真跑全绿。结构门：tmux_executor 退出违反清单（30→29），但**门余 29 项为全仓既有家族**（host.go 2877/loop.go 1637/exec.go 697 等）——本案登记面只含 tmux_executor 一项，29 项登记为 inherited 不扩面（该检查器不在 CI/lint 面）。
- **叶5 文档悬空清零**：GovernanceGate.Evaluate→Policy.Judge、approval/gate/checkIndex/gitrefine 五处悬空行改当前实现或移除已删能力行；`grep gitrefine|GovernanceGate docs/wiki` =0。
- **叶6 误判更正（负结果入库）**：`agent-interview-helper` 经查为**含内容的用户 skill 目录**（SKILL.md+16 项，untracked）——此前"data-less 文件"判定系误判（对目录 cat 当然读空）。未移动、保留原地；本会话 615 文件蒸发清单中该项划去。
- **FoldControl 复现率（环境恢复后复测）**：10 跑 9 绿 1 红（同签名 synthetic key 跨重启不等：pre c59b vs post ec70——折叠产物内容寻址键在并发时序下偶发不同）；受控重跑绿（含 -race 单跑）。定性维持：**时序 flake 非确定 bug**；修复面在 fold 合成键的时间戳排除语义，超本案范围，WATCH 续档。
- **03.11 双测运行前提**：smoke 全套裸跑时 LongRun/Calibration 按设计 FatalF "must run under -race"（测试自校验运行前提，非失败）；全套终证口径=`-skip '^TestEventIntegrityLongRun' -count=1` → **RC=0（185.7s 全绿）**；FoldControl 单独受控重跑绿。
- **doc-budget**：悬空行删减 -1 行经 --tighten 锁入（零差额维持）。
