# Design

## D1 拆包方向由既有立法锁定：接口注入，不是反向依赖

`architecture-guardrails`「分层依赖方向可机械断言」固化了 `agent 及其子包 MUST NOT import 根包`。世代族移入 `agent/org` 后需要的恰是根包的 `runtimeConfig`（候选事务持有 `rc *runtimeConfig`，7 处）与 `buildAgent`（1 处）。因此：

- `agent/org` 定义自己的窄契约：`ShellRuntime`（候选事务真正读写的字段子集快照）与 `ShellBuilder`（重建执行壳的回调签名）；
- 根包在装配时构造并**注入**，方向保持 root → agent；
- 这正是「唯一编排发布权与中性契约」条文的精神在物理布局上的延续：机制在 agent 域，构造权在组合根。

被否方案：org 包 import 根包取 runtimeConfig（直接违反机械断言，测试即红）；或把 buildAgent 一并挪进 agent/org（装配权下放，违反组合根独占发布的既有裁决）。

## D2 公共 API 面不变：别名 + 薄委托

盘点：`Org*` 导出类型（OrgStatus/OrgFailure/OrgLiveDebt/OrgCloseState/OrgAgentApply）在 tests/examples/tool/scripts **零引用**；`CheckOrgReload`/`Rollback` 等是 `TagentAgent` 方法，挂根包类型上天然不动。移动后根包保留 `type OrgStatus = org.Status` 形式的**类型别名**与方法薄委托——预发布期本可不兼容，但别名成本为零且免去下游（若有隐性引用）迁移。

## D3 世代集成测试留根包，纯逻辑单测随包走

三个巨型测试文件的主体是装配层集成测试：直接使用 `New/LoadConfig/StartLoop/InjectMessageContext/residentCacheForTest` 等根包未导出面，移出根包即失去被测通路。故处置是**按域拆文件**（org-candidate / org-hotreload / cross-generation / owner-retirement / partition-collision 各自成档，≤600 行/文件），留根包；candidateTxn 的纯事务逻辑、partition 碰撞的纯分配逻辑若有独立单测价值则随 `agent/org`。同位门的分组键含目录与锚点，拆分后逐一核对锚指向与生产镜像归属。

## D4 卫生门的形状（防的是"下一次"，不是这一次）

validators job 新增检查：① `git ls-files` 全集必须非空文件；② 追踪路径不得命中运行期产物模式（`*.lock`、`*.journal`、`*.tmp`、`*.prof`、`.tagent-writer.lock`）；③ 顶层目录白名单外的**新增目录**须在 README 布局表登记（白名单初始 = 现有 tracked 顶层集合，防漂移）。带负路径探针：临时制造空文件与 `x.lock` 入 index 必须红，撤除恢复绿，留痕。

## D5 阶段顺序与每步可回滚

P1 卫生（无行为）→ P2 train/ 挪移（纯路径）→ P3 拆包（接口化，最大的一步）→ P4 测试拆分（文本重组）。每阶段独立提交、独立全绿（`go build ./...`、根包与 agent 包 plain+race、`GOMAXPROCS=1` 根包、lint、`gen_godoc --check` 重生成后匹配）。P3 若发现 runtimeConfig 的字段子集难以快照化（候选事务写回运行态），停下上报而非放宽分层断言——那是设计层冲突，按规矩裁决。
