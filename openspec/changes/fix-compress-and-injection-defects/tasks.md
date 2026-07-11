## 1. InjectEventKeys 幂等化

- [x] 1.1 在 `agent/context_manager.go` 的 `injectEventKeyPrefixes` 函数中，在注入前缀前检查 `strings.HasPrefix(msg.Content, "[evt_")`，有则 `continue` 跳过
- [x] 1.2 新增幂等测试：构造已有 `[evt_` 前缀的消息，验证不重复注入

## 2. 删除 protectPendingAsyncSegments

- [x] 2.1 在 `agent/smart_compress.go` 的 `Compress` 方法中，删除 Step 4a（protectPendingAsyncSegments 调用及相关日志）
- [x] 2.2 删除 `protectPendingAsyncSegments` 和 `hasPendingAsyncResult` 函数
- [x] 2.3 更新引用这些函数的测试（删除或适配）

## 3. SmartCompressor old_segments=0 提前返回

- [x] 3.1 在 `agent/smart_compress.go` 的 `Compress` 方法中，Step 4 之后添加 `if len(oldSegments) == 0 { return messages }`
- [x] 3.2 新增测试：构造 oldSegments 为空的场景，验证返回原始消息且不添加 compress 消息

## 4. 配置修正

- [x] 4.1 修改 `examples/wechat-bot/tagent.yaml`：`max_tool_iterations: 200` → `50`
- [x] 4.2 修改 `examples/wechat-bot/tagent.yaml`：`keep_recent_tasks: 8` → `2`

## 5. 测试与验证

- [x] 5.1 回归测试：`go test ./agent/... ./plugin/... ./event/...` 全部通过
- [x] 5.2 构建验证：`go build ./...` 通过
