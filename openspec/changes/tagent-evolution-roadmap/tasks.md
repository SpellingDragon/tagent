# Tasks: Tagent Evolution Roadmap

> 本清单是阶段级检查点(父路线图)。各阶段的细粒度实施任务由派生子变更的 tasks.md 承载;
> 每阶段执行遵循 design.md D3 闭环(准入核对 → 派生 → 门禁 → 归档回写)。
> CONFIRM 任务 = 核对 design.md D4 预留确认项:形成决议或显式采用默认降级路径(标注 DEGRADED)。
>
> **2026-09-05 重编记录**(详见 openspec/changes/LEDGER.md):活跃变更集已清账——
> agent-package-rewrite / unified-event-consumer-and-async-tool / memory-storage-production-hardening
> 归档(实质完成或被后续演进覆盖);risk-mitigation-semantic-recall 重编为 hybrid-semantic-recall;
> 空壳 tagent-deep-source-evaluation 删除。本路线图相应修订:
> P0 降级为直做清单(不派生子变更);P1 语义检索由 hybrid-semantic-recall 承载(不再另起)。

## 0. 路线图启动

- [ ] 0.1 用户批准路线图(proposal/design/specs 通过评审)
- [ ] 0.2 确认执行模式:自驱连续推进 P0→P4(每阶段准出后自动进入下一阶段)vs 每阶段完成后暂停待用户放行(默认:每阶段暂停汇报,用户可升级为连续模式)

## 1. P0 工程可信度(直做清单,不派生子变更;半天工作量)

- [ ] 1.1 CONFIRM C1(CI 门禁范围;默认:GitHub Actions 最小集 build+vet+短测试,race 走 nightly)、C11(首个 tag;默认 v0.1.0)
- [ ] 1.2 examples/wechat-bot/tagent.yaml 硬编码绝对路径(plan description_file)修复为相对路径并验证加载
- [ ] 1.3 .github/workflows/ CI 就绪(real-LLM 测试经 Skip 保护不阻塞)
- [ ] 1.4 README 依赖声明补齐(tmux、Go ≥1.24、rustviking 可选、ZAI_API_KEY);CHANGELOG 建立;吸收 memory-storage-production-hardening 归档时的残余文档任务
- [ ] 1.5 首个 version tag 打出并 push;门禁①通过即算准出(直做项免派生流程)

## 2. P1 语义检索 + 评估基座

- [ ] 2.1 语义检索:由已就绪的 hybrid-semantic-recall 变更承载(工件完备,含 X1-X3 预留确认项)——放行即开工,准出后回写此处
- [ ] 2.2 CONFIRM C4(评估任务集来源与首批规模;默认从 tests/ real-LLM 契约测试提炼 10-20 个组件级 case)
- [ ] 2.3 评估基座派生子变更(evaluation-bootstrap):evals/ 目录 + trpc evaluation 模块桥接 + 压缩/冥想过程指标埋点(票据召回成功率/冥想产出引用率/退化 turn 率);real-LLM flaky 治理(隔离标签或多次运行取通过率)作为前置任务纳入
- [ ] 2.4 顺带项:nanobot skills/ 兼容技能并入 examples skills(SKILL.md 格式核对,注明来源)
- [ ] 2.5 两子项各自三道门禁 → commit → archive → 回写本清单

## 2A. P1.5 可观测 trace 骨架(已立项:observability-tracing,不依赖 P1,可先行甚至最先执行)

- [ ] 2A.1 CONFIRM:OTLP 后端选型(默认 Jaeger all-in-one docker);langfuse 仅评估不实施
- [ ] 2A.2 变更工件已就绪(2026-09-05,4/4;spike-first,D0 实录后才动实现)——放行即开工
- [ ] 2A.3 三道门禁 → commit → archive → 回写本清单;其轨迹互链字段是 P2 反馈归因的地基(P2 准入前须完成)

## 3. P2 反馈归因闭环(子变更建议名:feedback-attribution-loop)

