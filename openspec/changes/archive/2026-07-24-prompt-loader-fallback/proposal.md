## Why

Prompts are physically duplicated: the framework ships `resources/prompts/` and every example copies the full set into its own `prompt_dir`. The loader uses a single `BaseDir` with **no fallback**, so a consumer must carry a copy of every prompt it references. This duplication is the **root cause of drift** — it directly produced the bugs just fixed (a `recall_tool_desc` triplication, a contradictory `action_tool_desc`). Without a structural fix, the copies will drift again.

Goal (per the "沉淀到框架" principle): make the framework the single canonical source of shared prompts, and let a consumer override only the **special** ones (persona, meditation), inheriting the rest.

## What Changes

- **Embed framework default prompts** into the binary (`//go:embed resources/prompts` in the root `tagent` package) — location-independent defaults that ship with the framework.
- **Loader gains an optional fallback**: when a prompt file (or whole directory) is not found under the on-disk `BaseDir`, the loader resolves it from the embedded framework defaults. On-disk files always take precedence (override wins).
- **Wire the embedded defaults** into the loader in the composition root (`tagent.go`), so all agents get fallback automatically.
- **Examples stop duplicating shared prompts**: the wechat-bot example deletes its copies of the shared mechanism prompts (tool_desc / agent prompts) and keeps only its overrides (`meditation.md`, `SOUL/USER/AGENTS/TOOLS.md`, `plan_*`). Shared prompts then load from the embedded framework defaults.

## Capabilities

### New Capabilities
- `prompt-loader-fallback`: prompt resolution falls back to embedded framework defaults when a file/dir is absent under the configured `BaseDir`; on-disk entries override embedded defaults.

### Modified Capabilities
<!-- 无 requirement 级变更：framework-prompt 讲的是"框架 prompt 注入"，与加载解析无关。 -->

## Impact

- **代码**：`prompt/loader.go`（`LoadFromFile`/`LoadFromDir` 增加 embedded 回退 + `NewLoader` 可选 fallback FS）；新增根包 `//go:embed resources/prompts`（如 `prompts_embed.go`）；`tagent.go` 装配时把 embedded FS 注入 loader。
- **解耦**：loader 接受 `fs.FS`（不反向依赖根包），根包持有 embed。
- **example 清理**：删除 `examples/wechat-bot/resources/prompts/` 中与框架一致的共享 prompt（11 个），仅保留 override（meditation + 人格 + plan_*）。
- **行为**：磁盘优先、缺失回退嵌入；对不设 fallback 的既有调用完全兼容（回退为可选）。
- **非目标**：目录内 per-file 合并（disk 与 embedded 混合列目录）——仅做"整目录缺失才回退"；prompt 热重载仍只作用于磁盘 override（嵌入默认为静态）。
