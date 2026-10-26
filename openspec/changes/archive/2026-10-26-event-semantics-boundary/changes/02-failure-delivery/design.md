# Design — 02-failure-delivery（失败告知直投原始原因）

## D0 结论

本域只做一件事：**把"真失败"这一个终态从静默里捞出来，交给用户一句真实的错误原文**。四条约束同时成立才算交付：

1. 只认 `Terminal == model.KindFailed`（`cancelled`、`completed`、`partial-only`、`unknown` 一律不投）；
2. 只在**没有可交付 assistant 正文**时投（正文优先，绝不追加话术）；
3. 原因必须是 provider 的**原始错误文本**，不是原因码映射、不是柔化话术、不是示例层的兜底句子；
4. 投递必须留下**无模型载荷**的事实与一行**带原因**的日志，前者可审计、后者可定位，两者都不得把错误喂回模型。

## D1 症状 3 的两条路径（为什么必须同时改框架与示例层）

| 路径 | 现状 | 本域后 |
|---|---|---|
| 真机（2026-10-25 20:15:55） | `deliverFresh` 见空正文 → 仅 `assemblyDebug("... produced no deliverable assistant body")` → 用户零消息 | 同一分支内直投 `turn.Failure` → 用户收到 provider 原文 |
| smoke 台架 | 框架仍静默，**示例层** `hostRuntime.Drive` 见 `failed==true` 就发固定文案 `执行出错：本轮模型回合失败，未产出交付内容。` → 用户收到一句无原因的话 | 框架投原文；示例层删除固定文案 → 恰一条原文 |

两条路径的观测面不同（真机是零消息，台架是罐头一条），**只改框架会让台架变成两条**。因此示例层收敛与框架直投必须同轮落地（上报项 P-02）。

## D2 原始原因的取数：`Outcome.Cause()`

`Loop.Drive` 在模型真失败时返回 `Turn{Terminal: KindFailed}, nil` —— **`Invoke` 的 error 为 nil**（上报项 P-03）。原始错误只存在于归约的观测痕迹里：`Outcome.Body.Error`（若失败响应同时留下了 body）或 `Outcome.Trace[i].Err`。

新增唯一取数面（`api/model/terminal.go`）：

```go
// Cause reports the error this outcome's reduction observed: the error the
// retained body carries, or the first error on the observation trace, or nil
// when the reduction observed none.
func (o Outcome) Cause() error {
    if o.Body != nil && o.Body.Error != nil {
        return o.Body.Error
    }
    for _, item := range o.Trace {
        if item.Err != nil {
            return item.Err
        }
    }
    return nil
}
```

- **顺序与既有实现一致**：`rl.AttemptError`（`rl/produce.go:113`）今天就是"body error 优先、再扫 trace"。本域把 `AttemptError` 改为委托 `outcome.Cause()`，行为不变（`ResponseError.Error()` 与 `.Message` 逐字节相同），消灭第二份取数逻辑。
- **被否 A：用 `Invoke` 的 error 贯通**——实测为 nil（P-03）；若改成失败即返回 error，会把"模型失败"变成"调用协议失败"，所有 `Turn` 消费者与示例层 `if err != nil { return err }` 都会把它当 RPC 错误，连投递机会都没有。
- **被否 B：宿主从日志里抠原因**——日志不是取数面，违反"用户世界/模型世界/日志"三面分离。
- **被否 C：把 `Outcome` 整个塞进 `Turn`**——公开面过大，且会让宿主有权重新解释终态。

## D3 `Turn.Failure` 的贯通

```go
type Turn struct {
    ...
    Terminal model.Kind
    // Failure is the raw cause the latest decision's reduction recorded when
    // its terminal is KindFailed, and it stands empty otherwise. It is the text
    // a host delivers when a failed turn produced no deliverable body; a
    // reduction that observed no cause leaves it empty rather than inventing
    // one.
    Failure string
}
```

`Drive` 每轮与 `Terminal` **同源同步**：

```go
turn.Terminal = outcome.Kind
turn.Failure = ""
if outcome.Kind == model.KindFailed {
    if cause := outcome.Cause(); cause != nil {
        turn.Failure = strings.TrimSpace(cause.Error())
    }
}
```

