# Grill 审计：event-semantics-boundary 是否阻碍系统持续演进

> **审计者身份**：独立于实施者与主代理（本会话未参与任何一行实施）。
> **读面**：`8d34616^..HEAD`（11 提交，HEAD `fd550d8`）+ 未跟踪的 `openspec/changes/event-semantics-boundary/**`。
> **写面**：仅本文件。产品代码零改动，零提交（`git status --short` 除既有 `M scripts/doc_budget.json` 与既有未跟踪项外无新增）。
> **方法**：grill-me → grilling（design tree / frontier 轮次：先问最上游的"这份变更该不该存在"，再下沉到机制细节；❓Q / ➡️答，问句一律设计成"答 yes 即接受推荐"）。
> **方法偏差声明**：本会话的可用工具集里**没有 `Skill` 工具**，第一步的 `skill="grill-me"` / `skill="grilling"` 无法按字面执行。改为直读 `~/.qoder/skills/grill-me/SKILL.md`（8 行："Call the Skill tool with 'grilling'"）与 `~/.qoder/skills/grilling/SKILL.md`（31 行：design-tree、frontier、`❓ **Q1** - **title**: body` / `➡️ 推荐答案`、"事实是提问者的责任"）。方法论按原文执行，形式按原样产出。此为**流程偏差**，不是证据偏差，故在终报一并申报。

---

## 0. 结论（先给账）

**一句话**：这三条症状的修复本身是**成立的、值得的、红绿双证齐的**；但变更的**规格体量（410 行规范散文 / 5 个 spec 面）与为"不存在的生产者"写的机制**严重超出行为改动量，且留下了 **1 处模型面谎言（硬阻碍）**、**6 处会静默失效的隐性契约（软阻碍）**。

三条最硬的新发现（主代理未提）：

| # | 发现 | 级别 |
|---|---|---|
| **N1** | `tool/subagent/tool.go:125` 的**模型面描述仍在承诺**"回执携带 task 身份、progress 与终态以通知到达"——而 `1332064` 这一提交正是"回执改成裸 `ok`"，它改了 `loop.go` 与 137 行测试，**独独没改这句最直接的模型读物**，也无任何测试钉住它。变更自己修的症状，在隔壁工具的描述里原样保留。 | **硬阻碍** |
| **N2** | `host.go:768-769` 把**执行资源生命周期事件伪装成 `task.NoticeProgress`**，于是 `pipelineProgress`（`loop.go:1298`）必须靠 `Round>0` 把它区分出来；而生产侧**根本没有真正的 progress 生产者**（`Backflow` 的 progress 分支无上游）。一个判据，用来区分两个本不该共用同一 kind 的语义。 | 软阻碍 |
| **N3** | `design.md:70`（D8.4）写"failure-notice fact **进会话历史/回放**"，`changes/02-failure-delivery/specs/failure-notice/spec.md:31` 写"MUST NOT 进入任何模型请求、历史 transcript 或回放投影"，**互斥**；代码按 spec 实现。规范内部自相矛盾。 | 软阻碍 |

**净账**：生产代码净 +273 行（8 文件 `+365/−92`），测试净 +1797 行（`+1892/−95`），跟踪的文档/合同净 +93 行（`+118/−25`），另有未跟踪的 410 行规范散文。**换掉的是 3 个字符串**（受理回执、send 回执、罐头失败话术）。这个比例（≈273 生产行 : 3 个用户可见字符串）本身不是罪，但它解释了为什么会出现 N1–N8：**机制的增长速度超过了它服务的生产者数量**。

---

## 1. 可信度声明（复核 vs 采信）

### 1.1 我亲自复核（可复跑）

| 数字 | 命令 | 实测 |
|---|---|---|
| 提交数 | `git log --oneline 8d34616^..HEAD \| wc -l` | **11** |
| 变更文件数 | `git diff --name-only 8d34616^..HEAD \| wc -l` | **39** |
| 生产 Go 净增 | `git diff --numstat … \| awk '$3~/\.go$/ && $3!~/_test\.go$/'` | **+365 / −92**（8 文件） |
| 测试净增 | 同上 `/_test\.go$/` | **+1892 / −95** |
| 文档/合同净增 | 同上 `.md/.json/.yml` 非测试 | **+118 / −25** |
| 能力总数 | `ls openspec/specs \| wc -l` | **126**（4 个新能力全部 **ABSENT**） |
| 主规格 diff | `git diff --stat 8d34616^..HEAD -- openspec/specs/` | **1 文件 +28/−0**（仅 `persistent-event-loop`） |
| 嵌套 spec 体量 | `wc -l changes/*/specs/*/spec.md` | **362**（74+108+92+59+29）＋顶层 `specs/orchestration/spec.md` **48** = **410** |
| 需求/场景计数 | `grep -c '^### Requirement:' / '^#### Scenario:'` | symptom-anchors 4/9、quiet-receipts 5/14、failure-notice 4/12、tool-schema-clarity 3/7、04-delta 1/3 |
| doc budget | `python3 scripts/check_doc_budget.py --json` | measured **22220** = 工作树 baseline **22220**，`ok:true`，slack **0**；**committed baseline = 22192**（`git diff scripts/doc_budget.json`） |
| 棘轮上调 | `git diff scripts/doc_budget.json` | `22192 → 22220`，**未提交**，且文件尾 `\ No newline at end of file` |
| progress 生产者 | `grep -rn "NoticeProgress" --include="*.go" .` | 生产侧**零发射点**（仅 `host.go:768-769` 的元数据误标 + 消费者分支） |
| `IssueNotice` 调用者 | `grep -rn "IssueNotice(" --include="*.go" .` | **0**（仅定义于 `runtime/task/handoff.go:266`） |
| `admitNotice`/`commitNotice` 调用者 | 同上 | 仅 `loop.go` 内部 + 测试 |
| 测试实跑（本轮复跑） | `go test ./runtime/agent -run 'TestQuiet\|TestBackflow\|TestAdmission\|TestNotice\|TestTurn' -count=1` | `ok … 0.447s` |
| 同上 | `go test ./tests/integration/execution -run 'TestProgressNotificationStaysOutOfTheDecision\|TestQuietReceipts\|TestEventIntegrityExecStateEvents' -count=1` | `ok … 6.395s` |
| 同上 | `go test ./tests/integration/host -run 'TestFailureNotice\|TestQuietSendReceipt\|TestEventIntegrity' -count=1` | `ok … 11.543s` |
| 同上（先期） | `go test ./api/model ./rl ./tool/exec -count=1` | `ok`（`tool/exec` 首次因我猜错测试名得到 `[no tests to run]` exit 0——零匹配不报错，已作为 C4 的侧面证据记下） |
| 症状测试名 | `grep -n "func TestLocalSmokeSymptom" smoke_test.go` | 3 条，`:3220/3285/3376`，共 **198** 行 |
| CI 机械门 | 读 `scripts/test_check_runtime_workflow.py` symptom 段 | 三个 parent 名钉死、前缀选择器与 `smoke_test_names()` 双向交叉核、禁 `if:`、`if-no-files-found: error` |

### 1.2 我直接采信（未复跑，附理由）

- **真机红证 `20:12:26` / `20:15:55`**：属于用户现场观测，仓库内无原始报文，无法复跑。变更的正当性主要靠它，我按"存在且已由 F3 回滚红证二次确认"采信。
- **`F2-gates.txt` 的 short 十轮 / long 六片 / mutations 11/11**：原始日志在 `$TMPDIR/esb-final/`，本会话未复跑（耗时 >30 分钟且与本审计的问题无关）。我采信其**结论**，但**不采信其"lint 0"作为 HEAD 绿**——见 N5。
- **`TestLocalSmokeWave2Recall` 既有红**：采信为既有（P-09 同源）。
- **`openspec validate --all --strict` 的 44 tests OK**：采信（该命令只读规范，风险低）。

