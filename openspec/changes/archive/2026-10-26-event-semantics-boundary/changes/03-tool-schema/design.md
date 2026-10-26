# 03-tool-schema 设计（D1–D5，含被否方案）

## D1 描述文本契约（模型能读的最小充分信息）

两条 Declaration 的 `kind`/`resource` 描述按同一模板写，四要素缺一不可：

| 要素 | `kind` | `resource` |
|---|---|---|
| 合法取值 | `"tmux"` 与空串（并说明留空＝选默认 tmux） | — |
| 形态 | — | tmux 会话名（不是路径、不是 `<kind>:<key>` 复合键） |
| 字符集与上限 | — | `1`–`tmuxSessionMaxName` 字符，取自 `[A-Za-z0-9_-]` |
| 示例 | `tmux` | `wx-fetch` |

目标文本（执行时以实际常量内插，数值须来自 `tmuxSessionMaxName`，MUST NOT 写字面 `64`）：

- `kind`：`the execution resource family: "tmux" is the only family this tool serves (leave it empty to select tmux)`
- `resource`：`the tmux session name of the execution resource: 1-<max> characters from [A-Za-z0-9_-], for example wx-fetch; it is a session name, never a path and never a <kind>:<key> key`

选择 `wx-fetch` 作示例的理由：它是真机第四次成功调用用的名字（`kind:"tmux", resource:"wx-fetch"`），合法且短；示例必须**能通过 `validSessionName`**，否则描述自相矛盾。

## D2 机读约束（Enum/Pattern）与同源机制

**为什么不止改描述**：描述是自然语言，下一个人改校验器或改上限时不会记得回改描述——这正是本叶要根治的漂移。上游 `tool.Schema` 已有 `Enum []any` 与 `Pattern string`（`trpc-agent-go@v1.11.2-tagent.1/tool/tool.go:67,78`），把合法取值写成机读字段，模型与 provider 都能看到，且**可与校验器做等价性断言**。

**同源机制（唯一真值源 = 校验器所用的常量/函数）**：

```go
// 包内常量（不导出，避免 docs/api 再生与 public API 面扩张）
const (
    sessionNameCharset = "[A-Za-z0-9_-]"      // 与 validSessionName 的 switch 分支同义
    sessionNameExample = "wx-fetch"           // 必须通过 validSessionName
)

// 由常量生成，禁止字面量
kindSchema := &trpctool.Schema{
    Type:        "string",
    Enum:        []any{"", KindTmux},   // 空串=省略时选 tmux，与描述一致
    Description: fmt.Sprintf(`the execution resource family: %q is the only family this tool serves (leave it empty to select tmux)`, KindTmux),
}
resourceSchema := &trpctool.Schema{
    Type:        "string",
    Pattern:     fmt.Sprintf(`^%s{1,%d}$`, sessionNameCharset, tmuxSessionMaxName),
    Description: fmt.Sprintf("the tmux session name of the execution resource: 1-%d characters from %s, for example %s; it is a session name, never a path and never a <kind>:<key> key", tmuxSessionMaxName, sessionNameCharset, sessionNameExample),
}
```

三处"同源"由此成立：
1. `Enum` 的元素是常量 `KindTmux` 本身——改家族名必然同步（编译期同源）。
2. `Pattern` 的上限是 `tmuxSessionMaxName`、字符集是 `sessionNameCharset` 字面——上限同源由 `Sprintf` 保证；字符集同源由等价性测试兜住（见 D3）。
3. 描述与 `Pattern` 用同一组变量，改一处不会漏另一处。

**已知边界（必须如实登记）**：`Pattern`/`Enum` 是否被 provider 强制校验不由本仓决定；多数 provider 仅透传为提示。故 D1 的描述是主承载，D2 是加固，缺一不可，但**不得宣称"模型必然不再猜错"**——只能说"合法取值第一次就出现在模型可读面"。真机是否一次成功，由 00 域症状测试之外的实跑观察，本叶不承诺。

**`kind` 留空与 `Enum` 的兼容性**：`kind` 不在 `Required` 里（`tool.go:129`），省略即合法；`Enum` 只约束"出现时取什么值"。显式传 `kind:""` 在 `sessionNameOf` 里被当作 tmux（`tmux_executor.go:367`）。旧 `Enum:["tmux"]` 不含空串，而描述教模型"留空"——**描述与机读字段互相矛盾**（审计 C 段）。2026-10-26 P0 裁定：`Enum` 增 `""`（省略=空串，匹配），描述保留；代价是模型可见两个合法值，换取"描述怎么教、schema 就怎么许"的一致；provider 严格校验空串的风险由该裁定接受。

## D3 验收锚（三锚，全部落在既有测试文件）

测试文件：`tool/exec/exec_test.go`（`package exec`，可直读包内常量与 `validSessionName`）。测试名与 `-run` 正则逐字一致：

| 叶 | 父测试 | 子测试（具名） | 角色 |
|---|---|---|---|
| 3.1 | `TestExecRunDeclarationPinsTheResourceShape` | `names_the_only_family` | positive |
| 3.1 | 同上 | `names_the_session_name_format` | positive |
| 3.1 | 同上 | `pattern_agrees_with_the_validator` | positive（同源） |
| 3.1 | 同上 | `abstract_phrase_gone` | **negative** |
| 3.1 | 同上 | `prefixed_key_hint_absent` | **negative** |
| 3.1 | 同上 | `production_preset_carries_the_same_text` | **consumer** |
| 3.2 | `TestExecReleaseDeclarationPinsTheResourceShape` | 同上六项（release 声明） | 同上 |
| 3.2 | 同上 | `both_declarations_share_one_vocabulary` | **negative**（两条不一致即红） |

