## 1. 指纹与规范化（D3）

- [x] 1.1 实现 Agent 第 3 层组织配置的规范化函数（键序稳定、语义无关空白归一），并以单测验证：语义等价配置（键序/缩进/无关空白差异）产出相同规范化结果
- [x] 1.2 实现指纹计算（规范化中间表示 → SHA-256），单测覆盖双向：指纹未变（复用）与任一行为维度变化（身份/工具注册表/模型超时）指纹必变；以及**白名单/黑名单边界**——白名单字段（agents.* 结构字段、entry、prompt_dir、providers.*）变更指纹必变，黑名单字段（governance.dir、reliability.*.dir、agents.*.memory.type/path、mcp_servers、log_level 等）单独变更指纹不变（spec: 排除字段变更不触发重建）

## 2. 惰性重建与实例缓存（D1）

- [ ] 2.1 将第 3 层组织构建从启动期一次性构建改为按指纹惰性构建：指纹命中缓存时复用既有实例，行为等价改造（首次加载 = 指纹 miss → 构建），验证既有启动用例全部通过
- [ ] 2.2 实现整层原子快照替换：新配置解析+校验通过后构建新快照并原子切换活跃指针，单测验证切换后新事件使用新配置（spec: 组织结构惰性重建）
- [ ] 2.3 实现旧快照安全退役（不再接新事件、无引用后可回收），单测验证旧快照在在途引用归零后不再被活跃路径引用

## 3. 在途事件与并发（D2）

- [ ] 3.1 实现活跃快照指针的原子替换读路径（事件处理时读取当前活跃快照引用），单测/并发测试验证切换瞬间读到的快照要么旧要么新、且不可变；并按 design D2 依赖归属矩阵实现依赖装配：共享依赖（memStore 共享实例、govGate/govLedger、MCP registry、resolvedModels）跨代复用不重建，随快照走依赖（工具实例、compressor、bus）随代构建（spec: 在途事件处理共享依赖复用约束）
- [ ] 3.2 实现在途事件按旧快照完成处理：重建不等待在途排空、不杀死在途事件，集成测试模拟"事件 A 处理中 + 触发重建"，验证 A 按旧配置完成、后续事件按新配置处理（spec: 在途事件处理）；并发测试覆盖**共享 memStore 两代混写**：重建切换后旧代在途写入与新代新写入并发落同一共享 store，全部事件无丢失无覆盖、按时间序回放完整（race detector 下无数据竞争），以及"新配置删除工具 T 后旧快照在途任务调用 T 成功、新事件调用 T 走工具不存在错误"两个场景

## 4. 坏配置降级与可观测性（D4）

- [ ] 4.1 实现坏配置降级：解析/校验失败保留旧快照继续服务、进程不退出、降级事件记录（日志 ERROR + 计数器），单测覆盖坏配置与修复后再次生效两个场景（spec: 坏配置降级）
- [ ] 4.2 实现变更检测/重建开始完成/在途按旧完成/降级的全链路日志与计数器，验证一次完整重载流程可依时间序在日志中回放（spec: 热重载可观测性）

## 5. 端到端验证

- [ ] 5.1 端到端集成验证：进程不重启的前提下修改配置文件，验证指纹检测→重建→新配置生效→在途事件按旧完成→坏配置降级→修复再生效的完整闭环，产出验证记录（命令/日志摘录）

## 增量 A 执行记录（2026-09-11，主 agent）

- 1.1/1.2 已完成并带测试（org_hotreload.go / org_hotreload_test.go）。
- **D3 修订（实现期发现）**：CompressThreshold 移出指纹白名单——它可经 ApplyOrgParams（compressor 原子阈值换装）热生效，按 D3 判据"需重建实例才能生效才进指纹"不构成重建理由；留在白名单会使热平移分支永不可达（改阈值→指纹变→RESTART required，与特性目标自相矛盾）。单测双向断言已同步反转。
- 接线（tagent.New + WithConfigPath）：reloader 闭包 = mtime 检查→重解析→指纹比对→未变则热应用阈值/变则报 RESTART required（增量 B 占位）。
- 新增 ops/test 钩子：TagentAgent.CheckOrgReload / OrgThreshold（lifecycle.go）。
- e2e（org_hotreload_e2e_test.go）：热平移 0.8→0.5 / 结构变更保旧 / 坏配置 fail-closed 三场景全绿。
- 门禁：build/vet 绿；根包 86 PASS、agent 179 PASS（无 race；-race 下 7 处 DATA RACE 为 trpc-agent-go 上游存量，另案）、compress 97 PASS。
- 4.1 部分达成（fail-closed + ERROR 日志已落，降级计数器未接，随增量 B）；2.x/3.x/4.2/5.1 未动。
- **设计同步闭环（plan agent 复核补录，2026-09-11）**：design.md D3 白名单条目已移除 compress 阈值、黑名单显式登记 `agents.*.compress_threshold`（含修订依据）；此前仅在 tasks 执行记录注记、design 本体未同步，本次由 plan agent 补齐，消除设计-实现漂移。
- 提交实证：2aed0273 (feat(org): incremental A — compress_threshold hot-shift via config hot reload) 已在 dev 链上，origin/dev=4aeaa67（其子），推送属实。
