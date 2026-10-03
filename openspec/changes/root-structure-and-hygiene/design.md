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

## D6 实现期证伪：train/ 不是"误导性目录"，是一条陈旧训练桥（用户裁决：最彻底路线）

原 P2 前提（"纯路径移动 + 引用同步"）被三层证据推翻：① `train_tagent.py:34` 的 `sys.path` 把点号模块名当目录段（`/ "train.rl"`），指向不存在的路径；② 该脚本与 `train_rl_config.yaml` 的 `from train.rl import PPOTrainer`、`python3 -m train.rl.infra.rpc.rpc_server`、`workflow: train.rl.tagent_adapter.*` 全部指向 AReaL **改名前**的顶层包 `train`（现行为 `areal/`，`PPOTrainer` 在 `areal/trainer/rl_trainer.py`）；③ `run.sh` areal 命令族的默认配置 `areal_config.yaml` 不存在，CI 零 Python 覆盖。

**裁决（退役而非修补）**：三者无任何 openspec 规格承诺（`trajectory-recording` 只点名离线转换器；`rl-feedback`/`example-rl-visibility` 只承诺 Go 侧 HTTP 面），修补需要 AReaL 多卡环境才能验证，属"看着对"的提交。退役边界：

- 删除：`train/rl/tagent_adapter.py`、`examples/wechat-bot/train_tagent.py`、`examples/wechat-bot/train_rl_config.yaml`、`run.sh` 的 areal 命令族与 AREAL_* 变量、README 相关行；
- 保留并迁移：`convert_trajectories.py`（纯 stdlib、离线、被 `trajectory-recording` 点名）→ `scripts/convert_trajectories.py`，wiki 同步；
- 保留：`rl/` Go 包（轨迹记录、可换模型、HTTP API）、`tagent.rl.yaml` 运行时配置、bot README 的 RL 运行时环境变量表——它们是活面且各有规格；
- wiki `rl-architecture` 记录退役事实与重接条件（按现行 `areal.*` 布局重写，经 dotted-refs 门）。

## D7 dotted-refs 门的最小形状

只查**机器会真去 import 的位置**：shell 中 `python -m`/`python3 -m` 的后续 token、yaml 中 `workflow:` 的值。解析规则：点号换斜杠后在仓库内存在对应 `.py` 即通过；否则必须命中显式外部允许表（初值为空）。不做全文件点号 token 扫描——`scheduler.type=local` 这类配置覆盖语法会被误伤，门的强度来自位置精确而非范围贪大。

## D8 P3 前置盘点结论（3.1 落档）：三个依赖点，全部可切

对五文件与根包的耦合逐点核实（非猜测，逐行）：

1. **store-owner 注册表自包含**：`storeOwners/storeOwnersMu` 字段只在 `partition_collision.go` 内部出现，三个方法（register/unRegister/ownedAgentNames）的全部外呼只有 `build_agent.go` 两处（构建期注册、`SetStoreOwnerRevoker` 闭包）与 `org_candidate_txn.go` 一处——可作为独立类型整体移入 `agent/org`，根包持引用并调用（方向 root→agent 合规）。
2. **resident 缓存已是封装类型**：`rc.resident` 的使用全部经其自身 API（Snapshot/Get/Add/Unpublish），overlay 不碰内部；`residentMemFP` 的唯一写者就是 overlay 自己，属组织族自管状态，随族迁移。
3. **buildAgent 可闭包化**：`org_candidate_overlay.go:66` 把 rc 整个传给 buildAgent——改为根包构造 `ShellBuilder` 闭包（捕获 rc+loader），`agent/org` 只见回调签名。

据此 `agent/org` 的注入契约定格为三件：`ShellBuilder`（重建执行壳）、resident 缓存句柄、store-owner 注册表。`runtimeConfig` 本体不动、不留反向 import；D5 的熔断条件（写回运行态无法快照化）**未触发**——`residentMemFP` 是可随族迁移的族内状态，不是装配运行态。

