# tagent 配置热更新 + 配置还原（快照回滚）

## Why

tagent 框架（dev 分支，Go，事件驱动）存在两个已实证的运维断点：

1. **配置为启动期一次性加载**：全仓无 fsnotify/SIGHUP，配置经 `sync.Once` 形态加载（`tagent/config.go` 902 行 + `registry.go`），改配置必须重启进程。第 3 层组织配置（Agent 身份/工具注册表/模型超时）已有按指纹惰性重建的骨架（`tagent-org` 计划增量 A：`org_hotreload.go`，`compress_threshold` 已可热平移），但**变更检测仍是"按需 mtime 检查"**——没有文件级监听，事件驱动的"配置文件变了→自动重建"链路不存在。
2. **配置变更无版本化保障**：`examples/wechat-bot/restart-tagent.sh` 已具备转世通报 staging（4b 段）+ done-sentinel 握手 + env.snapshot 归档，但配置文件（`config.yaml`）的变更没有留档机制——改坏配置后无法快速回到"改前状态"，只能凭记忆手改。

**范围修订（用户定调）**：既然配置能热更新，回滚也只需要回滚配置文件本身——不需要在二进制层建 LKG/自动回滚机制。B 段从"二进制 LKG+自动回滚"整体降维为"配置版本化+还原即回滚"：改配置前自动留时间戳快照，恢复走 `cp` 还原，生效依赖 A 段 watcher 的热应用（300ms debounce 内生效，无需重启）。

## What Changes

- **A. 配置热更新**（不变）：
  - 新增配置文件监听（fsnotify，依赖缺失时降级为定时轮询 mtime+size）→ 触发 `org_hotreload.go` 既有 reloader 链（重解析 → 指代比对 → 未变则热应用阈值 / 变则 RESTART required 或全量重建）
  - 重新解析+校验成功后原子切换进程内配置快照（`atomic.Pointer` 模式）；失败（解析错误/字段非法）保旧快照并打审计日志（ERROR + 计情器）
  - 配置消费点全部改为读快照,不再持启动期一次性引用
  - 单测三分支：成功热载 / 坏 YAML 保旧 / 字段非法保旧
- **B. 配置版本化 + 还原即回滚**（按用户修订降维）：
  - 改配置前自动留时间戳快照（一行 `cp config.yaml run/config.yaml.<timestamp>`）
  - 提供还原操作（也是 `cp`）：`cp run/config.yaml.<timestamp> config.yaml`，恢复完全依赖 A 殭 watcher 的热应用——300ms debounce 内生效，无需重启
  - `restart-tagent.sh` 的既有 env.snapshot 归档与 done-sentinel 握手保持不动（它们是重启链既有职责，不新增机制）
  - 验证：一次 dogfood——改一个配置→观察热载→cp 快照还原→观察自愈，日志即证据
  - **删除项**：do_rollback 函数、二进制回滚、坏二进制真机演练（原 5.3）、回滚 NOTICE 分支等全部二进制级回滚机制

## Capabilities

### New Capabilities

- `config-hot-reload`: 配置文件变更的运行期监听（fsnotify 或轮询降级）、重新解析校验、原子快照切换、坏配置保旧与审计留痕
- `config-version-restore`: 配置变更前自动快照（时间戳命名）、`cp` 还原操作、还原后经 A 段热应用生效、快照目录管理

### Modified Capabilities

（无——两项均为新增能力，不动既有能力需求）

## Impact

**涉及文件：**
- `tagent/config.go` / `tagent/registry.go` — 配置加载入口（消费点改快照读）
- `tagent/org_hotreload.go`（+测试）— reloader 链接入文件监听触发
- 新增 `tagent/config_watch.go`（fsnotify/轮询 + 快照管理）+ 单测
- `tagent/examples/wechat-bot/main.go` — 启动时挂 watcher（dogfood 跳径）
- `tagent/examples/wechat-bot/restart-tagent.sh` — 仅添加"改配置前快照"一步（若走该脚本路径改配置）；其既有 env.snapshot 归档与 done-sentinel 握手**保持不动**
- 新增 `tagent/examples/wechat-bot/scripts/` 下还原操作入口（如 `restore-config.sh`，单职责 `cp` 还原脚本）——具体命名与落位执行时确认
- `go.mod` — 新增 `fsnotify` 依赖（tagent 系统 Go 正常构建，无需冻结版本）

**与存量计划的关系（已核清，无重复）：**
- `tagent-agent-hot-rebuild`（2/10）：指纹机制与 reloader 骨架已交付（增量 A）——本计划 A **复用并接线**其 reloader，不重建指纹算法
- `tagent-restart-wakeup-loop`（0/最新）：restart.sh 的建置计划——B 段不再在其上加二进制回滚，仅保留"改配置前快照"这一步轻改；该计划原有职责边界不受影响
- `wechat-bot-reincarnation-notice`（1/14）：通报钩子（main.go 读 REINCARNATION_«NOTICE）——B 河不再产出回滚路径 notice，不冲突
- `wechat-bot-rl`（1/最新）：RL 侧不做配置热更，不受影响

**明确不做（修订后收窄）：**
- 不做二进制 LKG 归档与自动回滚（do_rollback、探测收窄、坏二进制演练全部删除）
- 不做配置回滚到任意历史版本（快照目录按时间戳保留，还原是显式手动 cp，不设自动回滚）
- 不做集群级配置分发
-不做第 1/2/4 层配置加载方式改造（MCP servers 已有独立 hot-sync）
- 不改治理边界与 NOTICE 格式（约束继承）

**最终产物：** dev 分支 commit+push，Go 全包回归绿（基线五包全绿）；dogfood 实证：运行中进程改配置文件 → 日志显示热载生效无需重启；B 段一次 dogfood（改→热载→cp 还原→自愈）日志即证据。
