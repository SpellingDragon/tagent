# 边界闭合与收益验证（boundary-closure）

## Why

第三方评审（固定 main@3b727199，本地核验至 48d8eb93）与远端宿主邮件上报（2026-11-04 exec 通道假成功故障上报；2026-10-30 OOM 诊断包）共同指向同一结论：**事件驱动消费重构之后，"机制成立"已被行为测试支撑，但层与层之间的边界尚未闭合**——

1. **可靠交付未闭合**：`deliverFresh` 在 `recordDeliveryOwed` 失败时仅告警后仍发送（assembly/host_delivery.go:76-81）；`agent_output` 落盘与 `owed` 登记之间有漏送窗口（恢复只扫 `tagent/task/v1/delivery:` 记录，restorePendingDeliveries）；`retry` 状态恢复后无自动重试驱动，靠 `ApproveDelivery` 人工续（assembly/host_delivery.go:427-445）。
2. **完整请求预算缺失**：预算只约束历史投影（AssembleBudgetPrefix），固定提示词、技能正文、动态环境、工具定义不入最终请求总量；32 token 历史窗口下 10KB 固定提示仍完整进入请求（assembly/request.go）。
3. **模型上下文有界 ≠ 进程资源有界**：TestLocalSmoke 300 轮两次实测堆增长 175/159 KiB/轮，均超 ≤128 KiB/轮门；增长源已定位：本地 KV 分片全内存常驻（memory/ledger/localfile.go shards map 无淘汰）、事件存储读缓存无界（memory/ledger/segment_store.go cache map）。
4. **exec 回执与磁盘事实脱钩**（远端宿主 11-04 正式故障上报）：`exec_run` 在 tmux pane 即死后复用旧 scrollback 回放并回报 exit=0，touch 探针未落盘——静默失败被误报为成功；规避纪律是每命令新会话，框架侧未修。
5. **折叠摘要剥除时间维度**：`entry.Type+": "+entry.Summary`（memory/replay/age_segments.go:419）使跨重启回放的历史呈无时序团块，已在远端诱发"老版本轨迹化石"误判；折叠摘要也不携带 recorder 世代标识。
6. **收益未验证**：快结果先推进等机制的净收益（模型调用数、token 成本、任务成功率）从未测过；训练采集已有骨架但缺"请求可回读、结果独立验收、奖励有证据"的完整样本。

已排除项（2026-11-05 核验）：OOM 报告 S1（RL 轨迹捕获无界常驻）与 S2（事件链多层副本无删除）**已在 HEAD 修复**（TrajectoryCapture 驻留预算+swap store：features/rl/capture_wiring.go:270-310；facts/originals/seen/consumed delete 路径：assembly/facts.go:129,161、memory/replay/replay_budget.go:303-313）。长上下文熔断已有实现（runtime/agent/fuse.go）。本变更不再重复处理。

## What Changes

一级变更只司**编排**：九个二级子变更（spec 全部下沉二级），三波交付 + 收尾 F 任务。

| # | 子变更 | 一句话边界 | 症状锚（第一天红着） |
|---|---|---|---|
| 01 | delivery-closure 交付闭合（**事件化路线，用户 r1 终裁**） | 交付义务成为链上事实族、随答案同笔原子提交；消费挂点驱动发送/重试/审批；旧 KV 欠条迁移退役；概念文档同步 | ①登记失败→发送数=0 ②杀窗口→重启必送达 ③瞬时失败→无人工介入自动重发（④预算锚在 02） |
| 02 | request-budget 完整请求预算 | 最终请求边界统一预算（固定+技能+动态+工具+历史+输出余量），超限显式降级/拒绝 | ④32token 窗口+10KB 固定提示→请求被统一预算约束 |
| 03 | resource-bounds 资源有界 | KV 分页/淘汰、事件缓存淘汰、空闲地址回收、冒烟斜率过门、pprof 长跑钉死残差 | ⑤TestLocalSmoke ≤128KiB/轮（当前 159-175，评审两次实测为红证据） |
| 04 | exec-integrity exec 回执完整性 | 死 pane 复用检测、回执-磁盘事实一致性校验、CI 可跑集成测试 | ⑥死 pane 复用→exec 回执与磁盘事实一致（CI/fake） |
| 05 | fold-provenance 折叠可追溯 | 折叠摘要行携带事件时刻+recorder 世代；AGENTS.md 残留 2 处注记修正 | ⑦history summary 行含时刻+世代标识 |
| 06 | live-benchmark 真实任务对照 | 固定模型/工具/预算，最小基线 vs tagent vs DeepSeek Harness 对照实验 | 任务成功率/送达率/延迟/token 成本/人工介入次数入 run |
| 07 | training-sample-loop 训练样本闭环起步 | 少量任务做成"请求可回读、结果独立验收、验收结论入 rl annex"完整样本；reward 向量派生延后为显式欠账 | 样本三要素齐备率 100%（分母=全部样本，含 incomplete；run-id+指标存在） |
| 08 | config-liveness 配置活线（**链驱动**） | 文件=编辑界面、声明事实=链上账、唯一发布入口；发布记录上链；失败回执如实；watcher 与重启对账走同一入口 | 安装失败 /reload 回成功（红）；改文件→防抖→下一请求见新配置（负锚：mtime-only 不触发/坏配置不断线） |
| 09 | meditation-autonomy 冥想自主面 | 自持状态机（激活=真实事件带因果）＋工具面 file/exec/recall（refine 延后）＋自察主体/专属反思会话＋规格四 delta | 空闲窗自主整理端到端（产物可审计；越权被拒负锚）；激活因果与自管产出不计入状态积累 |

