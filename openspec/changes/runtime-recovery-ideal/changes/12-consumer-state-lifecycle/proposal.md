# 12 消费态生命周期（consumer-state-lifecycle）

> 状态：本轮规划重整**新增域**，四件套为首版。**只规划、不实施**；本文出现的类型名、字段名、切片载荷、接口签名一律是**候选、未冻结**，不得作为已批准实现签名引用。
>
> 域边界一句话：05 管任务账、07 管治理账、03 管压缩账（他域负责），本域只负责**跨域的两条横切契约**——「已提交」与「已消费」的边界，以及消费者生命周期（挂载/禁用/卸载/替换）中的状态与义务交接。

## Why

一级 [design.md](../../design.md) D1 P2（单一 g(x)）/P3（全部托管）/P4（有界恢复）与 D10 行「单一状态消费、不丢义务」在本域范围内未兑现。D8 的 E6 已登记「状态应用失败只记错误 / StateRegistry 卸载与 pending 恢复」，但未给出行为证据与边界定义；本轮读码补齐（✅ 实测，行号只作定位不作契约）：

1. **应用失败仍推进切面**：`reduceCommitted`（assembly/facts.go:142-155）把消费者折叠失败交给 `assemblyError` 记录后返回，`commitFactNoCadence`（facts.go:86-130，:124 调用）不因此失败；切面水位取自分区落地序号（assembly/checkpoint.go:297-306），`checkpointLocked` 只在 `flight.await`（checkpoint.go:355，等待在途提交单元结束，不等待其成功）之后取 `SnapshotState` 并把该水位写入 `r.checkpointOffset`（:373）。故一条「已提交、未应用」的事实会被下一个切面覆盖，而恢复只重放**水位之后**的事实（`eachFactPastCut` assembly/checkpoint.go:1124-1157、`pastCut` :1158-1166），该消费者此后永远落后于链，且无具名失败。
2. **消费失败被静默吞掉**：结算结果已经落链（`Advance` 返回 `CommitCommitted`）之后，`Wrapper.execute` 对信箱写入 `if err := w.mailbox(ctx, notice); err != nil { return }`（runtime/task/wrapper.go:312-314）直接返回，既不记录也不上抛；同一函数对 `Advance` 失败同样是裸 `return`（:309-311）。这就是「静默吞消费错误仍推进」的现存路径。
3. **生命周期无义务交接**：`StateRegistry.Register` 的 disposer（runtime/state/checkpoint.go:171-188）只删 `holders[name]` 与 `names` 条目，被删切片已陈述的状态与未完义务就地消失；`PendingRestore`（:402-415）只在 boot 期由 host 具名拒绝（assembly/host.go:777-779），运行期注销无对应检查。代际注册表 `GenRegistry`（runtime/state/genregistry.go:121-144）只有 `Register`/`Build`，替换代际时 `GenResources.Restore` 仅在候选未生效时回滚（:71-83），**成功切换的新实例不继承旧实例的已陈述状态**。
4. **切片注册早于插件挂载**：三个 run 级切片在装配期无条件注册（assembly/run.go:576-580），插件实例在之后挂载（:596-607）。`plugins: -<name>` 禁用某个 builtin 时，其切片仍然 `SnapshotState`/`RestoreState`，而消费方不存在；`compressSlice`（assembly/compress_checkpoint.go:73-80）对 `r.compress` 无 nil 保护（⚠️ 读码推断，**未实跑**，结论只写「无交接声明」，不写「必 panic」）。
5. **本域不重定义他域账**：tasks 切片与 mailbox 切片只实现三方法，无 `SnapshotSessionState`（assembly/task_checkpoint.go:32-50、runtime/state/mailbox_checkpoint.go 无会话面），故**任务账是 registry 全局账、不进会话切面**；压缩账的 `judgments/answered` 不清减属 03 域；governance 账的有效态属 07 域。本域不为它们各立一份清减判据。

用户原话锚（不得被工程化目标替换）：用户要的是「恢复后能接着做、不重不漏、不假装完成」。本域的症状直译锚草案见 design §五。

## What

承一级 D8 的 G2 与 D9 的「12.R2 依 G1/G2、12.R3 依 12.R2」：

