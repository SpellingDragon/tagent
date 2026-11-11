# Design: hot-update-and-purity-closure

## D1 症状锚（立案前提，R33）

- 锚 A·热更新：运行中 `Host.Reload` 移动某 agent exec 段、不重启 → published=true、新回合走新执行器、旧在途跑完旧代、旧资源排空释放一次。基线红（409，host_reload.go:790）。
- 锚 B·交付：已由 restart-recovery E3(e5cb2663) 兑现、基线绿（TestSymptomAnchorDeliveryOutsideWindowSurvivesRestart）；本变更不再以 B 为红锚，01 重 scopes 到 retry 有界/读错具名/legacy。红锚仅余锚 A。
- 工程目标只能追加，不得替掉两锚；工程全绿而锚红时以锚为准。

## D2 子域表

| # | 子域 | spec 能力（MODIFIED） | 一句话边界 | 外部依赖 |
|---|---|---|---|---|
| 01 | delivery-correctness | delivery-closure | owed 驻留/retry 有界/交付读错具名 | 无 |
| 02 | read-failure-honesty | recall-protocol | corruption≠missing、barrier 失败不 serve 未落盘 | 无 |
| 03 | retired-state-hygiene | config-state-twinship | 影子/死参/独立写权清理 | restart-recovery 归档（recovery/run 簇） |
| 04 | single-source-gates-docs | architecture-guardrails | 常量单源/gate 洞/文档清偿 | 无 |
| 05 | event-guided-hot-update | config-hot-reload + config-liveness | 事件指引整代装配清全 409 清单 | 03（run/host_reload 簇）、D6 契约冻结 |
| 06 | window-recovery | (转移 delta) | 折叠锚/派生态/默认锚 boot/时间纯度/真实重启 | 03 |
| 07 | ci-orchestration | (转移 delta) | 重启计数进 CI/分片超时重冻/具名 liveness | 06 |
| 08 | guards-and-tests | (转移 delta) | F0 边界/R3 登记/测试重写/R35 | 06/07 |
| 09 | runtime-simplicity | (转移 delta) | **本轮只含纯核心**：journal 帧/原子提交/单屏障/证据/导出（已勾 1.1–1.6、4.1，另有已勾 2.1/2.3/5.1/5.2/5.3/6.1）；流式与其余特性叶已拆至独立变更 | 02/03 |
| 10 | turn-segment | (转移 delta) | 消费关系闭段取代 agent_output | 01 |
| 11 | derived-faces | (转移 delta) | 派生读面收敛 | 10 |

**D2 注记（2026-11-08 用户裁定）**：09 的特性 program 已拆分，本变更本轮只含 09 的纯核心（追加日志/原子提交/单屏障/请求证据/导出，均已勾并附复跑凭据）。拆分去向：流式四叶 3.1–3.4 → `openspec/changes/runtime-streaming-increments/`；其余特性叶 0.1/0.2/2.2/2.4/2.5/4.2/4.3/5.4/6.2/6.3 与 09 收尾 F1–F4 → `openspec/changes/runtime-simplicity-features/`。两个承接变更为独立顶层变更，**deferred、不在本轮实施**，不占派发写面、不计入本变更 DoD；转移明细见 `changes/09-runtime-simplicity/tasks.md` §11。D3 波次表与 D5 变更级 DoD 不受影响（本轮波次本就不含 09 特性叶）。

## D3 波次表（波次仅汇报分组；解锁=依赖入 main）

| 波 | 内容 | 解锁条件 |
|---|---|---|
| W0 | 锚红 1.1/2.1/5.1 + 探针 5.2 + 契约冻结 5.3 | 立案即跑（探针失败回退不阻主线） |
| W1 | 01(1.2-1.7)、02(2.2-2.5)、04(4.1-4.4) 并发 | 触碰集不相交实测 |
| W2 | 03(3.1-3.5) | restart-recovery 归档 |
| W3 | 05 消费者 5.4-5.12 并发 | 5.2/5.3 入 main + 03 入 main |
| W4 | 5.13 混沌 + 5.14 wiki + F1-F4 | W3 全绿 |

## D4 合并规则

- 同文件簇合单或串行：host_delivery.go/host_tools.go/loop.go 归 01；read.go/get.go/localfile.go 归 02；commit.go/host.go/host_capture.go/run.go(SetRetrievalTuning)/store_searcher.go/recovery.go 归 03；fuse.go/request_assembly.go/window_bounds.json/scripts/gates 归 04；run.go(装配)/host_reload.go/tool-exec-memory-meditation 归 05。
- run.go 与 host_reload.go 为 03→05 串行簇（03 先清小改，05 后装配）；或编排者单写。
- 05 内部消费者共享 run.go 装配缝：按 D6 冻结签名级契约后各消费者只改本包+登记调用，独立派发（R22）。
- 每域入 main 才解锁下游；跨域缺陷由编排者唯一裁决修复（R8）。

## D5 三级验收

- 孙任务：verify 三锚 exit 0（正锚点名测试+-run 逐字符；负锚具名错误路径硬门；消费者锚=公共入口 Host.Reload/Run.Invoke/recall_get/boot 真实路径+负例）。
- 子域：specs/<cap>/spec.md 全部 Scenario 映射到测试或命令。
- 变更级 DoD：见 tasks.md F 清单；含锚 A/B 红转绿+回滚必红双证据、混沌≥10 重复+race、openspec --strict、scripts/gates 全绿。

