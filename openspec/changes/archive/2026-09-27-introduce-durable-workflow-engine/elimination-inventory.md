# 清理与保留盘点：整体审阅后修订

> 2026-09-25：仅修订本 change 工件，源码改造尚未执行，独立计划文件不动。已撤回的 Graph／durable 原型不恢复、不重复删除。下表将“应保留的原能力”与“应消除的重复实现”分开；不是立即执行删除的授权。当前设计与 tasks 优先于下方历史处置摘要。

## A 区：输入与持久恢复协议保留

| 位置 | 处置 | 边界 |
|---|---|---|
| `agent/reliability/inbox.go` | 留 | 接收、prepare、completion、ack、隔离与保留账目不改协议 |
| `agent/event_bus.go` | 留／定向适配 | 保持接收、claim、retention；绑定不写进持久格式 |
| `agent/event_loop.go`、`lifecycle.go` | 定向修复 | turn 取版、实际停止等待、组织 owner 收口；不另造工作流 |
| `agent/reconcile.go`、`completion.go` | 留 | 恢复资格不因换代重开；未固化结果诚实关闭 |
| 既有输入／恢复测试 | 留／补 | 原数据与身份合同不削弱 |

## B 区：TaskManager 保留状态职责

| 位置 | 处置 | 边界 |
|---|---|---|
| `agent/task/task_manager.go` | 留／必要上下文适配 | 原 Spawn、Resume、Cancel、TTL 及通知协议不替换，不解释编排版本 |
| `agent/task_record_sink.go`、`agent/tool_agent.go` | 修 | 正常 Spawn 与 WAL 重建统一新 Run 选版，闭包不永久捕获旧 wrapper |
| `tool/action` | 必要接线 | 把发起调用绑定传到重入适配，不新增任务 owner |
| 既有任务测试 | 留／补 | 真实任务 action、拒绝／dedup、存活送输入、已结束新 Run 均验收 |

## C 区：模型／流包装仅纳入必要停止边界

F1–F10 仍为后续台账。`agent/execution_gate_model.go`、`rl/swappable_model.go`、`rl/trajectory_recorder.go` 的流适配仅在直接影响实际停止与资源引用时定向修复，不借机全面重构；同步工具原兜底预算与来源 metadata 合同保持。

## D 区：已撤回的内部耐久试验

| 位置 | 当前状态 | 后续处置 |
|---|---|---|
| `workflow/runtime.go`、`saver.go`、`facts/` | 已删（历史） | 不恢复 |
| `agent/input_workflow.go`、`input_recovery.go`、`inbox_import.go` 及专用测试 | 已删（历史） | 不恢复灰度双路径 |
| `event/wf_facts.go`、`wf_facts_test.go` | 工作树必要新文件 | 保留被动排除及 6.2 已撤销新增 TTL 的成果；不能整文件删除 |
| 旧 change 目录与 F 项证据 | 历史保留 | 本轮不归档、不删除、不作为现行准出证明 |

## E 区：已撤回的独立编排原型

| 原位置 | 当前状态 | 可复用成果 |
|---|---|---|
| `org_exec.go` 与测试 | 已撤回 | 本地引用／环校验保留于现有入口 |
| `org_definition.go` 与测试 | 已撤回 | 不再将可调用关系改为必经流程 |
| `workflow/definition/`、`workflow/compile_test.go`、`workflow/doc.go` | 已撤回 | 有效字段与指纹审计保留 |
| `org_generation.go`、`org_version.go` 旧三层版本账本 | 已收敛 | 唯一协调器在 `org_hotreload.go`，不重新新增平行账本 |
| `arch_layers_test.go` | 已有守卫 | 继续约束无第二编排表示、无内部 durable 反向依赖 |

## F 区：审阅后定向修复与交付卫生

