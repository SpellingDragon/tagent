## 1. 前置核实

- [x] 1.1 读回 `run.sh` 确认冷启动路径是否写 `run/restart.done`（影响 design D1 假阳性面：若 run.sh 冷启动也写，检测逻辑需追加"insurance 标记"判定；结论与决策记录到本任务备注）
  - 结论（2026-09-11 读回核实 + plan agent 独立复核）：冷启动路径**不写** restart.done。证据：① run.sh 全文 `restart.done` 零命中；② `do_start()` 定义于 L359，函数体核心行 L383 仅 `nohup "$SCRIPT_DIR/wechat-bot" >> "$LOG_FILE" 2>&1 &`；③ start（L787）/ rl-start（L793）等入口均汇入 do_start，无旁路写入。**决策：D1 检测门（mtime<10min + PID≠self）无需追加 insurance 标记判定**（design OpenQ 3 答案 = no）
- [ ] 1.2 读回 `tagent/agent/inject.go` 与 `tagent/agent/event_loop.go`，确认 idle 期 `InjectMessageWithSource("meditation", …)` 经 persistentBus 的消费路径（BeforeModel TryPull 时机），确定 sleep 延迟值（5s 起步）与"注入是否立即触发一轮"的预期，记录结论；并核实 main.go 可达的 WAL 尾查询 API（时间倒序 limit N，D8 前置）

## 2. 代码实现

- [ ] 2.1 `restart-maintenance.sh` SUCCESS 分支新增写 `run/REINCARNATION_NOTICE`（design D3 模板：reincarnated_at/old_pid/new_pid/reason/build_sha/binary_size/log_pointer），写失败仅 log 不阻断主流程；`bash -n` 语法检查通过
- [ ] 2.2 `main.go` 新增转世检测函数：读 `run/restart.done`（mtime <10min 且 PID≠os.Getpid() 判定命中；异常静默跳过），命中则读 REINCARNATION_NOTICE 组装通报文本，缺失时生成降级通报（时间+PID 来自 restart.done，注明档案缺失）
- [ ] 2.3 `main.go` 注入接线：StartLoop 后、HTTPAPI 前起 goroutine（sleep 延迟后执行检测→`ta.InjectMessageWithSource("meditation", …)` 注入→成功后 rename restart.done 为消费标记防重；全程日志），不打扰 consumer 既有路由；组装通报时按 D8 拼 WAL 尾现场块（含断点标记），降级阶梯落 log
- [ ] 2.4 新增单测（如 `reincarnation_notice_test.go`）：覆盖新鲜度命中/超时、PID 等/不等、NOTICE 存在/缺失降级、消费标记 rename 幂等——纯文件系统逻辑，无网络依赖、现场块命中/降级路径

## 3. 门禁与提交

- [ ] 3.1 门禁全绿：`go build ./...` && `go vet ./...` && `go test ./...`（在 `tagent/examples/wechat-bot` 目录执行，新增测试全过、存量测试无回归）
- [ ] 3.2 git commit（含 main.go / restart-maintenance.sh / 新测试文件 / openspec 变更目录）并 push 到 dev 分支，记录 commit SHA

## 4. dogfood 自替换部署与验证

- [ ] 4.1 触发保险链换装：选低峰期 `kill -TERM <当前 bot PID>`，等待 cron 保险链（≤1min）接手完成 build→换装→探活；观察 restart.log 出现 SUCCESS 行且新增 NOTICE 写入动作行
- [ ] 4.2 验证档案与标记：`run/REINCARNATION_NOTICE` 存在且字段齐全；新进程日志可见检测命中 + 通报注入（grep meditation 触发源的转世通报内容）；`run/restart.done` 已改名消费标记
- [ ] 4.3 验证消费闭环：转世后 agent 首轮对话/事件日志确认通报进入上下文（事件流出现通报消费痕迹）；微信侧消息收发正常（healthz ok、用户消息可达）；验证保险链幂等门在哨兵改名后行为正常（kill -0 哨兵缺失→自发现→健康→回写基线，静默退出不误换装）
- [ ] 4.4 冷启动反证：手动重启（或等待 >10min 后 restart）验证不误发通报（日志见检测未命中、无通报注入），确认假阳性防护有效
- [ ] 4.5 事故复盘对照：验证 13:58 场景（换装后用户追问）在新机制下 agent 能从通报获知转世事实——转世通报抵达事件流即视为缺口闭合，记录验证证据到本任务备注；现场块核验：通报正文含 WAL 尾事件 key 与断点标记

## 5. 收尾

- [ ] 5.1 汇总验证证据（restart.log 关键行、NOTICE 内容、注入/消费日志行、消费标记、commit SHA），经 update 报账归档计划
