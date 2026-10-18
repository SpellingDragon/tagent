# 00-foundation 叶任务

本轮提前交付规划及检查工装，不等于下列完整验收完成。每项仅在产物、前置、设计/运行证据全部齐备后勾选。图与本文件统一由集成者维护。

> **两阶段**：本域 00.1–00.6 属阶段二（00.P0.3 之外的既有实施任务），阶段一 P0 骨架任务见下节；00.P0.3 是本 change 的骨架评审门，阶段二 01–09 全部 NN.M 以其为额外前置（以 `task-graph.json` 为准）。

## P0 骨架任务（阶段一：数据/接口定义，零逻辑）

> 阶段一骨架：只落数据/接口定义与 CI 合规注释文档，**零业务逻辑**；stub 仅 `return <零值>, errors.New("phase 2: not implemented")`、无返回值方法仅 `// TODO(phase2)` 空体、**禁止 panic**；每包 `skeleton_assert.go` 放 `var _ Iface = (*T)(nil)` 编译断言；导出符号英文 Godoc 完整句＋包注释；文件 ≤500 行；类型与阶段二逻辑同文件内聚（不建空壳拆分）。骨架约定冻结于 `design.md` D9。00.P0.1/00.P0.2 随本轮完成（evidence run_id=rev2phase-20261013）；00.P0.3 为评审门（未过门派发任何 NN.M 属编排违规）。

- [x] 00.P0.1 冻结骨架约定（stub 形态/编译断言/导出符号英文 Godoc/包注释/文件行数上限/类型与阶段二逻辑同文件内聚）入 `design.md` D9，并在 `ORCHESTRATION.md` 登记骨架评审门 —— 前置：无 —— 候选拟写文件（非本轮授权）：`openspec/changes/runtime-rebuild/design.md`、`openspec/changes/runtime-rebuild/ORCHESTRATION.md` —— 设计闸：G1=骨架约定条款与 00.3 共享合同口径一致；G3=规划文档白名单 —— 验证：`python3 -B -m unittest discover -s scripts -p 'test_check_runtime_*.py'`、`python3 scripts/check_runtime_plan.py --json`、`openspec validate runtime-rebuild --strict --no-interactive`（工装判据，非产品）—— evidence run_id=rev2phase-20261013

- [x] 00.P0.2 扩展规划检查器与自测：支持 NN.P0.M 编号、skeleton 相位与阶段门硬边，并补 P0 负控 —— 前置：00.P0.1 —— 候选拟写文件（非本轮授权）：`scripts/runtime_checklib.py`、`scripts/check_runtime_plan.py`、`scripts/test_check_runtime_plan.py`、`openspec/changes/runtime-rebuild/task-graph.json` —— 设计闸：G1=检查口径与 D9 骨架约定一致；G3=脚本白名单 —— 验证：`python3 -B -m unittest discover -s scripts -p 'test_check_runtime_*.py'`（全绿含新负控）、`python3 scripts/check_runtime_plan.py --json`、`openspec validate runtime-rebuild --strict --no-interactive` —— evidence run_id=rev2phase-20261013

- [x] 00.P0.3 骨架评审门：全部域 P0 done 后执行认真评审（CodeReview 子代理＋主代理亲自复核＋用户确认），产出评审记录后解锁阶段二 —— 前置：01.P0.1、01.P0.2、02.P0.1、02.P0.2、02.P0.3、02.P0.4、03.P0.1、03.P0.2、04.P0.1、04.P0.2、04.P0.3、04.P0.4、05.P0.1、05.P0.2、06.P0.1、06.P0.2、07.P0.1、07.P0.2、08.P0.1、08.P0.2、09.P0.1 —— 候选拟写文件（非本轮授权）：评审记录文件（路径待定）—— 设计闸：G1=全部域 P0 具备 build/vet/gofmt/断言绿证据且零逻辑 —— 验证：评审记录产出且用户确认；未过门派发任何 NN.M 属编排违规

## 1. 基线与设计闸

