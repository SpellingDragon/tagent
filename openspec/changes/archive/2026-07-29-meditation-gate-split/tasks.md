# Tasks: meditation-gate-split

## 1. MeditationManager 状态拆分（agent/meditation.go）

- [x] 1.1 移除 `lastEventTime` 字段与 `UpdateLastEventTime`/`LastEventTime` 方法，新增 `lastUserInput`、`lastTurnEnd` 两个 `atomic.Int64` 及 `UpdateLastUserInput(t)`/`UpdateLastTurnEnd(t)` 方法
- [x] 1.2 重写 `checkAndMeditate`：双闸门判定（`now - lastTurnEnd ≥ MinGap` && `lastUserInput > lastMeditation` && `lastUserInput != 0`），删除触发即重置（原 L157-159）
- [x] 1.3 digest 的 idle 参数改用 `now - lastTurnEnd` 计算（保持 `renderSelfStateDigest` 签名不变）
- [x] 1.4 重写 L122-130 及触发处的注释为双闸门语义，消除与 makeOnEventCallback 的矛盾表述

## 2. 锚点更新点接线

- [x] 2.1 `agent/inject.go`：`InjectMessageWithSource` 与 `InjectMessageWithMetadata` 在 `source == "user"` 时调用 `meditationMgr.UpdateLastUserInput(time.Now())`（判空保护）
- [x] 2.2 `agent/event_loop.go`：`runEventLoop` 每次 RunFlow 返回后（含错误与重试耗尽路径）调用 `meditationMgr.UpdateLastTurnEnd(time.Now())`（判空保护）
- [x] 2.3 `agent/session.go`：删除 `makeOnEventCallback` 中的冥想锚点更新分支（原 L246-249），保留 ★ 卡片标记分支不动

## 3. 混合批次防御（agent/event_loop.go）

- [x] 3.1 `runEventLoop` 在 `BuildInvocation` 前过滤批次：同批存在 meditation 与非 meditation external_input 时移除 meditation 事件并记 info 日志；过滤后批次为空则 continue

## 4. 测试

- [x] 4.1 适配 `agent/meditation_test.go` 既有用例到新 API（`FiresWhenGapMet`/`SkipsWhenGapTooSmall`/`SkipsWithoutNewActivity` 等改用 `UpdateLastUserInput`+`UpdateLastTurnEnd`）
- [x] 4.2 删除或重写 `TestOnEventCallback_MeditationFinalDoesNotResetIdleAnchor`：断言事件回调不再改变冥想管理器任何锚点
- [x] 4.3 新增永动机回归测试：冥想触发后仅更新 `lastTurnEnd`（模拟冥想衍生 task settle turn），断言多个 MinGap 后不再触发；新用户输入后恢复触发
- [x] 4.4 新增混合批次测试：`[task_settled, meditation]` 与 `[meditation, user]` 双向用例，断言冥想事件被移除、剩余 turn 的 trigger_source 正确；纯冥想批次正常执行
- [x] 4.5 新增触发自锁测试：触发后无重置的前提下，连续 check 均被新颖性闸门拦截

## 5. 验证

- [x] 5.1 `go build ./...` 与 `go test ./agent/... -race` 全绿
- [x] 5.2 全量 `go test ./... -race` 无回归；`openspec validate meditation-gate-split` 通过