### 1.3 我推翻的（主代理或我自己先前的假说）

- ❌ **"CI 前缀选择器会零匹配腐烂"**：**不成立**。`scripts/test_check_runtime_workflow.py` 已把三个测试名钉死并与 `smoke_test_names()` 交叉核对，多选/漏选都红。见 §6 排除项 E1。
- ❌ **"tool-schema-clarity 与 failure-polarity 完全重复"**：**不成立**。前者管 exec 资源参数形态，后者管退出码透传的引导语；两者是**不同主题**。真正的病是"模型面声明"这一关注点**在任何既有能力里都没有归属地**（见 Q3）。
- ⚠️ **"失败告知完全没覆盖 failNilStream"**：**部分成立**。`failNilStream` 确实 `Cause()==nil`（`api/model/terminal.go:349-354`）→ `deliverFresh` 只打 DEBUG（`host.go:1037`）；但归约侧有 `AlertNilStream`（`terminal_test.go:125`），**运维可见、用户不可见**。措辞必须精确，不能喊"全静默"。

---

## 2. 新发现问题（主代理未提，按严重度排序）

### N1【硬阻碍】`tool/subagent` 的模型面描述仍在承诺已完成被删除的语义

`tool/subagent/tool.go:125`：

```go
Description: "delegate one sub-agent call as an asynchronous task and answer with its admission receipt: " +
    "the receipt carries the task identity, and the progress and terminal of that task arrive as its notifications. " +
```

- 与 `quiet-receipts` 的核心条款（回执=裸 `ok`，不含 task 标识）**直接冲突**；与新语义"progress 永不进模型"**直接冲突**。
- `git log -S "the receipt carries the task identity"` 只命中 `1abd845`（工具诞生），**`8d34616^..HEAD` 的 11 提交没有任何一笔碰过它**——包括专门做"delegation receipt is a bare ok"的 `1332064`（它改了 `runtime/agent/loop.go` + 12 文件、137 行新测试，唯独漏了这句）。
- `tool/subagent/tool_test.go:33` 的 `TestSubagentToolDeclaration` 只断言 `agents_file/prompt/model/skills/tools/mcp/exactly one assembly source/structured handoff`，**不覆盖回执语义**——所以它永远绿。
- **危害机制**：这是模型读到的唯一权威说明。模型按描述期待"回执里有 task 身份 + 后续 progress 通知"，于是会**再问一次/复述一次**——正是本变更要消灭的症状形态。它把一个已修好的行为，用一个未修的说明书重新诱导出来。
- **为什么是硬阻碍**：本变更的验收矩阵第一行是"用户原话直译"。这句话会让症状从**另一条入口**复发，而 CI 的三条症状测试只走 delegate 的 smoke 装配，**测不到描述文本**。

### N2【软阻碍】执行资源观察者伪装成 `task.NoticeProgress`，`Round>0` 是缝合线而非判据

`host.go:768-769`（`observeExecState`）把 exec 生命周期事实的 `MetaKeyEventSummary` / `MetaKeyNoticeKind` 都写成 `string(task.NoticeProgress)`；`host.go:688` 构造 `task.Notice{TaskID, Kind, Event}` 时**不带 Round** → `fireNoticeTurn:694` 落成 `task:0:progress:0`。

于是 `pipelineProgress`（`loop.go:1298-1308`）必须"kind==progress 且 round>0"才能把「该被跳过的流水线进度」与「必须被读成材料的 exec 观察」分开。

三重问题：

1. **概念错位**：exec 资源生命周期（`execution-resource-lifecycle` 能力）与 task 流水线进度（`persistent-event-loop` 的通知身份）本是两族语义，共用一个 kind 纯属历史偶然。
2. **生产侧无真 progress 生产者**：`Backflow`（`loop.go:344`）的 `NoticeProgress` 分支的上游是 mailbox，而 mailbox 的生产者只有 `wrapper.go:302`（settled/failed）、`cancel.go:85`（cancelled）、`handoff.IssueNotice`（**零调用者**）。→ `admitNotice`/`commitNotice` 在生产中**永不被调用**，`pipelineProgress` 在生产中**恒为 false**。
3. **判据无钉**：没有任何测试断言 exec 观察事实的 `Round==0`。一旦 exec 事件将来带上"资源轮次"（这个领域天然会有轮次），材料会**静默消失**——用户看不到、测试不红、日志不响。

### N3【软阻碍】`design.md` D8.4 与 `failure-notice` 规格互斥，代码按规格实现

- `design.md:70`：「failure-notice fact **进会话历史/回放**（作为用户已见的系统口径事实），但不作为输入材料唤醒回合；用户追问"刚才怎么了"时模型可答。02.2 原"不进历史/回放"表述撤销。」
- `changes/02-failure-delivery/specs/failure-notice/spec.md:31`：「该事实 MUST NOT 携带 assistant 或 user 角色的消息载荷，MUST NOT 使用通知元数据……因此 **MUST NOT 进入任何模型请求、历史 transcript 或回放投影**。」
- 代码（`host.go:1072-1089`）：`event.Event` **无 Message** → 按 spec 实现，**D8.4 的意图没有落地**；且测试明确站在 spec 一侧：`tests/integration/host/failure_notice_test.go:267` 的 `failure_fact_left_without_model_payload` 断言它不在 `SessionHistory` 与 `ReplayEntries` 里，`:318` 的 `failure_fact_does_not_drive_a_turn` 断言它不唤醒回合。
- 危害：归档后 design 进 `archive/`、spec 进 `openspec/specs/`，未来读者看到的是 spec；但任何按 design 复核的人会得出相反结论。**这是"用户追问'刚才怎么了'模型答不出"的产品决策被一份设计文档悄悄否掉**——决策归属不清。
- 需要裁定的是**产品意图**（不是实现）：如果真要让模型能答，就必须给 fact 加 Message，并接受它进 transcript（这会推翻 `quiet-receipts` 的"系统确认不进模型"原则）；如果不要，就删 D8.4 第 4 条。

### N4【软阻碍】同一失败原因被截成三种长度，其中一种不截断

| 落点 | 代码 | 长度 |
|---|---|---|
| 用户收到的正文 | `host.go:1054-1064` | 2 KiB（`2<<10`）+「（已截断）」 |
| 事实 summary | `host.go:1095-1110` | 首行 + `mappedSummaryRunes` |
| 事实 meta `failure_reason` | `host.go:1083` | **原文全量，不截断** |

一条 100 KB 的 provider 堆栈会**完整进入会话事实链并持久化**（`CommitToSession`）。D8.2 只裁定了"直投前截断"，没人裁定 meta。这既是存储放大，也是把完整内部报错写进长期可用面（含路径、密钥形态的偶发泄漏面）。

### N5【软阻碍】doc budget 棘轮在工作树被上调且未提交，HEAD 干净 checkout 必红，且尾换行被吃