## Capabilities（spec 已下沉二级）

- 二级 `changes/01-delivery-closure/specs/delivery-closure/spec.md`：可靠交付闭合契约
- 二级 `changes/01-delivery-closure/specs/delivery-dispatch-receipts/spec.md`：发送动作身份与未确认结果（MODIFIED 于既有能力；全文保留既有条款+活进程内限定词+重启补送追加句）
- 二级 `changes/01-delivery-closure/specs/fact-vocabulary/spec.md`：事实族登记（ADDED 第十一族）
- 二级 `changes/01-delivery-closure/specs/failure-notice/spec.md`：静默边界（MODIFIED；仅增未清 held 义务场景的延迟告知豁免句，非新通道）
- 二级 `changes/02-request-budget/specs/request-budget-accounting/spec.md`：最终请求预算契约（MODIFIED 于既有能力）
- 二级 `changes/03-resource-bounds/specs/resource-bounds/spec.md`：运行资源有界契约
- 二级 `changes/04-exec-integrity/specs/exec-integrity/spec.md`：exec 回执完整性契约
- 二级 `changes/05-fold-provenance/specs/fold-provenance/spec.md`：折叠可追溯契约
- 二级 `changes/06-live-benchmark/specs/live-benchmark/spec.md`：对照实验契约
- 二级 `changes/07-training-sample-loop/specs/training-sample-loop/spec.md`：训练样本闭环契约
- 二级 `changes/08-config-liveness/specs/config-liveness/spec.md`：链上配置五不变量（ADDED）
- 二级 `changes/08-config-liveness/specs/config-hot-reload/spec.md`：配置热更维度矩阵链真词化（MODIFIED 于既有能力）
- 二级 `changes/09-meditation-autonomy/specs/meditation-autonomy/spec.md`：冥想自主面＋状态机不变量（ADDED）
- 二级 `changes/09-meditation-autonomy/specs/meditation-idle-gating/spec.md`：独立门控能力退役（REMOVED）
- 二级 `changes/09-meditation-autonomy/specs/meditation-agent-partition/spec.md`：自察主体/专属反思会话（MODIFIED 于既有能力）
- 二级 `changes/09-meditation-autonomy/specs/self-improvement-meditation/spec.md`：叙事对齐自察宿主（MODIFIED 于既有能力）
- 一级仅留 `specs/orchestration/spec.md`（编排契约：启动门/波次/契约守护/DoD）

## Impact

- 代码：assembly/（交付+预算 + 配置发布入口/声明事实）、memory/ledger/（KV+缓存）、memory/replay/（折叠）、tool/exec/（回执）、examples/wechat-bot/（冒烟门槛 + 配置 watcher）、features/evolution/meditation/（状态机与专属反思会话）、features/rl/（样本闭环）、新增 bench harness
- 文档：GLOSSARY 配置变更词条链真词化；config-hot-reload 主 spec 经 MODIFIED；冥想四 delta（含 idle-gating REMOVED）
- 契约冻结：`api/**` 零改动（唯一例外=`api/config` 新增两个可选字段，decode/fingerprint 配套行同例外清单；F1 断言）；`tagent/task/v1/delivery:` KV 记录退役为只读审计（01 迁移负锚锁幂等）；`tagent.record.v1` 既有字段语义不变（07 只新增 `RecordKindAnnex`）
- 风险：03 的冒烟斜率门依赖 300 轮长跑（本地实测约 30-60 分钟/次）；tmux 本机已实测可用（E11 更正），04 集成锚本地+CI 双跑
