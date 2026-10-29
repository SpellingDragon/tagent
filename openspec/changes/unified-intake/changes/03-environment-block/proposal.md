# 03 environment-block：环境块归位（末尾追加+特殊标记+清单补全）

父变更：unified-intake（域 D，用户加项）。

## 边界与依赖

- **依赖**：既有 `request->environment` 端口与 `environmentBlock`（内容已在，只动位置/标记/一行清单）；`BoundaryView` 既有结构扩字段；01 域的请求拼装面（接口常数——开发可并行，集成需 D2 后）。
- **被依赖方**：无。
- **接口面**：`BoundaryView` 加一个环境文本字段（既有结构扩字段，非新类型）；`environmentBlock` 文本加 `<environment>` 环绕+`prompt_version` 行。
- **禁止**：不新增端口/消息类型/常量族；不改工具清单来源（binding 既有）；不改 prompt 文件装配顺序（system prompt 本体仍在前部）。

## 症状

用户裁定：「可用 system prompt、MCP、tool、skill 属于动态环境信息……并在上下文最后追加」——现状环境块在前缀区（transcript 之前）且无标记环绕。
