# 03-tool-schema 任务

父变更：`event-semantics-boundary`（子域 `03`，一级 `2.3`，W1）。本文件登记 2 个叶任务（3.1、3.2）。本域只改 `tool/exec/tool.go` 两条 Declaration 对 `kind`/`resource` 的描述与机读约束（`Enum`/`Pattern`），测试落在既有 `tool/exec/exec_test.go`；不改校验器、不改执行行为、不改主规格、不新增导出符号。

**叶子与文件簇**：3.1 与 3.2 同动 `tool/exec/tool.go` 与同一测试文件（触碰集相交）→ **严格串行 `3.1 → 3.2`，禁止并发**（R21 热点合单的串行分支）；两叶与 01/02/04 域文件簇零相交，可与 W1 其它单并发。

**验收面冻结**：本域验收锚＝Declaration 文本/机读字段的**单元断言**（一级与 proposal 已批准此形态），MUST NOT 在执行期以"再加运行时测试更稳"为名扩面，也 MUST NOT 以"实跑 tmux 成功"替代或追认。

**原始日志**：写 `${TMPDIR:-/tmp}/event-semantics-boundary/<leaf>/`（CI 为 `${{ runner.temp }}/event-semantics-boundary/<leaf>/`）；change 内 `evidence/<leaf>-summary.json` 只留摘要与 `log_ref`。

## 任务

- [x] 3.1 **改写 `exec_run` 的 `kind`/`resource` 描述并落机读约束**：在 `tool/exec/tool.go` 的 `RunTool.Declaration()`（`tool.go:113-132`）内，把 `kind` 属性改为 `Enum: []any{KindTmux}` + 描述点名唯一家族并说明留空即选默认；把 `resource` 属性改为 `Pattern: fmt.Sprintf("^%s{1,%d}$", sessionNameCharset, tmuxSessionMaxName)` + 描述给出"1-<max> 字符 / `[A-Za-z0-9_-]` / 示例 `wx-fetch`"。描述与 Pattern MUST 由 `KindTmux`、`tmuxSessionMaxName`、包内新增小写常量（`sessionNameCharset`、`sessionNameExample`）生成，MUST NOT 写字面量 `64`；MUST NOT 新增导出符号；MUST NOT 触碰 `tool.go:203` 的 `executed %s: %s` 行（变异门 `MUT-CAND-01.10-alive-process-complete` 的 old 锚）；MUST NOT 改 `decl.go` 注释或 `docs/api/`。测试加在 `tool/exec/exec_test.go`（`package exec`）新增 `TestExecRunDeclarationPinsTheResourceShape`，六个具名子测试。**验证**：`go test ./tool/exec -run '^TestExecRunDeclarationPinsTheResourceShape$' -count=1 -race -json -timeout 2m > "${TMPDIR:-/tmp}/event-semantics-boundary/03.1/single.jsonl" 2>&1`；原样保留退出码，不追加 `echo`/`|| true`。**进程级复核（三轮，非十轮）**：同一命令独立执行 3 次写入 `run-$i.jsonl`，逐轮保留原始退出码——纯文本断言无并发/时序，十轮重复不增加判别力，以三轮＋同包全量回归替代（见 design D3 与一级 F2）。**回归**：`go test ./tool/exec -count=1 -race -timeout 5m`（证明未破坏同包既有测试）。**正锚**：`TestExecRunDeclarationPinsTheResourceShape/names_the_only_family`、`/names_the_session_name_format`、`/pattern_agrees_with_the_validator`（同源等价：`re.MatchString(name) == validSessionName(name)` 覆盖 `wx-fetch`/`a`/上界/上界+1/`""`/`tmux:local`/`a b`/`a.b`/`a/b`/`a:b`）。**负锚**：`/abstract_phrase_gone`（Declaration 字符串不含 `the resource family`、`canonical key of the execution resource`）、`/prefixed_key_hint_absent`（不匹配 `(?:tmux|kind|resource)\s*:\s*[A-Za-z0-9_-]`）。**消费者锚**：`/production_preset_carries_the_same_text`——断言对象取自生产入口 `exec.NewPreset(manager, MonitorConfig{})`（装配点 `tools.go:269`），MUST NOT 用手工 `NewRunTool` 构造的 Declaration 冒充。**前置**：无（独立文件簇）。**写集**：`tool/exec/tool.go`（仅 `RunTool.Declaration()` 函数体 + 包内小写常量）、`tool/exec/exec_test.go`、`evidence/03.1-summary.json`。

- [x] 3.2 **同步 `exec_release` 并钉住两条声明同词表**：在 `tool/exec/tool.go` 的 `ReleaseTool.Declaration()`（`tool.go:134-148`）内做 3.1 同型改动（同 `Enum`、同 `Pattern`、同描述模板，复用 3.1 的包内常量，MUST NOT 复制第二份字面量）；测试新增 `TestExecReleaseDeclarationPinsTheResourceShape`，除 3.1 的六个子测试（针对 release 声明）外增 `both_declarations_share_one_vocabulary`——`run` 与 `release` 的 `resource.Pattern` 字符串相等、`kind.Enum` 相等，不等即红。**验证**：`go test ./tool/exec -run '^TestExecReleaseDeclarationPinsTheResourceShape$' -count=1 -race -json -timeout 2m > "${TMPDIR:-/tmp}/event-semantics-boundary/03.2/single.jsonl" 2>&1`；原样保留退出码。**进程级复核（三轮）**：同命令独立执行 3 次写 `run-$i.jsonl`。**回归**：`go test ./tool/exec -count=1 -race -timeout 5m` 退出 0；`grep -c 'return fmt.Sprintf("executed %s: %s", key, output), nil' tool/exec/tool.go` 输出 `1`（变异门锚行未被碰）；`git status --porcelain docs/api/` 输出为空。**正锚**：`TestExecReleaseDeclarationPinsTheResourceShape/names_the_only_family`、`/names_the_session_name_format`、`/pattern_agrees_with_the_validator`。**负锚**：`/abstract_phrase_gone`、`/prefixed_key_hint_absent`、`/both_declarations_share_one_vocabulary`。**消费者锚**：`/production_preset_carries_the_same_text`——取自 `exec.NewPreset(...)` 返回的第二个工具（release），证明生产装配同时携带两条新声明。**前置**：3.1 完成（同文件单写，后写者先读回 3.1 产物）。**写集**：`tool/exec/tool.go`（仅 `ReleaseTool.Declaration()` 函数体）、`tool/exec/exec_test.go`、`evidence/03.2-summary.json`。

## 勾选纪律

- 父 selector 必须整体跑（`-run '^Test…$'` 一次跑全部子测试）；不得只列子测试名，也不得以父测试绿推断子场景已跑。改名必须同步任务行、`design.md` D3 与 delta 场景，否则零匹配空转而 `exit 0`。
- 负锚是勾选硬门：`abstract_phrase_gone`/`prefixed_key_hint_absent`/`both_declarations_share_one_vocabulary` 任一缺失、被 skip 或以编译失败呈现，整叶不勾选。
- 消费者锚必须是 `NewPreset` 产出的 Declaration；测试内手工构造对象的断言只能算正锚，不能算消费者证据。
- 勾选前把命令、原始退出码、`log_ref` 与三轮 `run-$i.jsonl` 路径写入本叶 `evidence/*-summary.json`；无凭据不勾选。
- 本域 MUST NOT 新增/修改 `golden/`、`openspec/specs/`、`.github/workflows/`、`scripts/`；越界即停报。