- `git diff scripts/doc_budget.json`：`"lines": 22192 → 22220`，新增 `rebaseline_reason` 末条"2026-10-25 event-semantics-boundary: +28 lines…"。
- 该文件的 `note` 自述"**只降不升**……禁止上调（`--tighten` 会拒绝上升的指标）"。这次是**手改基线**绕过机制，不是 `--tighten`。
- `python3 scripts/check_doc_budget.py --json` → `measured 22220 = baseline 22220, slack 0`。**HEAD 的 committed baseline 是 22192** → 干净 checkout 上 `bash scripts/lint.sh` 必红。
- `ORCHESTRATION.md` §四④ 已自认"lint 绿是「工作树绿」非「HEAD 绿」"——但 `tasks.md` F2 仍写"lint 0"。**同一事实，两处表述，归档后只留 F2 那句。**
- 附带 nit：该文件尾换行丢失（`\ No newline at end of file`），任何 `prettier`/`end-of-file-fixer` 类门都会再红一次。

### N6【软阻碍】4 个新 spec 目录里有一个自称"不新增产品能力"，归档步骤没有能力晋升

- `changes/00-symptom-anchors/specs/symptom-anchors/spec.md:3`：「**本文件不新增产品能力。** 它的对象是三条症状直译测试**自身**」。
- 但它占着一个 capability 目录。两条路都坏：
  - **晋升** → `openspec/specs/symptom-anchors/` 出现一个"测试形态"能力，126→130，其中 1 个不是能力。
  - **不晋升** → 归档后正文只活在 `archive/`，`openspec/specs` 仍 126，这三条锚的"不得缩水"契约**没有任何常驻规范承载**。
- **先例**：`f3e00ec`（event-consumption-integrity 归档）把嵌套 delta **手工并入 `openspec/specs`**（份数 17 取自该批的 `rebaseline_reason` ㉕ 自述；我复核的部分是：`session-fact-lifecycle` 与 `execution-resource-lifecycle` 两篇确实由该提交创建）。说明"晋升"是**手工步骤**，不是自动的。
- `tasks.md:49` F4 只写「归档 + 导览（域 → 证据链接；run-id 逐字符核对）」，**没有"晋升"二字**。→ 大概率带着 126 归档，4 份契约蒸发。

### N7【无碍·当前不可达】progress 事实以 `user` 角色进入投影，可能跨 range 回灌

- `loop.go:395`：`commitNotice` 写 `Message: progressNoteOf(notice)`，内容 `"tagent: task %s reports progress in round %d"`，Role=`user`。
- `request_assembly.go` 的 `RoleOf(TypeFeedback)=RoleUser`；`facts.go:159-196` 的 `admitsRange` **只认** `TypeExternalInput` 或 `TypeFeedback && TriggerSource=="task"` → progress notice **不推进 ended 边界**，因此它在**下一 range** 开始时成为 ended material，被 `projectedSegment`（`request.go:619-637`）整段渲染进请求。
- **但**：生产侧无 progress 生产者（见 N2）→ 不可达。**若 N2 的生产者将来落地，这条路径会让"系统确认"以 user 口吻回灌下一次模型请求**，与 quiet-receipts 的立身之本相反。
- 我未做动态验证（无生产者即无法构造），**只能声明"静态路径成立、未实测"**。

### N8【软阻碍】通知身份的四元组里 `Seq` 恒为 0，`IssueNotice` 零调用者

- `runtime/task/fact.go:18-20`：`FactKey = "%s:%d:%s:%d"`（task:round:kind:seq）。
- 三处生产构造全部 `Seq: 0`：`wrapper.go:302`、`cancel.go:85`、`skeleton_assert.go:59`、`host.go:688`（隐式零值）。
- 唯一能让 seq 递增的 `NoticeSequencer.Issue` 只被 `IssueNotice` 调用，而 `IssueNotice` **零调用者**（`grep -rn "IssueNotice(" | grep -v _test` 只有定义）。
- 后果：`commit.go:212` 用 `(task,round,kind,seq)` 做 outbox 去重键——**同 task 同 round 同 kind 的两条通知会合并成一条**。今天靠"round 会递增"兜住，但"四元组稳定身份"的说法与实现不符，且 `noticeFields` 的 `SplitN(raw,":",4)` 还额外依赖"task 不含冒号"（见 Q8）。

---

## 3. Grill 卷

### A. 概念增殖

**❓ Q1** - **三个新能力是否都必要，还是至少有一个可并入既有能力？** 我建议你对 `quiet-receipts` 与 `failure-notice` 判"可辩护"、只对 `tool-schema-clarity` 判"软阻碍"——你接受吗？

➡️ **接受，但需按消费者甄别。**
- `quiet-receipts` vs `delivery-dispatch-receipts`：后者管**内部回执总线**（`examples/wechat-bot/wechat.go` 的 `delivery_receipt` 血统、注入 agent inbox、依赖下一回合消费，`openspec/specs/delivery-dispatch-receipts/spec.md:9,21-24`）；前者管**模型可见文本**（tool result 是 `ok` 还是 `accepted task-54 (running)`）。消费者不同（inbox 消费 vs 模型读），**新能力成立**。
- `failure-notice` vs `failure-polarity`：后者是**终局判定**（`pane_dead_status` → 极性，`openspec/specs/failure-polarity/spec.md:6-15`）；前者是**终局告知**（failed + 无正文 → 直投原始错误）。判定与告知分离，**新能力成立**。
- `tool-schema-clarity`：见 Q3，**不成立**。它没有独立的消费者/触发面，只有"exec 工具的两个参数"。

**❓ Q2** - **`symptom-anchors` 作为第 4 个 spec 目录是否应该保留？** 建议判"软阻碍"：它是测试形态契约，不是产品能力，应当并入 `quiet-receipts`/`failure-notice` 的"验收锚"小节，而不是单独占一个 capability。

➡️ **接受。** 证据：`changes/00-symptom-anchors/specs/symptom-anchors/spec.md:3` 自述"不新增产品能力"；`proposal.md:34-38` 的 `### New Capabilities` 只列 3 个，**没有它**。所以它连变更自己的清单都没进——却占着目录、4 个 Requirement、9 个 Scenario、74 行。
两种归档后果都坏（N6）：晋升 → `openspec/specs` 多一个非能力；不晋升 → 契约蒸发。
**最小修法**：把三段"锚"条款移入 `quiet-receipts`（2 条）/`failure-notice`（1 条）的 `## 验收锚` 小节，删除 `00-symptom-anchors/specs/` 目录。测试本身留在 `smoke_test.go` 不动。

**❓ Q3** - **`tool-schema-clarity` 是否"偏细"？** 主代理判"偏"，我建议给出更精确的诊断：不是"细"，而是**"模型面声明"这一关注点在既有 126 个能力里没有归属地，这次是第三次给它开新家**。

➡️ **接受修正后的诊断。** 证据链：
- `failure-polarity/spec.md:66-73` 已有一条 Requirement「工具描述引导退出码透传」——**"工具描述"住在"失败极性"能力里**，本身就是错位。
- `execution-resource-lifecycle/spec.md` 拥有资源身份/查询/退出/释放 6 条 Requirement，**exec 的 kind/resource 形态天然属于它**。
- 现在 `tool-schema-clarity` 成了"模型面声明"的第三处落点。
**裁决**：软阻碍，但**不建议现在改**（改名/合并要重走一遍归档，收益 < 成本）。建议在 `design.md` 增加一条"模型面声明"归属裁定（归属 `execution-resource-lifecycle` 或新建 `tool-declarations`），并把它列为下一波的**前置决策**，避免出现第四处。

**❓ Q4** - **+1 主规格 requirement（28 行）是否必要？** 建议判"可辩护"。

