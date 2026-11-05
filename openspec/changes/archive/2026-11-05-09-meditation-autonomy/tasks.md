# 冥想自主面：孙任务

> 验证约定：所有 `go test -run` 命令默认附加 `-v`；用例数=`--- PASS:` 行数；零匹配不算通过。

- [x] 1.1 工具面接线（F25 触达集收窄；夹具明确：测试用**显式夹具配置**构造冥想目标 agent 声明，不依赖默认文件的注释状态；1.3a2 迁移后夹具同步指向宿主声明）：只改 `examples/wechat-bot/tagent.yaml` 的 meditator 声明（file 读/exec 受限执行/recall）＋ meditator 侧声明校验；**工具白名单文件不改**。`TestMeditatorToolFaceWired`（Boot 后 meditator 声明面含 file/exec/recall）+ 负锚 `TestMeditatorToolFaceStillGated`（越权目标/参数被既有门拒）。 —— 验证：`cd examples/wechat-bot && go test -v -run 'TestMeditatorToolFaceWired|TestMeditatorToolFaceStillGated' -count=1` 2 PASS 行。

- [x] 1.2a 负反馈进反思素材（插件挂点，self-improvement-meditation MUST 的承载）：digest 渲染扩展——观察面内自水位以来的 negative feedback 事件优先列入（带事件键）；零 LLM、单次扫描复用。`TestDigestIncludesNegativeFeedback`。 —— 验证：`cd examples/wechat-bot && go test -v -run 'TestDigestIncludesNegativeFeedback' -count=1` PASS。

- [x] 1.2b 提示词边界句更新：prompts/meditation.md 的"你没有 recall 与 file 工具"随接线改写（素材=摘要+可回查工具；纪律不变：结论只从事实出发）。 —— 验证：diff 入台账＋`python3 scripts/gates/check_doc_links.py` exit 0。

- [x] 1.3 自主整理端到端：`TestMeditationAutonomousTidyRun`（空闲门 fire→冥想轮用 file+exec+recall 整理→产物经既有路径落盘→send 驱动目标会话（可选））—— 验证：`cd examples/wechat-bot && go test -v -run 'TestMeditationAutonomousTidyRun' -count=1` PASS。

- [x] 1.3a2 自察迁移（r14/r14b；直接基于既有 `meditation` 节，不依赖挂载迁移）：wechat-bot 的冥想从独立 meditator 迁到 tagent 自身；反思回合跑在宿主的专属反思会话（`tagent-session:tagent:reflection`），经验卡片落 tagent 链；meditator 降格为可选跨域策展声明（默认注释）。四个测试全保留：`TestSelfMeditationFullContext`（反思材料含分区近期事实的真实内容，非仅 digest）+ `TestReflectionSessionIsolated`（用户会话链与投影零反思事实）+ `TestLessonsReachableByBusinessTurns`（业务回合经 recall 取到卡片＋send 可注入目标会话）+ `TestLessonsLandInOwnMemory`（卡片落反思会话、下轮折叠在望）。 —— 验证：`cd examples/wechat-bot && go test -v -run 'TestSelfMeditationFullContext|TestReflectionSessionIsolated|TestLessonsReachableByBusinessTurns|TestLessonsLandInOwnMemory' -count=1` 4 PASS 行。

- [x] 1.3b idle-gating 内化（r12）：门=状态机内部规则，不对外暴露；主规格 meditation-idle-gating 退役并入冥想对外契约——保留五条不变量（用户让路／锚点重启安全／触发即真实事件可审计／自管产出不计入状态积累／影响他人只经 send），机制条款（novelty 判定、双锚、rearm、fail-closed、抑制判据）下沉实现文档。spec delta=REMOVED meditation-idle-gating（旧四条款全 REMOVED＋**一条退役记录 ADDED**：OpenSpec 空 spec 不允许，归档实证；旧四条款 grep 0 命中）＋**ADDED meditation-autonomy 承接全部五条不变量**（分布在多条 requirement——grep 证据按关键词，不按单条计数）。 —— 验证：归档后主 spec 仅留退役记录、旧四条款文本 0 命中、冥想契约含五条不变量（grep 证据入台账）；**归档后 Purpose 改为退役说明（指向 meditation-autonomy；F4 ⑥）**。

- [x] 1.3c 挂载行为等价迁移（r16③ 用户裁决）：`meditation` 配置节保留既有形态；插件作为正常 agent 活动执行并落产物；不引入通用 `plugins:`、不动 `api/config`；挂载泛化与 schema 迁移**显式记欠账**（指向未来"agent 产物统一管理"变更）。`TestMeditationMountingBehaviorEquivalent`（迁移前后观察面/发送门/节奏/产物路径等价）+ `TestMeditationProductsLandAsNormalActivity`（产物经既有路径落盘、无专属运行时分支参与）。 —— 验证：`cd examples/wechat-bot && go test -v -run 'TestMeditationMountingBehaviorEquivalent|TestMeditationProductsLandAsNormalActivity' -count=1` 2 PASS 行。

- [ ] 1.3d 状态机类型与激活事件（F10）：状态机类型＋激活＝既有 `external_input` 携带因果元数据（激活规则/积累窗/来源）；状态持久经 `AnchorStore` 扩展；**不新立事实族**（GLOSSARY「冥想不登记事实族」不变）。`TestActivationEventCarriesCausality`（激活事件真实存在且携带完整因果）+ `TestEnergyConservationSelfLineageNotCharged`（自管谱系不充电，复用 `SelfManagedLineage`；激活即复位）。 —— 验证：`go test ./features/evolution/meditation -v -run 'TestActivationEventCarriesCausality|TestEnergyConservationSelfLineageNotCharged' -count=1` 2 PASS 行。

- [x] 1.4 规格与漂移（四个 delta）：meditation-autonomy ADDED（整理面＋状态机不变量）＋meditation-idle-gating REMOVED（四旧条款；附一条退役记录防空 spec）＋meditation-agent-partition MODIFIED（自察主体/专属反思会话，废"本 agent 循环 session"旧约；门控与锚点改由状态机持有，消除与 idle-gating 退役的矛盾）＋self-improvement-meditation MODIFIED（叙事对齐自察宿主）；README tick 措辞修正；`[reflection]` 头统一。 —— 验证：`bash scripts/gates/check-openspec.sh` exit 0＋`python3 scripts/gates/check_doc_links.py` exit 0＋diff 入台账。

- [ ] 1.5a 三钉固化（r15；F11 的 lint 形态）：①golden/runtime/architecture.json 登记冥想插件包＋插件注册表＋反思会话接线（packages+edges），`python3 scripts/gates/check_runtime_architecture.py --implementation` exit 0；②lint：既有 `meditation` 节与既有专属解析路径登记为**基线债**，新增特性专属声明节/新增 `resolve<Feature>` 解析路径/运行时专属分支即红（门带正反样例自测）；③两门接入 preflight 链。 —— 验证：golden 门＋lint 门＋preflight 链三道命令各 exit 0＋反向样例（临时注入新增特性专属节/新增专属解析路径/未登记包）各判红，正反证据入台账。

- [x] 1.5 域门禁：`cd examples/wechat-bot && go test -v -count=1 ./...` 全绿 —— 验证：exit 0。
