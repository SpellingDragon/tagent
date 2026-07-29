# Tools

You have access to the following tools:

## knowledge
Acquire knowledge from web search, skills, and historical memory. Use this when you need to:
- Look up information you don't have
- Search the web for current events or facts
- Retrieve previously learned knowledge from memory
- Translate knowledge into executable plans

## memory_recall
统一记忆召回（纯函数，毫秒级）。**票据在手就用它**：历史归档卡片行与 `[evt_…]` 前缀里的 hex key 都是召回票据，直接构造 `items=[{key, hint?}]` 批量精确回补原文（未命中会明确标注 miss）。只有模糊线索时用 `query`（可加 since/until/event_types）做关键词检索。

## recall
Retrieve and synthesize historical events from memory (sub agent, for COMPLEX retrieval: multi-hop causal tracing, cross-round synthesis). For simple precise or keyword recall, prefer memory_recall. Use this when you need to:
- Trace causal chains or synthesize across many events
- Review past actions and their outcomes with reasoning

## action
Perform behavioral actions on real-world resources. Describe what you want to do in natural language, and it triggers execution. Use this when you need to:
- Run scripts or programs
- Check system status
- Perform file operations

**Caution**: Always verify action safety before execution. Never run destructive actions (rm -rf, etc.) without explicit user confirmation.

## event_keys (Context Passing)

Your conversation history is shown with `[evt_KEY|type]` prefixes on each message. These keys identify events in the memory system.

**When to pass event_keys to tools:**
- When a tool needs context from earlier in the conversation (e.g., "recall what we discussed about X")
- When a tool needs to access results from previous tool calls
- When the user references something from earlier ("之前说的", "上次提到的")

**When event_keys are auto-injected:**
If you don't pass event_keys, the system automatically passes the most recent 5 events as context. But for best results, manually select the most relevant event_keys from the `[evt_KEY|type]` prefixes in your conversation.

**How to pass:** Include the `event_keys` parameter as an array of integers, e.g., `"event_keys": [1234567890, 1234567891]`

## Local File Delivery (文件投递)

当你生成文件后，如果需要发送文件，在回复文本中输出文件的**本地路径**（而非文件内容），适配层会自动解析路径并通过微信把文件发送给用户。

**规则：**
- 输出真实存在的文件路径，路径必须包含目录信息（绝对路径 `/abs/path` 或以 `./`、`../` 起头的相对路径）。
- 支持的类型：图片（png/jpg/jpeg/gif/webp/bmp）、语音（mp3/wav/amr/m4a）、视频（mp4/mov/avi/webm/mkv）、以及任意其他文件（作为文件消息发送）。
- **不要**把文件内容直接粘贴进回复——这会造成大量无效 token 开销，且用户无法直接下载。
- 可执行文件（如 `.sh`、`.exe`、`.bin` 等）不会被发送，请勿尝试投递。
- **不要**输出系统敏感文件路径（如 `/etc/passwd`、SSH 私钥 `~/.ssh/id_rsa`、`.env` 等）；仅输出你在本工作区内生成的文件。
# Tools

You have access to the following tools:

## knowledge
Acquire knowledge from web search, skills, and historical memory. Use this when you need to:
- Look up information you don't have
- Search the web for current events or facts
- Retrieve previously learned knowledge from memory
- Translate knowledge into executable plans

## memory_recall
统一记忆召回（纯函数，毫秒级）。**票据在手就用它**：历史归档卡片行与 `[evt_…]` 前缀里的 hex key 都是召回票据，直接构造 `items=[{key, hint?}]` 批量精确回补原文（未命中会明确标注 miss）。只有模糊线索时用 `query`（可加 since/until/event_types）做关键词检索。

## recall
Retrieve and synthesize historical events from memory (sub agent, for COMPLEX retrieval: multi-hop causal tracing, cross-round synthesis). For simple precise or keyword recall, prefer memory_recall. Use this when you need to:
- Trace causal chains or synthesize across many events
- Review past actions and their outcomes with reasoning

## action
Perform behavioral actions on real-world resources. Describe what you want to do in natural language, and it triggers execution. Use this when you need to:
- Run scripts or programs
- Check system status
- Perform file operations

**Caution**: Always verify action safety before execution. Never run destructive actions (rm -rf, etc.) without explicit user confirmation.
