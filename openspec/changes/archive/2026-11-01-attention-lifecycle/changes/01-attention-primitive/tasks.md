# Tasks: 01-attention-primitive

> 孙任务三要素：产出 + 关键约束 —— 验证：可复跑命令。verify 三锚随行冻结，
> 验收面被追认缩水 = 本叶重开。

- [x] T1 冻结事件类型与 meta 键并登记契约 —— 三枚 `TypeAttention*` 与五枚
      `MetaKeyAttention*` 进 types/meta/skeleton_assert，FACT-VOCABULARY 加登记行
      —— 验证（正锚）：`go build ./... && go test ./api/event/... -count=1` exit 0；
      `bash scripts/lint.sh` exit 0（编排者清偿后复验：doc 形态收一行意图、doc 预算 22267→22268 具名落锁、gen_godoc 重生成 69 文件）
      - 凭据（2026-11-01）：`go build ./...` exit 0；`go test ./api/event/... -count=1` → `ok github.com/SpellingDragon/tagent/api/event 0.527s`；
        三枚 `TypeAttention*` 落 `api/event/meta.go` 冻结类型块、五枚 `MetaKeyAttention*` 落冻结键块、
        `api/event/skeleton_assert.go` 两条冻结断言块各登记；`docs/FACT-VOCABULARY.md` 加第 10 族登记行。
        本单 Go 面已过 comment policy 严格口：`comment_policy -no-baseline assembly api runtime memory features tool internal cli`
        → `0 finding(s)`、exit 0。
      - **未勾原因**：`bash scripts/lint.sh` exit 1，两处红均不在本单可写文件内 ——
        ① comment policy 的两条 `test-doc-not-one-sentence` 落在 `tests/integration/host/attention_lifecycle_test.go`
        （本单禁改，红于改动前即存在）；② `docs/FACT-VOCABULARY.md` 新行使 doc 预算 22268 > 基线 22267（+1），
        基线锁步归编排者（本单禁改 `doc_budget.json`）。改动前后同跑一次 `bash scripts/lint.sh`：
        改前 EXIT=1、改后 EXIT=1，红点仍是同一处，`gofmt`/`modules`/`build`/`vet` 四段两轮皆绿。
- [x] T2 落链与读回两个方法 —— `Commit`/`Present` 走既有事实提交与链读，同身份
      question 幂等 —— 验证（正锚）：`go test ./runtime/task -run '^TestAttentionFactRoundTrip$' -count=1`
      - 凭据：exit 0 → `ok github.com/SpellingDragon/tagent/runtime/task 0.186s`，`-v` 下
        `--- PASS: TestAttentionFactRoundTrip`；重复提交回答同一 key 且链上仅 1 条事实（隔 1 秒再提交仍命中同一 key，链保留首次落链时刻），另 target 过滤为空。
- [x] T3 清除规则引擎（纯函数）—— answer 落链即清、读侧判过期、clearance 三值
      —— 验证（负锚）：`go test ./runtime/task -run '^TestAttentionAnswerWithoutQuestionRefused$' -count=1`；
      （正锚）：`go test ./runtime/task -run '^TestAttentionClearsOnAnswer$' -count=1`
      - 凭据：两条各 exit 0（`ok ... runtime/task 0.186s` / `0.184s`）→ `-v` 下
        `--- PASS: TestAttentionAnswerWithoutQuestionRefused`、`--- PASS: TestAttentionClearsOnAnswer`；
        另 `TestAttentionExpiresOnRead` 覆盖读侧过期与 clearance 三值（读侧判，无 goroutine、无定时器）；
        链上事实内容断言：question 正文以 user 消息落链、answer 携带裁决正文与裁决人（`event_summary` 行含 `by=`）。
- [x] T4 重启读回一致 —— 在场集全由事实推导，重建后一致 —— 验证（正锚）：
      `go test ./runtime/task -run '^TestAttentionSurvivesRestart$' -count=1`
      - 凭据：exit 0 → `ok github.com/SpellingDragon/tagent/runtime/task 0.181s`，`-v` 下
        `--- PASS: TestAttentionSurvivesRestart`；新 face 由同一批事实重建，在场集逐字段相等。
- [x] T5 概念预算自检 —— 对外接口 ≤2 方法，无内部结构导出 —— 验证（消费者锚）：
      `python3 scripts/gates/check_concept_budget.py --json` exit 0 且无新增超标
      - 凭据：exit 0，`ok: true`、`errors: 0`、`findings: 64`（`baseline_exempted: 64`，非 baseline 超标 0）；
        计数 `interfaces: 92`（含未导出的 `attentionChain` 2 方法，S1 窄口阈值 3 未触）、`structs: 635`
        （新增 `AttentionDetail`/`AttentionFact`/`AttentionQuestion`/`AttentionFilter`/`Attention` 各 ≤5 导出字段）。
        兜底：`go vet ./runtime/task ./api/event` exit 0；`gofmt -l` 四个白名单 Go 文件输出为空；
        `go test ./runtime/task -run '^TestAttention' -count=1 -v` 6 条 `--- PASS`。
- [x] T6 两条症状红锚落库且红着 —— `TestAttentionClearsApprovalOnAnswer` 读现状
      审批面（批准执行后目录与 Lookup 仍有残留即红）、`TestAttentionClearsNoticeOnWorkEnd`
      读现状 notice 投影（Close 后仍 owed 即红）；**断言语义改写须经编排者裁定并在
      台账留痕**（原条款「两锚此后不得改写断言语义」已按 F-5 改判：冻结的是改锚
      程序，不是断言文本）
      —— 验证（症状锚）：`go test ./tests/integration/host -run '^TestAttentionClears' -count=1`
      失败，失败输出贴入 ORCHESTRATION 台账
      - 凭据（落库当日红态）：两锚皆红——锚① 于 `attention_lifecycle_test.go:85`
        （Lookup 仍报 answered approval 为 approved）、锚② 于 `:164`（未消费通知
        Close 后以 phase "accepted" 挂着）；台账「2026-11-01 红锚落库」行
      - 凭据（改锚留痕）：锚① 经编排者裁定改读 `Pending` + `AttentionPresence`
        （台账 W2-22 行，改锚落 `4fc276d2`）；锚② 经编排者裁定改读 `ObjectNotice`
        相位（台账 W2-03' 行，改锚落 `d4aa4793`）
      - 凭据（基线复现）：`21ba86a3` + 立案版锚文本 → 两锚皆 FAIL（台账 W3-F3 行）