- [x] 00.1 盘点残件、用户保护面、两module和旧检查器；逐文件登记保留参考/原位重写候选/另需授权清理 —— 前置：无 —— W：`openspec/changes/runtime-rebuild/baseline.md`；R：只读Git状态与被盘点文件，不恢复删除项 —— 验证：`git status --short`、`git ls-files` 与baseline逐项核对，资源只记录存在性。（evidence: apply-00.1-20261013，主编排者亲自核对：残件处置与现场一致、保护面完好）
- [x] 00.2 将120主规格按业务能力映射到新任务、消费者与验收，重复/过时条款和待裁项具名 —— 前置：00.1 —— W：`openspec/changes/runtime-rebuild/coverage.md`；R：`openspec/specs/`及各域需求 —— 验证：`python3 scripts/check_runtime_plan.py --json`；另逐条语义核对，集合覆盖不代替601条Requirement审定。（evidence: apply-00.2-20261013，601 条逐条归属核验＋103 空洞经主编排者三类裁决清零＋9 张力处置；深度=标题级+声明级+17 条正文对位实证，边界见 §10.6）
- [x] 00.3 审定G1共享合同及tRPC复用矩阵、G2构造/执行/失败/并发/重启链，列生产消费者与最小签名探针 —— 前置：00.2 —— W：`openspec/changes/runtime-rebuild/contracts.md`、`openspec/changes/runtime-rebuild/design.md`；R：锁定框架原生接口及领域设计 —— 验证：`openspec validate runtime-rebuild --strict --no-interactive`，桌面推演及未来授权的最小编译探针逐项留证；没有探针不宣称G1通过。复用矩阵集中见 `trpc-reuse.md`；目标—KR 见 `expectations.md`。（evidence: apply-00.3-20261013，D10 五链推演留证＋15 条契约回填＋2 条待核 M1/M2；G2 留证完成，G1 通过仍待全部域 P0 done，不宣称）

## 2. 检查工装与门禁

- [x] 00.4 建立架构、场景与观测golden，使用值/消息/身份关系断言 —— 前置：00.3 —— W：`golden/runtime/architecture.json`、`golden/runtime/scenarios.json`；R：共享合同及正式消费者设计 —— 验证：`python3 scripts/check_runtime_architecture.py --check-policy --json`、`python3 scripts/check_runtime_evidence.py --check-contract --plan-graph openspec/changes/runtime-rebuild/task-graph.json --json`；不称运行取证。
- [x] 00.5 建立递归包覆盖、错误退出码、重复JSON键、Skip/空跑、错值、缺状态事件的检查器及负控 —— 前置：00.4 —— W：`scripts/runtime_checklib.py`、`scripts/check_runtime_plan.py`、`scripts/check_runtime_architecture.py`、`scripts/check_runtime_structure.py`、`scripts/check_runtime_evidence.py`、`scripts/test_check_runtime_plan.py`、`scripts/test_check_runtime_architecture.py`、`scripts/test_check_runtime_structure.py`、`scripts/test_check_runtime_evidence.py`；R：golden、临时fixture —— 验证：`python3 -m unittest discover -s scripts -p 'test_check_runtime_*.py'`；负控子进程必须非零，不能只比输出文字。
- [x] 00.6 迁移CI/lint/race/API门，覆盖next与双module，Go≥1.24.1；不提高baseline或机械拆包换绿 —— 前置：00.5 —— 本轮W：`.github/workflows/runtime-contracts.yml`、`scripts/test_check_runtime_workflow.py`；后续产品授权候选W：`.github/workflows/ci.yml`、`scripts/lint.sh`、`scripts/race_check.sh`、`scripts/gen_godoc.sh`，派发时逐文件收窄 —— 验证：`python3 -m unittest discover -s scripts -p 'test_check_runtime_workflow.py'`；另以现有YAML解析器检查语法，09.3完整门禁逐条取证；新资产CI不替代旧产品门迁移。

所有命令从仓库根运行。本轮未执行产品Go命令；G3读闭包实测和产品写权限均未开放。自测无网络预算，临时目录自动回收。


## 8. 报告响应修订轮（2026-10-14，docs/.dev/REPORT.md 响应；甄别与裁决见 ../../report-response.md）

三批串行（R2 全依赖 R1 尾 00.11、R3 全依赖 R2 尾 00.17）；批内可并行。修订性质＝阶段一延伸（签名/字段/Godoc＋级联断言与既有测试，零新逻辑）。

