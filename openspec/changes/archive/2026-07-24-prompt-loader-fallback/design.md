## Context

`prompt.Loader{BaseDir}` (`prompt/loader.go`) resolves relative prompt paths against a single `BaseDir` and reads from disk via `os.ReadFile` / `os.ReadDir`. `tagent.go` builds it with `prompt.NewLoader(cfg.PromptDir)` (default `resources/prompts`). Consumers set `prompt_dir`; because there is no fallback, each must carry a full copy of every referenced prompt. `resources/prompts/*.md` are plain files (not embedded).

## Goals / Non-Goals

**Goals:**
- Framework prompts become embedded, location-independent defaults.
- Loader resolves a missing file/dir from embedded defaults; on-disk overrides win.
- Consumers keep only overrides; shared prompts inherit from the framework.
- Backward compatible: fallback is optional; existing single-dir behavior unchanged when unset.

**Non-Goals:**
- Per-file merge of disk + embedded within one directory listing (whole-dir fallback only).
- Hot-reload of embedded defaults (embedded is static; disk overrides still hot-reload via `prompt.Source`).

## Decisions

- **D1 Embed in the root package, inject as `fs.FS`**: the root `tagent` package owns `//go:embed resources/prompts` (only it sits above `resources/`). The `prompt` package must NOT import `tagent` (cycle), so `Loader` accepts an `fs.FS` + prefix via an option; `tagent.go` passes the embedded FS. Keeps `prompt` decoupled.
- **D2 Option-style constructor**: `NewLoader(baseDir string, opts ...LoaderOption)` with `WithFallback(fsys fs.FS, prefix string)`. Zero options → today's behavior exactly (no fallback).
- **D3 Resolution order (file)**: `LoadFromFile` tries `BaseDir/path` on disk; on `os.ErrNotExist`, tries `fs.ReadFile(fallbackFS, prefix + base(path))`. Disk always wins. Absolute paths bypass fallback.
- **D4 Resolution order (dir)**: `LoadFromDir` tries the disk dir; if it does not exist (or is empty of `.md`), it lists the embedded dir instead. No per-file merge — a present disk dir is authoritative as-is.
- **D5 Higher-level methods inherit**: `LoadFiles` / `LoadComposite` / `LoadBootstrap` call the two primitives, so they get fallback for free.
- **D6 Example cleanup**: delete the 11 shared prompts from `examples/wechat-bot/resources/prompts/`; keep `meditation.md` + `SOUL/USER/AGENTS/TOOLS.md` + `plan_agent.md` + `plan_tool_desc.md`. Verify the example still resolves every referenced prompt (disk override or embedded fallback).

## Risks / Trade-offs

- **Embedded defaults must be complete + correct**: they are now (just aligned; 11/12 shared prompts identical). Deleting example copies + fallback yields identical resolved content.
- **`go:embed` path coupling**: the embed directive fixes `resources/prompts` relative to the root package; moving that dir breaks embedding (compile-time failure — caught immediately, not silent).
- **Whole-dir (not per-file) dir fallback**: if a consumer wants to add ONE file to an embedded-backed dir scan, it must copy the whole dir. Acceptable — shared prompts are referenced by filename (file-level fallback), and dir-scan is used for consumer-owned persona bootstrap.
- **Silent override surprise**: an on-disk file shadows the embedded default. This is the intended override semantics; a debug log line on fallback-vs-disk resolution aids diagnosis.
