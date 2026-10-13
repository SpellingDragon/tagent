# 00-foundation 叶任务

本轮提前交付规划及检查工装，不等于下列完整验收完成。每项仅在产物、前置、设计/运行证据全部齐备后勾选。图与本文件统一由集成者维护。

## 1. 基线与设计闸

- [ ] 00.1 盘点残件、用户保护面、两module和旧检查器；逐文件登记保留参考/原位重写候选/另需授权清理 —— 前置：无 —— W：`openspec/changes/runtime-rebuild/baseline.md`；R：只读Git状态与被盘点文件，不恢复删除项 —— 验证：`git status --short`、`git ls-files` 与baseline逐项核对，资源只记录存在性。
- [ ] 00.2 将120主规格按业务能力映射到新任务、消费者与验收，重复/过时条款和待裁项具名 —— 前置：00.1 —— W：`openspec/changes/runtime-rebuild/coverage.md`；R：`openspec/specs/`及各域需求 —— 验证：`python3 scripts/check_runtime_plan.py --json`；另逐条语义核对，集合覆盖不代替601条Requirement审定。
- [ ] 00.3 审定G1共享合同及tRPC复用矩阵、G2构造/执行/失败/并发/重启链，列生产消费者与最小签名探针 —— 前置：00.2 —— W：`openspec/changes/runtime-rebuild/contracts.md`、`openspec/changes/runtime-rebuild/design.md`；R：锁定框架原生接口及领域设计 —— 验证：`openspec validate runtime-rebuild --strict --no-interactive`，桌面推演及未来授权的最小编译探针逐项留证；没有探针不宣称G1通过。复用矩阵集中见 `trpc-reuse.md`；目标—KR 见 `expectations.md`。

## 2. 检查工装与门禁

- [ ] 00.4 建立架构、场景与观测golden，使用值/消息/身份关系断言 —— 前置：00.3 —— W：`golden/runtime/architecture.json`、`golden/runtime/scenarios.json`；R：共享合同及正式消费者设计 —— 验证：`python3 scripts/check_runtime_architecture.py --check-policy --json`、`python3 scripts/check_runtime_evidence.py --check-contract --plan-graph openspec/changes/runtime-rebuild/task-graph.json --json`；不称运行取证。
- [ ] 00.5 建立递归包覆盖、错误退出码、重复JSON键、Skip/空跑、错值、缺状态事件的检查器及负控 —— 前置：00.4 —— W：`scripts/runtime_checklib.py`、`scripts/check_runtime_plan.py`、`scripts/check_runtime_architecture.py`、`scripts/check_runtime_structure.py`、`scripts/check_runtime_evidence.py`、`scripts/test_check_runtime_plan.py`、`scripts/test_check_runtime_architecture.py`、`scripts/test_check_runtime_structure.py`、`scripts/test_check_runtime_evidence.py`；R：golden、临时fixture —— 验证：`python3 -m unittest discover -s scripts -p 'test_check_runtime_*.py'`；负控子进程必须非零，不能只比输出文字。
- [ ] 00.6 迁移CI/lint/race/API门，覆盖next与双module，Go≥1.24.1；不提高baseline或机械拆包换绿 —— 前置：00.5 —— 本轮W：`.github/workflows/runtime-contracts.yml`、`scripts/test_check_runtime_workflow.py`；后续产品授权候选W：`.github/workflows/ci.yml`、`scripts/lint.sh`、`scripts/race_check.sh`、`scripts/gen_godoc.sh`，派发时逐文件收窄 —— 验证：`python3 -m unittest discover -s scripts -p 'test_check_runtime_workflow.py'`；另以现有YAML解析器检查语法，09.3完整门禁逐条取证；新资产CI不替代旧产品门迁移。

所有命令从仓库根运行。本轮未执行产品Go命令；G3读闭包实测和产品写权限均未开放。自测无网络预算，临时目录自动回收。