## D6 共享面冻结契约（5.3 产出，本任务不产代码；下列签名与 5.2 探针落地代码逐字一致，后续消费者域按此独立派发）

- 每代 builder 签名（runtime/state/genregistry.go）：`type GenBuilder func(ctx context.Context, decl *config.Declaration, gen Generation) (GenResources, error)`；登记结构 `type GenRegistration struct { Name string; BuildGen GenBuilder }`；`GenResources` 携带 `Resources *Leases`（`ReleaseAll(ctx)` 释放独占资源、`Refs()` 列清单）与 `Restore func()`（候选未生效时放回旧面）。注册表驱动面：`func (g *GenRegistry) Build(ctx context.Context, decl *config.Declaration, gen Generation) (GenInstances, error)`、`func (g *GenRegistry) Register(reg GenRegistration) error`；`GenInstances` 携带 `Restore()` 与 `Release(ctx)`。
- 宿主方言面（编排裁决·批次3 G 上报）：request_timeout_seconds/governance/telemetry 属宿主文档方言、不进 Declaration；冻结附加面 `GenHostSpec`（publish 时自 host document 解析注入 GenBuilder 闭包），provider 经其读取，不改 GenBuilder 公开签名、不动 api/config（F1 零改动）。storage.type/root 仍留重启档（Q3）。
- 登记键：`func GenLeaseKey(gen Generation, consumer string) string` 返回 `fmt.Sprintf("gen:%d:%s", gen, consumer)`（即 `gen:<id>:<consumer>`）；驱动以 `cand.Resources.Register("gen", state.GenLeaseKey(gen, consumer.Name), consumer.Resources.Resources.ReleaseAll)` 登记并把返回 ref 回写 `cand.Plan.Leases`（诊断读面消费）；整代退役追踪复用 `Publisher.Publish` 内 `retire.track(got, cand.Resources)`（runtime/state/retire.go track/sweep 零改动）。
- 事件：复用既有 org.* 声明事实作装配指引；retired 以确定性 Key `gen-retire:<id>` 走 commitFactChecked 按键去重得 exactly-once（该 Key 为冻结保留常数，事实落地归 5.12 崩溃窗口退役收敛，W0 探针未产该事实）。退役事实仅作链上记录与重放锚，资源释放由退役台账 drain 触发执行（Q2 裁定），不作释放触发器。
- 装载时序（W0 急装载路径，assembly/run.go publishDeclared）：声明事实先落链（Reload 的 recordDeclarationFact，既有）→ `genRegistry.Build(ctx, decl, gen)` 构整代消费者（gen = `publisher.Current().Generation + 1`，先于 prepare）→ prepare 构绑定（`NewPreset(manager *Manager, rhythm MonitorConfig, opts ...RunToolOption) []trpctool.Tool` 快照当世代实例）→ `publisher.Publish` 整代指针切换（复用）→ 借还 drain（复用）→ 退役 sweep 释放（复用）。观察滞后窗口以"按代 ID 惰性幂等装载"兜底（5.15 落地）。候选未生效（prepare 失败/拒发布/unchanged）一律 Restore 旧面并只释放候选独占资源；publishMu 串行化整次尝试。
- 资源三分（ADR 0007，exec 探针实证）：进程私有每代资源随绑定生灭——exec Manager 实例（账本+tracker）每代新建，退役释放 = `func (m *Manager) Detach(_ context.Context) error`（停 tracker、丢账本，不动外部执行）；外部耐久资源（tmux session/磁盘 store）跨代存活、重探不重建——新代调用 Query→Adopt 重探接管（锚 A "reattached" 收据实证）；共享 natives（model）复用不重建（既有 lease 机制）；executor 及其 session generation token 跨代共享（模板 backend）。
- 消费者自注册形态（W0）：exec 经既有 DI 选项 `func WithExecManager(manager *tagentexec.Manager) Option` 挂载 `func GenRegistration(template *Manager) state.GenRegistration`（tool/exec/registry.go）；装配根驱动只认 `state.GenRegistry`，不认特性名（R2 门绿）。5.16 归位为无条件自注册（覆盖 boot 未声明 exec 而 reload 新增 agent 携 exec 的场景）。
- 被否方案：① 每个活消费者收事件就地自换（否：撕开整代原子性、drain/release 义务摊给每 hook）；② 新造中心化插件卸载树（否：违背事件驱动哲学且与退役台账重复）；③ 仅 exec 热换其余留后续（否：用户裁定全清单一次清）；④ 每特性 typed per-gen builder（否：犯 R2、留 N 条突破例外与 N 条热换路径）。

## D7 风险与回退

- storage.type/root 属根状态，具名保留 restart 档（Q3 裁定），不属热更缺口；全清单＝非根消费者。最大未知降为其余非根消费者的在途安全，探针 5.2 先以 exec 验证机制。
- 回退：每域独立入 main，可单域回滚；05 失败保旧代（fail-closed），回滚=旧绑定作为新代发布。