- **12.R1 设计门（与 05.R1、07.R1 共用一张 G2 判据表）**：分别证明各族状态的处理水位、未完集合、有效历史状态与按身份查询入口；补齐「已提交未消费」与「插件生命周期」两类证据，并把缺口固化成**红着的**验收锚。只允许调查、只读取证与红锚原型，未通过不得进入 R2/R3 的实现。
- **12.R2 已提交但未消费的处理边界与同一状态应用（依 11.R2 + G2）**：定义并实现「一条事实被切面覆盖前必须已被其消费者应用」的边界（拒绝切面 / 保留该切片的应用水位 / 重放补齐，三选一由 R1 裁定，**均未冻结**）；把静默吞掉的消费失败改为具名上报并保留义务；运行期与恢复期继续走同一条 `Reduce`。不承诺无接收方支持的 exactly-once，不把多个外部动作塞进一个事务。
- **12.R3 动态挂载/禁用/卸载/替换的义务交接（依 12.R2）**：状态切片与消费实例的**生命周对齐**——禁用/卸载/替换时，未完义务与已陈述状态具名交接或具名拒绝，不得静默消失；复用**既有**注册声明（builtin 目录 + `plugins:` 声明 + `Stateful` 注册），**不新增第四挂点、不新建第二注册体系**。

**不做的（显式登记）**：不在本域清减压缩账（03 负责）；不改 tasks/governance 切片的字段语义（05/07 负责）；不承诺「每种状态 O(1)」，也不以删除 `answered`/已结身份来伪造有界；不因为跨代际重启而额外承诺 exactly-once。

## Capabilities

四能力**全部复用既有主规格**，不新建同义能力（先查重叠后的准确 Requirement 标题）：

- `session-checkpoint-material`：
  - 「派生态有界且不删除已提交事实」——本域追加「水位不得覆盖未应用事实」「清减不得删除判重所需的已结身份」「当前态按有效工作计费」三条。
  - 「`Stateful` 收编消费者读态且不新增挂点」——本域追加「注销/替换不得静默丢义务」「复用既有声明面」两条。
- `consumer-loop-convergence`：「重入输入恰一次」——消费失败保留义务、由具名错误上报，重启后仍恰一次。
- `runtime-resource-ownership`：「递归获取责任与关闭责任唯一」——持有可恢复态的消费者卸载/替换时责任按名转移。
- `config-hot-reload`：「执行代绑定完整性」——结构热更换入/移除消费者时新代绑定同一状态权威。

## 边界与依赖

- **依赖**：G2 判据表（05.R1/07.R1 共建，本域只消费）；11.R2 的事实提交结果（「已提交」的权威定义与新增/已存在/冲突语义）；`Stateful` 接缝（runtime/state/checkpoint.go:36-45，签名冻结，一级 §五）；切面水位与重放（assembly/checkpoint.go:297-306/:341-373/:1124-1157）；结算与信箱（runtime/task/wrapper.go:301-316、runtime/state/{mailbox,memory}.go）；插件声明面（`assembly/plugin_catalog.go`、`plugins:` 解析、`state.GenRegistry`）。
- **被依赖方**：05.R2/07.R2 的清减实现以本域「水位不覆盖未应用事实」为前置；其后所有清单减消费方（含 03 压缩账清减）继承同一条边界；F3 级联回滚证据需要本域的负锚（静默吞错 → 红）。
- **接口面（候选、未冻结）**：`Stateful` 三方法**不变**；可能的改动只有两处候选——切片应用水位（候选名 `AppliedThrough`）与注销/替换时的交接声明（候选名 `HandsOver`），二者都必须在 R1 与编排者冻结后才可实施；不给每个消费者新增挂点或端口。
- **禁止事项**：
  - **SHALL NOT 删除/覆盖/改写已提交事实**（`session-checkpoint-material` 的 Requirement「派生态有界且不删除已提交事实」）；有界化只改「读什么、切面覆盖到哪」，不改链。
  - 不造第四挂点、不建第二注册体系；builtin 与扩展走**同一**注册声明。
  - 不承诺无接收方支持的 exactly-once；不把多外部动作强行合成一个事务。
  - 不触外来未跟踪文件（`runtime-architecture-convergence/`、`scripts/gates/check_event_semantics_symptoms.py`、`runtime_budgets.py`、`test_check_runtime_symptoms.py`、`skills/interview/agent-interview-helper/`）；不触 `config-surface-normalization` 未提交变更。
  - 不改写冻结归档（`openspec/changes/archive/**` 零 diff，只附勘误）；不改主规格（`openspec/specs/**`）、不改 `scripts/gates/**` 与 `golden/**`。
  - 本文档阶段**只规划不实施**；不提交、不推送、不归档。