➡️ **可辩护。** `persistent-event-loop` 是循环面的唯一权威规格，"什么进模型、什么不进"需要一条常驻 requirement 钉住，否则三族边界只在变更期有效。**但**：这段文字在 4 处重复表述（主规格 1 + `quiet-receipts` 5 + `failure-notice` 4 + `design.md` D1），任何措辞调整要同步 4 处——这是**真实的维护面**，只是还没到该拒的程度。建议主规格只留"一句边界 + 指针"，细节不复述。

**❓ Q5** - **`failure-notice` 引入的 `trigger_source="host"` 是新词汇值，它登记了吗？** 我建议判"软阻碍（小）"。

➡️ **接受。** `grep -rn "MetaKeyTriggerSource:" --include="*.go" .` 的生产赋值只有 `"task"`（`host.go:698`）、`"host"`（`host.go:1080`，本变更新增）、`req.TriggerSource`（`invoke.go`）、meditation 的 lineage。`docs/FACT-VOCABULARY.md` **完全没有 `trigger_source` 行**——既有缺口，但本变更往里加了一个新值却没补。`admitsRange`（`facts.go`）正是靠 `TriggerSource=="task"` 决定"是否推进 ended 边界"，所以这个字段是**语义开关**，值域不是装饰。建议补一行词汇表：`trigger_source: task | host | <lineage> | <user-declared>`。

**❓ Q6** - **+1 个 CI job 是否必要？** 建议判"无碍"，并且是**本次做得最对的一件事**。

➡️ **接受。** `.github/workflows/ci.yml:367-397` 的 `event-semantics-symptoms`：无条件、`-run '^TestLocalSmokeSymptom'`、`-json` 出产物、`if-no-files-found: error`；且 `scripts/test_check_runtime_workflow.py` 把三个测试名钉死并与 `smoke_test_names()` 双向交叉核对（多选/漏选皆红）。这是**唯一一处"声明被机械门钉住"的地方**——对比 N1 的描述文本，零机械门。

---

### B. 隐性契约

**❓ Q7** - **`pipelineProgress` 的 `Round>0` 是可接受的工程妥协，还是未来陷阱？** 主代理判"应显式化"。我建议**加重**：它不是妥协，是**用数值字段做类型判别**，且生产侧恒 false、无测试钉零值。

➡️ **接受加重版。** 三锚：
- `loop.go:1306-1307`：`round, err := strconv.ParseInt(fields[1],10,64); return err == nil && round > 0`。
- `loop.go:1213-1216` 的注释已经承认："The progress category alone does not name that report — an execution-resource observation commits its unattended material under the same category… hence the skip keys on the round"。**注释在解释一个不该存在的共谋。**
- **生产侧无 progress 发射点**（§1.1 表），所以 `pipelineProgress` 只在 `false` 分支被真实使用；`true` 分支只有测试走到。
- **无测试钉 `Round==0`**：`grep -rn "Round:" --include="*.go" tests runtime/agent host.go` 在 exec 路径上零命中。
**最小修法**（不重构）：在 `tests/integration/execution/progress_notification_test.go` 加一条子测试，断言 exec 观察因子的 `Round==0` 且其材料**确实进入**下一次请求；同时在 `loop.go:1290-1297` 的注释首句写明"这是**类别复用**的补丁，不是进度语义的一部分；若 exec 观察者将来携带轮次，本判据会静默失效"。**一句话 + 一条测试，把陷阱变成路标。**

**❓ Q8** - **`Notice.FactKey()` 用冒号拼接且无转义，这是新的一处隐性契约吗？** 建议判"软阻碍"。

➡️ **接受（这是我自己找的，主代理未提）。** `runtime/task/fact.go:18-20`：
```go
return fmt.Sprintf("%s:%d:%s:%d", n.TaskID, n.Round, n.Kind, n.Seq)
```
`loop.go:1272` 用 `strings.SplitN(raw, ":", 4)` 反解。**若 TaskID 含 `:`**（delegation 的 task id 来自 `event.TaskID`，形态未受约束），字段位移 → `fields[1]` 变成 taskID 的尾段 → `ParseInt` 失败 → `pipelineProgress=false` → **progress 材料漏进模型**。
这是 **fail-open**：`loop.go:1296-1297` 明确写"malformed round keeps the material: dropping a notice is louder than reading one"。方向选对了（宁可多读不可漏读），但代价是"身份编码损坏 = 语义判据失效 = 反向泄漏"。
**最小修法**：`FactKey` 改为 `taskID + "\x00" + ...` 或对 taskID 做 `strconv.Quote`/长度前缀（仓库里 `noticeKeyPrefix` 已经用了 `"notice\x00" + len + "\x00"` 的**同一模式**，`loop.go:424-426`——照抄即可）。或在 `noticeFields` 里用 `LastIndex` 从右往左切三段。

**❓ Q9** - **`deliverFresh` 的"正文优先"次序是可辩护的，对吗？** 建议判"可辩护"。

➡️ **接受，且负锚已就位。** `host.go:1021-1022` 的注释把次序写死了："A body wins over a failure: a turn that produced a deliverable answer delivers it. A failed turn without a body delivers the raw recorded cause; no cause stays silent." `tests/integration/host/failure_notice_test.go:366` 的子测试名就叫 **`failed_turn_with_body_delivers_body_only`**。**这是本变更里注释质量最高的一处**：它把"为什么"和"边界"都写在了会被读到的地方，且边界有同名测试兜底。

**❓ Q10** - **`ok` 在两处各自字面量（`loop.go:1555` 的 `const` / `host.go:1877` 的 inline `"ok"`），该不该共享常量？** 建议判"可辩护（小）"。

➡️ **接受，但方向要拧对。** 两处 `ok` **语义不同**：`admissionReceiptText` 是"工具被受理"的模型可见回执；`hostSendTool` 的 `"ok"` 是"消息已投递"的模型可见回执。共享一个常量会把两个语义绑死，**反而是耦合**。真正该做的是**把区别写进注释**（现在两处都没有说"另一处也有一个 ok，故意不共享"）。维护面实测：`grep -rn '"ok"' --include="*_test.go"` 命中 **37 处**——改契约文本要动的是测试，不是常量。所以共享常量省不了什么，注释才省。

**❓ Q11** - **`failureNoticeText` 的 2 KiB 截断是可辩护的？** 建议判"可辩护"，但 meta 的 `failure_reason` 不截断是**新问题**（N4）。

➡️ **接受拆成两条判。** 2 KiB 有 D8.2 明文裁定 + 按 rune 边界切（`host.go:1059-1062` 的 `utf8.RuneStart` 回退）+ 截断标注，实现正确。**但**同一原因在 meta 里全量入库（`host.go:1083`），三处三种长度（N4）。**同一份数据三个策略 = 三个未来要同步的地方。**

**❓ Q12** - **`noticeKindOf`/`noticeFields` 依赖 key 编码（hex(prefix)+hex(四元组)），这是"用 key 承载语义"，可辩护吗？**

➡️ **可辩护。** 这与本仓"身份即事实"（fact 的 key 是一等公民、projection 靠 key 前缀选面）一致；`loop.go:409-419` 的两个函数也把编码规则集中在一处。**真问题不是"用 key 承载语义"，而是"用 key 承载了 key 自己都表达不了的语义"**（Round 的"是不是进度"含义）——那是 Q7，不是这里。

**❓ Q13** - **`observeExecState` 用 `NoticeProgress` 当元数据，这是可以接受的复用吗？** 建议判"软阻碍"（= N2）。

