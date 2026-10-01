# 任务：认知资产防线（终态-脚手架结构版）

> **跑偏防护总则**——遇下列任一情况停下回 design.md 重评：
> ① 走私提示需要拦截/延迟命令（纯提示是硬约束）；② 审批规则需要改 ApprovalManager/gate.go 契约（预期零 gate 改动，规则命中自动走 gate.go:148 既有链）；③ 漂移审计需要网络上报/消息注入（事件只入事实链+Info 日志）；④ 新增必填配置（零新旋钮）；⑤ 检测正则误伤框架自建命令（检测点必须在 Call 入口单点）。
> **结构约定**：组 1-2 为终态（长期存在），组 3-4 为脚手架（拆除条件已记入 design 拆除账本），组 5 交付。**脚手架实现时禁止就地加分支补丁——与终态假设冲突即停下重评。**
> **任务 ↔ 决策 ↔ 场景对照**：1.x↔D1↔「漂移审计(终态)」；2.x↔D4↔「走私引导(脚手架)」；3.x↔D2↔「资产写审批(脚手架)」；4.x↔D3↔「权限域分离方向」；5.x↔D5 交付。

## 0. 基线与前置侦察

- [x] 0.1 基线：`go test ./tool/action/ ./agent/governance/ ./evolution/ ./event/ -short -count=1` 全绿
- [x] 0.2 **路径清单同源决策（暗礁①，先做再继续）**：`evolution/evolve.go:64` 默认清单未导出——优先 evolution 导出 `DefaultProtectedPaths`（先确认 governance↔evolution 依赖方向无环）；次选提取到共同基础包；最后手段两处声明+互指注释（5.2 登记技术债）。**决策：采用首选**——实测 evolution 仅 import event/memory，governance 与二者无交叉引用，evolution 导出无环。
- [x] 0.3 **事件类型属性预研**：定稿 `{Role:user, Skeleton:true, TTLDays:30, Embeddable:true, Recallable:true}`（NonProjection 默认 false=进投影）。微调说明：Special/ToolLineSummary 仅作用于 GenerateEventSummary 的 bus 消息视图，审计事件直构 FullEvent 不经该路径，不设；Embeddable 对齐 TypeConsolidation（审计笔记需可召回发现漂移）。快照目录：`$TMPDIR` 会被清、`resident_meta_dir` 多数部署未配——跨重启保留要求落 `WorkingDir/.tagent/`（实现注明，PR 说明）。

## 1. 漂移审计（D1 · 终态）

- [x] 1.1 `event` 包登记 `TypeCognitiveAssetChanged`：常量（types.go）+ EventTypeSpec（registry.go，按 0.3 定稿）+ registry_test 模式断言（含「必须进投影」+ Embeddable/Recallable 钉住 + DefaultTypeTTL 表补一行）
- [x] 1.2 快照器：文件集枚举（0.2 清单去 `/**` 后的目录段 + 主配置 yaml，相对 working_dir 解析）、SHA-256、快照 JSON 原子写（tmp+rename 对齐 budget.go 模式）。**目录按 0.3 决策落 `WorkingDir/.tagent/cognitive-assets.snapshot.json`**（非 resident meta 目录：默认 $TMPDIR 会被清、resident_meta_dir 多数未配，无法满足跨重启保留）
- [x] 1.3 比对引擎（纯函数）：`DiffAssetSnapshots(prev, cur) []AssetChange{File, OldHash, NewHash, Size, Mtime}`——新增（OldHash 空）/删除（NewHash 空）也算变更；字典序稳定输出
- [x] 1.4 周期触发：**采用独立 10min ticker**（reconcile 为事件驱动 + List 懒触发，非定时检查点，挂载语义不符）；间隔常量 `assetAuditInterval` 非配置
- [x] 1.5 变更产出：Diff 非空 → Info 日志 + `report` 回调产 `cognitive_asset_changed` 批次事件入事实链；启动比对（上一代快照 → 事件 → 新基线；无历史/损坏静默建基线）
- [x] 1.6 wiring 接线（build_agent.go wireAgent：entry+bindsProcessShared 构造 + report sink 经 `ta.RecordCognitiveAssetChange`→`persistTaskRecord`、Close 停 ticker 经 `RegisterCloser`）
- [x] 1.7 测试（asset_drift_test.go）：运行中改→事件；停机改→启动事件；无变更零事件；文件增删→变更行；绕过文本匹配写入仍被捕获（不变量）；并发不漏报；损坏快照重建基线不误判；Diff 纯函数
- 红线：事件不注入消息路由；比对失败 Warn 跳过该文件不整体失败

## 2. 走私入舱引导（D4 · 脚手架，拆除条件：fp 上线+观察期未复发）

