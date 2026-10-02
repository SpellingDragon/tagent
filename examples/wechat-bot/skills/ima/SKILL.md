---
name: ima-skill
description: |
  统一的 IMA OpenAPI 技能，支持笔记管理和知识库操作。
  当用户提到知识库、资料库、笔记、备忘录、记事，或者想要上传文件、添加网页到知识库、
  搜索知识库内容、搜索/浏览/创建/编辑笔记时，使用此 skill。
  即使用户没有明确说"知识库"或"笔记"，只要意图涉及文件上传到知识库、网页收藏、
  知识搜索、个人文档存取（如"帮我记一下"、"搜一下知识库里有没有XX"），也应触发此 skill。
homepage: https://ima.qq.com
metadata:
  openclaw:
    emoji: 🔧
    requires:
      env:
        - IMA_OPENAPI_CLIENTID
        - IMA_OPENAPI_APIKEY
    primaryEnv: IMA_OPENAPI_CLIENTID
  security:
    credentials_usage: |
      This skill requires user-provisioned IMA OpenAPI credentials (Client ID and API Key)
      to authenticate with the official IMA API at https://ima.qq.com.
      Credentials are ONLY sent to the official IMA API endpoint (ima.qq.com) as HTTP headers.
      The file-upload flow also sends requests to COS endpoints (*.myqcloud.com) using
      short-lived, scoped temporary credentials returned by the IMA API (create_media);
      the user's Client ID / API Key are never sent to COS.
      No credentials are logged, stored in files, or transmitted to any other destination.
    allowed_domains:
      - ima.qq.com
      - '*.myqcloud.com'
---

# ima-skill

## 🔄 知识库工作流（2026-09-10 用户拍板：云端单源，本地仅暂存）

**ima 云端知识库 = 唯一长期知识资产；本地不再沉淀知识库**（旧 `knowledge_base/` 目录冻结为历史档案，不再新增条目）。本地只做**临时编辑暂存**（/tmp 或 scratch 目录），成稿即上传 ima，上传成功后本地临时件即删。

**LLM Wiki 维护范式**（用户给原文 → 我产出理解）：
0. **核心价值准入门**（用户 2026-09-10 纠偏，优先级最高）：条目只收**脱离原文仍有引用价值**的知识点——独特数据/反直觉结论/可复用机制/结构性洞察。**不收**教科书常识（RAG 基础链路/Chunking/Embedding/HNSW/RRF 这类 IR 通识，需要时读教材而非查库）和原文复述。检验：写条目前自问"我未来什么场景会检索它？答案里有没有原文才有的信息？"——一篇文出 0-3 条是正常分布，**数量不是 KPI，检索价值是**；凑数拆条 = 污染信噪比。
1. 用户提供原文（文章/论文/链接），说"理解这篇/维护这篇"
2. 我通读原文 → 产出 LLM-wiki 条目：**我的结构化理解**（定义/机制/工程要点/与既有条目的关联），不是摘抄
3. 条目本地临时写成 md → 文件上传四步（check_repeated_names → create_media → COS → add_knowledge）入 Agent 自属知识库
4. 命名规范：`LLM Wiki/<主题>.md`（title == file_name 铁律不变）；同主题再维护 = 出修订版条目，头部标注修订日期与变更点
5. **引文链接铁律**（用户 2026-09-10 硬要求）：每条目头部溯源行后必带 `> 引文链接：<原文URL>`；同主题修订版条目同样必带。上传前自检：md 内无 mp.weixin/原文 URL = 不合格不上传
6. **官方 add-only 边界**（knowledge-base/SKILL.md GATE 3 原文，2026-09-10 实证）：API **不支持 replace/delete**（"Replace is NOT supported"）。同名重复时官方两条路：保留两者（时间戳后缀 `{name}_YYYYMMDDHHmmss`，批量查重上限 2000）或取消。LLM Wiki 语义修订走 `主题 v2/v2.1` 后缀条目 + 修订记录；**旧版本条的删除只能在 ima 客户端手动做**（API 无 delete_knowledge，实测空返回）——每次出修订版，在回复里列"建议客户端删除"清单给用户。
7. **提交前结构完整性**（用户 2026-09-10 要求）：上传前 md 必须自检四项——① title==file_name（GATE 2 铁律）② 溯源行+引文链接 ③ 修订记录 ④ [[关联条目]]指向已存在或本次同步上传的条目（避免悬空链接）。本地成稿 → 四步 gate → 上传 → get_knowledge_list 验证在库 → 删本地暂存件。
8. **文件夹组织**（官方 folder 机制）：条目可入文件夹（`folder_id`，`folder_` 前缀；根目录则省略该字段）。LLM Wiki 条目量大后应建「LLM Wiki」文件夹归置；定位文件夹用 `search_knowledge`（从 info_list 取 media_id 作 folder_id）。**不要**把 knowledge_base_id 当 folder_id 传。
9. **原文收录双轨**：微信文章官方原生支持 `import_urls`（1-10 条/次，无需 GATE 3-5）——理解条目（我写）与原文收录（import_urls）双轨并行，条目内引文链接 ↔ 原文互证。
10. **批量维护并发范式**（用户拍板"充分并行"）：三 track 同回合并行——抓取（curl 批量落 `wiki_raw/`）、理解（每回合并行读 2-3 篇骨架+关键段）、上传（`scripts/wiki_upload.sh` 耐久版，KBID 外置 `~/.config/ima/wiki_kbid`，输出 ASCII 安全；/tmp/wikistage 旧件废弃不维护）后台批量跑。上传与阅读必须交叠，禁止"传完再读"串行空转；多条目可单 action 批量写盘。

