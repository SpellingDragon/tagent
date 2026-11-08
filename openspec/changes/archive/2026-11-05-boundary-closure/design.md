# 边界闭合与收益验证：一级设计决策（静态）

## D1 基线与证据表（2026-11-05 于 48d8eb93 逐项核验）

| # | 断言 | 证据坐标 | 状态 |
|---|---|---|---|
| E1 | 登记失败仍发送 | assembly/host_delivery.go:74-89（`recordDeliveryOwed` err→`assemblyWarn` 后 `owed=bodies` 继续发） | 属实 |
| E2 | 输出↔义务窗口 | 恢复仅扫 `hostDeliveryPrefix`（host_delivery.go:495）；`deliverClosedSegment`（host.go:931-943）先落 terminal 事实再登记 | 属实（静态） |
| E3 | retry 无自动驱动 | host_delivery.go:213/217/221 失败→`restoreHeldDelivery(retry)` 仅回表；恢复路径 held/retry 只入 pendingDeliveries | 属实 |
| E4 | 预算只及投影 | assembly/request.go:477-478（AssembleBudgetPrefix 只约束投影）；固定文件+技能正文经 projectionAssembler 直入请求 | 属实 |
| E5 | 冒烟斜率超门 | 评审两次实测 175/159 KiB/轮 vs ≤128（examples/wechat-bot/smoke_test.go TestLocalSmoke + harness 门） | 红 |
| E6 | KV 分片全内存 | memory/ledger/localfile.go:89（shards map[string]map[string][]byte，open 全量加载，无淘汰） | 属实 |
| E7 | 事件缓存无界 | memory/ledger/segment_store.go:98（cache map[EventID]StoredEvent，无淘汰） | 属实 |
| E8 | exec 假成功 | 远端宿主 11-04 上报：死 pane 回放旧 scrollback + exit=0，touch 未落盘；规避=每命令新会话（tool/exec/tmux_executor.go） | 属实（远端实证） |
| E9 | 折叠剥除时间 | memory/replay/age_segments.go:419（`entry.Type+": "+entry.Summary`）；世代标识不携带 | 属实 |
| E10 | OOM S1/S2 已修 | capture_wiring.go:270-310 swap store；facts.go:129,161、replay_budget.go:303-313 delete | 已闭合，本变更不重复 |
| E12 | **预算能力回退**（grill r1 F33 归因更正）：`request-budget-accounting` spec 在（2026-10-08 归档）；其实现（modelutil/agent/compress 预算）被 **6625dff8（2026-10-26）** 删除（同提交建压缩请求事件族），170ef267（11-04）另增投影侧 replay_budget；当前 HEAD=投影预算在、**最终请求统一预算面缺**（归档测试全仓 0 命中） | git log --diff-filter=D 实测 + grep 0 命中 | 02 域=能力恢复（5.9 裁决：MODIFIED delta 于既有能力，02 任务 1.0 以 6625dff8 为审计基线） |
| E13 | `delivery-dispatch-receipts` 明文：结果未知 MUST NOT 自动重发、显式重试走正式入口或人工处置 | openspec/specs/delivery-dispatch-receipts/spec.md「发送动作身份与未确认结果」 | 01 域边界：自动重试仅限**明确失败**；未知/歧义结果维持人工处置（不冲突，边界入 01 design） |
| E14 | 配置真相双轨（r17）：声明族有登记无生产点；config_change 无载荷；发布记录内存化（assembly/commit.go 自注 durable ledger 未接）；**安装失败 /reload 仍回成功**（探针实证） | api/config/fact.go（FactType 已冻结）；assembly/host_reload.go；assembly/commit.go:17-33；探针 `TestReloadReportsSuccessWhenTheModelRebuildIsRefused`（临时） | 08 域：链驱动重写（声明事实+唯一发布入口+回执如实） |
| E15 | 冥想自主整理面未接（r16）：meditator 工具面只有 send；file/exec/recall 待接线；三处漂移（旧两锚、README tick、日志头） | tagent.yaml:261-262；features/evolution/meditation/；meditation-agent-partition:44-46 | 09 域：工具面接线+状态机+四 delta |
| E11 | 本机 tmux 可用 | 2026-11-05 00:20 实测：new-session/命令执行/落盘/capture 全通过；`go test ./tool/exec -count=1` 全绿（12.3s）。（更正：先前『本机不可用』系旧 workspace-write 沙箱 pty 限制与评审环境混同的误传） | 环境事实 |

## D2 子域表

