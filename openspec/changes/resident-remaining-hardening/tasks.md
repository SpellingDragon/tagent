# Tasks: resident-remaining-hardening

> 继承 `2026-09-17-resident-readiness-plan` 归档的 25 项未完成任务，重整为四批次。
> 已闭环项（WP0-WP4、6.8 两轮审查修复）不在本清单；授权类任务显式标注 BLOCKED。

## 1. 批次 A：压缩回收提升（6.7 展开 + Major 5）

- [ ] 1.1 task.TaskManager 增加 OnBatchRetire([]RetiredReceipt) 可选回调：RetireOrphans/reconcileZombies 循环收集 (task, sig)，循环结束统一回调一次；回调为 nil 保持逐个 onSettle 旧行为（design D1；fail-before：先写多 envelope 风暴场景测试）。
- [ ] 1.2 agent 层注册 OnBatchRetire：per-task settle 记录逐条 record-only 落链（task_record_sink 同款，供 RebuildTaskRegistry 归并），bus 只发布一条汇总 external_input（N 行票据摘要，复用 newTaskSettledEvent 行格式）。
- [ ] 1.3 settled 类 external_input 纳入压缩票据化折叠：折叠判定识别 `[task settled]` 前缀（或等价 Metadata 子型），多条连续 ref 合并单张汇总卡片（`✗/✓ [evt_key] 摘要行` + recall 提示文案）；测试覆盖「50 条结算折叠 ≥80% 字符回收 + 事实链/registry 不受影响 + recall 可取回原文」。
- [ ] 1.4 rl 包新增 EndpointRedirectPolicy(allowlist) 导出 helper（返回 http.Client 可用 CheckRedirect func：逐跳 host ∈ allowlist，空 allowlist 全拒）；宿主装配到 LLM client（先侦察 openai SDK 的 http.Client 注入点，不可行则按 design D3 降级为 swappable 层 transport 包装）。
- [ ] 1.5 测试：302 跳出 allowlist 被拒且无对越界主机请求；未启用重定向时跳转全拒；README 的「已知限制」注释更新为已实现。
- [ ] 1.6 估值器测量结论（非立项）：D2 上线后用远端 24h 基线数据出 chars/token 实测报告，决定是否另案立项。

## 2. 批次 B：票据守卫与基准（继承 6.1–6.6）

- [ ] 2.1 补卡片反例模型：非空但无 key、未知 key、丢首尾、丢高亮、不可解析、多行；旧路径错误接纳的用例先红（6.1）。
- [ ] 2.2 curateCards 接纳前校验输入/输出 key 集合，失败复用原卡片确定性下沉；验证合法浓缩、无模型/超时、计数正确且原文不变（6.2）。
- [ ] 2.3 处理单卡超预算与 budget-unrepresentable：保留可解析票据/截断标记，无法表达状态传给诊断；序列化/重启恢复后守卫仍成立（6.3）。
- [ ] 2.4 组合测试：压缩预算、under-budget 零整理、前缀冻结、tool 配对、recall items=50、engine 失败→关键词；声明集与调用路由不变（6.4）。
- [ ] 2.5 离线 benchmark：1k/10k/100k 事件 × 1/10/100 探测任务 × fsync 两档；记录 p50/p95、allocs、RSS、fork、扫描量、中英/代码/JSON token 估算误差；fixture 附 tokenizer 版本（6.5）。
- [ ] 2.6 性能回归对照：超 design D7 阈值先 profile 定因；实测收益优化另立 change，不自动引入替换（6.6）。

## 3. 批次 C：综合验证（继承 7.1–7.6；7.7–7.9 授权类）

- [ ] 3.1 soak 骨架子进程化：独立 write/terminate/reopen，禁用进程共享 registry 捷径；真实 localfile、默认 fsync、至少一次 compaction、校验进程 ID 与恢复实例不同（7.1）。
- [ ] 3.2 30 轮快速 E2E：accepted ID 对账、store→projection→实际请求→recall→宿主投递门；Content≠Summary、无锚/有锚/TTL 失效/partial（7.2）。
- [ ] 3.3 任务完整链：spawn Origin→事实记录→registry→R3 detector→watch/settle→反馈→投递门；nil 恢复、unknown 来源、service/job、stale/deadline、迟到信号、resume 换绑（7.3）。
- [ ] 3.4 governance/evolution/meditation 开关组合回归：证据不可用明确 unavailable/insufficient；无用户新颖性不自馈电；无审批不执行 critical（7.4）。
- [ ] 3.5 CI 扩展：根+bot 双模块、memory 子包/plugin/rl race、Python 验证器负例、OpenSpec strict；底层测试状态为准，wrapper 不掩盖失败（7.5）。
- [ ] 3.6 有界 fuzz 与错误注入：事件协议/键解析/压缩投影；保存种子、失败夹具、重放命令；lastEventKeys 淘汰后的因果语义核查（7.6）。
- [ ] 3.7 [BLOCKED: needs-authorization] 72h 隔离机器长跑（7.7）。
- [ ] 3.8 [BLOCKED: needs-authorization] 真实模型契约矩阵与限定预算样本；未授权明确 SKIP，不记 PASS（7.8）。
- [ ] 3.9 [BLOCKED: needs-authorization] 实施 diff 的独立代码审查（修复级问题清零，两轮失败升级 BLOCKED）（7.9）。

## 4. 批次 D：准出与文档（继承 5.7 / 7.10 / 8.1–8.6）

- [ ] 4.1 [BLOCKED: needs-environment] 隔离部署夹具验证：非 root、只读根、可写路径、符号链接越界、凭据权限；working_dir/分类器非安全墙确认（5.7）。
- [ ] 4.2 准出对照表：双模块 build/vet/short、定向 race、E2E、长跑、基准、上游豁免逐能力汇总；BLOCKED 项列「未验」，不声称全部完成（7.10）。
- [ ] 4.3 中英文档/wiki 同步：turn 间事件驱动/turn 内 ReAct、类型 TTL、票据诚实 miss、时间窗压实、进程/整机重启差异、volatile/durable/processed/delivered 边界（8.1）。
- [ ] 4.4 基于真实引用清理 TypeToolUse 残留与过时注释；核验 invBus/modelref 候选后仅删确认不可达项（8.2）。
- [ ] 4.5 WP3 owner/执行代职责可维护性复评：登记剩余大文件耦合/重复与重构候选；不改核心代码以免长跑证据失效（8.3）。
- [ ] 4.6 升级/回滚演练步骤：旧 spill 排空、inbox-v1 回滚条件、目录锁、配置冲突、分区冲突只读诊断、HTTP 限额与 allowlist；本地夹具演练（8.4）。
- [ ] 4.7 「headline→配置→实现→测试→证据边界」对照表；30 天受控部署观察清单（不算完成证据）（8.5）。
- [ ] 4.8 `openspec validate resident-remaining-hardening --strict` 与 `scripts/check-openspec.sh`；正常 archive 同步主 spec；发布候选 checklist（提交/推送/tag/部署待授权）（8.6）。