**抓取配方（实证有效，2026-09-10）**——"给我一篇/理解这篇"时先抓原文：
- 微信文章是**静态 HTML**：`curl -sL -A "<浏览器UA>"` 直抓即可，**不要动用 Playwright**（仅当返回"环境异常/验证页"才切换无头渲染）
- 标题在 `#activity-name`，正文在 `#js_content`，去标签 + html.unescape + 压缩空行即得纯文本
- **引用一层下钻**（用户要求深度时）：从正文 HTML 提取 `mp.weixin.qq.com/s/` 及站外引用链接 → 去重 → 逐个同法抓取；单篇失败记 FAIL 行**不中断整批**
- **充分并行**：多篇文章 = 多 track 同回合并行（抓取 batch N+1 与理解上传 batch N 流水线并行）；用户会分批投喂，逐批维护不等齐

**意图路由**：
- "搜知识库/搜一下 XX" → `search_knowledge`（云端单源，不查本地）
- "记一下/存一篇/维护一篇" → 本地暂存成稿 → 上传 ima（不再落本地长期）
- "看原文" → `get_knowledge_list`/`get_media_info` 定位 → 读回

**已实证的账号事实**（2026-09-10）：现有知识库 `一头喷火龙的知识库`、`面试`（id 一律经 get_addable_knowledge_base_list 实时拉取，勿凭记忆写死）。**Agent 自属知识库（LLM Wiki）：OpenAPI 无 create KB 端点（endpoint 全集已 grep 实证，仅 media/knowledge/search/list 类），需用户在 ima 客户端建一次空库，建好后我在库列表认领。** 凭据 `~/.config/ima/{client_id,api_key}`（600，git 不可达）。

**调用配方**（实证可用）：
```bash
S=tagent/examples/wechat-bot/skills/ima
node $S/ima_api.cjs /openapi/wiki/v1/get_addable_knowledge_base_list '{"limit":10}'
node $S/ima_api.cjs /openapi/wiki/v1/search_knowledge '{"knowledge_base_id":"<id>","query":"...","limit":3}'
node $S/ima_api.cjs /openapi/wiki/v1/add_knowledge '{"knowledge_base_id":"<id>", ...}'  # 见 knowledge-base/SKILL.md
```

Unified IMA OpenAPI skill. Currently supports: **notes**, **knowledge-base**.

## ⛔ MANDATORY RULES — read before ANY operation