- **为什么每轮清空**：`Turn.Terminal` 的契约是"最新决策的终态"，`Failure` 必须跟着走；否则一个先失败又被后续轮覆盖的回合会带着过期原因。
- **为什么允许为空**：`failNilStream`（nil stream 句柄）会站 `KindFailed` 而 Trace 无错误项。此时**不编兜底话术**（那正是 D6 否掉的柔化文案），留空让宿主走既有 debug 路径；缺口由 2.2 的日志暴露（`terminal=failed` 行没有 `error=`，同时 reduction 的 alert 行在场）。
- **不改签名**：`Host.Invoke` / `Loop.Drive` / `Run.Invoke` 的签名全部不变，新增字段对既有 keyed 字面量零影响（已核对：仓内 `agent.Turn{}` 全是空值或 keyed）。

## D4 `deliverFresh`：正文优先，失败次之，静默最后

```go
func (h *Host) deliverFresh(ctx context.Context, addr event.Address, baseline int, sender MessageSender, turn agent.Turn) {
    if sender == nil {
        return
    }
    contents := freshAssistantOf(h.run.SessionFacts(addr), baseline)
    if len(contents) == 0 {
        if turn.Terminal == model.KindFailed && turn.Failure != "" {
            h.noteFailureNotice(ctx, addr, turn)
            if err := sender.Send(ctx, addr, turn.Failure); err != nil {
                assemblyWarn("tagent: the failure notice to %s was refused: %v", addr.SessionID, err)
            }
            return
        }
        assemblyDebug("tagent: the invocation of %s produced no deliverable assistant body", addr.SessionID)
        return
    }
    for _, content := range contents {
        if err := sender.Send(ctx, addr, content); err != nil {
            assemblyWarn("tagent: the host delivery to %s was refused: %v", addr.SessionID, err)
        }
    }
}
```

- **四道闸的次序就是契约**：`sender` 存在 → 有正文则投正文（**失败但带正文的回合也投正文，不追加错误**）→ 失败且无正文且有原因则投原文 → 其余保持既有 debug 静默。
- **为什么把 `turn` 传进来而不是在 `deliverFresh` 里再查**：终态是归约的唯一事实，宿主 MUST NOT 重新解释；`freshAssistantOf` 只看正文，判不了终态。
- **唯一调用点**：`driveLocked`（`host.go:651`）。`fireNoticeTurn` 也走 `driveLocked`，因此"结算跟进轮自己失败"同样会告知用户——这是同一条契约的必然结果，不是额外功能。
- **为什么 `deliverFresh` 而不是新开一条投递路径**：它是回合结束后**唯一**的框架直投通道（`baseline` 语义在这里），另开一条会与它竞争同一条 sender，出现双投。
- **被否**：① 在 `driveLocked` 里 `if turn.Terminal == KindFailed { sender.Send(...) }`（绕过 baseline，失败且带正文时会双投）；② 把错误拼进 `assemblyWarn` 让上层去发（框架不投递，用户仍然收不到）。

## D5 失败留痕事实：无载荷、不冒充通知

```go
const failureNoticeSummary = "failure-notice"

func (h *Host) noteFailureNotice(ctx context.Context, addr event.Address, turn agent.Turn) {
    if h.run == nil {
        return
    }
    fact := event.Event{
        Type:      event.TypeFeedback,
        Timestamp: time.Now(),
        Meta: map[string]string{
            event.MetaKeyTriggerSource:          "host",
            event.MetaKeyEventSummary:           failureNoticeSummaryOf(turn.Failure),
            event.MetaPrefix + "terminal":       turn.Terminal.String(),
            event.MetaPrefix + "failure_reason": turn.Failure,
        },
        InvocationID: turn.InvocationID,
        // Message stays zero: the fact carries no model payload.
    }
    if _, err := h.run.CommitToSession(ctx, addr, fact); err != nil {
        assemblyWarn("tagent: the failure notice of %s left no fact: %v", addr.SessionID, err)
    }
}
```

