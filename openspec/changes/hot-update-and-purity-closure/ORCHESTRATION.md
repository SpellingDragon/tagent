# hot-update-and-purity-closure 编排文档

## 一、结论

抵达 DoD 的唯一路径：W0 先立两锚红+exec 探针证明事件指引整代装配可行并冻结共享契约 → W1 并发清交付/读面/门禁文档 → W2 hygiene → W3 全清单消费者每代装配 → W4 混沌+收尾；当前最大风险＝storage 根变更的在途安全（5.7），探针失败则具名保留 restart 档并回写 proposal，不阻主线。

**甲案阶段（r47/r48 后，用户裁决「一口气做干净」）**：Wave1 夹带清零（S/C/D 三轨并行）→ Wave2 会话级协调者下沉＋`Stateful` 收编 g(x) → Wave3 有界投影 → 收口合并。当前最大风险＝Wave3 若再以「删已提交事实」或「跳过压缩判决集」换有界，会原样重演 12.12/12.13 的整段回退（证据见 12/tasks.md 五节判决理由）；故 Wave3 重开的前置是 answered 切片，不是压缩手段本身。

## 二、编排四问

### 2.1 次序（就绪孙任务表；派发原子=孙任务）

| 孙任务 | 域 | 前置 | 状态 | 触碰集 | 派发单 |
|---|---|---|---|---|---|
| 1.1/2.1/5.1 锚红 | 01/02/05 | 立案 | ready | 各自新增测试文件 | W0-1 |
| 5.2 探针 | 05 | 5.1 | ready | tool/exec+assembly/run.go(探针隔离) | W0-2 |
| 5.3 契约冻结 | 05 | 5.2 | ready | design D6（不产代码） | W0-3 |
| 1.1-1.8 | 01 | restart-recovery 归档（host_delivery 活跃编辑重叠） | blocked | host_delivery/host_tools/loop | W1-1 |
| 2.2-2.5 | 02 | 2.1 | ready | read/get/localfile | W1-2 |
| 4.1-4.4 | 04 | 无 | ready | fuse/request_assembly/window_bounds/gates/docs | W1-3 |
| 3.1-3.5 | 03 | restart-recovery 归档 | blocked | commit/host/host_capture/run/store_searcher/recovery | W2-1 |
| 5.15/5.16 注册表+归位 | 05 | 5.3+03 入 main | blocked | assembly 注册表缝(编排者)+各消费者包 | W3-0 |
| 5.4-5.12 | 05 | 5.15/5.16 入 main | blocked | 各消费者包+登记调用（run.go 缝归编排者） | W3-* |
| 5.13/5.14/F | 05/一级 | W3 全绿 | blocked | integration/chaos/wiki | W4-1 |

并发准入=触碰集不相交（实测 `go list -deps`）；W1 三单触碰集互斥已核（01 delivery 簇 / 02 read 簇 / 04 gates 簇）。本轮做不了：03/05（启动门未过）。

### 2.2 授权

每单白名单精确到上表触碰集文件；禁 git commit/checkout、禁动他域文件；过程件落 /tmp/<change>-<单号>/。每单含"自清白名单内 finding"条款（门命令 `scripts/gates/lint.sh` 随令下发，自面清零判据随令）；共享面/全局 finding 归编排者；编排者终跑门是唯一终态判据（agent 各自绿≠整体绿）。共享面 run.go 装配缝、host_reload.go、api/event、api/config 归编排者单写（05 消费者经 D6 冻结签名只改本包+登记调用）。偏离分级：白名单内替身/mock 预授权；扩 409 保留档、改 D6 签名、storage 不可热换裁决=上报。

### 2.3 凭据

五查按派发单核；每条孙任务 verify 三锚 exit 0（正锚 -run 逐字符+非空转用例数>0；负锚具名硬门；消费者锚=Host.Reload/Run.Invoke/recall_get/boot 公共入口+负例）。必亲验数字：409 清单 grep 计数=0、混沌重复次数≥10、race 报告空、锚 A/B 红绿退出码、F1 diff 空。

### 2.4 释放（锁必携等待表）

| 锁/占用 | 释放条件 | 等待表（释放后当场补派） |
|---|---|---|
| restart-recovery 归档门 | 该 change archive | 补派 W2-1(03)；03 入 main 后补派 W3-* |
| run.go 单写锁 | 编排者落 D6 缝 | 补派 05 各消费者单 |
| 探针设备/chaos 槽 | 5.2 / 5.13 结束 | 补派 W3 消费者 / W4 收尾 |

