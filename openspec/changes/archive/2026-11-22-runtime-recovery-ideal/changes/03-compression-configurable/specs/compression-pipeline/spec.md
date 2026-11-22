> **历史输入（2026-11-20）**：本目录不再作为当前派发或自动规格同步输入；下文旧勾选、方案与限制按原时点保留，不授权后续实施。当前只按[编排入口](../../../../ORCHESTRATION.md)派发三个语义单元，全部旧要求由[承接表](../../../../CARRYOVER.md)逐项接收，不因转历史而取消。

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
- **AND** 同一请求的已提交产物 SHALL 保持原正文，重试读取该产物而不重新生成第二份；预算同源 SHALL NOT 被解释为模型重复调用必定产生相同文字

#### Scenario: 机械兜底保留
- **WHEN** 会话 agent 无模型、摘要请求失败、终态非 completed，或回答为空
- **THEN** 摘要正文 SHALL 保留折叠自己铸的机械有界型，且 SHALL 服从同一预算
- **AND** 折叠 SHALL NOT 被阻塞，SHALL NOT 提交空正文

## ADDED Requirements

### Requirement: 压缩恢复状态只陈述仍需继续的处理

压缩消费状态 SHALL 保留处理进度、未完成请求与避免重复处理所需的证明；已结历史 SHALL 由事实查询提供，SHALL NOT 仅因曾经处理过就全部复制进每次检查点。清减方案 SHALL 先通过乱序完成、旧身份再次到达与跨检查点回放的验证。

#### Scenario: 固定未完集合增加已结历史

- **WHEN** 未完成请求及当前有效产物相同而已结请求历史增长
- **THEN** 检查点恢复状态与生成成本 SHALL 按设计门确定的有效状态规模接受验证，不以删除历史事实减少成本

#### Scenario: 较晚请求先完成

- **WHEN** 后到请求已完成而先到请求尚未完成
- **THEN** 处理进度 SHALL 仍保留先到请求的执行义务，不能以最高序号覆盖它

#### Scenario: 旧请求身份再次到达

- **WHEN** 已有产物的请求再次被正常消费或恢复路径遇到
- **THEN** 消费者 SHALL 识别原产物，不重复调用模型并提交第二份产物

#### Scenario: 应用状态失败后取检查点

- **WHEN** 产物已提交而消费者状态尚未成功应用
- **THEN** 检查点 SHALL NOT 将该处理无声标成完成，后续推进遵守消费者失败语义
