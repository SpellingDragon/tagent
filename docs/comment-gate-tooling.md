# 注释门禁的工具面

本页承载 `scripts/codetools` 与 `scripts/comment_policy` 两个门禁可执行的机制判据。源码注释只留一行职责与指向本页的索引，理由写在这里。

<a id="usage"></a>
## 一、命令面

| 可执行 | 子命令 / 主要 flag | 产出 |
|---|---|---|
| `codetools strip` | `<file>...` | 去注释后的规范化代码（`go/printer` 输出），跨修订可比对 |
| `codetools decls` | `<file>...` | 每个顶层声明一行 JSON：是否 test/benchmark、去注释体哈希、断言调用数、`t.Parallel` 数 |
| `codetools comment-check` | `--base-root DIR --head-root DIR <file>...` | 逐文件证明"只改了注释" |
| `codetools merge-check` | `--base-root DIR --head-root DIR [--map FILE] [--explain FILE] <dir>...` | 证明并入批无损（体一致 + 断言数不降） |
| `codetools package-layout` | `<dir>...` | 包布局：文件名拼写、骨架断言唯一性、测试文件镜像 |
| `comment_policy` | `[-baseline F] [-update-baseline] [-strict] [-no-baseline] [-v] [dir...]` | 注释形态棘轮 |

两条共同纪律：

- 路径一律是**模块相对路径**，必须在两个根下都存在（`comment-check`）或存在于 head 根（`merge-check`）；基线根由 shell 包装用 `git archive` 物化，不依赖工作树状态。
- 任何不可读或不可解析的输入都**以非零退出**——静默跳过绝不能伪装成通过。

<a id="equality-witness"></a>
## 二、等值见证的语义：什么算"只改了注释"

门禁比较的是**去掉注释之后的代码**，因此"什么被算进这份见证"就是判据本身。

### 清注释必须清干净

`clearComments` 丢文档注释而**保留编译器与工具指令**（`//go:build`、`//go:embed`、`//go:noinline`、`//export`、`//line`、cgo 预处理行）——指令增删改变构建行为，必须留在被比较文本里。实现只读 `File.Comments`（含附着声明的每个注释组）：漏一个注释槽就等于放行一次未证的改动。

### 空行是格式，不是代码

`dropBlankLines` 删纯空白行并去行尾空白：注释留下的空位是**格式**而非代码（`gofmt` 另有门）。**带字符串字面量的行一律豁免**（含原始字符串内部空行）——那些行是数据，修剪会让被改过的 fixture 冒充未改动。

### 限定符遮蔽

`qualifierBlind` 把包限定符前缀替换成固定标记：并入批允许**逐文件**统一 import 别名，包级文本映射表达不了这种决定，遮蔽后选择器名（调用语义）仍可比。遮蔽止于字面量边缘，fixture 里的点分路径不受影响。

<a id="table-discipline"></a>
## 三、影子表与豁免表：一次只允许一份

`--map`（影子索引表）与 `--explain`（豁免条目）各自**只持有一份文件**，且属于被检查的那个包：

| 约束 | 后果 |
|---|---|
| 重复传同一个 flag | 用法错误，直接拒 |
| 某条豁免在本次运行中没有免除任何声明 | 拒绝该条目 |
| 表文件读不到 | 让调用失败，而不是当作空表加载 |

三条同源：被遮蔽的表、不生效的豁免、静默为空的表都会让"通过"成为从未挣到的通过。表按构造**每包一份**：跨包的表会把一个域的归一化规则套到另一个域的基线上。多包运行须逐包各调一次 `merge-check`。

<a id="ratchet-scope"></a>
## 四、comment_policy 的棘轮语义与作用域

`comment_policy` 检查的是白名单：文档注释只允许陈述契约，任何指向长寿命文档的指针必须使用索引形态（`契约:` / `规格:`）。

- **长度从不决定合规**：唯一裁判是内容形态——长契约注释合法，短设计叙述不合法。
- 计数高于基线即非零退出；`-v` 打印全部 finding；`-strict` 额外要求 finding 为零的收敛终态；`-no-baseline` 为纯测量模式。
- 目录参数递归扫描并**跳过自带 `go.mod` 的子目录**；棘轮拒绝把嵌套模块留在门外或集合与基线不同源。
- **作用域是仓库，不是工作树**：被 git 忽略的 Go 文件从计数剔除（忽略文件不存在于任何 checkout，其 finding 基线不可复现）。目录键排除其下一切（git 仅在子树无被跟踪文件时折叠为目录路径）；无工作树或无 git 时集合为空、扫全部 Go 文件；除"无忽略项"外的查询失败一律上报。
- `scripts/lint.sh` 持有权威目录集合：批次作者与 CI 同树，读/降基线必须经它。

<a id="tracked-hygiene"></a>
## 五、追踪卫生门

`codetools tracked-hygiene` 守仓库的**索引**，四条判据各自具名拒绝：

| 拒绝原因 | 含义 |
|---|---|
| `runtime-artifact-tracked` | 路径名是运行产物（`.lock`/`.journal`/`.tmp`/`.spill`/`.prof`/`.test`/`.out`）：测试或进程在工作树写出的东西进了索引 |
| `empty-tracked-file` | 零字节文件被提交 |
| `tracked-path-missing` | 索引里有、工作树里没有 |
| `unregistered-top-level` | 出现未登记的顶层目录 |
| `tracked-and-ignored` | 文件被索引却同时被 ignore 规则排除：它是"追踪但隐形"，`git add <path>` 要 `-f`、新姊妹文件永不入库。判据必须问 `git ls-files -ci --exclude-standard`，**不能**把路径喂给 `git check-ignore`——后者连否定规则（`!README.md`）也算命中，白名单式 .gitignore 会被整片误报 |

