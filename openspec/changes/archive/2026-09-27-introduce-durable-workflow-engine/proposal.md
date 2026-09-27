# 同构 tagent 的事件协作与编排热更新

> **状态与权威（2026-09-26 轮六十九同步重写）**：本 change 的推进依据＝三件套——本文件（What/Why）＋design.md（How＋全局不变量）＋tasks.md（核心思想卡／判例卡／Order-A 程序）；现状口径以 tasks 勾选状态与 evidence §0 为准（**21/34；S 阶段已收口，全 `agent -race` 整包绿为回归基线**）。**名称中的 durable 不代表新增耐久引擎**——只指输入事实链的持久协议，且按解释 A 专属外部 claim 批，派生子调用永不入信封。此前「31/31 闭合」口径作废（evidence §3 撤回台账）；生产存储／掉电／72h／真实渠道实验、索引／提交／发布／归档均不在当前授权内。

## Why

**核心一句**：本变更消灭的是**第二套机制**——一个 turn 原语、一条事件管线、一份已提交应用记录、一个任务域；entry 与被调方的一切差别都退化为「输出交给谁」。

长期常驻 agent 需要不重启调整模型、工具与子 agent 拓扑，且修改必须到达真实请求与资源。用户哲学同时明确：入口与子 agent 是同一种 tagent——各有事件总线，能像入口一样管理自己的任务；输入都走各自的事件管线，协作差异主要是输出交给谁；"无状态调用"不应成为另一套架构，后续由 session 自然表达。

当前代码已具备同构基础（`NewTagentAgent` 为每个实例创建 bus、projection 与 TaskManager，本地 task_settled 回本实例总线），真正的脱节有两处：子调用路径构造私有上下文后直调 RunFlow，绕过本 agent 的事件消费，且该上下文未接本 agent 的任务控制器；热更为提取执行配置再造一个同名完整 agent 壳。因此本变更统一事件通路、补齐本 agent 任务接线、消除重复壳，而不是把子 agent 降格为纯定义或集中任务服务。

## What Changes

- **统一事件入口**（✅ S 阶段已落地）：StartLoop/Inject/Run 复用同一接纳→批次→提交→投影→执行→输出管线；Run 只是协作边界适配（输入经自身 bus 由共享壳消费，无直调快路径）。消除 activeBus/lastSessionID/pendingExternalEvents 式隐式传参；不相容输入不混批。
- **每 agent 自有任务域**（✅ 归属与越窗闭环已落地；任务 TTL 消费边界归 6.4）：请求级执行上下文接所属 agent 的 TaskManager；B 作为被调方仍可用自己的任务层启动 C/exec，结算先回 B、再按关联向 A 输出；父委派任务与子内部任务分离，不建全局任务服务。
- **输出关联显式**（✅ S 阶段范围内已落地，session 接缝留后续）：来源、请求关联与接收者随输入携带（invocation_id 绑定表，控制字段不透传模型）；即时响应、越窗 ACK 后续通知、宿主直连三形态各有明确交付与失败边界，不改投最近父对象。
- **session 为正交接缝**：本轮只保留请求级隔离与关联接缝；"新会话/续会话"仍走同一 tagent 管线，不实现 session 存储/CRUD/TTL，不因输出对象重造 agent 类型。
- **热更去重复壳**（待 P1/2.3）：已存在 agent 换代只装配执行配置/工具；热新增仍完整构造新 tagent。reload 与 rollback 共用一次候选事务（有序获取责任表、单一短提交、失败逆序回收）。
- **执行视图与依赖保有**（待 P1/3.2）：既有 binding 内按 agent 取执行描述，Tools 仍是调用关系真源；版本持有其可调用闭包的使用权，含尚未调用的合法子 agent；被移除 agent 的内部排空面只服务自身收尾。
- **热参消费读取**（待 P1/6.4）：五热参合同不变，从所属 agent 的统一有效源在安全边界读取（push 扇出反转为 pull），删除逐实例广播与 shell 私有快照。
- **关闭最终收敛**（待 P2/4.1）：有界返回未收敛清单，真实停止后由同一尾部恰一次释放组件、lease 与登记。
- **验收**：以"同一 B 可直连宿主也可作被调方、且以自己的任务域完成多级协作"的贯穿测试（d8/d11/d12/d14 已建，常驻回归）与 RV1–RV7 回归为准；复杂度准出证明减少的是重复壳与角色旁路，而非子 agent 的完整能力。

## Capabilities

### New Capabilities

- `workflow-config-compilation`：沿用能力名保留回链；表示既有 YAML 到执行绑定的校验、准备与原子发布，不提供第二配置 DSL。

### Modified Capabilities

- `config-hot-reload`：完整有效配置、消费时热参与真实回执，字段删除回归原默认。
- `swappable-executor`：整份执行视图发布，业务懒检测不阻塞，回滚复用同一事务。
- `resident-continuity`：完整 tagent 唯一、调用树依赖保有、正常退役与状态保持。
- `persistent-event-loop`：统一入口下的 turn 取版、派生输入排队继承、回流新 turn 取当前。
- `subagent-turn-execution`：被调方经同构事件管线处理，本地任务域闭环，不另造单轮专用架构。
- `async-task-execution`：任务归所属 tagent；ACK 后实际生产者持引用，迟后停止完成最终退出。
- `task-registry-rebuild`：按所属 tagent 恢复，有／无发起者的多级重入，闭包不保旧执行能力。
- `event-sourced-projection`：请求级投影经原 context 管道隔离，事实链／被动排除／TTL 保持。
- `runtime-resource-ownership`：候选责任、版本使用权、agent 退出分清，不另建资源注册框架。
- `architecture-guardrails`：完整 tagent 同构、唯一发布／路由源、去重复状态与新增抽象约束。
- `resident-release-evidence`：真实生产入口、资源尾部、同构贯穿测试、复杂度对照与完整补丁验收。

## Impact

- 主要改造：`agent/{agent,session,event_loop,inject,tool_agent,context_manager,task_record_sink,lifecycle}.go`、`build_agent.go`、`tagent.go`、`org_hotreload.go`；7.1–7.3 已先行统一入口与任务接线；余下按 tasks Order-A 推进候选事务与执行视图。TaskManager 保持每 agent 语义，不解释编排版本。
- 保留现有 YAML、动态委派、并行语义、事件存储→投影、原接收／恢复／任务协议。语义保证锚定成功发布后的新调用；prompt 热读与五热参维持现行合同。
- 主规格迁移义务（归档时按 delta 同步）：`task-registry-and-board` 的"org 级单例"措辞改为"每个 tagent 生命周期内唯一"；`framework-flow-adapter` 的子调用私有模式、`subagent-turn-execution` 的直调要求由 MODIFIED delta 替代。
- 不新增 Graph/actor/broker 框架、集中任务服务或 session 子系统；不热迁 entry／存储身份；F1–F10 留账。当前两模块钉 `v1.11.2-tagent.1`，fork 能力保留不重发。
- 源码接口可按同构设计一次迁移仓内调用者，不保留永久双实现；公开工厂能力不静默丢弃，合同确需变化时停下裁决。
