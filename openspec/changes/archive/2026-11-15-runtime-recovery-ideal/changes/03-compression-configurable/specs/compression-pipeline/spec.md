## MODIFIED Requirements

### Requirement: 压缩的判定与执行各成事实

压缩 SHALL 分三段：producer 判定并提交 `compress.request`；consumer 读该 request 执行折叠并提交 `compress.product`；投影 SHALL 从 `compress.product` 恢复产物条目。

老带摘要正文（`history_summary` 的正文）SHALL 由模型产出、由 `compress.product` 定案，其字节预算 SHALL 由声明面 `summary_budget_bytes` 配置，SHALL NOT 以任何机制常量作为承载界。未声明预算时 SHALL 读声明缺省 `DefaultSummaryBudgetBytes`（2048）；声明正预算时 SHALL 原样采用，SHALL NOT 静默抬到缺省。机械有界型（折叠自己铸的正文）与模型 gist 两侧 SHALL 服从同一预算：界只有一个来源（声明 → `FoldWindow.SummaryBudgetBytes` → `AgeFoldPlan.SummaryBudgetBytes`），判定侧与执行侧 SHALL 取同一预算，SHALL NOT 在同一材料上产出两个不同正文。摘要提示词 SHALL 按该预算陈述字节上界，SHALL NOT 携带固定字面界。

摘要请求 SHALL 经会话 agent 的模型租约发出：agent 挂了租约时 SHALL 用租约的稳定句柄（按请求起手借用版本），SHALL NOT 绕开租约直取宿主贡献的模型；请求 SHALL 携带该 agent 声明的采样参数（温度、思考开关、推理力度、输出上限），只填请求自身未命名的项。无租约的装配 SHALL 回落宿主贡献的模型并同样携带声明采样。

三条降级 SHALL 保持且不阻塞折叠：会话 agent 无模型（`ErrNoSummaryModel`）、请求失败或终态非 completed、回答为空时，SHALL 保留机械有界型的正文，SHALL 记一行可检索告警，SHALL NOT 提交空正文或伪造正文。模型访问 SHALL 只在 assembly 一侧发生；`memory/replay` SHALL 保持纯读，SHALL NOT 读声明层。

#### Scenario: 一次判定一次产物
- **WHEN** 判定产出一条 `compress.request`
- **THEN** 消费方 SHALL 至多提交一条同 request 的 `compress.product`，其键为产物负键、覆盖区间按原序

#### Scenario: 未接消费方时行为等价
- **WHEN** 宿主只接判定、未接消费
- **THEN** `compress.request` 照旧提交，产物不生成，投影呈现等价于无该机制时的现算结果

#### Scenario: 恢复不重折
- **WHEN** 重启恢复读到 `compress.product`
- **THEN** 投影 SHALL 以该事实的键与正文重建合成条目，MUST NOT 重新折叠；只有读不到 product 的 request 才视为未执行的判定

#### Scenario: 摘要预算按声明生效
- **WHEN** 声明 `summary_budget_bytes: 512`，折叠把老带材料收成一条 `history_summary` 且会话 agent 有模型
- **THEN** 落链 `compress.product` 的正文长度 SHALL 至多为 512 字节
- **AND** 未声明该键时正文长度 SHALL 至多为缺省 2048 字节

#### Scenario: 界无硬编码来源
- **WHEN** 声明任意正预算，或无声明而读缺省
- **THEN** 正文界 SHALL 只来自该声明（`FoldWindow.SummaryBudgetBytes` → `AgeFoldPlan.SummaryBudgetBytes`），代码与提示词 SHALL NOT 出现固定字面界
- **AND** 预算小于正文形状预留时正文 SHALL 仍不超过该预算且折叠 SHALL NOT 失败，退化形态 SHALL 仍是最新一行加按 key 引用计数

#### Scenario: 摘要模型产出经租约且带声明采样
- **WHEN** 会话 agent 挂了模型租约并声明了采样参数，随后折叠请求老带摘要
- **THEN** 摘要请求 SHALL 由租约当前版本的模型服务，SHALL NOT 由宿主贡献的裸模型服务
- **AND** 请求 SHALL 携带声明的采样参数；租约换版后开始的请求 SHALL 由新版本产出摘要

#### Scenario: 判定与执行同界
- **WHEN** 声明预算为 B，producer 判定与 consumer 执行各自构造折叠计划
- **THEN** 两侧计划 SHALL 取同一预算 B
- **AND** 同一材料经两次执行 SHALL 得到同一正文，SHALL NOT 因两侧预算不同而产出两条正文不同的产物

#### Scenario: 机械兜底保留
- **WHEN** 会话 agent 无模型、摘要请求失败、终态非 completed，或回答为空
- **THEN** 摘要正文 SHALL 保留折叠自己铸的机械有界型，且 SHALL 服从同一预算
- **AND** 折叠 SHALL NOT 被阻塞，SHALL NOT 提交空正文