- [x] 2.1 检测器（tool/action `smuggle_hint.go`）：后台化（`\bnohup\b\s` 且 `&\s*(?:disown\b)?`、`>\s*\S+\s+2>&1\s*&`）与嵌套 tmux（`\btmux\s+(new-session|new\s+-s\b)`）两类正则，`detectSmuggle` 返回命中类别（nested-tmux 优先）
- [x] 2.2 hint 文案（单行）：托管缺失说明 + `mode:resident`+大 `ttl` 指引 + 退出码由框架捕获 + 可忽略声明
- [x] 2.3 注入点：`Call` 入口对 `args.Command` 单点检测（`smuggleHint := smuggleHintFor(args.Command)`），同步完成/ack/无 spawner 直取三个出口尾部各追加一行；relaunch 不经 Call→天然幂等
- [x] 2.4 测试（smuggle_hint_test.go）：两类命中；正常命令结果逐字节不变（`+=`空串无副作用断言）；echo 含 "nohup" 无 & 配对不误报；hint 幂等不重复；buildAckResult 追加后原说明在前
- [ ] 2.5 **拆除条件观察任务（记账·延后）**：已登记于 design.md 拆除账本（条件：fp 上线+两周观察走私未复发）。本任务属未来运维动作，**依赖 failure-polarity-passthrough 先行上线**，非本变更可实现——留待 fp 交付后回访触发（对应 5.5）
- 红线：只读命令文本；不改命令；零延迟

## 3. 资产写审批（D2 · 脚手架，拆除条件：权限域分离落地）

- [x] 3.1 classifier.go `DefaultRules()` 插入 `exec.cognitive-asset-write` → critical（destructive 邻位）；判定逻辑独立文件 `agent/governance/asset_write.go`：同源清单（`evolution.DefaultProtectedPaths` 派生前缀）路径 **且** 写形态（`>`/`>>` 紧跟资产路径、tee/sed -i/mv/cp/rm/unlink/rmdir 与路径同现、python open(w)）；RE2 无环视、空清单短路不命中
- [x] 3.2 确认零 gate 改动：gate.go:148 `approval.Request(ctx.ToolName, ctx.ArgsJSON, ctx.ArgsJSON, ...)` 自动携带命令全文（含路径），critical 走既有 Hold→Decide→Record 链；测试 `TestCognitiveAssetWriteGateHoldThenApprove` 未改 gate 即通过验证。Reason 静态文本说明清单范围
- [x] 3.3 测试（asset_write_test.go）：写形态命中矩阵（python open(w)/sed -i/tee/cp/rm→critical）；cat/grep 只读不命中、`cat asset > /tmp` 读资产写他处不误伤；strict 挂起拒绝/warn 挂起不硬拒；批准重试放行+审计；governance 关闭零变化；refine/meditation 触发源不豁免；非 exec 工具不受影响
- 红线：不误伤日志/临时文件写；不评估「意图」（judge 是 evolution 的事）

## 4. 权限域分离方向（D3 · 终态锚定，非本期实现）

- [x] 4.1 文档锚定：新建 `docs/wiki/platform/cognitive-asset-guard.md`（终态/脚手架结构表、防线 mermaid、D3 权限域分离 systemd ReadOnlyPaths/ProtectSystem+ReadWritePaths 白名单或 mount namespace、与审批规则拆除条件绑定、同源清单、拆除账本、运行前提）；README 平台子系统节增「例外——认知资产防线」说明块 + 深入阅读表链接；platform-subsystems.md 治理闸增段指向本页
- [x] 4.2 未起草独立运维变更 proposal（可选、不阻塞本变更交付）：权限域分离的落地形态与前置条件已在 wiki §4 完整锚定，立项条件成熟时另起变更引用本能力

## 5. 验证、交付与账本

- [x] 5.1 `go build ./...` ✓；`go vet ./...` ✓；`go test ./tool/action/ ./agent/governance/ ./event/ ./agent/ -race` ✓；`go test . ./evolution/ -race`（动了 evolution+根包）✓；全量 `go test ./... -short` 触达 6 包 `-p 1` 全绿。**两处失败均与本次改动无关**：① `TestLiveSessionStaysWatchedAcrossToolGeneration` 为跨包并行 tmux server 争用（`-p 1` 与单包跑均绿）；② `scripts/codetools/TestMergeCheckUsageStatesTheFlagShape` 属仓内既有 WIP（`check.go`/`check_test.go` 非本次触及，单跑绿）
- [x] 5.2 文档：README 子系统节终态/脚手架+默认态说明块、深入阅读链接；`docs/wiki/platform/cognitive-asset-guard.md`（desc-patch 起因、防线 mermaid、拆除账本、0.2 同源决策记录=选首选无技术债、运行前提）；platform-subsystems.md 交叉指引
- [x] 5.3 **拆除账本登记（交付物）**：审批规则（条件：权限域分离落地验证）与走私提示（条件：fp 上线+两周观察未复发）两账目写入 wiki 拆除账本页 §5，含触发动作与「不允许滞留」约定
- [x] 5.4 远端交付邮件（一次发全）：①desc-patch 毒药段撤销指令（删「MUST tmux 套壳」段与「nohup gets reaped」句；保留 framework-owns-waiting 段）②**带证据链的正确方法论注入**（10MiB 断点+速率换算+audit 零痕迹 → 代理掐流非清扫）③验证任务单（restart.log 对齐 12:01/13:11、HF 代理掐断复测、maxTokens 512K→288K 认领、insurance 尾段日志）④部署顺序（先审计+提示观察一周事件流，再议 governance 开启）⑤HF token 轮换（`hf_tTct…` 2 处明文）
  > 草稿已成文（与 fp 7.4 合并一封）：见本目录 `delivery-email-draft.md`；**发送待收件人与授权确认**
- [ ] 5.5 交付后一周回访：审计事件流可见性、走私提示命中频次（消纳假设的早期信号）、governance 开启决策输入
