# request-budget-accounting Specification

## Purpose
定义最终请求统一预算：固定提示、技能正文、动态环境、投影历史、工具声明、reasoning、ContentParts 与输出余量同入计量，窗口语义三分；超限按可观察优先级降级或显式拒发，拒发以 failed 落链并按既有 failure-notice 告知。

## Requirements
### Requirement: 单次请求声明快照
模型请求的工具声明 SHALL 在本次调用内冻结并被预算与采集共同消费；真实执行工具及其生命周期 MUST NOT 被声明快照替换。

#### Scenario: 文件描述变化与同次调用
- **WHEN** 工具描述源在两次调用间发生变化
- **THEN** 每次调用内部budget/SDK请求/采集声明一致，下一次可见新值。

### Requirement: 完整预算输入与诚实精度

预算 SHALL 包括 system、messages、工具 schema/参数、reasoning、ContentParts、任务板和恢复提示；无法可靠估计的媒体 MUST 标 unknown，不能当零或宣称精确。输入预算与输出 limit 语义保持分离。预算裁决 MUST 位于最终请求拼装处（模型请求离开系统前的最后一步），对全部已知输入面统一计量：固定提示、技能正文、动态环境材料、投影历史、工具声明、reasoning、ContentParts、任务板与恢复提示同入总量。窗口语义 MUST 三分：provider context window（新增可选声明）、输出余量（请求层 GenerationConfig.MaxTokens 或声明值；未声明时 MUST 取有界缺省余量，具体常量在 design）、replay/fold window（AgentSpec.MaxTokens 既有语义 MUST NOT 改变）。输入侧预算=provider 窗口−输出余量。窗口缺省规则 MUST 可判定：**预算门开 ⟺ 显式 provider 窗口声明 ∨ AgentSpec.MaxTokens>0（缺省推导=MaxTokens 本值；AgentSpec.MaxTokens 仍只作 replay 窗口，预算仅读它推导缺省）；MaxTokens=0 且未显式声明 = 预算门不启用（记 debug 事实，不裁剪不拒发）**。超限处置 MUST 按可观察优先级降级（可让位材料先让位），系统必需面不可让位；降级后仍超限 MUST 显式拒发并落可观测事实，MUST NOT 静默全量进入请求。**拒发 MUST 以 failed 终态落链，并按既有 failure-notice 告知（不新增通道、不改 failed 触发条件）。**

#### Scenario: 大 schema 与长参数

- **WHEN** 正文很短而 schema 或参数很长
- **THEN** 触发判定包含这些已知开销，纯固定开销超限时明确拒发

#### Scenario: 未知媒体

- **WHEN** 请求包含缺乏可用计量元数据的媒体
- **THEN** 保留现有请求能力并报告 unknown，不宣称已保证 provider 总窗口安全

#### Scenario: 固定提示超出窗口（新增）

- **WHEN** 历史窗口被声明为 32 token 而固定提示约 10KB
- **THEN** 最终请求被统一预算约束：按声明优先级降级或显式拒发

#### Scenario: 不可降级时拒发（新增）

- **WHEN** 系统必需面自身已超限
- **THEN** 请求被显式拒绝，拒绝事实可被后续读取
- **AND** 该轮以 failed 终态落链，并按既有 failure-notice 告知（不新增通道）

#### Scenario: fold 窗口语义不变（新增）

- **WHEN** 预算恢复启用后既有 FoldWindow/压缩阈值配置运行
- **THEN** replay/压缩行为与恢复前一致（回归锚锁定）

### Requirement: 单一压缩及安全消费
历史压缩 SHALL 只由现行判定—执行—恢复链承担：`AgeFoldPlugin.ObserveCommit` 判定并提交 `compress.request`，`CompressionConsumer.Execute` 读该 request 执行 `FoldByAge` 并提交 `compress.product`；同轮内外预算取同一热参组。最终模型门禁 MUST 保留凭据与惰性语义，不允许 provider 隐藏裁剪充当本能力。

#### Scenario: 拒发或未消费迭代器
- **WHEN** 请求未实际进入模型
- **THEN** 不吞一次性恢复提示、不产生第二套压缩。

### Requirement: 同步摘要可界定等待
真折叠摘要 SHALL 共用局部总deadline，默认5秒；超时回退已有工程票据/旧叙事，父取消停止当轮。MUST NOT 后台迟到覆盖新投影。

#### Scenario: 摘要超时与父取消
- **WHEN** 卡片或叙事生成停滞，或父ctx取消
- **THEN** 前者有界降级且票据保留，后者不继续主模型请求。

### Requirement: usage 尾包归约与单终局消费

模型流末尾携带的 usage（token 用量）尾包 SHALL 归约进**同一**终局消费路径，MUST NOT 触发独立第二轮请求或第二循环。一次请求的 token 结算 SHALL 恰好消费一次 usage；重复到达的 usage 与尾包 MUST 去重，MUST NOT 二次计入预算账目。

usage 尾包 SHALL 与被拒发（未实际进入模型）区分：请求未实际进入模型时 MUST NOT 消费 usage、MUST NOT 吞掉一次性恢复提示。归约入口 SHALL 单一；异常尾包 SHALL 明确呈现，MUST NOT 静默丢弃。

#### Scenario: 尾包单次消费

- **WHEN** 一次流在末尾给出 usage 尾包
- **THEN** 账目 SHALL 恰好计入一次，尾包不再另起一轮请求

#### Scenario: 重复 usage 去重

- **WHEN** 同一请求的 usage 被重复到达
- **THEN** 账目 MUST NOT 二次计入

#### Scenario: 未实际进入模型不消费 usage

- **WHEN** 请求因门禁被拒发、未实际进入模型
- **THEN** MUST NOT 消费 usage、MUST NOT 吞掉一次性恢复提示

#### Scenario: 异常尾包可观测

- **WHEN** usage 尾包格式异常
- **THEN** 异常 SHALL 明确呈现（含日志），MUST NOT 静默丢弃后默认成功