| 对象 | 处置 | 禁止事项 |
|---|---|---|
| 递归构建及临时候选 | 以候选级获取日志和显式责任交接收敛 | 不靠 map 倒序，不先暴露半成品，不关闭借用 store |
| 已移除 owner／多代运行实例 | 真实引用和输入义务收敛后正常退役 | 不永久保留全部历史实例，不拿回滚配置当存活引用 |
| `ExecutorConfig` 访问器 | 配置／声明隔离、内部句柄受限 | 不以写入拷贝加读取只读约定冒充隔离 |
| parentProjection 接线 | 复用透明工具解包，恢复既有 fallback | 不原地重绑已发布共享 wrapper |
| `elimination_latest_path_test.go` | 关闭验收豁免，保留需要的失败诊断 | 不吞纯上游 race 或新版本失败 |
| 测试 store 路径（含 `hottest-drop-main`） | 使用测试生命周期临时根，失败不回落工作树 | 不清理未知目录或生产路径 |
| HEAD 已跟踪的 `hottest-sub1/sub2` lock／journal | 先列交付问题，索引处置单独授权 | 不删除本地文件、不擅自提交或取消跟踪 |
| 未跟踪必要源码／测试 | 显式加入完整补丁清单并验证 | 不把 HEAD-only 结果当作完整补丁验收 |

## G 区：自然演进的实现收敛清单（待实施）

| 现有实现 | 保留能力／收敛方式 | 明确撤掉的重复机制 | 任务 |
|---|---|---|---|
| config-driven 完整 executor shell | 拆开“新 agent 完整准备”与“已存在 agent 的执行配置装配”；热新增真实 agent 仍完整构造 | 为取配置对**已存在** agent 新造的同名完整 TagentAgent/TaskManager/cleaner，及仅为壳存在的跳过／强制重挂分支；不删真实子 agent 的 bus/任务域 | 2.3、3.2 |
| 子调用直调 RunFlow 旁路 | Run 作为边界适配复用统一事件入口与处理原语（7.1） | 绕过本 agent bus／任务接纳的专用单轮执行链与 isSubAgent 分支 | 7.1 |
| 调用上下文未接本 agent 任务域 | 请求级上下文接所属 agent 服务与 taskController（7.2） | 私有 CM 无任务控制器导致子层委派静默同步化 | 7.2 |
| activeBus/lastSessionID/pending 隐式传参 | 输入关联随消息显式传递（7.3） | 调用方改写被调方共享可变字段作为通信手段 | 7.1、7.3 |
| reload 与 rollback 两套构建／清理 | 同一 prepare/commit/discard，输入配置来源不同 | 空 rebuilt 缓存、提前 Add、事后 owner 差集清理 | 2.3、2.4 |
| 单 owner 的 execBinding.face | 原绑定内增加逐 owner 执行描述，Tools 仍判调用资格 | 祖先直接工具表代替子 owner 面、第二路由表或可执行代理层 | 3.2、4.2 |
| 每实例 hotSnapshot＋私有 CM 广播 | owner 稳定读取同一已提交快照，消费边界取完整组 | 全实例 setter 扇出、shell 私有权威快照、仅为热参同步存在的注册列表 | 6.4、5.1 |
| 初次 Close 返回即终结全部清理责任 | 同一关闭尾部迟后收敛，至多一个 owner 等待者 | 超时后永久丢掉 store 退出责任；新周期扫描器／持久清理队列 | 4.1、4.3 |
| PID＋固定测试目录 | testing.TB 临时根，测试内显式共享重启路径 | 跨测试隐式共享及事后列表证明卫生的推断 | 6.7 |

有状态 ActionTool 的恢复／monitor 不能随工具声明草率丢弃：候选准备与在线激活分开，实际任务保有其必要资源。旧 ToolAgentFactory 保留注册合同，能力无法安全分离时先裁决，不以去壳为由直接删除工厂路径。同构迁移不删除真实子 agent 的 bus 或任务域；删除对象限于为提取配置而重复构造的壳与角色专用旁路。已有效的隔离、投影、逐代引用和停止能力测试保留；有缺口的断言显式迁移，不能批量移除。

## 守卫

- 既有 agent→plugin→memory 和 event 叶子边界保持；TaskManager 不读根编排状态，绑定不序列化。
- 修复回归必须覆盖真实声明、实际子目标、宿主结果和资源停止／回收，符号消失不代表正确。
- 历史 baseline 与当前候选补丁分别记录；必要未跟踪文件是补丁的一部分，不是自动删除对象。
- 本轮只改本 change 工件，独立计划文件不动；不改已提交可靠性基线、源码、测试、依赖、用户数据、Git 索引或远端。
