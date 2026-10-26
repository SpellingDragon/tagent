# 设计决策（静态，D1–D6，含被否方案）

## D1 模型输入三族分类与四规则

事件流向模型的全部输入按“模型需要什么”分三族：

| 族 | 内容 | 进模型？ | 落地 |
|---|---|---|---|
| 用户世界 | 用户消息 | 必须（构成回合） | 现状保留 |
| 业务需注意 | 任务终态（settled/failed/cancelled）、具名拒绝（send_denied、工具错误） | 必须（行动/汇报素材） | 现状保留 |
| 系统内部确认 | 受理回执、投递成功（send_ok）、progress 状态 | **绝不** | 本案改 |

四规则：①初始派发静默（受理回的 tool result 仅最小 `ok`，无 task id/状态/承诺）；②回执是凭据不是消息（send 成功→`ok`，失败→具名 denied 保留）；③progress 只落 fact 不进 intake；④模型说话的唯一合法素材=用户问题+业务结果。

**被否**：把回执加 `[receipt]` 前缀但保留内容——仍是可复述素材，治标不治本；回执完全不返回（空字符串）——tool result 协议要求有内容，且空串让模型无从判断调用是否发生，保留最小 `ok` 更稳。

## D2 子域表

| # | 子域 | spec 能力 | 一句话边界 | 外部依赖 |
|---|---|---|---|---|
| 00 | symptom-anchors | （golden 测试，无新能力） | 三条症状直译测试先红：受理后无 outbound / 完成恰一条 / 失败→原始信息告知 | 既有 smoke 台架（fake provider、独立注入会话）、500 注入先例（harness:3750） |
| 01 | silent-receipts | quiet-receipts | 受理回执→`ok`、send_ok→`ok`、progress 只落 fact；denied 保留 | loop.go admissionText/admitNotice、host.go send 回执、delegate 回执面 |
| 02 | failure-delivery | failure-notice | 真失败且无正文→sender 直投原始错误 + fact 留痕 + 日志带原因 | host.go deliverFresh/Invoke err 链、model 观察面（terminal=failed 原因） |
| 03 | tool-schema | tool-schema-clarity | exec_run/exec_release 的 kind/resource 枚举与格式进 schema | tool/exec/tool.go Declaration |
| 04 | contract-gates | （MODIFIED persistent-event-loop + golden 合同） | 三新场景入主规格；progress 语义修订波及的既有场景逐条核对；症状测试入 CI | openspec specs、golden/runtime/event_integrity_scenarios.json、ci.yml |

## D3 波次

| 波 | 内容 | 说明 |
|---|---|---|
| W0 | 00 域全部（麻雀）+ 01.1（受理回执 ok，最小机制） | R34：先杀一只麻雀——失败告知红证（00.1）+ 受理静默红证（00.2）先红；01.1 是 00.2 转绿的最短闭环 |
| W1 | 01 域余量 + 02 域 + 03 域 | 三静默余项、失败告知、schema 下沉并行（文件簇零相交） |
| W2 | 04 域 | 全部行为落地后一次性修订合同与既有场景 |
| F | 收尾 | F1–F4；F3 含症状锚红转绿双证据（回滚即红） |

波次仅汇报分组；解锁=依赖入 main。

## D4 合并规则

- 00 域三叶各自独立测试文件段（同文件 smoke_test.go 但不同函数——触碰集相交→**合单为一条症状测试单**，00 域实际派发为单：三测试一次写入）。
- 01 域 1.1/1.2 同动 host.go+loop.go（触碰集相交）→ 合单；1.3 progress 动 loop.go → 与 1.1/1.2 串行或合单（同文件，按 R21 热点合单）。
- 02/03/04 与 01 的文件簇：02 动 host.go（与 01 相交→串行）；03 独立（tool/exec）；04 独立（specs+golden）。
- 实测：01 与 02 都动 host.go——**合单或严格串行**，禁止并发。

## D5 三级验收