| # | 子域 | spec 能力 | 一句话边界 | 外部依赖 |
|---|---|---|---|---|
| 01 | delivery-closure | delivery-closure | **事件化路线（r1 终裁）**：交付义务=链上事实族+同笔原子提交+消费驱动发送/重试+KV 迁移退役+概念文档同步；不改 api/ 公共面 | 无 |
| 02 | request-budget | request-budget | 最终请求边界的统一预算与超限处置；配置面可声明 | 无 |
| 03 | resource-bounds | resource-bounds | 存储层条目级驻留/淘汰、空闲地址回收、冒烟斜率达标、pprof 长跑 | 无（pprof 仅本机诊断） |
| 04 | exec-integrity | exec-integrity | 死 pane 检测、回执-磁盘一致性、CI 集成测试 + fake 单测 | CI runner 有 tmux |
| 05 | fold-provenance | fold-provenance | 折叠摘要行带时刻+完整世代；AGENTS.md 注记防回归 | 无 |
| 06 | live-benchmark | live-benchmark | 固定模型/工具/预算的三方对照实验 harness 与 run | 真实 LLM 凭据（ZAI_API_KEY，探针三态门） |
| 07 | training-sample-loop | training-sample-loop | N 个任务的完整样本（请求回读+独立验收+验收结论入 rl annex）；reward 向量派生=显式欠账 | 复用 06 的任务集 |
| 08 | config-liveness | config-liveness + config-hot-reload（MODIFIED） | **链驱动（r18）**：文件=编辑界面、声明事实=链上账、唯一发布入口；发布记录上链；失败回执如实（r17 缺陷结构性修复）；watcher/重启对账同一入口；不动世代机制 | W1 入 main（同触 assembly 窗口） |
| 09 | meditation-autonomy | meditation-autonomy + meditation-idle-gating（REMOVED）+ meditation-agent-partition（MODIFIED）+ self-improvement-meditation（MODIFIED） | 自持状态机（激活=真实事件带因果；不新立事实族）＋工具面 file/exec/recall（refine 延后=规格债）＋自察主体/专属反思会话＋挂载行为等价迁移＋三处漂移修正；三件套语义不动；assembly 注册面默认不动（F25 收窄） | 01 入 main（工具面/世代校验稳定） |

## D3 波次表（波次仅汇报分组；解锁=前置入 main 并过核验，不等整波）

| 波 | 域 | 前置 | 备注 |
|---|---|---|---|
| W0 | 锚定波（编排者亲自）：7 症状锚测试落盘并确认红 | 无 | R33：第一天红着；锚绿=误诊，回写 D1 |
| W1 | 01 交付闭合 ∥ 04 exec 完整性；**05 串行在 01 后**（loop.go 提交面稳定，grill r1 F26 裁定） | W0 | 01（host_delivery*、host.go、loop.go、request_assembly.go、memory/query_events.go 的 QueryOptions 键前缀过滤（仅返回集过滤）、**assembly/run.go 的 r.replays 会话枚举（仅会话枚举；结算事实与 owed 均读 r.facts 事实窗口内、PartitionIDs 由注册键派生；该事实窗口内面与 03 的 1.2a 共用，集成测试归 03 的 1.2a）**、memory/ledger 生命周期注册）∥ 04（tool/exec，含 tool.go held 短路）不相交；05（facts.go mappedEntryOf+replay）在 01 入 main 后即派（R31 单完即派） |
| W2 | 02 请求预算 ∥ 03 资源有界 ∥ 08 配置活线 ∥ 09 冥想自主面 | **W2=02∥03∥08∥09**；08 前置=W1 入 main；09 前置=01 入 main（03 仍=W1 全入 main） | 02 触 assembly/request.go+config；03 触 memory/ledger+smoke+golden；08 触 assembly/{host_reload,host_document,facts,commit}.go+runtime/state/publish.go+examples/wechat-bot watcher；09 触 examples/wechat-bot/tagent.yaml+features/evolution/meditation+prompts/docs+golden（assembly 注册面默认不动，F25；若必须动=与 08 同文件，合单/串行）；集成序：02 先入（预算影响 smoke 的 token 面），08/09 按各自前置解锁 |
| W3 | 06.1a 夹具与验收器共享包先行 → 06 其余 ∥ 07 训练样本 | 06.1a 前置=02 入 main（不依赖 03；W2 全入不必要） | grill r1 F27：07 复用 06.1 的夹具与验收器包，故 06.1a 先行入 main，06 其余与 07 并行消费它；06.1 登记 bench 目录 golden（bench/、bench/tasks/、bench/runners/、bench/metrics/、bench/report/）、07.3 登记 features/rl/sample 包与 **features/rl/sample→bench 边**（复用 bench 根包；不是 features/rl→…；触碰 golden 时同步登记） |
| F | 收尾 | W3 | F1-F4 |

## D4 合并规则（硬拦）

