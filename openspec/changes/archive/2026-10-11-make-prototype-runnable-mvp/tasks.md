## 1. 事件循环与任务执行

- [x] 1.1 为原型模型、工具、Reply 和 Run 统一接入 context/错误语义，更新内部调用方，保留函数式骨架；先完成可编译的签名迁移 —— 验证：`GOPROXY=off go test ./prototype -run '^$'`。
- [x] 1.2 修通 FIFO 队列、首事件、持续循环、关闭后拒绝与协作式退出，增加真实 Input/Run 生命周期与批量发布测试 —— 验证：`GOPROXY=off go test ./prototype -run '^TestMVPLoop' -count=10`。
- [x] 1.3 区分模型中间输出、ACK、任务终态和最终答复；保留参数/结果，收齐同批任务后按派发顺序装配，覆盖错误和取消 —— 验证：`GOPROXY=off go test ./prototype -run '^TestMVPTask' -count=10`。

## 2. 状态权威与记忆

- [x] 2.1 收敛 state 的 config 快照、治理记录和任务归属，移除重复规则与无消费方字段；验证非法发布、在途模型/工具绑定及状态保留 —— 验证：`GOPROXY=off go test ./prototype -run '^TestMVPState' -count=10`。
- [x] 2.2 完成并发安全的内存事实、真实票据与 mock 投影压缩；覆盖输入前记录、最终输出、伪造/坏键、压缩后真实 recall 及同进程快照恢复 —— 验证：`GOPROXY=off go test ./prototype -run '^TestMVPMemory' -count=10`。

## 3. 组合能力与可运行 demo

- [x] 3.1 接通真实父模型→子 BaseTAgent→子工具→父模型路径，验证独立任务域、来源与接收方、结束/取消后的子循环回收 —— 验证：`GOPROXY=off go test ./prototype -run '^TestMVPChild' -count=10`。
- [x] 3.2 将手动冥想的 pending/watermark 转换收口到 state，修正 meditation 来源，接入观察事实；覆盖无新事实、重复 tick、派生来源、注入/模型失败重试 —— 验证：`GOPROXY=off go test ./prototype -run '^TestMVPMeditation' -count=10`。
- [x] 3.3 将 config 中的伪运行示例移为 example_test.go 的可运行 Example；真实展示工具调用/父子回流、压缩召回、显式热更与 mock 评估，所有循环都可退出 —— 验证：`GOPROXY=off go test ./prototype -run '^Example$' -v -count=10`。

## 4. 文件职责与 README

- [x] 4.1 为每个 prototype Go 文件（含测试/示例）补中文头部「职责 / 未实现 / 接线」，逐一核对实际符号与生命周期，删除已过期的不可运行描述 —— 验证：逐文件读取与检索 `职责|未实现|接线`，并执行 `GOPROXY=off go vet ./prototype`。
- [x] 4.2 新增 prototype/README.md，含 Mermaid 组件图/时序图、预期特性与已实现/mock/未实现矩阵、模块所有权与接线表、真实执行命令及边界；图和符号与代码一致 —— 验证：读取 README 并执行其中 `GOPROXY=off go test ./prototype -run '^Example$' -v` 与 `GOPROXY=off go test ./prototype`。

## 5. 集成验证与交付

- [x] 5.1 格式化仅本轮修改文件，运行原型全部测试十次及静态检查；逐项核对 specs 场景已有实际测试承载 —— 验证：`gofmt -l prototype` 无输出；`GOPROXY=off go vet ./prototype`；`GOPROXY=off go test ./prototype -count=10`。
- [x] 5.2 运行全部原型用例竞态检测十次，确认不存在 sleep 驱动的顺序断言、无未回收的合作生产者，列出实际测试和示例名称 —— 验证：`GOPROXY=off go test -race ./prototype -count=10`；`GOPROXY=off go test ./prototype -list .`。
- [x] 5.3 核对改动仅落白名单、任务勾选与凭据相符，验证本 change 并提交五段战报；不执行 git commit/push/archive —— 验证：`git diff --check -- prototype openspec/changes/make-prototype-runnable-mvp`；`openspec validate make-prototype-runnable-mvp --strict`；`openspec instructions apply --change make-prototype-runnable-mvp --json`。