➡️ **接受。** `host.go:768-769` 两行都是 `task.NoticeProgress`。它的动机在注释里没写（注释只说"one action never produces two consumption paths"）。**复用 namespace 省了一个常量，付的代价是"progress"这个词从此有两个意思。** 最小修法：在 `runtime/task/registry.go` 加 `NoticeObserved NoticeKind = "observed"`，`observeExecState` 用它；`pipelineProgress` 简化为 `kind == NoticeProgress`（round 判据可删）。**但这会改 spec（`quiet-receipts` 的 progress 条款与 `persistent-event-loop` 的场景）**，属"下一波"级别的改动，不建议在归档前做。

**❓ Q14** - **`Seq` 恒 0，四元组名不副实——这是概念增殖还是无害冗余？** 建议判"软阻碍（小）"（= N8）。

➡️ **接受。** 四元组、`FactKey`、`commit.go:212` 的去重键、`sortEnvelopes` 的排序（`handoff.go:315-325`）都按四元组写，但第四个分量在生产中恒 0，唯一能递增它的 `IssueNotice` 零调用者。**读代码的人会以为"seq 能区分同轮同类通知"，实际不能。** 最小修法二选一：(a) 在 `fact.go:15-20` 注释写明"seq 目前恒 0，保留给未来的多通知轮次"；(b) 删 `Seq` 并同步 4 处。倾向 (a)（(b) 会碰公开 API）。

---

### C. 抽象失当

**❓ Q15** - **`admitNotice` 名实不符，主代理这条成立吗？** 建议：**成立，且比主代理说的更糟——它下面挂着一整簇死代码。**

➡️ **接受。** `loop.go:369-374`：
```go
func (l *Loop) admitNotice(invocationID event.InvocationID, notice task.Notice) {
	in := l.intakeFor(invocationID)
	if addr := in.rangeAddress(); addr.IsComplete() {
		l.commitNotice(invocationID, addr, notice)
	}
}
```
"admit"在 `Backflow:334-362` 里的语义是"交给 intake，驱动下一次决定"（`in.deliver(...)`）；这里**什么也没 admit**，只 commit 一条 fact。
**死代码簇**（本变更删了 park 路径但留了壳）：
- `intake.notify`（`loop.go:1187`）：**零调用者**（`grep -rn "\.notify("` 只命中 `evolution/meditation/driver.go:182,197` 的另一个接收者 `*Driver.notify`，与 `intake` 无关）。
- `takeNotices`（`loop.go:1196`）：仅被他自己的测试引用。
- `noticeEntry` / `in.notices`：仅 `loop_test.go:751/796/830/849/969` 断言"缓存是空的"。
→ **测试在为一段不存在的功能保活。** 最小修法：`admitNotice` → `recordNotice`；删 `notify`/`takeNotices`/`noticeEntry`/`in.notices` 与对应的 5 处测试断言。

**❓ Q16** - **`Cause() → Turn.Failure(string) → failureNoticeText(string)` 三层搬运是否失当？** 建议判"软阻碍"。

➡️ **接受。** 三次字符串化、两次 `TrimSpace`：
- `api/model/terminal.go:203` `Cause() error`
- `loop.go:293-296` `strings.TrimSpace(cause.Error())` → `Turn.Failure string`
- `host.go:1055` `strings.TrimSpace(cause)` → 再截断
- 平行的第四条路：`rl/produce.go:114-118` `AttemptError(outcome)` 又自己 `cause.Error()`，**不 TrimSpace**。
→ 同一条 provider 错误，在「用户看到的」「事实 meta 记的」「训练记录记的」「观测行记的」四处**可能有四种文本**（TrimSpace 与否、截断与否）。这不是 bug，是**没有单一文本化点**。
可辩护的部分：`Turn` 是跨包 API，用 `string` 避免把 `error` 引到宿主（P-03 的裁定）。**但至少该有一个 `model.CauseText(outcome)`**，让 loop / host / rl / observe 四处都调它。

**❓ Q17** - **症状测试与 03.x 断言的重叠，维护成本到底多大？** 我建议给数字而不是感觉。

➡️ **接受用数字裁定。**
- 变更总量：**39 文件 / +2603 / −218**。
- 其中测试 **+1892/−95（72.7%）**；症状测试本身 **198 行**（`smoke_test.go:3220-3417`）。
- 域测试：`failure_notice_test.go` 448 + `host/quiet_receipts_test.go` 121 + `execution/quiet_receipts_test.go` 137 + `execution/progress_notification_test.go` 133 = **839 行**。
- 重叠点具体是 **`ok`**：`grep -rn '"ok"' --include="*_test.go"` = **37 处**，跨 12+ 文件。若契约文本改成 `ok (accepted)`，这 37 处里所有"断言 tool result 等于 ok"的都要改。
- **裁决：可辩护，但设一条纪律**——症状测试只准断言"用户可见面"（outbound 文本 + 模型实际收到的请求消息），**不准断言内部字段**。现有三条测试**已经这么写了**（`symptom-anchors` 的「受理静默症状锚」Requirement 正文，spec:11 明说"只允许读 fakeChannel 的 outbound 事件与脚本 provider 记录的请求消息"）。这条纪律是本变更的**正面资产**，应写进 `tests/` 的 README 或 `CONTRIBUTING`，否则下一个人会加内部断言。

**❓ Q18** - **`tool/exec` 的 `kindSchema` 描述与 `Enum` 自相矛盾，这算抽象失当吗？** 建议判"软阻碍（小，但是硬矛盾）"。

➡️ **接受。** `tool/exec/tool.go:127-128`：
```go
Enum:        []any{KindTmux},
Description: `the execution resource family: "tmux" is the only family this tool serves; leave it empty to select tmux, and any other family is refused by name`,
```
- `Enum: ["tmux"]` 意味着**模型不能传空**（JSON Schema enum 不含 `""`）；描述却说"留空以选择 tmux"。
- `tmux_executor.go:367` 确实接受空串——所以**服务端宽容、schema 严格、描述又教留空**，三者不一致。
- 更糟：`exec_test.go:730-742` 的 `assertExecKindNamesTheOnlyFamily` **同时断言** `Enum==[KindTmux]` **和** `strings.Contains(Description,"leave it empty")` ——**把矛盾钉进了测试**，未来想修必须同时改两行。
- 裁决：**软阻碍**。修法二选一（需产品裁定）：(a) enum 加 `""`，描述保留；(b) 描述删"leave it empty"，executor 拒绝空 kind。倾向 (a)（向后兼容既有调用）。

**❓ Q19** - **`Turn.Failure` 是新增的公开 API 字段，它值这个价吗？**

➡️ **可辩护。** 它替代的是"宿主自己从 stream 里再判一次失败"（P-03 明确要避免的二次判定）。字段有完整 doc（`loop.go:181-185`），语义是"最新决策的归约记录的原始原因，未记录则空，绝不发明"。**这是本变更里抽象做得最干净的一处**：一个字段，一条注释，一个来源。唯一可议的是 Q16 的"没有单一文本化点"，不是字段本身。

---

### D. 失败语义盲区

**❓ Q20** - **"无原因静默"真的存在吗？** 主代理债项 4 说存在。我建议**拆成三条，只有一条是新盲区**。

➡️ **接受拆分。** 逐条实证：

