> **历史文档（historical-verbatim）**：本篇描述已移除的生产实现；当前架构见 [docs/wiki/README.md](README.md#当前架构速览)。文中文件引用按各篇快照提交锚定。

# tagent/prompt 模块架构文档

## 一、模块定位

`tagent/prompt` 是 tagent 的 **Prompt 模板加载层**：从文件系统加载 prompt 文件（.md）并组装为完整 prompt 字符串供 Agent 使用。核心职责：从文件加载内容、支持单文件/目录/多文件组合、按预定义顺序加载 bootstrap 文件（Agent 系统提示词）。设计原则三条：**纯加载，无生成**（不依赖 LLM，仅做文件 IO 与字符串组装）；**确定性顺序**（目录加载按文件名排序，bootstrap 按预定义顺序）；**容错性**（空文件不报错，跳过而非中断）。

<a id="file-layout"></a>
## 二、文件清单

| 文件 | 职责 |
|------|------|
| `loader.go` | Prompt 加载器：单文件/目录/组合/bootstrap 加载 + 内嵌 FS 回退（`WithFallback`） |
| `source.go` | `Source`：mtime 感知的热重载 prompt 源（工具描述热更新用；nil-receiver 守卫） |
| `getter.go` | `Getter` 接口（`Get() (string, error)` + `IsEmpty() bool`）：热配置提示词源抽象缝；`*Source` 编译期满足 |
| `loader_test.go` / `loader_fallback_test.go` / `source_test.go` | 加载器与内嵌回退专项（磁盘优先、miss 回退、绝对路径不回退）、热重载 Source（mtime 变更重读、inline-only 只加载一次） |

组件关系：`Config`（`system_prompt.files` / `description_file`）→ `Loader`（BaseDir + 内嵌 FS 回退）→ 优先读磁盘 prompt 文件、miss 回退内嵌 FS → 产出 Agent system prompt（bootstrap：AGENTS→SOUL→TOOLS…）与工具描述（`AgentToolWrapper.Declaration`，经 `Source` 热重载）。

<a id="loader-methods"></a>
## 三、Loader 与加载方法

`Loader` 是轻量结构：`BaseDir` 为所有相对路径提供基准，两个私有字段 `fallbackFS`/`fallbackPrefix` 承载内嵌缺省（`NewLoader(baseDir, opts...)` 逐个应用选项）。无选项时只读磁盘 `BaseDir`（行为与引入 fallback 前一致）；传 `WithFallback(fsys, prefix)` 才启用内嵌缺省回退。

### 3.1 LoadFromFile — 单文件加载

支持绝对路径与相对路径（相对路径以 `BaseDir` 为基准），路径先 trim 空格再判空（空路径报 `prompt file path is empty`），避免意外空格导致路径错误。空文件返回空字符串（`nil` 错误）而非报错。磁盘读失败时先试内嵌 FS 回退（`fallbackFile`，仅 `os.ErrNotExist` 且非绝对路径），未配置回退或回退也 miss 才返回 `fmt.Errorf` 包装的错误，调用方可通过 `errors.Is` 解包。

### 3.2 LoadFromDir — 目录加载

**确定性顺序**（文件名排序保证每次加载顺序一致）；**非递归**（不递归子目录，仅处理当前目录文件）；**仅 .md**（大小写不敏感，目录无 `.md` 时报 `no .md prompt files in directory`）；**严格错误处理**（任何文件加载失败都会中断整个目录加载）；**整目录回退**（磁盘目录不存在时回退内嵌同名目录，同样排序 + `\n\n` 拼接；不与磁盘内容做逐文件合并——磁盘目录存在即完全以磁盘为准）。空内容段被跳过，其余以 `\n\n` 连接。

<a id="load-files"></a>
### 3.3 LoadFiles — 多文件加载（可选文件 load-if-present）

保留 `paths` 参数顺序；空路径与空内容跳过；**磁盘上不存在的文件（且内嵌回退 FS 也无）跳过而非硬失败**，并记 Info 留痕——USER.md/HEARTBEAT.md/MEMORY.md 等个人/可选上下文遵循 LoadBootstrap 的 skip-missing 契约；仅容忍 not-exist，权限/IO 等真实错误照常向上传播。与 `LoadFromDir` 的区别：来源是显式文件列表而非目录遍历，顺序按参数而非文件名排序。

<a id="load-composite"></a>
### 3.4 LoadComposite — 组合加载

**优先级** `inline > files > dir`，三者用 `\n\n` 连接；任一段加载出错即向上返回。典型用途是加载 Agent 系统提示词（inline 文本来自配置、files 指定文件、dir 载入目录下所有 `.md`）。

## 四、Bootstrap 加载 — Agent 系统提示词

`BootstrapLoadOrder` 是当前装配序列的唯一真源：`AGENTS.md`（Agent 自身定义/角色、能力边界、行为准则）、`SOUL.md`（核心价值观/决策优先级、伦理底线）、`USER.md`（用户偏好与上下文）、`TOOLS.md`（可用工具列表与使用说明）、`HEARTBEAT.md`（健康检查机制）、`MEMORY.md`（记忆策略、压缩阈值）。`LoadBootstrap(dir)` 先判基目录存在（否则报 `bootstrap directory %s does not exist`），按上述固定顺序载入缺失即跳过的文件，再补充目录内其余 `.md`（已载入者不重复），全部以 `\n\n` 连接。该入口是 `LoadComposite` 之外的备选路径，适合固定 bootstrap 约定场景；当前主流程用 `LoadComposite` 以获得更大灵活性。

## 五、辅助函数与模块关系

`SplitCSV` 把逗号分隔字符串（如 `"a.md, b.md, c.md"`）拆成 slice，过滤空元素并 trim 空格。依赖方向：`tagent/agent` 的 `ContextManager` 经 `llmagent.WithInstruction` 把组装后的 prompt 注入 system message；实际调用链是 `tagent.New()` → `buildAgent()` → `loader.LoadComposite(inline, files, dir)` → `NewTagentAgent(cfg{SystemPrompt})` → `NewContextManager(cfg{SystemPrompt + SystemPromptSource(Getter: 热载或 bundle 版本切换)})`。`BaseDir` 使 prompt 文件可用相对路径引用（`prompt.NewLoader("resources/prompts")` 后 `LoadFromFile("recall_tool_desc.md")` 实际读 `resources/prompts/recall_tool_desc.md`），从而让路径引用与部署环境解耦。

## 六、关键设计决策

**为什么用 `"\n\n"` 而不是其他分隔符**：两个换行在 Markdown 中通常表示段落分隔——可读性（源文件里是自然的段落分隔）、LLM 友好（大多数模型能正确理解段落边界语义）、无歧义（不与单换行或代码块内换行混淆）。**为什么空文件不报错**：Bootstrap 场景中某些可选文件（如 `HEARTBEAT.md`）可能不存在，"不存在"与"存在但为空"都应跳过，不中断整个加载过程。**为什么目录加载不递归子目录**：避免意外的加载顺序（子目录深度不确定）、鼓励显式目录结构、保持 `BootstrapLoadOrder` 可控；需要子目录时显式用 `LoadFiles` 指定完整路径。

<a id="embedded-fallback"></a>
## 七、内嵌 FS 回退（prompt-loader-fallback）

`NewLoader(baseDir, WithFallback(fsys, prefix))` 注入内嵌 prompt FS：磁盘 `BaseDir` 下找不到文件/目录时回退到 embed FS（`prefix` 为 FS 内 prompt 根路径，如 `resources/prompts`）。**磁盘永远优先**——用户可覆盖任意内置 prompt，二进制单文件分发时又不缺省；`fallbackFile/fallbackDir` 在 `LoadFromFile/LoadFromDir` 的 miss 路径内生效，调用方无感知。

| 入口 | 契约 |
|---|---|
| `LoadFromFile` | 相对路径按 `BaseDir` 解析；空文件返回空串而非错误；绝对路径**永不**走内嵌回退；仅在 `ErrNotExist` 时试回退 |
| `LoadFiles` | 空路径与空内容跳过；**磁盘上不存在该文件也跳过**；真实读错误（权限、I/O）仍向上传播 |
| `LoadFromDir` | 只取该目录一层的 `.md`，按文件名排序以保证顺序稳定；目录无 `.md` 时报错 |
| `LoadComposite` | 组装顺序固定为 inline → `Files`（按给定顺序）→ `Dir`，各段以空行拼接 |
| `LoadBootstrap` | 先按 `BootstrapLoadOrder` 固定顺序载入，再补充目录内其余 `.md`；缺失文件跳过 |

**`LoadFiles` 为什么容忍缺文件**：装配清单指向的是可选上下文文件（如 `USER.md`、`HEARTBEAT.md`、`MEMORY.md`），干净的检出库合理地不含它们——若缺文件即致命错误，一份引用可选文件的已提交配置就无法从干净检出启动。因此"存在即加载"，缺失记 info 日志（**拼错必需文件名仍可见**，不被静默吞掉），只宽恕 `ErrNotExist`。

<a id="source-hotreload"></a>
## 八、Source — 热重载 prompt 源

`prompt.NewSource(loader, prompt.CompositeConfig{Files: []string{"recall_tool_desc.md"}})` 的 `Get()` 读盘并缓存，文件被修改后 mtime 变化即自动重读。用途：`AgentToolWrapper.SetDescriptionSource` 使工具描述**热更新**——`Declaration()` 每次经 Source 取描述，改 prompt 文件立即生效、无需重启进程；inline-only（无 files）配置只加载一次并缓存。

| 情形 | 行为 |
|---|---|
| 所有配置文件 mtime 均不晚于上次成功载入时刻 | 命中缓存，不重读磁盘 |
| 任一文件 mtime 晚于上次成功载入时刻 | 重读全部文件与 `Dir` 下的 `.md`，刷新缓存与 `modTime` |
| `stat` 或重读失败且已有缓存 | 返回缓存内容且不报错（提示词读盘失败不应使该回合失败） |
| `stat` 或重读失败且无缓存 | 返回错误 |
| 静态源（`NewStaticSource`，`loader == nil`） | 返回固定内容，不监听文件 |
| nil 接收者 `(*Source)(nil)` | `Get` 返回空内容、`IsEmpty` 返回 `true`，不 panic |

比较基准必须是**上次成功载入的时刻**，不能是"当前遍历到的最大 mtime"：后者的初值是零时刻，会使任何非空文件列表都被判为已变更，缓存永不命中（每回合 `BeforeModel` 都要重读全部提示词文件）。**Getter 缝的存在理由**：`ContextManager` 的 `SystemPromptSource` 与 `MeditationConfig` 的 `PromptSource` 字段类型是 `prompt.Getter` 而非 `*prompt.Source`，使提示词源可在运行期被替换实现（文件热载、内存常量、测试替身）而不触及消费方；默认实现即 `*prompt.Source`，走 mtime 热载（**文件即真源**，经 `refine register` 登记纳入评估保护，见 [platform 篇](../platform/platform-subsystems.md)）。例外：工具描述路径 `SetDescriptionSource(src *prompt.Source)` 仍取具体类型。

<a id="getter"></a>
## 九、Getter — 提示词源的运行期抽象

`Getter` 只有一个方法：`Get() (string, error)`。消费方（工具描述、子 agent 装配、投影写入等）**依赖这个接口，而不依赖任何具体实现**。

| 动机 | 说明 |
|---|---|
| 消费方不知来源 | 同一消费点可能拿到静态字符串、热重载文件源或测试替身；来源变了不必改调用方 |
| 降级路径单一 | 读取失败只需向上传一个 `error`，调用方统一决定"保留上一次好值/退回内嵌默认"，不必为每种来源写分支 |
| 可测 | 测试注入常量 Getter 即可覆盖消费方逻辑，无需真读盘 |

约束：`Get()` MUST NOT 在实现内部做业务级回退（回退属于调用方的降级策略），MUST NOT 返回带截断或加工过的内容——加工是 `Loader`/`CompositeConfig` 的职责。热重载语义见「八、Source」。

## 已知缺口与演进方向

| 缺口 | 现状与防线 | 候选方向 |
|------|-----------|---------|
| ~~参数/模型热切换仅存储就绪~~（已随 bundle 体系退役） | git-native 后提示词=文件直读热重载，无版本遮蔽层；改进经 refine register 留痕评估 | — |
| **bootstrap 顺序固定** | AGENTS→SOUL→TOOLS… 顺序编码在 LoadBootstrap，不可配置 | 保持固定（顺序即契约）；如需自定义走 system_prompt.files 显式列表 |