- [ ] 3.1 CONFIRM C5(反馈来源;默认仅 HTTPAPI /feedback 最小面)
- [ ] 3.2 派生子变更并完成工件(EventKey 作 record-id 的 Reef 模式设计;AReaL reward 消费路径衔接)
- [ ] 3.3 检查点:HTTPAPI 新增 POST /feedback(event_key/task_id/score/label/reason),评分作为新事件写入 MemoryStore 并关联目标事件(RelationStore 因果边)
- [ ] 3.4 检查点:TrajectoryRecorder 输出附反馈关联率指标;轨迹 JSONL 可被 AReaL reward 侧消费(格式衔接验证)
- [ ] 3.5 检查点:recall 可查询反馈事件(票据路径);"反馈→事件→召回"闭环用集成测试验证
- [ ] 3.6 三道门禁通过 → commit → archive + specs 同步 → 回写本清单

## 4. P3 Harness 自进化 + 陷阱注册表(子变更建议名:harness-self-improvement)

- [ ] 4.1 CONFIRM C6(RHI 优化器形态;默认离线脚本先行)、C7(陷阱事件类型命名与 TTL;默认 pitfall 类型 TTL 豁免)
- [ ] 4.2 派生子变更并完成工件(prompt 制品版本化方案依托 prompt.Source;pairwise 比较用 P1 评估器 + P2 反馈分数作信号)
- [ ] 4.3 检查点:prompt 版本目录与激活机制(候选版本先评估后激活,可回滚;激活=文件替换即热载生效,零结构变更)
- [ ] 4.4 检查点:Harness 优化器最小闭环(读相邻两版轨迹→pairwise→生成候选→评估→报告;人工确认后激活)
- [ ] 4.5 检查点:陷阱注册表(失败教训事件类型入库;meditation 空闲期提炼陷阱卡片;recall 可按 pitfall 类型召回)
- [ ] 4.6 三道门禁通过 → commit → archive + specs 同步 → 回写本清单

## 5. P4 工具治理 + 组织模式(子变更建议名:tool-governance-and-collaboration;建议拆两个子变更)

- [ ] 5.1 CONFIRM C8(Docker 沙箱引入;默认不替换 exec,container 仅作可选 code-block 工具)、C9(审批通道;默认 allow-list+auto-approve 先行)、C10(trpc team 复用 vs 自研 critic tool agent;默认子变更 design 期 spike 对比后定,倾向自研)
- [ ] 5.2 派生子变更并完成工件(crush permission 设计借鉴清单:allow-list/session 缓存/异步审批/通知;waterfall 中间件链设计:权限→审批→审计→执行,各层可旁路)
- [ ] 5.3 检查点:工具执行治理链落地(exec 高危命令 allow-list + 审计日志;审批通道按 C9 决议实现或 DEGRADED 标注)
- [ ] 5.4 检查点:沙箱可选路径按 C8 决议落地或明确不做(记录决策)
- [ ] 5.5 检查点:critic/verifier 协作模式最小可用(plan 产出经 critic 对抗评审后放行;与 AgentToolWrapper/prefix-cache 约束兼容性验证)
- [ ] 5.6 三道门禁通过 → commit → archive + specs 同步 → 回写本清单

## 6. 路线图收尾

- [ ] 6.1 全部阶段检查点完成确认(P0-P4 各自 archive 且回写)
- [ ] 6.2 两篇评审文档处置:文档现已移至 docs/.dev/(不随 git 走);按核查结论修订(#10 handoff 断言撤回、#9 MCP 时效标注、"glm-5.3 未发布型号"存疑标注——本机 coding 端点实测可用,疑似评审知识截止误判,见 LEDGER);入库与否由交接方决定
- [ ] 6.3 路线图自身 archive(全阶段完成)或"部分完成"归档(记录终止点与成果清单)
- [ ] 6.4 终版差距复盘:对照两篇评审的七项差距清单逐项标注状态(已解决/部分/未做+原因)