| 路径 | 触发 | 用户看到 | 运维看到 | 定性 |
|---|---|---|---|---|
| **(a) `failed` + `Cause()==nil`** | `failNilStream`（`terminal.go:349-354`）只置 `KindFailed`+`AlertNilStream`，**不设 Err** → `Cause()==nil` | **无**（`host.go:1037` DEBUG） | `AlertNilStream` 告警 | **契约内**：`failure-notice/spec.md:53` + 场景（`:64-70`）明文"失败但原因为空 MUST NOT 渲染空 `error` 字段；该缺口 MUST 由归约自身的告警可观测，MUST NOT 由失败告知编造一句兜底原因"。设计选择正确，**可辩护**。 |
| **(b) `Drive` 返回 err**（occupy/borrow/boundary/dispatch record 失败） | 见 `host.go:644-648` | **无**——注意 `deliverFresh` 在 `if err != nil { return }` **之后**，**整段跳过** | `assemblyWarn("…ended by error…")` | **新盲区**：spec 只谈"终态 failed"，从未谈"Drive 直接报错"。用户侧完全静默。 |
| **(c) `succeeded` 但无正文** | `host.go:1040` | 无 | DEBUG | **契约内**（`:74-80`），可辩护。 |

→ **主代理的债项 4 只覆盖了 (a)，而 (a) 是设计过的**；(b) 才是没人裁过的洞。**这是我对主代理结论的实质修正。**

**❓ Q21** - **(b) 该不该修？** 我建议：**修，但不是这个变更的活**。

➡️ **接受。** 修法最小：`host.go:644-648` 在 `err != nil` 分支里，若 `turn.Terminal == KindFailed || turn.Terminal == KindUnknown`，用 `sender.Send` 发一句**具名**的宿主错误（区别于 provider 错误）。但这会破 `failure-notice/spec.md:9` 的"MUST 只认 `failed` 终态；宿主/协议错误 MUST NOT 触发"——**要改 spec**。所以：立单，不塞进本变更。

**❓ Q22** - **部分文本丢弃（`failed` 时的部分 assistant 正文）可辩护吗？**

➡️ **可辩护。** D8.3 的理由（"部分输出被当作完整答复是更重的误导"）站得住；且用户收到的是**真实原因**而不是"出错了"，信息量是净增的。**训练侧不受影响**：`rl/produce.go:82-104` 的 `AttemptOf` 把 `outcome.Body` 原样入 `AttemptRecord.Response`，部分正文仍在训练记录里。→ 用户少看一段、训练多看一段，方向正确。

**❓ Q23** - **`cancelled` 静默，以及对训练采集的影响：这是"服务端取因果边"的合理结果，还是 train-serve skew？**

➡️ **可辩护，但要点名一处结构性不对称。**
- `cancelled` 静默是契约（`failure-notice/spec.md:9`），且取消通常是用户主动（`/stop`），无需告知 → 可辩护。
- **真正的不对称**：`AttemptRecord.Error`（`rl/produce.go:100`）**有**失败原因，而模型可见的 transcript **没有**（`failure-notice` 明令 fact 无 Message，`host.go:1072-1089` 照做）。也就是说：**训练数据里有"这次失败了、原因是什么"的标签，但没有任何一条模型可见的上下文教模型"失败时该怎么对用户说话"**。服务端因果边（attempt.Error）与模型可见面（transcript）在产品上分叉。
- 这**不是本变更的缺陷**（本变更只是把这条分叉写清楚了），但它是**训练采集必须记录的前提**：任何"失败告知"类的行为，都不能指望从 transcript 学出来，只能靠 reward/SFT 从外部注入。建议写入 `findings.md` 的"现实对齐"一节，避免下一波把它当回归。

---

### E. 反向评估（净账）

**❓ Q24** - **这份变更移除的复杂度，够不够抵它新增的复杂度？**

➡️ **抵得过"行为复杂度"，抵不过"规范复杂度"。** 分开记：

**移除（真实收益）**
1. **受理回执文案族**：`accepted <taskID> (running): …` → 裸 `ok`。消灭了"回执里带状态词"这一整类诱导（模型读什么说什么）。`loop.go:1547-1565` 一个常量收口。
2. **`[send_ok]` 逐条回执** → 裸 `ok`（`host.go:1877`）。
3. **progress 的 park/take 路径**：`in.notices` 缓存不再被读（`loop.go:1182-1205` 的注释明确"no decision of a range reads this cache"）。
4. **示例层罐头失败话术**：`examples/wechat-bot/wechat.go` `−2` 行（`9cc4b20`），失败文案只剩框架一处（`failure-notice/spec.md:91-92` 的"恰一条"）。

**保住（没有误伤）**
- 通知身份四元组与 `NoticeFactKey` 的幂等提交；6 态终局归约单点；重试内闭环（"不干预"裁定）；`delivery-dispatch-receipts` 的内部回执总线；`failure-polarity` 的终局判定。**一个都没动。**

**新增（成本）**
- 生产 Go **+365/−92（净 +273，8 文件）**。
- 测试 **+1892/−95**。
- 跟踪文档/合同 **+118/−25**（含 `ci.yml` 45、`docs/api` 42、主规格 28、golden 6）。
- 未跟踪规范散文 **410 行**（362 嵌套 + 48 orchestration）。
- 1 个新 API 字段、1 个新 fact 形态（`trigger_source=host`）、1 个新 CI job、**1 簇死代码**（N2/Q15）、**1 处未提交的棘轮上调**（N5）。

**净裁决**：**净增**。但这笔净增买到了"三条用户原话 → 三条可红可绿的测试 → 三条机械钉住的行为"，这是**可观测性的真实增值**，不是纯负债。**真正该警惕的不是"增"，是"增在生产者为 0 的路径上"**（N2：progress 机制 471 行，生产者 0）。

**❓ Q25** - **下一波要不要设一条"增的准入线"？** 我建议：**设，且只设一条**。

➡️ **接受。** 准入线：**任何新增规范条款，必须能指名一个生产侧发射点或一个用户可见面；指不出的，进 `findings.md` 的"待未来"，不进 `specs/`。**
- 按这条线复盘：`quiet-receipts` 的 5 条 Requirement 里，"progress 只落 fact"这一条的发射点在**HEAD 上为 0** → 它本该是"待未来"，而非"已交付"。
- 反面参照：`failure-notice` 的每条都有发射点（`deliverFresh`）和用户可见面（sender），**全部合规**。
- 这条线不需要门，只需要写进下一波的 `ORCHESTRATION.md`。

**❓ Q26** - **"回滚即红"是否足以证明价值？**

➡️ **是，且这是本变更方法论上最强的一点。** `tasks.md` F3 记录了三条逐字符的还原/复原（blob == HEAD、sha256、diff 0），G1/G3 有判别力，**G2 明确记载"无判别力"并改用另一红锚**——**如实报告了一次失败的红证**。这在同类变更里罕见，应当保留为模板。

---

### F. 与既有能力的边界（D7 是否可执行）

**❓ Q27** - **`design.md:58-63` 的 D7 三对边界声明，是可执行的，还是只是一段散文？**

➡️ **是散文，不是门——但三对里有 1.5 对已被测试覆盖。** 逐对核：

| 边界对 | D7 声明 | 机械/测试证据 | 判定 |
|---|---|---|---|
| `delivery-dispatch-receipts` ↔ `quiet-receipts` | 消费者不同、互不改写、send 失败触发面不变 | `tests/integration/host/quiet_receipts_test.go` + `execution/quiet_receipts_test.go`（send 成功→`ok`；失败面用 `[send_denied]` 保留具名原因） | **已钉住（半）**：模型可见文本面有测试；"内部回执总线的触发面不变"**没有**跨层测试（`delivery_receipt` 在 `examples/wechat-bot/wechat.go`，与根 `host.go` 不同层）。 |
| `delivery-dispatch-receipts` ↔ `failure-notice` | 前者注入 inbox 待下一回合；模型真失败时无下一回合，故由后者直投 | **无** | **只靠论证**。可辩护（层不同、生命周期不同），但**若某天宿主在 failed 时仍排了一个随动回合，两者会同时触发 = 用户收到两条**。 | 
| `failure-polarity` ↔ `failure-notice` | 前者定终局，后者消费终局 | `failure_notice_test.go` 的终态断言 + `failure-polarity` 自己的测试 | **已钉住（强）**。 |

