## 1. bundle 体系退役(P3:版本管理复用 git)

- [ ] 1.1 删除 `evolution/bundle.go`/`bundle_test.go`(Bundle/BundleStore/InitBaseline/Active 切换)与 `evolution/source.go`(VersionedSource/BundleProvider 遮蔽层);`go build ./...` 绿
- [ ] 1.2 删除 `evolution/release.go` 发布状态机(Lane/ReleaseStage/Submit/approve/ProtectedPrompts/每日预算 Gate)**及 ActivationLog**(优雅化:窗口由 improvement 事件承载,见 1.4)与 `release_test.go` 对应用例;`go vet ./evolution/` 绿
- [ ] 1.3 清理 `tagent.go` 装配:移除 VersionedSource 包装/InitBaseline/N1 seed/BindPosterior 旧接线/`NewRefineTool` 旧构造;evolution 启用时 system prompt 回归文件直读(热重载);回归:evolution 启用配置下改 prompt 文件 → 下一 Get 生效(fail-before:bundle 遮蔽)
- [ ] 1.4 删除 `ActivationLog`(优雅化:窗口不再有组件——由 improvement 事件承载,见 2.3);`StoreEvidenceSource` 的锚点查询改造为「按 improvement 事件 ts 起算」(setter 与 mock 同步调整);既有时刻表测试迁移或删除
- [ ] 1.5 `config.go` evolution 段重构:删发布道字段(protected_prompts/skip_approval/canary_hold_seconds/bundle dir);新增 `protected_paths`(**默认三目录:`resources/prompts/**`,`skills/**`,`scripts/**`——scripts 缺失则冥想脚本产物断链**)与 `judge_delay`;Validate 更新;config 测试同步

## 2. GitRefine 核心(P3:git 原生改进通道)

- [ ] 2.1 新建 `evolution/gitrefine.go`:**纯函数集(无状态)**——`IsRepo()`(启动自检 Warn)、`AddCommit(paths, note) → sha`(message `[self-improve] <note>`,仅 add 显式受控路径;**每命令 `-c user.email=tagent@local -c user.name=tagent` 防裸环境身份缺失**)、`LogFiltered(tag)`(**行首锚定 `^\[self-improve\]` 排除 Revert commit**)、`RevertSafe(sha)`(先验标记,冲突返回详情)——无组件、无状态、无生命周期
- [ ] 2.2 受控路径校验:`MatchProtectedPaths(paths, patterns)`(**路径先相对运行 cwd 归一**——绝对/相对/workspace 相对三态均先 Rel+Clean 再 glob;越界拒绝并列出清单);单测:命中/越界/通配边界/三态路径
- [ ] 2.3 `Register`:校验→AddCommit→**写 improvement 事件即开窗口**(Content={sha,ts,paths,note};仿 memory/feedback.go 直写模式:FullEvent+StoreEvent+SnowflakeEventKey,不经 governance 包——evolution↛governance 红线);返回 sha+窗口提示;**自愈**:status 检测「git log 有标记 commit 但无事件」时按 commit 时间补提示
- [ ] 2.4 git 回归(tempdir git 仓):register 产生带标记 commit 且不含工作区其他改动;非 git 仓返回明确错误;revert 拒绝非标记 commit;revert 冲突路径返回详情

## 3. refine 工具重写与装配

- [ ] 3.1 重写 `evolution/refine.go`:op=register/status/rollback(jsonschema 参数:paths/note/sha);status=git log 过滤+窗口结论 join(结论四态:健康/劣化/**样本不足**/未到期——不足不得冒充健康)+「已回滚」识别+未登记产物提醒(受控路径 mtime > 最后登记时间);删除 propose/diff
- [ ] 3.2 `tagent.go` 接线:**单一构造单元 `NewGitEvolution(store, model, cfg)`→(tool, lifecycle)**(judge/guardrail/evSrc 同单元,替代 BindPosterior 接线,同点位换接——memStore 就绪时序保持);评估 goroutine 的 {sha,ts} 由 register 闭包快照携带(零反查);evolution 启用时注册新 refine 工具(entry only,先于治理包裹——A3 不变);DigestExtra 装配处组合(consolidation 候选+evaluation 结论两来源);classifier refine 规则核对(动词更新)
- [ ] 3.3 工具面回归:三操作 happy path+越界拒绝+非 git 仓降级(mock 或 tempdir)

## 4. 评估锚点迁移与建议式信号

- [ ] 4.1 `StoreEvidenceSource` 窗口锚点改造:按 improvement 事件 ts 起算(优雅化:事件即窗口,ActivationLog 删除);W4 测试迁移:窗口从登记 commit 时刻起算(fail-before:仍从 bundle 激活时刻)
- [ ] 4.2 劣化输出重构(优雅化:信号事件化):guardrail Breach/judge 劣化 → **写 evaluation 事件**(Content={sha,verdict,reason,samples,建议文案 `refine rollback <sha>`+「先 diff」引导})——零新缝(evolution↛agent 红线天然满足),渗透走冥想 DigestExtra 既有钩子(下轮反思必现)+recall;删除状态机回滚触发路径(release.go L202-212 双回滚段);回归:窗口劣化 → evaluation 事件落库且 git 无 revert、无消息注入(fail-before:自动回滚路径存在)
- [ ] 4.3 评估触发:register 后 `judge_delay` 到期执行一次评估(goroutine 定时;**生命周期挂 TagentAgent(Stop 清理+waitgroup 收敛)**;并发窗口→judge 并发调 model(线程安全已证);中断=无结论如实降级);无窗口不评估
- [ ] 4.4 feedback 章迁移:`bundleIDFn` 来源改**最新 improvement 事件的 sha**——实现=内存缓存+原子更新(register 成功写 atomic.Value,盖章读 O(1))+重启惰性恢复(首次盖章前查一次 governance 事件,查不到不盖章;缓存=性能层,事件=真源);无事件不盖章;竞态回归(-race);`TestBindFeedback_InheritsBundleID` 等既有 join 测试保持绿(8.4 语义不变)

## 5. 冥想 prompt 与文档统一

- [ ] 5.1 改写 `resources/prompts/meditation.md`:§3.3 确认直改文件+删搁置话术;§3 末新增登记纪律(refine register,未登记=无评估保护);§4 adoption 核查改 refine status;开头补「登记与三途径正交」一句
- [ ] 5.2 README:冥想行改「自我改进引擎」定位(删「★卡片沉淀」)+refine 行改 git 原生通道+evolution 配置表重构;wiki platform 篇自进化节重写(架构图/design §1)+tool 篇 refine 工具段更新;agent 篇冥想节定位修正
- [ ] 5.3 roadmap 联动:`tagent-evolution-roadmap` D4 replay/shadow 门条目改挂 git 载体注记、D5 发布道条目标注由本变更替代;§5A 相关裁定行修订引用

## 6. 门禁与收尾

- [ ] 6.1 全量门禁:`go build ./...`+`go vet ./...`+全量 `-short`+evolution/agent/memory/rl `-race` 全绿;静态检查无 bundle/VersionedSource 残留引用(grep 零命中)
- [ ] 6.2 文档核对:README/wiki 与代码零漂移(bundle_id 键名保留、refine 三操作、配置字段)+ **README 部署边界注记**(生产=独立部署仓;开发仓跑 bot 关 evolution);LEDGER 记账(方向变更:发布道退役→git 原生,含维护者三裁决+K1-K14 坑预判);**前置:先 archive design-report-closeout**(两变更 tasks 交叉防护)