一条路径命中多条规则时逐条都报。顶层目录集合以代码里的显式白名单为准：新增目录必须**同时**登记 README 布局说明，布局给人读、白名单给机器执行，二者同批变更。判定逻辑是对文件列表的**纯函数**（尺寸由注入的 `sizeOf` 提供），单测不需要 git 仓库；live 命令只把 `git ls-files` 结果喂进去。接入点 `scripts/lint.sh`，本地与 CI 同一条命令。

<a id="dotted-refs"></a>
## 六、点号引用门

`codetools dotted-refs` 查的是**运行时会真去 import 的位置**：shell 里 `python -m <模块>` 的后续 token、yaml 里 `workflow:` 的值。判定：点号换斜杠后在仓库内存在对应 `.py`（或包的 `__init__.py`）即通过；否则必须命中显式外部允许表 `externalModuleAllowlist`（前缀匹配，每条须附用途说明，**空表才是健康态**）。不做全文件点号扫描是有意的：配置覆盖语法与文档行文里的包名会被误伤，门的强度来自位置精确。扫描面为 git 索引里的 `*.sh`/`*.yaml`/`*.yml`，排除 `openspec/`（规格原文引用已退役路径作为记录）、`archive/` 与 `_tmp_report`。拦截对象如 `train_rl_config.yaml` 指向 AReaL 改名前包布局的"看着对"死配置。

<a id="test-file-split"></a>
## 七、测试文件的域锚切分

巨型测试文件按域切分时，同位门定死了合法形状（三条事实）：

| 事实 | 后果 |
|---|---|
| 文件的域锚 = **第一条** `契约:`/`规格:` 行的值 | 想让文件代表某域，就把该域锚写在文件头（先于任何内部索引行） |
| 同 (目录, 包, build-tag, 锚) 出现 ≥2 个测试文件时，缺 `<stem>.go` 精确镜像者判碎片 | 域文件必须各占**唯一锚**；同锚的测试只能留在同一文件里 |
| 无任何顶层 `Test*` 的文件不参与计数 | 共享 stub/fixture/基准可另立 `*_support_test.go`（零 Test 声明 ⇒ 豁免），这通常是最大的一刀 |

推论：**被门挡住的拆分不是好拆分**——锚被占用时正确动作是"收敛锚"或放弃拆分，不是起假镜像名。镜像比对仅 `_real` 后缀白名单。拆分必须无损：整函数搬运、断言零改动，判据为 `go test -list` 计数相等 + `check_test_merge.sh` 判 intact。

<a id="commit-scope"></a>
## 八、提交范围一致性门

`codetools commit-scope` 查的是**提交信息与实际改动范围是否相符**：标题以 `docs(` / `docs:` 开头的提交，若改动集里出现代码路径（`*.go`/`*.sh`/`*.yml`/`*.yaml`/`*.py`，`docs/`、`openspec/` 与 `*.md` 除外）即具名拒绝。形状来自一次真实事故：`git mv` 即写索引，随后不带 pathspec 的 `git commit` 把它扫进下一个提交——标称 docs 的提交带着半套包迁移上了远端。规则只看标题承诺：要关的是"把代码伪装成文档"，不是文档量。默认检查 HEAD（`commit-scope [ref]` 可指定）。

<a id="package-layout"></a>
## 九、包布局门

`codetools package-layout` 守**文件名**这一层：目录内的文件属于哪个语义家族，读名字就该知道。三条判据各自具名拒绝：

| 拒绝原因 | 含义 |
|---|---|
| `file-name-not-lower-snake` | 文件名不是 `lower_snake.go`：出现大写、连字符或空格。Go 允许这些拼写，正因如此门必须钉住一种——排序、import 差异与大小写不敏感文件系统都依赖唯一拼写 |
| `skeleton-variant` | 包里有第二个骨架断言拼写（任何含 `skeleton` 却非 `skeleton_assert.go` 的生产文件）：编译期形状钉是一份契约，一个包只能有一个文件承载它 |
| `skeleton-collision` | 同一目录出现 ≥2 个骨架断言文件：变体与正名并存时两条都报，残迹不会因为"正名还在"而隐身 |
| `test-mirror-missing` | `*_test.go` 的词干对不上同目录任何生产文件：改名压力必须落在"它是哪个文件的测试"上 |

镜像比对复用 §七 的**同一份**定义（仅 `_real` 后缀白名单），不另立第二份变体表。两处豁免是"对象不存在"而非宽容：`examples/contract/` 文件名即其实现的接口（`<Iface>_example.go`，由 `check_concept_budget.py` 强制）、`tests/integration/` 是夹具包——均无可镜像的生产家族。判定是对文件列表的**纯函数**，单测不需仓库。自测 `test_check_package_layout.sh` 造五个 fixture 跑**真命令**，见证为进程退出码（只打印规则却退出 0、或不可达的规则都会红；干净 fixture 是正向对照）。目录 root 不存在是**不可用输入**，退出 2。