**❓ Q28** - **交界会随演进漂移吗？有没有机械门钉住？**

➡️ **会漂移，且只有一处有门。**
- **有门**：CI 症状 job（`scripts/test_check_runtime_workflow.py` 钉 job 形态、测试名、选择器宽度）+ `check-openspec.sh`（`openspec validate --all --strict`，主规格失败即红）。
- **无门**：①"模型面声明必须与回执语义一致"（→ N1 的成因）；②"能力晋升"（→ N6）；③"exec 观察者不得携带 Round"（→ N2）；④"`failure-notice` 与 `delivery-dispatch-receipts` 不得同时触发"（→ Q27 第 2 行）。
- **最有价值的一条门**：把 N1 变成**负锚**——`tool/subagent/tool_test.go` 里断言 `Description MUST NOT` 含 `receipt carries the task identity` / `progress`。一行 `strings.Contains` 负断言，就能把"说明书与实现不一致"这类最隐蔽的漂移钉死。

**❓ Q29** - **`persistent-event-loop` 主规格新增的 28 行，会和 4 个能力规格重复表述而产生漂移吗？**

➡️ **会，但目前方向单一（主 → 细）。** 主规格的三场景是"边界级"，能力规格是"条款级"；只要主规格不写实现细节（它现在没写），漂移是可接受的。**风险点**：主规格的 Scenario 文案被 golden 引用（`scenario 69→72`），改一个字要同步 golden（`golden/runtime/scenarios.json` 本变更动了 3 行）。这是一条**已有的**同步负担，本变更把它从 69 增到 72，不构成新问题。

**❓ Q30** - **归档动作本身会不会破坏边界？**

➡️ **会，且这是最后一道口子。** F4 只写"归档 + 导览"。若照 F4 执行：
- 4 个能力规格（3 真 + 1 伪）**不晋升** → `openspec/specs` 仍 126。`check-openspec.sh`（`openspec validate --all --strict`）校验的是"现存的 specs + 活跃 change 的 delta"，它**校验不到"本该晋升却沉睡在 archive 里的条款"**——归档那一刻门就是绿的，且永远绿；
- `symptom-anchors` 的"不得缩水"契约随 archive 沉底，无人再约束；
- `design.md` 的 D8.4 与 spec 的矛盾（N3）随 archive 一起被封存，**未来读者只能看到 spec 那一半**。
→ **F4 必须加一行"能力晋升清单"**，并在晋升时**同时**决定 `symptom-anchors` 的去向。

---

## 4. 裁决表

评级口径：**硬阻碍** = 归档前必修，否则变更声明不成立或会立即误导；**软阻碍** = 应尽快立小单，可归档；**可辩护** = 记录即可；**无碍** = 不是问题。

| # | 条目 | 裁决 | 是否行动 | 依据 |
|---|---|---|---|---|
| A1 | `quiet-receipts` 新能力 | 可辩护 | 否 | 消费者（模型 vs inbox）不同 |
| A2 | `failure-notice` 新能力 | 可辩护 | 否 | 判定/告知分离 |
| A3 | `tool-schema-clarity` 新能力 | **软阻碍** | 是（下一波优先） | "模型面声明"第三次开新家（failure-polarity:66 已有先例） |
| A4 | `symptom-anchors` 占 capability 目录 | **软阻碍** | 是（归档前） | 自称"不新增产品能力"却占目录；晋升/不晋升两难 |
| A5 | +1 主规格 requirement | 可辩护 | 否 | 唯一权威面需要它；但 4 处复述 |
| A6 | `trigger_source="host"` 新值未登记 | **软阻碍（小）** | 是 | `FACT-VOCABULARY.md` 无 `trigger_source` 行 |
| A7 | +1 API 字段 `Turn.Failure` | 可辩护 | 否 | 替代二次判定，doc 完整 |
| A8 | +1 CI job | **无碍** | 否 | 本次做得最对的一处，已被 workflow 自测钉住 |
| B1 | `Round>0` 判据 | **软阻碍** | 是（归档前，成本 <30 行） | 生产恒 false、无测试钉零值、注释自认"类别复用" |
| B2 | `FactKey` 冒号无转义 | **软阻碍** | 是 | fail-open 反向泄漏；`noticeKeyPrefix` 已有安全模式可抄 |
| B3 | 2 KiB 截断 | 可辩护 | 否 | D8.2 + rune 边界正确 |
| B4 | `deliverFresh` 正文优先 | 可辩护 | 否 | 注释 + `TestFailureNotice` 覆盖 |
| B5 | `ok` 双字面量 | 可辩护（小） | 否（建议加注释） | 语义不同，共享反而耦合 |
| B6 | `Seq` 恒 0 | **软阻碍（小）** | 是（注释级） | 四元组名不副实；`IssueNotice` 零调用者 |
| B7 | 症状测试依赖 outbound 计数 | 可辩护 | 否（建议把纪律写进 CONTRIBUTING） | 现有三条已只读用户可见面 |
| C1 | `admitNotice` 名实 + 死代码簇 | **软阻碍** | 是 | 零调用者 `notify`/`takeNotices`；测试为不存在的功能保活 |
| C2 | `Cause→Turn.Failure→text` 三层 | **软阻碍** | 是（下一波） | 4 处文本化、2 处 TrimSpace、1 处不 Trim |
| C3 | exec 伪装 `progress` | **软阻碍** | 是（下一波，需改 spec） | N2 |
| C4 | 维护面 39 文件 / +2603 行 | **软阻碍** | 否（设纪律，不返工） | 72.7% 是测试；症状测试纪律是正面资产 |
| C5 | "3 个新能力" vs 实际 5 个 spec 面 | 可辩护 | 否（口径问题） | proposal:34 列 3，另有 `symptom-anchors` + `orchestration` |
| D1 | `failed` 无 cause 静默 | 可辩护 | 否 | spec:64-70 明文，AlertNilStream 可观测 |
| D2 | `Drive` 返回 err 时 `deliverFresh` 整段跳过 | **软阻碍** | 是（立单，不塞本变更） | `host.go:644-648` 用户侧全静默 |
| D3 | 部分文本丢弃 | 可辩护 | 否 | D8.3；训练侧保留在 `AttemptRecord.Response` |
| D4 | `cancelled` 静默 | 可辩护 | 否 | 契约 + 用户主动取消 |
| D5 | 训练因果边与模型可见面分叉 | 可辩护 | 否（记录） | `AttemptRecord.Error` 有、transcript 无 |
| E1 | 移除 3 文案族 + 1 park 路径 | **正面** | — | 真实降复杂度 |
| E2 | 净账 | **净增（可辩护）** | 否 | +273 生产行买可观测性；病在"为 0 生产者写机制" |
| F1 | D7 是散文非门 | **软阻碍** | 是 | 三对里 1.5 对无测试 |
| F2 | 模型面声明无门 | **硬阻碍**（= N1） | 是 | 一行负断言即可钉死 |
| F3 | 能力晋升无门且未列 F4 | **软阻碍**（= N6） | 是（归档前） | `f3e00ec` 先例证明是手工步骤 |

**统计**（共 30 行）：硬阻碍 **1**（N1）；软阻碍 **13**；可辩护 **14**（含 E2 净账）；无碍 **1**；正面 **1**。

---

