## 1. 嵌入框架默认 prompt

- [x] 1.1 新增根包 `//go:embed resources/prompts` 的 `embed.FS`（如 `prompts_embed.go`，`package tagent`），导出访问入口供装配使用
- [x] 1.2 确认 `resources/prompts/*.md` 全部被嵌入（`go build` 通过；嵌入清单含 11 个共享 prompt）

## 2. loader 回退能力

- [x] 2.1 `Loader` 增加可选 fallback：`fs.FS` + prefix 字段；`NewLoader(baseDir, ...LoaderOption)` + `WithFallback(fsys, prefix)`；零选项时行为不变
- [x] 2.2 `LoadFromFile`：磁盘 `BaseDir/path` 不存在（`os.ErrNotExist`）时回退 `fs.ReadFile(fallback, prefix+base(path))`；磁盘优先；绝对路径不回退
- [x] 2.3 `LoadFromDir`：磁盘目录不存在/无 `.md` 时改扫描嵌入同名目录（整目录回退，不做 per-file 合并）；`LoadFiles`/`LoadComposite`/`LoadBootstrap` 经由二者自动获益
- [x] 2.4 回退命中时打一条 debug 日志（便于诊断 override vs fallback）

## 3. 装配

- [x] 3.1 `tagent.go` 用 `prompt.NewLoader(cfg.PromptDir, prompt.WithFallback(embedded, "resources/prompts"))` 注入嵌入默认

## 4. example 去重

- [x] 4.1 删除 `examples/wechat-bot/resources/prompts/` 中与框架一致的 11 个共享 prompt；保留 `meditation.md` + `SOUL/USER/AGENTS/TOOLS.md` + `plan_agent.md` + `plan_tool_desc.md`
- [x] 4.2 验证：example 每个引用的 prompt 都能解析（磁盘 override 或嵌入回退），resolved 内容与删除前一致

## 5. 测试与收尾

- [x] 5.1 loader 单测（用内存 `fstest.MapFS` 作 fallback）：磁盘缺失→回退命中；磁盘存在→override 胜出；绝对路径不回退；目录整目录回退；未配置 fallback 时行为不变
- [x] 5.2 回归：`go test ./prompt/ ./... -short -count=1` 全绿；`go build ./...` + example 目录 `go build .` 通过
- [x] 5.3 `scripts/check-openspec.sh` 通过；`openspec validate prompt-loader-fallback --strict` 通过；按 Conventional Commits 提交
