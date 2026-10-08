# O2 设计

## 依据
agent/tool_agent.go 的 RegisterModelReference/LookupModelReference 与调用覆盖是既有能力；wiring.go 实际模型解析、agent/exec_lease.go 的 runCfg、org提交记录及 per-call-subagent-overrides 为复用面。

## 决策
1. 独立 Register/Lookup 导出API保留。配置驱动实例在候选构造时，从现有注册表和实际agent模型构造只读引用快照；配置模型用保留名 `agent:<name>`，自定义注册冲突具名拒绝。候选失败不更新进程全局注册表。
2. 引用只提供模型选择，不提供agent调用路由；合法工具仍由owner Tools上界决定。默认无override行为不变。
3. 一次覆盖在已有子执行视图上解析引用一次，校验和装配使用同一个model实例；不得先查旧、后查新。在途保留发起代，新独立调用读取后续有效代。旧独立使用场景在每次调用入口快照一次，仍接受后续Register替换。
4. 重入保持既有“发起者代/当前有效代”规则及Declarative覆盖语义，不为复活旧模型持久化指针或新增出生代模型快照。缺目标/引用明确拒绝，不能fallback到父模型。模型注册的可重建前提写进契约，跨重启失配不冒称同权重。
5. 统一模型解析缓存键：区分agent/global用途、provider别名、实际protocol、model、endpoint及凭据来源配置名；不保存凭据原文。候选缓存独立，避免显式agent模型沿用旧endpoint。
6. 现有热更协调器比较完整有效配置并分类FP/SRC/FILE/RESTART；对尚无消费接线的governance/reliability/采集资源字段返回restart_required及字段差异，混合修改整批不提交，不推进成功revision。不可热迁不算能力缺失，但静默applied不允许。
7. 允许从tool_agent抽取纯调用覆盖帮助函数，使验证/装配共用解析结果；禁止添加另一条调度入口。所有root/config/org回执接线由编排者写。

## 写面及兼容
独占agent/tool_agent.go、agent/exec_lease.go、agent/org及对应测试；root/wiring/build_agent/config/agent状态载体为集成者单写。成功结构回执字段保留，拒绝差异追加；不更改数值现读、FILE懒读、resource Close归属。默认TTL和权限不变。回滚仍是完整旧配置发布新代。

## 真实模型验收
在隔离测试中让真实模型选具名覆盖引用，调用无副作用nonce工具；用屏障钉住在途调用、提交新配置，再核对请求实际model/工具声明/结果所属invocation。第二模型仅在显式提供时宣称覆盖，不把提示文本变化当换权重证据。另一用例混合修改可热prompt和不可热资源字段，证明旧面继续服务、拒绝被宿主看见。缺凭据/Skip不能过必需门。

文档：org-hot-reload、execution-generations、platform-subsystems、工具调用覆盖小节及README配置边界。不得以机械改名引用代替真实请求证据。