1. `api/**` 零改动，唯一例外=`api/config` 新增两个可选字段（provider 窗口、输出余量；缺省推导生效，非行为不变；**`decode.go`/`fingerprint.go` 配套行同例外清单**）；F1 断言排除该例外并列例外行供复核。
2. 交付演进仅 01 允许：新事实族登记（FACT-VOCABULARY）+`tagent/task/v1/delivery:` KV 记录迁移转只读（幂等负锚锁死）；`ApproveDelivery`/`RefuseDelivery` 公共入口语义不变。
3. 二级域间文件触碰集不相交才可并发（R28 孙任务粒度）；同文件簇合单。
4. 每单完成先过本包门（build+test）再入集成树；编排者每波末全量门（scripts/preflight.sh）+ race（scripts/gates/race_check.sh）。
5. 契约守护三层：spec 条款（各二级 spec）+ D4 硬拦 + F1 diff 断言。

## D5 三级验收

- 孙任务：任务行自带验证命令 exit 0（非空转：用例数>0）+ 正/负/消费者三锚齐备。
- 子域：spec scenarios ↔ 测试映射齐全（scenario 每条有具名测试）。
- 变更级 DoD（一级 tasks.md F 节可勾选）：7 症状锚全绿 + 全量门禁绿 + **08 证据位**（声明事实/发布记录/失败回执三链测试 + watcher 三例 + 重启对账/跨维窗口）+ **09 证据位**（自主整理、自察四测试、激活因果、自管产出不计入状态积累）+ 06 对照 run 落盘 + 07 样本三要素（请求回读/独立验收/验收结论入 rl annex）齐备率 100%（分母=全部样本，含 incomplete）+ F1 契约零改动；reward 向量派生=显式欠账，不属 DoD。

## D6 契约冻结清单

- `api/**`：零改动，**唯一例外（r3 F5 定界）**：`api/config` 允许**新增两个可选字段**（provider 窗口声明、输出余量；缺省推导生效，非行为不变——见 02 design；进 decode/fingerprint），F1 断言相应排除并列例外行供复核。api 其余面零改动。**08 与 09 对 `api/**` 零改动**（08 的声明类型常量驻 assembly；09 保留既有 `meditation` 节，不动 api/config）。（memory/ledger 存储面不在 api/**；本变更不另立第二份读路，事件缓存冷读**内存保留既有 `{pid}:idx:{event_key}` 键的轻量映射（键驻留=既有索引的驻留形态，值=`{window}:{seq}` 极小；body 仍惰性）**，GetEvent miss 由该映射定位 pid→窗口段读回。）
- 交付记录前缀与既有状态字面量（owed/held/retry/sent/refused）：保留语义；新增状态需 01 design 冻结并给旧读兼容负锚。
- `tagent.record.v1` 记录字段语义：07 只新增 Line 记录种类 `RecordKindAnnex`、不改既有字段。
- openspec/specs/ 主规格：只经归档同步改动，不由本变更手改。**唯一例外（r8 F8）：归档后在各主 spec 上直接补写 Purpose（非 TBD）是本变更允许的主 spec 手改，其余仍禁。**

## D7 风险与回退

| 风险 | 缓解 | 回退 |
|---|---|---|
| 03 斜率门（口径已裁决：量真占用） | 深挖档：pprof 长跑 + GC 后稳定采样分离真占用/虚胖；过关线只卡真占用≤128KiB/轮，虚胖观测上限=真占用门的 50%（64KiB/轮，超限记告警事实） | KV 条目级驻留与缓存淘汰独立成果保留 |
| 04 集成锚漂移 | 本地真实 tmux + CI 双跑同锚；任一红即修（fake 锚仍保留作快速回归） | — |
| 06 真实 LLM 凭据缺失 | R24 双层门：无凭据 **run 级 Skip 不阻断 W3 其余**；**change 级收尾仍无凭据=blocked 上报（不结项）**；有凭据才跑 | 延后 run，harness 先行 |
| 08 配置链驱动风险（声明事实链增重/对账漂移/安装失败仍回成功复发） | 声明载荷走既有内容寻址引用；重启一致零发布负锚；`install-failed` 回执读账硬锚（当前红）；watcher 内容 hash＋防抖 | 整体 revert 08（发布入口/世代语义未改写） |
| 09 状态机/自察风险（上下文预算/双源计时/挂载迁移回退） | 状态只经 AnchorStore 扩展、单次迁移旧水位只读退役；自察靠折叠与卡片有界；挂载为行为等价迁移（既有 meditation 节保留） | 工具面逐项回退；状态机整体 revert（不新立事实族） |
| 01 事件化迁移风险（旧 KV 记录形态意外/链增重/文档失同步） | 不可解析记录具名拒启动（不猜，负锚）；义务事实小+豁免折叠；斜率由 03 门兜底；概念文档列入 1.7 任务 | 整体 revert 本域（无实验开关；r22 纯化） |
| 冒烟 300 轮长跑阻塞编排 | R32：长跑单后台化，先收快单 | — |
