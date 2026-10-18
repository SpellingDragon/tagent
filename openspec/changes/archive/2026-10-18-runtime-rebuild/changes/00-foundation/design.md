# 00-foundation 设计

## Context
基线、120能力映射及共享责任分别位于 ../../baseline.md、../../coverage.md、../../contracts.md。它们是设计输入，不是旧实现验收结果。

## Goals / Non-Goals
建立可复跑的规划、结构、观测检查；不实现运行时，不默认保留旧包、算法或冻结未被消费者验证的签名。

## Decisions
### D00-1 三类证据不能替代
- check_runtime_plan：65编号、无环前置、四件套、delta来源、覆盖责任、完成证据。
- check_runtime_architecture：政策模式只验人工包/边表；implementation 模式递归获取实际依赖。未登记子包、缺必需包、越界依赖和命令失败均拒收。
- check_runtime_structure：产品500行/测试1000行上限及文件结构；只报告，不机械拆包或修改 baseline。
- check_runtime_evidence：合同模式只验预期；证据模式核对原始测试事件和实际观测值。输入无证据、空跑、Skip/Fail、缺终局、重复键、错值、未知断言均拒收。
- 所有输出 productVerified=null；不把 runner 自报指纹冒称已认证来源。

备选未采纳：从残件 import 学习允许边；仅检查根目录；把正确 JSON 当真实运行；在 CI 内另写一套校验规则。

### D00-2 先消费证明后签名冻结
G1 的每个接口列生产者/消费者、所有权、取消/失败语义及最小编译探针。需要探针的 state 借用、task提交、插件相位、Query令牌、训练schema 见 ../../contracts.md §7；本轮不运行产品探针。

G2 的10组金样覆盖正常/错误/并发/重启。金样仅是期望合同；09.1 正式入口观测器必须读取实际结果。检查器自测允许复制期望构造 fixture，但不得将该 fixture 交付为产品 run。

### D00-3 白名单和质量门
本轮共享写面：父变更规划文档与 task-graph.json；golden/runtime/architecture.json、scenarios.json；scripts/runtime_checklib.py、check_runtime_plan.py、check_runtime_architecture.py、check_runtime_structure.py、check_runtime_evidence.py 及同名 test_check_runtime_*.py；.github/workflows/runtime-contracts.yml。精确文件展开见 ../../ORCHESTRATION.md。

旧 .github/workflows/ci.yml、scripts/lint.sh、race_check.sh、gen_godoc.sh 只读；00.6 后续根据根包/双 module 新路径迁移，逐条保留有效质量门，改变碎片化规则前先给正反例。新检查资产 CI 覆盖 main/dev/next，实际结构/运行验收显式启用；没有输入不冒充真实验收绿。

### D00-4 负控隔离
自测仅在 TemporaryDirectory 中构建假仓、假 go-list 和假 JSONL，断言子进程退出码。不得在共享产品树变异，不能安装依赖或修改 Git index。完整测试命令为 `python3 -m unittest discover -s scripts -p 'test_check_runtime_*.py'`。

## Risks / Trade-offs
- 能力映射不是601条语义全审；剩余条款在00.2/09.6逐项核对，不虚报完成。
- 尚无可运行产品基线；implementation 模式诚实报错，不因此放宽架构策略。
- G1可能改变允许边；必须先论证实际消费者，统一修改合同/负控再派发。
- YAML文本断言不是完整语法验证；解析使用环境现有解析器，不为本轮安装依赖。

## Migration Plan
保留新检查资产独立入口，不替换尚未迁移的产品门。未来实现开工先复核现场，完成共享设计，再冻结适用 golden 摘要；归档独立授权。