## 三、明细

派发单-孙任务矩阵见 2.1；合单判据：同域同文件簇合单（01 的 host_delivery 簇含 1.2/1.3/1.4/1.7）；单均<2 不拆。预算：W1 三单并发、W3 消费者按包并发上限=不相交包数；混沌 5.13 为长跑单后台跑不占并发槽（R32）。档位与真实路径欠账：storage 远端 daemon 真实路径若缺凭据按 R24 三态（无门 Skip/有门缺凭据 Fatal/门开非空转）。

**W5（合并阻断增补）domain 12-checkpoint-state**：LongRun boot 二次扫描挂死（Unsettled 逐站点全史读，O(站点×事件)）阻断 PR#37。用户裁决统一 checkpoint 事实族（Flink 框架状态管理），取代 06 D3「快照装全状态否决」一条、继承 D4 折叠锚一致性切面。契约冻结于 12/design.md D1-D7。派发：12.2-12.6 耦合簇（api/event+assembly/run+host_delivery+runtime/state 共享）合单派一个 openspec-apply 顺序实施；12.7 测试契约冻结后可并行；编排者终跑门为唯一终态判据。触碰集白名单＝12/tasks.md 所列；禁动他域。合并门＝LongRun 六分片全绿 + 每代 boot 成本平坦。

**W6（甲案三波）domain 12 终态收口**：Wave1 夹带清零 → Wave2 会话级＋g(x) → Wave3 有界投影 → 收口合并；派发原子仍为孙任务，准入＝触碰集不相交。

| 波 | 就绪孙任务/对象 | 前置 | 触碰集 | 派发单 | 验收锚（缺一不放行） |
|---|---|---|---|---|---|
| Wave1-S 语义夹带清零 | 12.3b/12.10 静点与结算语义的残留核对；r48-1（reload「有效声明」应为随代指针事实，既有缺陷）单列 | 12.3b/12.10 已绿 | assembly/checkpoint.go、host_delivery.go、host_reload.go | W6-1 | 正锚：`TestCheckpointWaitsForTheQuietPointOfThePipeline`；负锚：删 `r.flight.await` 必红；消费者锚：LongRun 真实宿主过 gen60 |
| Wave1-C cadence 收口 | 12.14 已记（保留 markFlightHeld 旁路、回退 close/fold 接线）；12.15 触发器②③另行接线 | 12.14 绿 | assembly/checkpoint.go、compress_consumer | W6-2 | `go test ./assembly/ -count=1` 全绿；`TestCheckpointEmittedAtCadenceAndFold`/`TestRestoreFoldCounts` 不因新增载体改判 |
| Wave1-D 文档台账 | 本单：wiki 四页收编＋GLOSSARY＋本文件三波计划＋新变更骨架 | 无 | docs/wiki/、docs/GLOSSARY.md、openspec/ | W6-3 | 改后 grep 实证；`openspec validate hot-update-and-purity-closure --strict` 绿 |
| Wave2 会话级＋g(x) | `session-checkpoint-material-unification`：协调者会话级下沉、`Stateful` 收编消费者读态（reducer=g(x) 纯派生面、emitter=observation 驱动者）、六端口收敛 Material 契约 | Wave1 全绿入 dev | 新变更自述白名单 | W6-4 | 新变更 proposal/tasks 三锚冻结后派发；症状直译锚第一天红 |
| Wave3 有界投影 | 12.12 B3 载体有界（不得删已提交事实）、12.13 B4 投影 past-cut（先补 `compress.answered` 为 `Stateful` 切片） | Wave2 的 answered 切片落地 | memory/ledger、assembly/run.go 投影种子 | W6-5 | 三方 parity（锚定/回退/全量）判决集相等；`TestTierQueryMatchesObservedRead`/`TestObservedEventsReadsBeyondHotWindow` 绿 |
| 收口合并 | 12.8 编排者终跑门、12.9 8.1 卡 superseded 标记、F1-F4 | Wave1/Wave2/Wave3 全绿 | 门命令与台账 | W6-6 | 12.8 逐条 exit 0；F1 diff 空；`openspec validate --strict` 绿 |

释放在 Wave1 三轨：S/C 同改 `assembly/checkpoint.go`，触碰集相交 ⇒ 合单串行或 S 先落再派 C；D 与二者不相交可并行。Wave2 开工前 Wave1-S/C 的 `assembly/` 写锁必须释放（等待表：12.8 之外不得留未落盘编辑）。Wave3 的 12.12/12.13 已按「回退＋立卡」记于 12/tasks.md，重开时先读其判决理由与后续纯化判据，不得以新测试期望替代回退事实。