- [x] 00.7 [R1.1] 终局归约边界声明：model 合同面 Godoc 定稿＋persistent-event-loop delta 措辞修正 —— 前置：00.P0.3 —— W：model/terminal.go（Godoc）、02 delta；验证：go build/vet/gofmt 0＋strict
- [x] 00.8 [R1.2] 派生代借用：state 增 BindingRef/BorrowDerived/排队保有协议 —— 前置：00.P0.3 —— W：state/binding.go＋断言级联；验证：build/vet/gofmt 0＋TestStateOwnsPipeline 回归
- [x] 00.9 [R1.3] 审批身份：ApprovalID+ExpectedDigest+作用域+等待状态+幂等 —— 前置：00.P0.3 —— W：event/types.go、host_bridge.go、plugin/governance.go、task 相关＋级联；验证：build/vet/gofmt 0＋TestNativeToolAdmission 回归
- [x] 00.10 [R1.4] 通知信封：NoticeEnvelope 冻结目标+CommitRecord 自包含扩展 —— 前置：00.P0.3 —— W：task/commit.go+registry.go＋级联、state AcceptNotice 同步；验证：build/vet/gofmt 0＋TestIdentityAndCommitContract/TestTaskLifecycle 回归
- [x] 00.11 [R1.5] 工具前变换：TransformCall 窄接口与 Enricher 分界 —— 前置：00.P0.3 —— W：plugin/phases.go＋断言；验证：build/vet/gofmt 0
- [x] 00.12 [R2.1] 原文读作用域 —— 前置：R1 全部 —— W：memory/reference.go 级联；验证：build/vet/gofmt 0＋TestOriginalReferenceIsolation 回归
- [x] 00.13 [R2.2] 保留保护租约 HolderID/lease —— 前置：R1 全部 —— W：memory/guard.go 级联；验证：同上
- [x] 00.14 [R2.3] 检索内容 Text/Strategy/Cursor —— 前置：R1 全部 —— W：memory/search/port.go；验证：build/vet/gofmt 0
- [x] 00.15 [R2.4] 字节有界分段 SegmentRequest/Result —— 前置：R1 全部 —— W：memory/reference.go、workspace/externalize.go 级联；验证：build/vet/gofmt 0＋回归
- [x] 00.16 [R2.5] mailbox 事实关联+连续前缀 —— 前置：R1 全部 —— W：state/session.go 级联；验证：build/vet/gofmt 0＋TestSessionAuthority/TestMailboxConsumption 回归
- [x] 00.17 [R2.6] Query 载体（分页/水位/Update/逐义务推进者/InstanceID）—— 前置：R1 全部 —— W：query/**；验证：build/vet/gofmt 0
- [x] 00.18 [R3.1] RL 证据合同（快照引用/逐文件封账/reward 实例版本）—— 前置：R2 全部 —— W：rl/*.go 级联；验证：build/vet/gofmt 0
- [x] 00.19 [R3.2] 自演进读面 GateReport/后验四态/RefineRecord 接回 —— 前置：R2 全部 —— W：evolution/gate.go+refine.go；验证：build/vet/gofmt 0
- [x] 00.20 [R3.3] 根入口消费面 Bridge()/待投递读面/模型工厂候选 —— 前置：R2 全部 —— W：tagent.go/build.go/host_bridge.go；验证：build/vet/gofmt 0＋全套

## C 系列（概念收敛战役，见 ../../concept-slim-plan.md）
- [x] 00.21 概念预算检查器（S1-S4 机器门）+CI 步骤 —— 验证：见 task-graph 00.21（静态锚+回归锚双轨）
- [x] 00.22 run.go 拆文件族（≤400 行/文件） —— 验证：见 task-graph 00.22（静态锚+回归锚双轨）
- [x] 00.23 红线清单化（MUST/MUST NOT 全窄口/端口） —— 验证：见 task-graph 00.23（静态锚+回归锚双轨）
- [x] 00.24 范例单点全仓（每接口一 canonical 范例） —— 验证：见 task-graph 00.24（静态锚+回归锚双轨）
- [x] 00.25 event.Event 文档分组+ParsedMeta 瘦身 —— 验证：见 task-graph 00.25（静态锚+回归锚双轨）

- [x] 00.26 接口自足性补差（盲测七缺口：类型底座/装配图/负向范例/error 后果/Phase 映射/语义陷阱明文化）—— 验证：见 task-graph 00.26