- 孙任务：验证命令 exit 0（正锚点名测试逐字、负锚具名、消费者锚正式入口）；
- 子域：scenarios 全绿（新能力 spec 场景）；
- 变更级 DoD：三条症状测试红转绿（回滚即红双证据）+ 主规格三新场景 + CI 门绿 + F1–F3。

## D6 关键取舍（被否方案）

1. **回合内 send 单次纪律**：被否（用户裁定）——不干预 agent 主动 send；根因是系统诱导，三静默治本。若三静默后仍多话，再另案。
2. **tagent 自建重试**：被否——trpc-agent-go 框架内重试已闭环（`model_retry.go` 的 modelRetryCallbackBinder），本次失败为耗尽后的真失败；重试配置面如需调整属部署配置，不在本案。
3. **失败告知用柔化系统话术**：被否（用户裁定“原始信息”）——直投原始错误文本，用户自行判断；不引入原因码映射。
4. **progress 业务白名单**：暂不做——线上 progress 实际内容均为状态类；留待出现真实业务 progress 需求时另案（防过度设计）。

## D7 与相邻既有能力的边界（5.9 重叠检索结果）

- **delivery-dispatch-receipts（内部回执）↔ quiet-receipts（模型可见文本）**：后者管 send 成功回执在模型上下文中的形态（→`ok`），前者管内部回执总线的触发与血统（发送失败 ERROR 回执、扣留 WARN 回执）——两者消费者不同、互不改写；send 失败的回执触发面不变。
- **delivery-dispatch-receipts（注入 inbox）↔ failure-notice（直投用户）**：前者把失败回执注入 agent inbox、依赖**下一位回合**消费；模型真失败时无下一回合可消费，用户唯一下落是后者——框架经 sender 直投 outbound。两者并存，不是重复。
- **failure-polarity（终局判定）↔ failure-notice（终局告知）**：前者已正确规定模型失败→failed 终局（20:15:55 实证终局正确）；后者消费该终局产出用户告知与 fact/日志留痕。不改终局规则。
- 三项均**不扩展**既有能力（单一职责），边界条款随 01/02 域 design 各有一节复述。

## D8 第二轮 grill 裁定（机制细节，2026-10-25）

1. **受理→`ok` 失去 task id 的 tradeoff**：模型失去“我已派过 task-54”的自我对账材料。裁定保留 `ok`——模型自身上下文保有工具调用历史，跨回合对账走查询面；tradeoff 显式记录，不回头。
2. **失败告知文本截断**：错误原文可能超长/超微信上限——直投前截断到 2 KiB 并标注“（已截断）”；截断是实现细节，不改“原始信息”裁定。
3. **部分文本不投递**：terminal=failed 时一律只发失败告知（含原始错误），此回合的部分 assistant 文本丢弃——部分输出被当作完整答复是更重的误导。cancelled 不发告知。
4. **失败事实的模型可见性（按实现与 failure-notice 规格定稿）**：failure-notice fact **不进任何模型请求、历史 transcript 或回放投影**（不带 assistant/user 载荷，不带通知元数据），也不作为输入材料唤醒回合；其可见面是会话事实链/审计与日志（`trigger_source=host`）。「用户追问『刚才怎么了』时模型可答」属于**未裁定的产品议题**，由后续单独裁定；在裁定前不得以本设计为依据给 fact 加消息载荷。
5. **主规格 requirement 形态**：04.1 以 ADDED 新增一条「循环面的模型感知边界」（三场景），细节引用 quiet-receipts/failure-notice 不复述；不是挂到既有通知身份 requirement 下。
6. **exec Enum/Pattern 双保险**：Enum（kind=["", "tmux"]：省略即空串，选 tmux）+ Pattern（resource）+ 描述文本同时写死；provider 若忽略机读字段，描述仍是模型可读的最低保真。
7. **失败告知不节流**：provider 长故障时连续回合各发一条告知（信息真实）；刷屏风险记录，出现再另案。