- **无模型载荷是硬要求**：`mappedEntryOf`（`facts.go:409`）对"没有 content、没有 tool call、没有 tool name"的事实返回 `mapped=false`，既不进 `history` 也不进回放投影。因此这条事实**永远不会**变成下一轮模型材料——失败原因不得被模型复述成又一条消息。
- **summary 携带原因首行**：`memory.BodyFromEvent` 只把 `MetaKeyEventSummary` 写进持久 body 的 `Summary`；完整原因在 `Meta[meta_failure_reason]` 里（同进程可读）。故 summary MUST 以 `failure-notice: ` 开头并带原因首行（复用包内既有的 120 rune 上界与 `…` 截断，`mappedSummaryRunes`），完整原文 MUST 在同进程事实元数据中可得——**持久层只有摘要行**，这是既存储格式的边界，不属本域可改范围。
- **不复用 `MetaKeyNoticeKind` / `MetaKeyTaskID`**：通知消费者按 kind 识别并驱动跟进轮；失败事实一旦长得像通知，就会在投递之外再驱动一轮（双重投递 + 双重计费）。失败不是任务通知。
- **事实先于投递**：先留痕再 `Send`，投递被拒时痕迹仍在；但事实 MUST NOT 被读成"已送达"——它只写 `terminal=failed` 与原因，不写"delivered"。投递结果只进日志（成功不写、失败 `assemblyWarn`）。
- **提交失败不阻断投递**：审计是旁路，用户告知是主责；提交失败记 warn 后照常 `Send`。
- **被否**：① 用 `RoleUser` 的 feedback 事实留痕（会被读成用户输入，下一轮模型看到系统错误）；② 用 assistant 正文留痕（冒充模型输出，最危险）；③ 不落事实只写日志（audit 断链，04 域无法核对）。

## D6 日志：一行、带原因、不加空字段

`api/model/observe.go` 的 `end(outcome)` 保持"一次请求一行 INFO"，只在失败且有非空原因时于**同一行**追加 `error=%q`：

```go
args := []any{callModel(c.model), outcome.Kind, outcome.Usage.PromptTokens,
    outcome.Usage.CompletionTokens, outcome.Usage.TotalTokens, c.responses, elapsed.Milliseconds()}
format := "tagent: model response model=%s terminal=%s prompt_tokens=%d completion_tokens=%d total_tokens=%d responses=%d latency_ms=%d"
if outcome.Kind == KindFailed {
    if cause := outcome.Cause(); cause != nil {
        if text := strings.TrimSpace(cause.Error()); text != "" {
            format += " error=%q"
            args = append(args, text)
        }
    }
}
quietCallDebug(format, args...)
```

- **同一行**：既有契约是"one request is one INFO line"（`TestModelCallObservation` 断言单行含 `responses=3`）；另起一行会破契约并让 INFO 流在故障风暴下翻倍。
- **`%q`**：provider 错误常带多行 body，`%q` 转义换行，保证仍然是**一行**。
- **成功零字段**：`KindCompleted` 走不到追加分支 → 成功行 MUST NOT 出现 `error=`（负锚 `TestModelCallObservation/a_completed_request_names_no_cause`）。
- **空原因零字段**：`cause != nil` 且 `text != ""` 双条件，避免既有 `ResponseError{}`（无 Message）测试出现 `error=""`。
- **取数唯一**：`Cause()` 是本域唯一取数面；`rl.AttemptError` 委托它，`attempts.jsonl` 的 `error` 与观测行同源（既有 smoke 证据已证实该字段含 `500`，见 `examples/wechat-bot/smoke_test.go:2303` 的断言）。
- **红线保持**：该行仍不渲染 transcript/message/tool 输出；provider 错误不是 transcript。
- **被否**：① 单独一条 `terminal_failed` 日志（与既有 INFO 行重复计数，运维要 join）；② 只在 ERROR 级别打（失败是模型请求的终态事实，不是宿主错误，级别会误导告警）；③ 把原因写进 summary 之外的 meta 就算（持久层读不到）。

## D7 示例层收敛（P-02）

`examples/wechat-bot/wechat.go:276-278` 的固定文案删除：

```go
if failed {
    log.Debugf("[host] the turn of %s ended failed; the notice is delivered by the host", addr.SessionID)
}
```

- **为什么删而不是改**：示例层只拿到 `failed bool`，拿不到原始原因；要拿到就得扩 `invoke` 面（5 处赋值连锁）。职责上"用户告知"属于框架直投通道，示例层不该有第二份文案。
- **`failed` 仍然被使用**：`invoke` 的 `(bool, error)` 签名与 `bindWechat`、4 处测试赋值全部不动，diff 只有 2 行。
- **连带**：`smoke_errors_test.go` 的 `smokeErrorsFailureText` 常量与三处断言改为"原始错误片段恰一条 + 不含旧文案"；`smoke_harness_test.go:3548` 的 `append(wants, "执行出错")` 改为等待原始错误（`500`，依据 D6 的 attempts 证据）。
- **被否**：① 示例层改成"发原始错误"（拿不到原因，且与框架双投）；② 保留文案但加开关（默认开就会双投，关掉就退回无原因，两个状态都不对）。

## D8 写集与串行

