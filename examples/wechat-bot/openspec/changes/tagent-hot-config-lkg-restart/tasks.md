## 1. A-盘点与设计复核

- [ ] 1.1 复核配置加载链现状：`tagent/config.go`（Config 结构、解析入口）、`tagent/registry.go`（sync.Once）、`tagent/tagent.go:315-359`（reloader 闭包接线）、`org_hotreload.go` 既有链路（指纹比对→热应用/RESTART required），产出定位纪要（行号锚点 + 消费点清单）
- [ ] 1.2 盘点配置消费点：全仓 grep `LoadConfig`/`config.` 类引用，列启动装配期一次性读 vs 运行期重复读清单，圈定"改快照读"改动面（含 `examples/wechat-bot/main.go` 与 cmd/ 启动链）
- [ ] 1.3 设计复核会签：对照本 change design.md（D1 mtime 轮询触发器、D2 atomic.Pointer 快照、D3 watcher 只做触发器、D4 验收口径），确认 watcher→reloader 接线点、快照语义与轮询参数；有出入先改 design 再动工

## 2. A-实现：configWatcher + 快照 + 消费点改造

- [ ] 2.1 新增 `tagent/config_watch.go`：configWatcher = mtime+size 周期轮询触发器（默认秒级间隔可配置，**不引 fsnotify**，go.mod 零变更），轮询间隔即天然 debounce（窗口内多次写合并为一次重读）；暴露 `WithConfigWatch(false)` 等价开关供整体回退
- [ ] 2.2 快照管理：`atomic.Pointer[Config]` 持活跃配置，重载链（重解析→校验→指纹比对）全过才 Store；失败保旧 + ERROR 日志（含原因/路径）+ `config_reload_failures_total` 失败计数器递增；统一 getter
- [ ] 2.3 消费点改读快照：按 1.2 清单逐个改（启动装配期一次性读保留除外），消除裸 `*Config` 引用缓存
- [ ] 2.4 单测三分支（t.TempDir 写真实配置文件）：成功热载（改白名单字段→getter 返回新值）/ 坏 YAML 保旧 / 字段非法保旧 + touch 不触发 + 轮询窗口内多次写合并为一次重载；**定位为回归与边界覆盖，不作为生产语义证明**（生产语义由 3.x dogfood 真机实证承担）
- [ ] 2.5 wechat-bot 接线：`main.go` 消费点改快照读 + 启动挂 watcher（读 config.yaml），确认日志链路"检测到变更→重载开始→快照已切换"可回放

## 3. A-dogfood 真机实证（验收主路径，日志即证据）

- [ ] 3.1 成功热载实证：wechat-bot 运行实例上改真实 `config.yaml`（如 compress_threshold 或白名单字段），采集运行日志摘录证明"检测到变更→重载开始→快照已切换"链路完整、消费行为随之变化、进程全程未重启
- [ ] 3.2 坏配置保旧+自愈实证：运行实例写入坏 YAML → 日志出现 ERROR 级重载失败、进程不崩、行为保持旧值；随后修复为有效变更 → 再次热载成功（自愈）；两次日志摘录作验收证据
- [ ] 3.3 A 段小结报账：日志摘录 + 单测结果 + git diff 概览，对照 spec `config-hot-reload` 全场景逐条核验

## 4. 总验收与交付

- [ ] 4.1 Go 全包回归：`go build ./...` + `go vet ./...` + `go test ./...`（五包基线全绿不劣化）
- [ ] 4.2 续作合并关系标注：确认本计划收口后，`tagent-agent-hot-reload` 剩余任务（2.x/3.x/4.2/5.1）的合并处置方案已写入报账
- [ ] 4.3 dev 分支 commit + push，commit 信息说明 A 段 pure 范围（轮询 watcher + 快照切换）；推送后报账附 commit hash
- [ ] 4.4 收尾：删除已作废 spec 目录 `specs/config-version-restore/` 与 `specs/restart-lkg-rollback/`（墓碑文件已标明，plan agent 无删除文件能力，需执行方删除），对照 `config-hot-reload` 全场景核验通过，向 plan update 带证据报账后归档本 change
