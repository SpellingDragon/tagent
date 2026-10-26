# symptom-anchors Specification

## Purpose

症状直译锚（golden 现状契约）：三条用户原话测试——受理静默、完成恰一条、失败必告知——的存在性、取数面、红证形态与不得缩水。本能力不新增产品行为，它只保证“用户原话”始终有一个红得起来、绿得可证的测试盯着，避免“工程全绿、现象复现”。产品行为条款在 quiet-receipts 与 failure-notice，本能力只管测试本身。

## Requirements

### Requirement: 受理静默症状锚

系统 SHALL 提供一条用户视角测试 `TestLocalSmokeSymptomAcceptedSilent`：在独立注入会话上驱动一次委派，断言**受理被受理后、任务结算前**该会话的 outbound 事件数为 0，且受理后的下一次模型请求中本次调用的 tool 结果恰为最小确认 `ok`（MUST NOT 含 task 标识、执行状态词、结算承诺或轮询提示）。该测试 MUST 在修复前失败于"受理后下一请求的 tool 结果不是最小 `ok`"，修复后通过。测试 SHALL NOT 以内部字段、日志文本或 golden 文件取数，只允许读 fakeChannel 的 outbound 事件与脚本 provider 记录的请求消息。

#### Scenario: 第一天红在模型世界

- **WHEN** 在修复前的代码上运行 `TestLocalSmokeSymptomAcceptedSilent`
- **THEN** 该测试 MUST 非 0 退出，且失败断言 MUST 是"受理回执不是最小 `ok`"（当前值为 `accepted <taskID> (running): …`）
- **AND** 失败信息 SHALL 打出模型实际收到的回执正文

#### Scenario: 用户世界绿不得替代模型世界红

- **WHEN** 脚本模型在收到受理回执后不回正文，使"受理后 outbound=0"在修复前即为真
- **THEN** 该子断言 SHALL 如实记录为绿，红证 SHALL 由"回执非最小 `ok`"承担
- **AND** 测试 MUST NOT 为了制造用户世界红而让脚本复述回执（伪造红）

### Requirement: 完成恰一条症状锚

系统 SHALL 提供一条用户视角测试 `TestLocalSmokeSymptomSettledOnce`：同一条委派链路从受理到结算，断言该会话本轮 outbound 文本数恰为 1、该条不命中确认词表（`accepted`/`(running)`/`background task`/`will arrive as a notification`/`reports progress`/`[send_ok]`/`delivered to`）且等于委派任务的结算正文；并断言结算正文不被第二条重复投递。该测试 MUST 在修复前失败于"本轮 outbound 为 2 条（受理确认 + 结算结果）"，修复后通过。

#### Scenario: 修复前多出确认一条

- **WHEN** 在修复前的代码上运行 `TestLocalSmokeSymptomSettledOnce`
- **THEN** 该测试 MUST 非 0 退出，失败断言 MUST 指明 outbound 文本数大于 1，且首条命中确认词表
- **AND** 测试注释 SHALL 写明该脚本"见状态素材即应答"是**行为仿真**；若评审否决该仿真，诚实结论 SHALL 记录为"fake 模型下用户世界不红"，红证退回受理静默锚的模型世界断言

#### Scenario: 结算正文不得重复

- **WHEN** 结算正文在同一轮被投递第二次
- **THEN** 该测试 MUST 失败并指出重复的正文与次数

### Requirement: 失败必告知症状锚

系统 SHALL 提供一条用户视角测试 `TestLocalSmokeSymptomFailureNotice`：经生产入站路径（`hostRuntime.Drive`）驱动一轮，脚本 provider 对指定标记返回 500，断言该会话 outbound 恰有一条失败告知、正文含原始错误标识（MUST 含 HTTP `500`；框架错误若携带 provider 注入的文案片段，该片段 MUST 一并保留），且 MUST NOT 只含柔化系统话术。该测试 MUST 在修复前失败于"正文不含任何原始错误信息"（当前只收到示例宿主的固定降级文案），修复后通过。同一测试 SHALL 断言成功回合不产生失败话术。

#### Scenario: 修复前只有罐头话术

- **WHEN** 在修复前的代码上运行 `TestLocalSmokeSymptomFailureNotice`
- **THEN** 该测试 MUST 非 0 退出，失败断言 MUST 指明收到的正文缺少原始错误标识
- **AND** 失败信息 SHALL 打印实际收到的全部文本

#### Scenario: 成功回合零失败话术

- **WHEN** 一轮模型调用以成功终态结束并产出正文
- **THEN** 该会话 MUST NOT 收到任何失败告知
- **AND** 该子断言在修复前后均 MUST 为绿

#### Scenario: 恰好一条（跨域约束）

- **WHEN** 框架直投原始错误后，示例宿主仍追加自己的固定降级文案
- **THEN** 该测试 MUST 失败并指出两条失败话术
- **AND** 收敛示例层文案属 02 域（上报项 P-02），本测试不因此放宽"恰一条"

### Requirement: 症状锚不得缩水

三条测试 SHALL 保持为独立父测试函数、可被 `^<TestName>$` 精确选中、无 `t.Skip` 与环境开关；其断言面 MUST NOT 被降级为日志断言、内部字段断言或"结构存在"证明。任何一次"为了让套件变绿"而放宽断言、删除子断言、改跑 `-short` 或改成非生产入站路径的改动，SHALL 视为验收面缩水并被拒绝。

#### Scenario: 以日志替代行为被拒

- **WHEN** 某次改动把用户世界断言替换为"日志里出现了失败行"
- **THEN** 该改动 MUST 被拒绝，症状锚保持对 outbound 事件与请求消息的断言

#### Scenario: 以跳过代替红证被拒

- **WHEN** 某条症状测试以 `t.Skip` 或环境变量开关跳过
- **THEN** 该测试 MUST 判为未交付，对应叶不得勾选
