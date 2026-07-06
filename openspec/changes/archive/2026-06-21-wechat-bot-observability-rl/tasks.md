## 1. LocalFileKV 实现（替代 rustviking）

- [x] 1.1 创建 `memory/local_file_kv.go`：LocalFileKV struct 实现 KVStore 接口，JSON 文件持久化（内存 map + 启动加载 + 写入时 flush）
- [x] 1.2 创建 `memory/local_file_kv_test.go`：基础 CRUD + Scan + Range + Batch + 持久化恢复测试
- [x] 1.3 `tagent.go` resolveMemoryStore 添加 `type: localfile` case，使用 LocalFileKV 替代 RustVikingClient
- [x] 1.4 `config.go` MemoryConfig 文档更新：新增 `localfile` 类型说明

## 2. 旧文件清理

- [x] 2.1 删除 `.wechat-config/agent-events/` 目录（已清理，目录为空）
- [x] 2.2 确认 `.gitignore` 已排除 `.wechat-config/`（含新的 `data/` 子目录）和 `trajectories.jsonl`

## 3. tagent.yaml 配置切换

- [x] 3.1 tagent agent 的 memory 从 `type: memory, path: shared` 改为 `type: localfile, path: .wechat-config/data`
- [x] 3.2 recall agent 的 memory 从 `type: memory, path: shared` 改为 `type: localfile, path: .wechat-config/data`（共享同一数据目录）
- [x] 3.3 knowledge agent 的 memory 保持 `type: memory`（无跨 agent 共享需求，不需要持久化）

## 4. main.go — HTTPAPI + TrajectoryStore

- [x] 4.1 添加 `net/http` 导入
- [x] 4.2 读取 `TAGENT_HTTP_PORT` 环境变量（默认 8089）
- [x] 4.3 将 TrajectoryStore 创建改为无条件（默认路径 `.wechat-config/trajectories.jsonl`）
- [x] 4.4 在 StartLoop 之后，用 goroutine 启动 `http.ListenAndServe(":"+port, agent.NewHTTPAPI(ta, trajStore))`
- [x] 4.5 HTTPAPI 启动日志输出端口信息
- [x] 4.6 确保 TrajectoryStore 和 RewardFunc 总是传入 tagent.New

## 5. run.sh — 默认环境变量

- [x] 5.1 设置 `TAGENT_TRAJECTORY_FILE` 默认值为 `.wechat-config/trajectories.jsonl`
- [x] 5.2 设置 `TAGENT_HTTP_PORT` 默认值为 `8089`
- [x] 5.3 更新帮助文档：新增 `TAGENT_HTTP_PORT` 说明，更新 `TAGENT_TRAJECTORY_FILE` 为默认开启

## 6. 编译验证

- [x] 6.1 `cd examples/wechat-bot && go build ./...` 编译通过
- [x] 6.2 `cd /Users/pengweiye/Documents/codes/tagent && go build ./...` 框架编译通过
- [x] 6.3 `cd /Users/pengweiye/Documents/codes/tagent && go test -short -count=1 ./memory/ -timeout 60s` 测试通过
- [x] 6.4 `cd /Users/pengweiye/Documents/codes/tagent && go test -short -count=1 ./agent/ -timeout 60s` 测试通过

## 7. 运行验证（需用户手动执行）

- [ ] 7.1 启动 wechat-bot，确认 HTTPAPI 在 8089 端口监听
- [ ] 7.2 `curl http://localhost:8089/healthz` 返回 `loop_active: true`
- [ ] 7.3 发送一条消息后，`curl http://localhost:8089/trajectories` 返回非空数组
- [ ] 7.4 `curl http://localhost:8089/trajectory/{id}` 返回完整轨迹数据
- [ ] 7.5 确认 `.wechat-config/trajectories.jsonl` 文件被创建且包含轨迹数据
- [ ] 7.6 确认 `.wechat-config/data/kv.json` 文件被创建（LocalFileKV 持久化）
