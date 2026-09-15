# 1.1/1.2 定位纪要 — tagent-hot-config-lkg-restart（A 段）

> 2026-09-11 22:50-23:00 实证。行号基于 dev @ ff93015。每条均有 grep/read_file 证据。

## 1. 配置加载与消费全景（1.1）

| 链路 | 位置 | 消费时机 | 热更新现状 |
|---|---|---|---|
| 主配置解析 | `config.go:832 LoadConfig` | 启动一次（main.go:37）+ reloader 内重解析（tagent.go:346） | — |
| **org reloader（既有热载链）** | `tagent.go:315-369`（SetOrgReloader 接线） | **每次 LLM 调用前惰性探针**：stat mtime → 变了才重解析 → 指纹比对 → 分支处置 | ✅ 已实现完整骨架 |
| 指纹白名单 | `org_hotreload.go:70-118`（orgSubset/agentSubset） | 重解析后 | 指纹不变=热应用路；指纹变=RESTART required（fail-closed） |
| **compress_threshold 热应用** | `tagent.go:359-363` ApplyOrgParams | 指纹不变分支 | ✅ **唯一已热应用的参数**（org_hotreload.go:88-92 注释明说刻意排除出指纹） |
| 治理配置 cfg.Governance | `tagent.go:261-268` | **纯启动装配期** | ❌ 运行期不读，改动需重启 |
| 进化配置 cfg.Evolution | `tagent.go:248-252` | 纯启动装配期 | ❌ 同上 |
| MCP 段 | `tool/mcp/registry.go:206`（mtime）+ config.go MCPServers 注释 | 懒 mtime 检查 | ✅ **已有独立热同步**（不依赖 org reloader） |
| wechat App 段 | `main.go:61 loadWechatConfig(tagentCfg.App)` | 启动期一次反序列化 | ❌ 运行期不读 |
| registry.go sync.Once | `registry.go:56 registerOnce` | 工具注册去重 | **与配置无关**（plan 原 1.1 锚点有误，本纪要更正） |

## 2. 消费点分类（1.2）

- **启动期一次性读（改动=RESTART）**：Governance、Evolution、App.wechat、Memory 类型/路径（org指纹黑名单已明示 memory 不可热迁）
- **运行期惰性重读（已有热载）**：MCP 段（独立机制）、compress_threshold（reloader 热应用）
- **指纹白名单内、可热应用面**：MaxToolIterations / MaxTokens / Temperature / Thinking* / ReasoningContentMode / Compress.*（summary_model 等）/ Meditation 参数 / TaskTerminalTTL / ResumeContextRounds / KeepRecentTasks ——这些字段改了目前只触发"指纹变化→RESTART required"日志，**既不热应用也不重建，等于白改**

## 3. 关键设计发现（供 1.3 会签）

**现状已有「mtime 惰性探针 + 指纹 + 分支处置」完整链，A 段的真实增量不是新建 watcher，而是：**

1. **热应用面扩容**：指纹不变分支目前只热应用 compress_threshold 一个字段 → 扩到 §2 白名单参数面（每个字段一个 ApplyOrgParams 风格的原子热应用）
2. **触发器形态（1.3 会签结论）**：design D1 通读后放行**周期轮询**——原疑虑（惰性探针 vs 轮询）经 D1 自带论证解决：事件驱动框架空闲期无流量，lazy 探测延迟无界，违反"秒级生效"验收目标；轮询间隔即天然 debounce，且 vim/sed 写临时文件再 rename 的 inode 丢失天然免疫。既有 LLM 调用前 lazy 探针**保留**作为第二道兜底层（与 watcher 调同一 reloader 闭包，mtime single-flight 幂等，共存无冲突）。快照消费形态（getter 读 vs ApplyOrgParams 推送）按 2.2/2.3 执行时逐消费点定，可热应用白名单面见 §2。
3. **指纹变化分支维持 RESTART required 不做快照重建**（增量 B 范围，现状 log 已 fail-closed，不劣化）

## 4. 5.1 回归取证状态

- build + vet 已绿（VET_OK，/tmp/tagent_51_regression.log 47B 截断教训：输出全部重定向致监控判死，已改 stdout 回声退出码打法）
- 普通测试 / -race 双轨后台进行中（/tmp/t_normal.log、/tmp/t_race.log，落盘防截断）
