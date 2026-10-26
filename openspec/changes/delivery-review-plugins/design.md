# 设计决策（D1–D6）

## D1 两个插件的职责边界

- **发送插件**：投递的唯一出口。`deliverFresh`（回合正文）与跨代理 send（未来）共用；只负责"把内容送到渠道"，不判断该不该送。
- **审核插件**：统一语义映射。输入=语义类别 + 内容 + 血统 + 目标；输出三态 allow/record/hold（governance 既有定义，不改）。
- 衔接：发送插件在投递前把**投递语义**交审核插件裁决；hold 时挂起（不投），批准后补投，拒绝则丢弃并留审计。

## D2 投递语义（第四类审核对象）

`Delivery{Content, Lineage, Target}`：内容正文、血统（user/notice/meditation/system）、目标地址。血统复用 trigger_source，与工具调用裁决同源。

**被否**：按"正文来源工具名"判定——send 移除后无工具名可用，语义必须在投递侧。

## D3 hold 语义

**批准后补投**（governance 契约"hold is a wait, not a denial"的延伸）：挂起条目持久化（复用 governance 审批目录），批准→补投一次、拒绝→丢弃+审计。**被否**：hold 即丢弃——审批失去意义。

## D4 entry 去 send 的边界

entry 文档不再声明 send → 模型工具面无 send；send 工具代码保留（跨代理装配位），当前无消费者不装配。校验 `declaresSendOutside` 与 sender 注入解耦（sender 仍需存在：自动投递依赖它）。

## D5 默认不拦截

审核对投递默认 allow；hold 需配置规则（如按血统或内容）显式触发。行为面变化最小：现有用户路径在无规则时与今天完全一致。

## D6 测试迁移原则

三个红测试（Wave2Errors hold/retry、Wave2Exec 收尾轮两例）**只换对象不删场景**：把"模型 send 被 hold"改为"投递被 hold（正文来源回合）"；收尾轮治理场景迁移为"收尾轮的投递同样经审核出口"。