**W7 合并门根因修复 domain 13-bounded-compression**：长程校准六分片全红的真正根因＝压缩产物无界（88MB 超帧上限）＋三段龄名存实亡（级别算了但 FoldByAge 只分新/非新，中+老塌进一条摘要）＋摘要累积全文（historySummaryLine 对上一产物抄 entry.Content）。用户裁决：三段龄按**世代递推**（新=recent 保留／中=上次新这次折叠过程细节／老=上次中老这次摘要）；老带 **LLM 语义摘要优先**（落摘要事实保恢复确定，无配置降级机械有界 recent-K＋key 引用，失败降级/欠账）；摘要**有界幂等**（fold(fold(x))==fold(x)，上一产物不抄全文）。此域取代 Wave3 的 B3/B4 局部回退方案（12.12/12.13），从根上落地有界压缩。契约冻结于 13/design.md D1-D7；耦合核心 13.1-13.4 合单串行，测试/golden 13.5-13.7 随后。合并门＝LongRun 六分片整代 PASS＋产物有界＋boot 平坦。方法论教训（用户）：反复修 CI 失败暗示架构设计不清，须整体思考定位核心而非逐 symptom 打补丁。

## 四、台账

| 轮 | 五查结论 | 集成验证 | 跨域裁决 | 勾选凭据 | 下轮情报 | 锁释放 |
|---|---|---|---|---|---|---|
| 规划 | 自审见下 | — | — | — | 基线 dev 036c3500；两锚基线红证据=host_reload.go:790(409)/host_delivery.go:547+1481(trim) | — |
| 终审轮 | R35 两轮：首轮三阻断(doc-budget/meditation 水位代际/文档脱节)由 R4 修；复审余 R35-1(拒绝候选水位滞留)已修+TestRefusedMeditationOverNoMeditationBootClearsWatermark 钉；concept 基线重指可达 main(3b727199)、removed_min 校准 5；CI 待绿后 squash | 见 /tmp/hupc-R4、C5、R35 报告 | 登记例外 anchors/approvals/RL 为受治理债(review_by 2027-05-05) | 6.3/6.4 勾 | CI watch bd0rgbyxs | — |
| W5 域12 进度 | 12.1-12.7a/12.7c/12.7d、12.10/12.11/12.14 已勾（事件类型+载荷、Stateful 接缝+安静点协调者、五切片接线、恢复重写删三全史读者、RecoveryTicket 退役、B1 水位、B2 NewestOfType、C3 收口） | 12.8 编排者终跑门未跑；12.7b LongRun 卡在 r48-1（reload 回退点既有缺陷）；合并门＝LongRun 六分片全绿+每代 boot 成本平坦 | 8.1 由 12.4/12.4b 开账嵌入 checkpoint 构造消解，8.1b 待 12.8 的 `TestEventIntegrityTaskContinuation` 复跑；12.12/12.13 已回退立卡，重开前置见 W6 Wave3；06 D3 一条已由 r46 勘误取代 | 12.3b `TestCheckpointWaitsForTheQuietPointOfThePipeline` 绿 ×3 -race；12.10/12.11 各自 `-run` 绿；12.14 `go test ./assembly/ -count=1` 全绿 | 12.8 终跑门；12.9 收尾回写；W6 三波 | —（12/tasks.md 写锁仍归 domain12 实施代理） |
| W6-D 文档台账 | 文档收编完成：wiki 四页＋GLOSSARY 收敛到当前实现（B1 水位／B2 `NewestOfType`／安静点无锁／`seedFact` 隔离），目标态（会话级下沉＋`Stateful` 收编 g(x)）标于平台篇 §六；三波计划与 12.12/12.13 回退立卡核一致；新变更骨架已建 | 未跑代码门（本单禁碰代码，assembly/runtime 另代理在改） | 无 | ORCHESTRATION W6 表＋台账；12/tasks.md 小节编号修正；`docs/wiki/{platform/platform-subsystems,memory/memory-architecture,agent/agent-architecture,mechanism-tour}.md`；`docs/GLOSSARY.md`；新变更 proposal/tasks/spec | 两变更 `validate --strict` 均绿；执行期文档随 W6 各波回写 | — |

（执行期逐轮回填；本档为派发唯一依据，派发前重读二节。）