1. **UTF-8 encoding (notes writes only):** Before calling `import_doc` or `append_doc`, ALL string fields (`content`, `title`) MUST be validated as legal UTF-8. Non-UTF-8 content causes irreversible garbled text. See [Detailed Rules](#detailed-utf-8-encoding-rules) for platform-specific methods.
2. **File upload naming:** `title` MUST equal `file_name` (with extension). Never rename, shorten, translate, or modify the original filename.
3. **Unsupported file types:** Reject immediately with a clear message. Do NOT ask user "do you still want to try?" Video files, Bilibili/YouTube URLs, and `file://` URLs are not supported — tell user to use IMA desktop client.
4. **File upload integrity:** Keep file content as-is during upload. No encoding conversion for binary files (PDF, images, Excel, etc.).
5. **PowerShell 5.1 (all modules):** If running in PowerShell, detect version before first API call. PS 5.1 silently converts request Body to GBK — must use UTF-8 byte array mode. See [Detailed Rules](#powershell-51-environment-detection).

## 模块决策表

| 用户意图                                                                                   | 模块           | 读取                      |
| ------------------------------------------------------------------------------------------ | -------------- | ------------------------- |
| 搜索笔记、浏览笔记本、获取笔记内容、创建笔记、追加内容                                     | notes          | `notes/SKILL.md`          |
| 上传文件、添加网页链接、搜索知识库、浏览知识库内容、获取知识库信息、获取可添加的知识库列表 | knowledge-base | `knowledge-base/SKILL.md` |
| 查看原文、分析原文、导出原文（需要 media_id）                                              | knowledge-base | `knowledge-base/SKILL.md` |

### ⚠️ 易混淆场景

| 用户说的                                                 | 实际意图                 | 正确路由                                                    |
| -------------------------------------------------------- | ------------------------ | ----------------------------------------------------------- |
| "把这段内容添加到知识库XX里的笔记YY"                     | 往已有**笔记**追加内容   | **notes** — 先搜索笔记获取 `note_id`，再用 `append_doc`     |
| "把这个写到XX笔记里"、"记到XX笔记"                       | 往已有**笔记**追加内容   | **notes** — `append_doc`                                    |
| "把这篇笔记添加到知识库"                                 | 将笔记关联到**知识库**   | **knowledge-base** — `add_knowledge` with `media_type=11`   |
| "上传文件到知识库"                                       | 上传**文件**到知识库     | **knowledge-base** — `create_media` → COS → `add_knowledge` |
| "新建一篇笔记记录这些内容"                               | **创建**新笔记           | **notes** — `import_doc`                                    |
| "帮我记一下"、"记录一下"、"保存为笔记"（未指定已有笔记） | 意图不明确，**需要确认** | **notes** — 先询问用户是创建新笔记还是追加到哪篇已有笔记    |
| "添加到笔记里"（未指定具体哪篇）                         | 意图不明确，**需要确认** | **notes** — 先询问用户是创建新笔记还是追加到哪篇已有笔记    |

### ⚠️ 跨模块任务 — 必须读取两个子模块

某些意图跨越 notes 和 knowledge-base 两个模块。**不要只读取一个子模块就开始执行**，必须先读取两个模块的 SKILL.md 再按顺序操作。

| 用户说的                             | 实际流程                                      | 读取顺序                                               |
| ------------------------------------ | --------------------------------------------- | ------------------------------------------------------ |
| "把知识库里的XX内容记到笔记"         | KB 搜索/读取 → Notes 创建/追加                | 先读 `knowledge-base/SKILL.md` → 再读 `notes/SKILL.md` |
| "查看原文"（知识库中的笔记类型媒体） | KB `get_media_info` → Notes `get_doc_content` | 先读 `knowledge-base/SKILL.md` → 再读 `notes/SKILL.md` |
| "把这篇笔记添加到知识库"             | Notes 搜索获取 note_id → KB `add_knowledge`   | 先读 `notes/SKILL.md` → 再读 `knowledge-base/SKILL.md` |

**规则**：如果用户意图同时涉及「笔记」和「知识库」，或者 API 响应揭示需要另一个模块（如 `media_type=11` 表示笔记类型），必须读取两个子模块再继续。

**核心判断规则**：

- 目标是**笔记的内容**（读、写、追加）→ notes 模块
- 目标是**知识库的条目**（上传文件、添加链接、关联笔记到知识库）→ knowledge-base 模块
- 目标是**获取知识库条目的原始内容**（查看原文、分析原文、导出原文）→ knowledge-base 模块（若原文是笔记，会跨模块到 notes `get_doc_content`）
- 用户提到"知识库"只是在**描述笔记的位置**（如"知识库里的那篇笔记"），真正操作对象仍是笔记 → notes 模块

## Credential Check

!`test -f ~/.config/ima/client_id && test -f ~/.config/ima/api_key && echo "✅ Credentials configured" || echo "⚠️ NO CREDENTIALS — setup required before any API call"`

**If ⚠️ NO CREDENTIALS:** Guide the user through setup BEFORE attempting any API call:

1. 打开 https://ima.qq.com/agent-interface 获取 **Client ID** 和 **API Key**
2. 存储凭证（二选一）：

**方式 A — 配置文件（推荐）：**

```bash
mkdir -p ~/.config/ima
echo "your_client_id" > ~/.config/ima/client_id
echo "your_api_key" > ~/.config/ima/api_key
```

**方式 B — 环境变量：**

```bash
export IMA_OPENAPI_CLIENTID="your_client_id"
export IMA_OPENAPI_APIKEY="your_api_key"
```

Agent 会按优先级依次尝试：环境变量 → 配置文件。缺少凭证时，`node ima_api.cjs ...` 会以程序错误退出（`code: -100`），并在 stderr 输出对应 `msg`。

> **Security note:** Credentials are only sent as HTTP headers to `ima.qq.com` and never to any other domain, file, or log.
> **Runtime dependencies:** Check `meta.json` → `required_binaries`

## API 调用模板

所有请求统一为 **HTTP POST + JSON Body**，仅发往官方 Base URL `https://ima.qq.com`。

`ima_api` 已抽离到脚本：`./ima_api.cjs`

```bash
# Example usage (cross-platform, pass credentials via options JSON)
SKILL_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" && pwd)"
OPTS=$(printf '{"clientId":"%s","apiKey":"%s"}' "$IMA_OPENAPI_CLIENTID" "$IMA_OPENAPI_APIKEY")

# stdout 返回正常响应；stderr 返回结构化错误 {"code":-100|-200,"msg":"..."}
if ! resp=$(node "$SKILL_DIR/ima_api.cjs" "openapi/list_docs" '{"limit":10}' "$OPTS" 2>/tmp/ima_err); then
  err_json=$(cat /tmp/ima_err)
  err_code=$(echo "$err_json" | jq -r '.code // empty' 2>/dev/null)
  err_msg=$(echo "$err_json" | jq -r '.msg // empty' 2>/dev/null)

  if [ "$err_code" = "-200" ]; then
    # 有新版本，原请求未发送；stdout 中带有更新上下文 JSON（含 instruction）
    echo "[update] $err_msg" >&2
  else
    # -100 或其他程序错误：msg 已包含可直接展示给用户的说明
    echo "[error] $err_msg" >&2
  fi
  exit 1
fi

echo "$resp"
```

> **错误处理有两层，必须都检查：**
>
> **第一层 — 脚本执行错误**（进程非 0 退出，错误在 **stderr**）：
>
> - `-100`：程序错误（缺少凭证、参数非法、网络错误等），`msg` 可直接展示给用户
> - `-200`：skill 需要更新，原请求未发送，stdout 中有更新上下文 JSON
>
> **第二层 — 后端业务错误**（进程正常退出，响应在 **stdout**）：
>
> - stdout 返回 JSON `{"code": 0, "msg": "...", "data": {...}}`
> - `code=0` 表示成功，从 `data` 提取业务字段
> - `code≠0` 表示后端业务错误（如参数不合法、权限不足、资源不存在等），**直接将 `msg` 展示给用户**
> - 常见后端错误码见各子模块的「错误处理」章节

## SKILL Update

`ima_api` 已内置更新检查：默认**每天首次 API 调用自动检查一次**，同一天内不会重复检查。

- `latest_version`：最新版本号，格式为 `MAJOR.MINOR.PATCH`
- `release_desc`：最新版本发布说明
- `instruction`：更新指引（prompt 文本）

### 错误返回与后续处理

> 出错时进程以非 0 退出，并在 **stderr** 输出结构化 JSON：`{"code":-100|-200,"msg":"具体错误描述"}`。

- `-200`（skill 需要更新）
  - 含义：检测到可用更新，原请求**未发送**
  - 后续处理：从 `ima_api.cjs` 的 stdout 读取更新上下文 JSON，根据其中 `instruction`（prompt）引导用户完成更新，然后重试原请求
- `-100`（程序错误，兜底）
  - 含义：其他所有错误（缺少凭证、参数非法、缺少 apiPath、网络错误等）
  - 后续处理：直接读取 `msg` 向用户展示；`msg` 已指出具体原因与修复建议

> 更新检查调用本身失败时，会**直接跳过本次检查并继续原请求**，不会抛错。

如需主动触发（忽略"每天一次"限制），可在调用前设置：

```bash
export IMA_FORCE_UPDATE_CHECK=1
```

---

## Detailed Rules Reference

> The sections below contain full platform-specific examples for the mandatory rules above. Refer to these when you need implementation details.

### Detailed UTF-8 Encoding Rules

> **此规则为强制性要求，不可跳过。** 非法编码会导致内容在 IMA 中显示为乱码，且无法修复，必须重新写入。
>
> **适用范围：notes 模块**（`import_doc`、`append_doc` 等文本写入 API）。
>
> **不适用于 knowledge-base 模块的文件上传**：上传文件时必须保持文件原始内容，不得转码。文件以二进制方式上传，服务端自行处理。

**每次调用 notes 写入类 API（`import_doc`/`append_doc`）之前，必须对 `content`、`title` 等所有字符串字段执行 UTF-8 编码校验/转换。** 无论内容来源如何——用户直接输入、从文件读取、WebFetch 抓取、剪贴板粘贴、外部 API 返回——都不能假设已经是合法 UTF-8，必须显式确认。

#### 强制检查清单（notes 模块写入前）

在构造 notes 写入请求的 body **之前**，完成以下步骤：

1. **来自文件的内容**：先检测文件编码，转为 UTF-8 后再读入变量（注意：这是指读取文件内容作为笔记正文写入，不是上传文件到知识库）
2. **来自 WebFetch / HTTP 请求的内容**：响应可能为 GBK/Latin-1 等，必须转码
3. **来自用户输入或变量拼接的内容**：清洗非法 UTF-8 字节（`\xff\xfe` 等）
4. **标题字段同理**：`title` 也必须为合法 UTF-8

#### 各环境转码方法

**Python（推荐，几乎所有环境都有）：**

```bash
# 读取文件，自动检测编码并转为 UTF-8
content=$(python3 -c "
import sys
data = open('tmpfile', 'rb').read()
for enc in ['utf-8', 'gbk', 'gb2312', 'big5', 'latin-1']:
    try:
        sys.stdout.write(data.decode(enc))
        break
    except (UnicodeDecodeError, LookupError):
        continue
" 2>/dev/null)

# 如果内容已在变量中，清洗非法 UTF-8 字节
content=$(printf '%s' "$content" | python3 -c "import sys; sys.stdout.write(sys.stdin.buffer.read().decode('utf-8','ignore'))")
```

**Node.js：**

```bash
content=$(node -e "const fs=require('fs');const buf=fs.readFileSync('tmpfile');process.stdout.write(buf.toString('utf8'))")
# 已知编码（如 GBK）：
content=$(node -e "const fs=require('fs');process.stdout.write(new TextDecoder('gbk').decode(fs.readFileSync('tmpfile')))")
```

**Unix (macOS/Linux)：**

```bash
content=$(iconv -f "$(file -b --mime-encoding tmpfile)" -t UTF-8 tmpfile 2>/dev/null || cat tmpfile)
```

**Windows PowerShell：**

```powershell
# 读取非 UTF-8 文件并转码
$content = [System.IO.File]::ReadAllText('tmpfile', [System.Text.Encoding]::Default)
[System.IO.File]::WriteAllText('tmpfile.utf8', $content, [System.Text.Encoding]::UTF8)
```

### PowerShell 5.1 Environment Detection

> **此问题影响所有 API 调用（notes、knowledge-base 等）**
>
> **此问题极其隐蔽：PowerShell 5.1 下 `Invoke-RestMethod` 会静默将请求 Body 从 UTF-8 转为系统 ANSI 编码（中文 Windows 为 GBK），即使设置了 `Content-Type: charset=utf-8` 也无效。结果是请求看起来发送成功，但服务端收到的内容已经是乱码，且无任何错误提示。**

**当 agent 运行在 PowerShell 环境时，必须在首次 API 调用前检测版本：**

```powershell
# 检测 PowerShell 版本 — 在任何 API 调用之前执行（notes 和 knowledge-base 都需要）
if ($PSVersionTable.PSVersion.Major -le 5) {
    Write-Host "⚠️ 检测到 PowerShell 5.1，将使用 UTF-8 字节数组模式发送请求"
    $useUtf8Bytes = $true
} else {
    Write-Host "✅ PowerShell 7+，默认 UTF-8，无需额外处理"
    $useUtf8Bytes = $false
}
```

**PowerShell 5.1 下必须使用以下方式发送请求**（用 `ConvertTo-Json` 构建 JSON 以避免手动拼接的转义风险，再显式转为 UTF-8 字节数组）：

```powershell
# PowerShell 5.1 安全请求模板（适用于所有模块的所有 API 调用）
$body = @{ title = "标题"; content = $content; content_format = 1 } | ConvertTo-Json -Depth 10
if ($useUtf8Bytes) {
    # CRITICAL: 必须转为字节数组，否则中文/非ASCII内容会变成乱码
    $utf8Bytes = [System.Text.Encoding]::UTF8.GetBytes($body)
    Invoke-RestMethod -Uri $url -Method Post -Body $utf8Bytes -ContentType "application/json; charset=utf-8" -Headers $headers
} else {
    # PowerShell 7+ 可直接传字符串
    Invoke-RestMethod -Uri $url -Method Post -Body $body -ContentType "application/json; charset=utf-8" -Headers $headers
}
```

> **总结：** 在 PowerShell 5.1 环境中，**所有** API 调用（无论 notes 还是 knowledge-base）都必须将 Body 显式转为 UTF-8 字节数组。不检测版本直接发请求 = 中文内容必乱码。这是 PowerShell 5.1 的已知设计缺陷，不是 bug 可以被修复。