**正锚判定（可复跑、非空转）**：

- `names_the_only_family`：`kind.Enum` 恰为 `["", "tmux"]`（空串在前），且每个枚举值都能被 `sessionNameOf` 接受；`kind.Description` 含 `fmt.Sprint(KindTmux)` 与 "leave it empty"。
- `names_the_session_name_format`：`resource.Description` 含 `fmt.Sprint(tmuxSessionMaxName)`、含 `sessionNameCharset`、含 `sessionNameExample`。
- `pattern_agrees_with_the_validator`（同源核心）：编译 `resource.Pattern`，对样本表逐项断言 `re.MatchString(name) == validSessionName(name)`。样本表至少含：`"wx-fetch"`（合法）、`"a"`（下界）、`strings.Repeat("a", tmuxSessionMaxName)`（上界，合法）、`strings.Repeat("a", tmuxSessionMaxName+1)`（上界+1，非法）、`""`、`"tmux:local"`（真机错例）、`"a b"`、`"a.b"`、`"a/b"`、`"a:b"`。
- `production_preset_carries_the_same_text`（消费者锚）：断言对象取自**生产入口** `exec.NewPreset(manager, MonitorConfig{})` 返回的两个 `trpctool.Tool` 的 `Declaration()`，而不是测试里手工 `NewRunTool/NewReleaseTool` 构造的实例。生产装配点：`tools.go:269` `tagentexec.NewPreset(r.execManager, monitorConfigOf(spec), tagentexec.WithRunOwner(r.execOwner))`——即模型拿到的就是这两个对象。**手工构造的 Declaration 不算消费者证据。**

**负锚判定（"错误路径走不通"）**：

- `abstract_phrase_gone`：两条声明的 `kind`/`resource` 描述 MUST NOT 含 `"the resource family"`、`"canonical key of the execution resource"`（旧短语）。断言对象是 **Declaration 返回的字符串**，不是文件 grep——`decl.go:45` 的文档注释仍含同义短语且不在本叶写集内，按文件 grep 会误伤。
- `prefixed_key_hint_absent`：描述 MUST NOT 匹配 `(?:tmux|kind|resource)\s*:\s*[A-Za-z0-9_-]`（即不得给出 `<前缀>:<键>` 形式示例）。真机错例是 `resource:"tmux:local"`，这条负锚正面堵住该引导。
- `both_declarations_share_one_vocabulary`：`run` 与 `release` 的 `resource.Pattern` 字符串必须相等，`kind.Enum` 必须相等；不等即红。它拦的是"只改一处"这类最常见的半修。

**已知不可机检项（交评审，不冒充机器门）**：示例 `wx-fetch` 是否"贴切"、描述是否通顺——只做"示例必须通过 `validSessionName`"这一可判定约束。

## D4 风险与回退

| 风险 | 触发条件 | 处置 |
|---|---|---|
| **变异门被连带打红** | 改到 `tool.go:203` 的 `executed %s: %s` 行 | 写集限定在 `Declaration()` 函数体内；3.1/3.2 收尾各跑一次 `grep -c 'return fmt.Sprintf("executed %s: %s", key, output), nil' tool/exec/tool.go` 必须 `== 1` |
| `docs/api` 生成物与源码不一致 | 改 `decl.go` 注释或新增导出符号 | 只加包内小写常量；收尾 `git status --porcelain docs/api/` 必须为空 |
| 注释策略门（`scripts/lint.sh` 的 comment-policy ratchet） | 新增注释超基线 | 新增常量各一行注释、不复制长注释；`bash scripts/lint.sh --only policy` 退出 0 |
| `Enum`/`Pattern` 被 provider 严格校验 | 模型显式传 `kind:""` | 见 D2 边界：不加 `""` 进 `Enum`；若真机出现拒绝，另案（不在本叶临时放宽） |
| 描述改动被视为"行为变更" | 有人要求补运行时测试 | 本叶验收面在 tasks 冻结：Declaration 单元断言是**批准的验收面**，不因"想更稳"扩面；需要运行时证据走 00 域症状观测，不在本域加 |

**回退**：本域是纯文本/字段变更，`git revert` 单提交即可完全回退，无持久化格式、无迁移、无副作用。

## D5 被否方案

1. **只改描述、不加 `Enum`/`Pattern`**（被否）：描述仍可能被下一次改动带偏；且无法做"模式与校验器等价"这类机器断言。上游字段现成，不用是浪费。
2. **放宽 `validSessionName` 接受 `:` 或路径**（被否）：真机错的是**模型输入**，不是校验器太严。放宽会把 `<kind>:<key>` 复合键变成合法输入，直接影响 tmux 会话寻址（`sessionNameOf` 的整个存在理由就是"永不以多于一个词到达 tmux"，见 `tmux_executor.go:362-365`）。
3. **改 `decl.go` 的 `ResourceKey.Kind` 注释、让 godoc 也同步**（被否）：注释不是模型可见面；改它会牵动 `docs/api/` 生成物与文档门，收益为零、面变大。
4. **用 `runtime` 层行为测试证明"模型更容易成功"**（被否）：那是模型行为，不可确定性断言；能确定断言的只有"合法取值已出现在 Declaration 里"。把不可判的东西写成任务验收面＝造一个永远无法勾选的叶（R29 反模式）。
5. **为两条声明各写一套常量**（被否）：正是漂移来源；`both_declarations_share_one_vocabulary` 负锚就是为了让这种写法红。
