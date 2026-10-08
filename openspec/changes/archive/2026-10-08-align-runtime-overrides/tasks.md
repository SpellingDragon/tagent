# O2 叶任务（均未执行）

- [x] 2.1 添加已注册引用/配置模型/重复引用/未注册引用的基线契约测试 —— 验证：`go test ./agent -short -run '^TestModelOverride_ReferenceResolution$' -count=1` → EXIT=0，4 subtest 全 PASS（`TestModelOverride_ReferenceResolution/{registered reference,reserved name,custom registration occupying the reserved name,unknown reference}`）。修复前红测：`/tmp/tagent-w1/o2a/2.1-red.log`（EXIT=1，reserved name 与 conflict 两 subtest 断言失败）。
- [x] 2.2 实现已选执行面内单次引用解析，校验与实际调用共用结果 —— 验证：`go test ./agent -short -run '^TestModelOverride_GenerationBinding$' -count=1` → EXIT=0，2 subtest PASS。旧「两次查可变全局表」形态下同一套测试 EXIT=1（in-flight `Condition never satisfied`；两阶段 `Not same: expected first / actual later`），见 `/tmp/tagent-w1/o2a/red-demo.log`。跨框架边界的装配点接线（session.go 传入 `ExecLease.ModelReferences()`）留给 2.6。
- [x] 2.3 完善默认/覆盖/重入/回滚矩阵，保持Tools上界和跨调用不泄漏 —— 验证：`go test ./agent -short -run '^TestModelOverride_ReentryIsolation$' -count=1` → EXIT=0，4 subtest PASS（无覆盖不变、引用不授予路由、覆盖不跨调用泄漏且解析不写回全局表、已钉代快照不含的引用具名拒绝且 `OutstandingRefs()==0`）。全包回归 `go test ./agent ./agent/org -count=1` → EXIT=0。
- [x] 2.4 由编排者统一实际模型解析缓存键与候选隔离，不存凭据内容 —— 验证：`go test . -short -run '^TestModelResolution_EndpointIdentity$' -count=1`。
- [x] 2.5 实现完整配置差异分类、restart_required拒绝及desired/effective回执 —— 验证：`go test . -short -run '^TestOrgReload_UnsupportedConfig$' -count=1`。
- [x] 2.6 编排者单写装配引用快照/状态，运行发布-在途-回滚-常驻身份整体回归 —— 验证：`go test -short -race . ./agent/... -count=1`；新增TestOrgReload_ModelReferenceContinuity必须匹配运行。
- [x] 2.7 增加并本地运行真实模型覆盖/热更拒绝用例；入口只能既有tagent.New/Run —— 验证：`TAGENT_REQUIRE_REAL_MODEL=1 go test ./tests -run '^TestRealModel_RuntimeOverrides$' -count=1 -json`；按D17非SKIP验收。
- [x] 2.8 交编排者同步热更维度矩阵、模型注册调用契约与API生成物，附单一解析/发布权证据 —— 验证：`bash scripts/lint.sh && bash scripts/check-openspec.sh`。

> 编排者核销：2.4=modelCacheKey 三路统一（C1b，TestModelResolution_EndpointIdentity 变异红→绿）；2.5=restartRequiredChanges+restartOnlyConfigBlocks（含编排者追加 trajectory_dump/dir 两项，TestOrgReload_RestartOnly* 变异红→绿 C3）；2.6=stageOrgGenerations 钉定+session 变参接缝（TestOrgReload_ModelReferenceContinuity）；2.8=D③行7/8+lint 全链绿。2.7 真模型待本 runbook 执行。

> 编排者核销（W4）：acceptance5 单轮四场景全 pass（go-test.json 4 entry、无 Skip）；第二颗调用断言按机制契约修正（不要求模型必发 override，禁复用旧钉定代由 NotContains 钉住）。