## 6. 新发现的问题修复（新增）

主代理首轮复验：现有测试与 Example 通过，但以下边界尚无完整真实路径或与文档不一致。先完成本节，再重跑第5节；无须扩大为生产框架。

主代理最终复验：本节修复后，删除运行时 medSink 的测试通知参数，完成屏障仅留在 `_test.go` 包装器；Observe 改为复用本批快照。新增 `TestMVPStateObserverSnapshot`，临时还原活配置读取时稳定失败（`在途步骤的观察器被新发布替换`），恢复快照读取后整包 `go test -race ./prototype -count=10 -timeout=120s` 通过。普通整包 `-count=10`、`go vet`、独立 Example、`gofmt -l`、`git diff --check` 与本 change 的严格验证均通过。最终用例清单为 25 个 Test 函数及 1 个 Example；Example 单独验证，不将 `-count=10` 误报为 Example 重复执行。

- [x] 6.1 修复子模型直接报错时 subAgentTool 只等 replies/ctx、不消费 done 的挂起；移除仅为测试建立的 onExit 回调，以真实函数返回/上下文传播证明回收；补子错误与取消测试 —— 验证：`GOPROXY=off go test ./prototype -run '^TestMVPChild' -count=10 -timeout=30s`。
- [x] 6.2 为 Run 的所有返回路径建立自身取消与生产者收敛，禁止 loop 错误退出后 Wait 等待未取消的工具；所有测试失败路径也用清理函数取消并等待，特别补 Reply/子错误及模型取消路径 —— 验证：`GOPROXY=off go test ./prototype -run '^TestMVPLoop|^TestMVPChild' -count=10 -timeout=30s`。
- [x] 6.3 memory.record 拒绝负数键而不越界；一致重放校验完整事实（含来源/错误）且不污染事实副本；保护 snapshot 中的指针别名；保留工具部分输出与错误、模型带工具时的 Output/Reasoning 事实 —— 验证：`GOPROXY=off go test ./prototype -run '^TestMVPMemory|^TestMVPTask' -count=10 -timeout=30s`。
- [x] 6.4 补齐重复终态幂等归约，避免 pending 重复扣减提前收齐；收敛 taskSeq/active/pending 等可派生镜像与两套任务 id；补反转完成次序、重复结算测试，确保模型可见的结果标签可判定 —— 验证：`GOPROXY=off go test ./prototype -run '^TestMVPTask' -count=10 -timeout=30s`。
- [x] 6.5 接通运行时反思失败/取消时的自动 pending 释放，移除测试手工 medRelease 冒充接线；明确投递完成与水位推进的确认顺序，不把收到 buffered channel 等同于回调已返回，补可控同步屏障测试 —— 验证：`GOPROXY=off go test ./prototype -run '^TestMVPMeditation' -count=10 -timeout=30s`。
- [x] 6.6 自动压缩使用已声明配置且在长批次/多任务前保留当前待处理材料，验证多次自动压缩仍保留真实历史首尾导航，不能只手工发 EventCompact 代替自动路径；明确历史容量与当前活跃材料边界 —— 验证：`GOPROXY=off go test ./prototype -run '^TestMVPMemory' -count=10 -timeout=30s`。
- [x] 6.7 恢复模块单责：subAgentTool 执行适配归任务域，手动反思状态转换/运行终结接线归 state，config 只配置及装配；删除自证测试后门与无效断言，初始配置也有明确有效性判定；注明 Observe 生效时机或与快照一致更新 —— 验证：逐文件检查；`GOPROXY=off go vet ./prototype`；`GOPROXY=off go test ./prototype -count=10 -timeout=60s`。
- [x] 6.8 修订 README 和注释：使用相对运行路径；图反映真实 snapshot/record 顺序、ACK 回本实例而非直接交外部；去掉‘几百行’等失实承诺，异步审批说明非阻塞 hold/重试接线而非在 Allow 中等待；去除泄漏式 design Dn/测试论证；wiki 现有 HTML 锚已存在，不改 wiki —— 验证：逐文件读回并执行 README 中 Example 与全部测试命令。

