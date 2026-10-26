# 03-tool-schema 子变更提案

父变更：`event-semantics-boundary`（子域 `03`，W1，一级 `2.3`）。本域只改 `exec_run`/`exec_release` 两条声明对 `kind`/`resource` 的描述与机读约束，使模型第一轮就能构造合法键；不改校验器、不改执行行为、不改主规格、不改根装配。

## Why

真机实证：模型为同一个 exec 调用**三连猜错**——`kind:"shell"`、`kind:"bash"`、`resource:"tmux:local"`（含冒号，非法会话名），第四次 `kind:"tmux", resource:"wx-fetch"` 才成功。三次失败各自消耗一个回合，用户看到的是"工具一直报错"。

原因不是模型不聪明，是 schema 没给可用信息。当前两条 Declaration 的参数描述（`tool/exec/tool.go:124-125`、`:142-143`）是：

- `kind`：`"the resource family the key identifies within"`
- `resource`：`"the canonical key of the execution resource"`

两句都是抽象名词短语：没有枚举、没有格式、没有示例、没有长度。而实际校验规则是硬的（`tool/exec/tmux_executor.go:366-379` `sessionNameOf`）：`kind` 只接受空或 `"tmux"`（`tool/exec/tmux.go:14` `KindTmux`）；`resource` 必须是合法 tmux 会话名——非空、长度 ≤ `tmuxSessionMaxName`（64）、字符集 `[A-Za-z0-9_-]`。模型看不到这些，只能按训练分布猜（猜的是 shell 家族名），猜错三次是必然结果。

一句话：**合法取值存在但没写进模型能读的地方**。改描述即改模型可见面，是本变更里成本最低、收益最直接的一叶。

## What Changes

- **3.1 `exec_run` Declaration**：`kind` 写死枚举（唯一值 `tmux`，并说明留空即选默认 tmux）＋ `resource` 写出会话名格式（1–`tmuxSessionMaxName` 字符、`[A-Za-z0-9_-]`、含合法示例）。机读侧同时落 `Enum`/`Pattern`，人读侧描述与机读约束由**同一常量**生成。
- **3.2 `exec_release` Declaration**：同型同步。两条声明共用一份词表（同一常量、同一 Pattern 串），禁止各写一份字面量；3.2 与 3.1 同文件簇（`tool/exec/tool.go`、同一测试文件），必须串行。
- **不做的**：不放宽 `sessionNameOf`/`validSessionName`（不许因描述难写就接受冒号或路径）；不改 `decl.go` 的 `ResourceKey.Kind` 注释与 `docs/api/` 生成页（改注释会牵动 godoc 再生与文档门，与"让模型读懂"无关）；不动 `RunTool.Call`/`ReleaseTool.Call` 任何行为；不新增导出符号。

## Capabilities

### New Capabilities

- `tool-schema-clarity`：exec 资源参数的可枚举、可校验描述——`kind` 的合法家族、`resource` 的会话名字符集与上限、描述与校验器同源不漂移、两条声明不得各写一份。delta 见 `specs/tool-schema-clarity/spec.md`。

## 边界与依赖

### 1. 依赖（接口常数 vs 实现依赖）

- **接口常数依赖**：`exec.KindTmux`（`tool/exec/tmux.go:14`，值 `"tmux"`）、`tmuxSessionMaxName`（`tool/exec/tmux_executor.go:35`，值 64）、`validSessionName` 的字符集判定（`tool/exec/tmux_executor.go:376-394`）。这三个是"真值源"：描述与 `Pattern` 必须由它们生成或在断言中与它们对齐，MUST NOT 在描述里另写一份字面数字。
- **无行为依赖**：本域不依赖 00/01/02 域的任何行为落地，也不依赖 04 域合同修订；可在 W1 与 01 余量、02 域并发（文件簇零相交）。
- **域内依赖**：`3.2 ← 3.1`（同文件单写，后写者先读回 3.1 的产物）。

### 2. 被依赖方

- 一级 `F2` 全量门禁（`go test ./... -short` 会跑到本域新增断言；CI `test` job 第 `Test (short)` 步已覆盖 `./tool/exec`，无需新增 CI job）。
- 无其它域消费本域产物：04 域不读工具描述，01/02 域不动 `tool/exec`。

### 3. 接口面

- 变更面：`RunTool.Declaration()` 与 `ReleaseTool.Declaration()` 返回值的 `InputSchema.Properties["kind"]`（新增 `Enum`、改写 `Description`）与 `Properties["resource"]`（新增 `Pattern`、改写 `Description`）。
- **不新增** public API、不新增包级导出符号、不改 `trpctool.Schema` 消费方式（`Enum []any` 与 `Pattern string` 是上游 `github.com`/`trpc.group` 既有字段，见 `tool/tool.go:67,78`）。
- 证据面：`changes/03-tool-schema/evidence/03.*-summary.json`（摘要 + `log_ref`，原始日志写 `${TMPDIR:-/tmp}/event-semantics-boundary/03.*/`）。

### 4. 禁止事项

- MUST NOT 为了让描述"好写"而放宽 `validSessionName`、`sessionNameOf` 或 `tmuxSessionMaxName`；真值源只有一个方向：校验器是权威，描述向它对齐。
- MUST NOT 在两条 Declaration 里各写一份字面量（`"64"`、`"[A-Za-z0-9_-]"`、`"tmux"` 均须来自同一常量或用同一表达式生成）。
- MUST NOT 触碰 `tool/exec/tool.go:203` 的 `return fmt.Sprintf("executed %s: %s", key, output), nil` —— 该行是变异门 `MUT-CAND-01.10-alive-process-complete` 的 `old` 锚（`scripts/check_event_integrity_mutations.py:564-577`），改它等于让变异门在 W1 就红。
- MUST NOT 改 `tool/exec/decl.go` 的 `ResourceKey.Kind` 文档注释（`docs/api/github_com__SpellingDragon__tagent__tool__exec.md:498` 是它的生成物，改注释要连带 `gen_godoc.sh`，超出本叶）。
- MUST NOT 用运行时执行行为（真跑 tmux、真起进程）验证本域——本域验收锚是 Declaration 文本/机读字段的单元断言，不需要 tmux 二进制。

## Impact

- 代码：`tool/exec/tool.go`（两处 Declaration 的属性描述 + 必要的包内常量）；测试：`tool/exec/exec_test.go`（新增两条断言测试）。
- 合同：无（本域不动 `golden/`、不动 `openspec/specs/`）。
- CI：无新增 job；既有 `test` job 的 `go test ./... -short -count=1` 覆盖本域测试。
- 风险：`Enum`/`Pattern` 属上游 schema 字段，模型侧是否强制校验不由本仓决定（多数 provider 只透传为提示）。因此**描述仍是主承载，机读约束是加固**；两者都改，任一失效仍有另一层。见 `design.md` D4。