| 叶 | 生产写集 | 测试写集 | 只读 | 串行 |
|---|---|---|---|---|
| 2.1 | `host.go`、`runtime/agent/loop.go`、`api/model/terminal.go`、`rl/produce.go` | `tests/integration/host/failure_notice_test.go`（新）、`api/model/terminal_test.go`（`Cause`） | `freshAssistantOf`、`mappedEntryOf`、`Run.CommitToSession`、`hostSendTool` | 前置 01.1 红证；与 01 在 `host.go` + `runtime/agent/loop.go` 相交 → **01 全部落完再启动** |
| 2.2 | `api/model/observe.go` | `api/model/observe_test.go` | `ReduceChannel`、`quietCallDebug`、`AttemptError` | 依赖 2.1 的 `Outcome.Cause()` → 2.1 后串行 |
| 2.3 | `examples/wechat-bot/wechat.go` | `examples/wechat-bot/smoke_errors_test.go`、`smoke_harness_test.go`；`tests/integration/host/failure_notice_test.go`（扩展负锚） | `examples/wechat-bot/smoke_test.go`（00 域所有，只读核对 G3 转绿） | 前置 2.1；与 00 在 `smoke_test.go` 同文件族（只读）、与 01 在 `smoke_harness_test.go` 相交 → 00、01 均先落 |

- **同一编译单元**：`host.go` 与 `examples/wechat-bot/wechat.go` 分属两个 module，但 `tests/integration/host` 在根 module、smoke 在 `examples/wechat-bot` module；`go -C examples/wechat-bot test` 走 `replace` 到本地根 module，故 2.1 的根 module 改动必须先落地再跑 2.3。
- **`api/model` 内两文件（terminal.go / observe.go）不同文件但同包**：2.1、2.2 仍串行，避免同包两次 `go test ./api/model` 归因互相覆盖。
- **禁并发域**：本域与 01 在 `host.go`、`runtime/agent/loop.go`、`smoke_harness_test.go` 三处相交，**整域 MUST 在 01 全部勾选后启动**（一级 W1 已写"与 2.1 串行"）。

## D9 风险与回退

| 风险 | 影响 | 缓解 / 回退 |
|---|---|---|
| 失败但带正文的回合被追加错误 | 用户收到"答案 + 报错"两条，语义混乱 | D4 的正文优先分支；负锚 `failed_turn_with_body_delivers_body_only` |
| 把 `cancelled` 当失败 | 关停/超时向用户推错误 | 只认 `KindFailed`；负锚 `cancelled_turn_stays_silent` |
| 失败事实进了模型材料 | 下轮模型复述错误、成本上升 | 无载荷事实；正锚读 `SessionFacts` 的 `Message.Content == ""`，并读下一轮请求断言不含原因 |
| 失败事实像通知 | 既有通知消费者再驱动一轮，双重投递 | 不复用 `MetaKeyNoticeKind`/`MetaKeyTaskID`；负锚 `failure_fact_does_not_drive_a_turn`（`provider.count()` 不增） |
| 日志一行变两行 / 多行注入 | INFO 流爆量、grep 断 | 同行追加 + `%q`；负锚断言行数与原格式 |
| `Turn.Failure` 为空（nil stream）时又静默 | 极窄缺口 | 2.2 的日志缺口可观测；spec 明写"无原因时不得编造"，04 域核对时按已知缺口登记 |
| 示例层漏改导致两条 | G3 永远不绿 | 2.3 的负锚"不含 `执行出错`"；smoke 全量 `-run TestLocalSmoke` |

## D10 本域不做的

- 不做原因码映射、错误分类、重试（一级 D6 已否；重试在 trpc-agent-go 内已闭环）。
- 不改 `cancelled` 的静默、不改 `[send_denied]`、不改终态通知与受理回执（01 域）。
- 不把失败写进 assistant 正文、不改 `event.Event` 结构、不新增事件类型与持久字段。
- 不改 golden 与主规格（04 域）、不改 CI、不提交。
- 不为 nil-stream 之类"无原始原因"的失败编造用户话术。

## 相邻能力边界（5.9 检索裁定）

- 与 `delivery-dispatch-receipts` 的边界：后者把失败回执注入 agent inbox（依赖下一回合消费）；模型真失败时无下一回合，用户唯一下落是本能力的 sender 直投 outbound。
- 与 `failure-polarity` 的边界：后者判定终局=failed（现状正确）；本能力只消费该终局产出用户告知与 fact/日志留痕，不改终局规则。