## 5. 最小行动清单（文件级，**不实施**）

> 全部为最小改动；按"归档前 / 下一波"分组。**本审计不实施任何一条。**

### 归档前（3 条，缺一即不应归档）

1. **修 N1（模型面谎言）**
   - `tool/subagent/tool.go:125`：删 `"the receipt carries the task identity, and the progress and terminal of that task arrive as its notifications. "`，替换为 `"the receipt is a bare ok; the terminal of that task arrives as a notification of its own follow-up turn. "`
   - `tool/subagent/tool_test.go`（`TestSubagentToolDeclaration` 内）：加负锚——`strings.Contains(declaration.Description, "receipt carries the task identity")` 必须为 false；`"progress"` 必须为 false。

2. **修 B1（`Round>0` 陷阱变路标）**
   - `runtime/agent/loop.go:1290-1297`：注释首句加「这是类别复用的补丁，不是进度语义的一部分；exec 观察者若携带轮次，本判据会静默失效」。
   - `tests/integration/execution/progress_notification_test.go`：加子测试，断言 exec 观察因子的 `task.Notice.Round == 0`，且其材料**进入**下一次请求（正锚），而 `Round>0` 的 progress 事实**不进入**（负锚）。

3. **修 N5 + N6（归档前置）**
   - `scripts/doc_budget.json`：二选一——(a) 提交 `22192→22220` 的上调与 `rebaseline_reason`（并补文件尾换行）；(b) 把主规格 28 行压到 ≤0 净增后回退基线。**不允许**"工作树绿、HEAD 红"地归档。
   - `tasks.md:49`（F4）：追加一句"晋升 `quiet-receipts` / `failure-notice` / `tool-schema-clarity` 到 `openspec/specs/`；`symptom-anchors` 按 §5-1 并入前两者后删除目录"。

### 下一波（6 条，各自独立）

4. **修 N3（规范自相矛盾）**：`design.md:70` 与 `changes/02-failure-delivery/specs/failure-notice/spec.md:31` 二选一。**需产品裁定**（模型能否回答"刚才怎么了"），不是工程能定的。
5. **修 N2/C3（exec 伪装 progress）**：`runtime/task/registry.go` 加 `NoticeObserved`；`host.go:768-769`、`loop.go:1298-1308`、`quiet-receipts` 规格同步。
6. **修 N4（meta 不截断）**：`host.go:1083` 对 `failure_reason` 复用 `failureNoticeMaxBytes` + 截断标注。
7. **修 B2（冒号转义）**：`runtime/task/fact.go:18-20` 改长度前缀（抄 `loop.go:424-426` 的 `noticeKeyPrefix` 模式）。
8. **修 C1（死代码）**：删 `runtime/agent/loop.go:1182-1205`（`notify`/`takeNotices`/`noticeEntry`/`in.notices`）及 `loop_test.go:751/796/830/849/969` 的对应断言；`admitNotice` → `recordNotice`。
9. **修 D2（Drive 错误用户静默）**：`host.go:644-648` 的 err 分支加具名直投；同步改 `failure-notice/spec.md:9` 的触发面。

### 记录即可（不动代码）

10. `findings.md` 增"现实对齐"条目：进度机制的生产者为 0（N2）；训练因果边与模型可见面分叉（Q23）；`notice_input_without_projection` 的跨 range 泄漏当前不可达（N7）。
11. `CONTRIBUTING.md` / `tests/README` 增一条纪律：症状测试只准断言用户可见面（outbound + 请求消息），不准读内部字段（Q17）。

---

## 6. 已排除的候选（我搜索过、但**不成立**，附排除理由）

| # | 假说 | 排除理由 |
|---|---|---|
| E1 | **CI 的 `^TestLocalSmokeSymptom` 前缀选择器会零匹配腐烂**（"测试改名了但 job 还绿"） | `scripts/test_check_runtime_workflow.py` 把 `SYMPTOM_PARENTS` 三个测试名钉死，并与 `smoke_test_names()` 做**双向**交叉核对（多选/漏选皆红）、禁 `if:`、禁 `continue-on-error`、`if-no-files-found: error`。**这是全仓机械门做得最扎实的一处。** |
| E2 | **`send_ok` / 旧受理文案在代码或文档里残留** | 全仓 `grep -rn "send_ok\|accepted .*running"` 无生产命中；`symptom-anchors` spec 还把旧词表列为**负向断言清单**（`accepted`/`(running)`/`background task`/`will arrive as a notification`/`reports progress`/`[send_ok]`/`delivered to`）。 |
| E3 | **`delivery-dispatch-receipts` 与 `failure-notice` 在同层冲突**（"发送失败必回执 ERROR" vs "告知被拒只记警告"） | **层不同**：前者在 `examples/wechat-bot/wechat.go`（应用层，`delivery_receipt` 血统），后者在根 `host.go`（框架层直投）。D7 的论证成立。**但**"若 failed 时仍有随动回合，两者会同时触发"这一**未来**风险保留（Q27 第 2 行），不计为本变更缺陷。 |
| E4 | **`tool-schema-clarity` 与 `failure-polarity` 完全重复** | 主题不同（资源参数形态 vs 退出码透传引导）。真问题是"模型面声明无归属"（A3），不是"重复"。 |
| E5 | **`Turn.Failure` 是抽象失当（应传 `error`）** | `Turn` 跨包暴露给宿主，`error` 会把归约内部类型泄到 API；P-03 的裁定成立。可辩护（C2 只针对"无单一文本化点"）。 |
| E6 | **症状测试与 golden 场景重复**（`scenario 69→72`） | 两者取数面不同：症状测试读 outbound + 请求消息（黑盒），golden 读投影/事实（白盒）。**互补，非重复。** |
| E7 | **`observeExecState` 的 goroutine 泄漏**（`h.execWG` + `hostShutdownGrace`） | 有 `execWG` 收口、`h.closed` 早退、`hostShutdownGrace` 超时；`host.go:835` 还有"资源在 close 后仍被持有"的 WARN。**既有机制健全**，非本变更引入。 |

---

## 7. 对主代理四个债项的独立裁定

| 债项 | 主代理结论 | 我的裁定 | 差异 |
|---|---|---|---|
| 1 `pipelineProgress` `Round>0` 应显式化 | 成立 | **成立，且应加重** | 生产恒 false、无测试钉零值、注释自认类别复用；"显式化"不够，要**当路标 + 加测试**（B1） |
| 2 `admitNotice` 名实不符 | 成立 | **成立，且更糟** | 下面挂着零调用者的 `notify`/`takeNotices` 死代码簇，测试在为不存在的功能保活（C1） |
| 3 `tool-schema-clarity` 偏细 | 成立 | **成立但诊断要改** | 不是"细"，是"模型面声明第三次开新家"（`failure-polarity:66` 已有先例）；**不建议现在合并**（A3） |
| 4 失败无原因静默盲区 | 成立 | **部分推翻** | `failed` + 无 cause **是设计过的**（spec:64-70 + AlertNilStream），可辩护；真正的洞是 **`Drive` 返回 err 时 `deliverFresh` 整段被跳过**（`host.go:644-648`），用户侧全静默（D2） |

---

## 8. 给编排者的一句话

**这份变更该归档，但不能按现在的 F4 归档。** 三条动作（N1 的描述、B1 的测试、N5/N6 的棘轮与晋升）做完，它就是一份**净正**的变更；不做，它会在归档那天把"回执=ok"这条最重要的语义，留成一个只在 `runtime/agent` 里成立、在 `tool/subagent` 的说明书中被否掉的**局部真理**。
