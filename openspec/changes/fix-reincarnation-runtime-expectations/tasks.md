# Tasks

## 1. P0 止血：pruneTerminal nil detector（已完成）
- [x] 1.1 根因定位：完整 panic 栈 + `RestoreTask` 不设 detector 的赋值点全量审计
- [x] 1.2 修复 `agent/task/task_manager.go` L769：持 `t.mu` 读 detector + nil 判空
- [x] 1.3 回归测试 `agent/task/task_prune_nil_detector_test.go`（真 RestoreTask 路径 + List/Spawn 双入口）
- [x] 1.4 `gofmt` / `go build ./...` / `go vet` / `go test ./agent/...` 全绿
- [x] 1.5 commit `4aedeef`
- [x] 1.6 换装部署到运行进程，确认新二进制携带该修复
      证据口径（三项齐备即可判定）：a. 新二进制 build_sha `f882854a5aae`、size 55283368 bytes；
      b. 运行进程为新二进制：pid 启动时间晚于二进制 mtime；c. logs/restart.log 含 RESTART OK，healthz ok。

## 2. P1 plan 工具 provider 配置（已完成待部署）
- [x] 2.1 根因：`wiring.go:37-40` 空 agent provider 回退全局 deepseek
- [x] 2.2 修复 `tagent.yaml` plan 段补 `provider: zhipu`
- [x] 2.3 commit `d438666`
- [x] 2.4 换装后实测 `plan` 工具恢复可用（证据：调用返回摘要，非 400）

## 3. 转世运行预期（设计）
- [x] 3.1 成文《转世运行预期》：三类状态 × 预期行为 × 降级策略 × 可观测证据
      产出物：`reincarnation-expectations.md`（本 change 目录下独立文档）
- [x] 3.2 projection 无 compaction 快照时的 fallback 重建设计（并入 expectations 文档「记忆投影」章节）
- [x] 3.3 nil-probe 任务（plan/subagent）的回收策略设计（并入 expectations 文档「任务看板」章节）
- [x] 3.4 验收标准与演练脚本（并入 expectations 文档末章）

## 4. 部署与验证
- [x] 4.1 换装部署（与 1.6 同一动作，一次部署覆盖 1.6/2.4/4.1/4.3 前置）
- [x] 4.2 验证 panic 消失（换装后 ≥5 轮含工具调用的消息处理，日志无 panic/nil pointer/detector 栈）
- [ ] 4.3 验证 plan 工具可用（与 2.4 同一动作，合并报账）

## 5. 模型配置五坑处置（本阶段由 plan 扩充，2026-09-13）
- [x] 5.1 坑1+坑2：统一模型解析链路——已落地（commit 见 git log fix(wiring)）：全局默认走 resolveGlobalDefaultModel 注册表工厂（热更可感知，缓存 key 含 endpoint）；entry SwappableModel 保留双链路（AReaL TAGENT_API_ENDPOINT + 运行时 swap，评审成文 decision-log D-2026-09-13-02）；org 指纹纳入全局 Model/Provider + 新增测试；死配置 api_endpoint 已删；gofmt/build/vet/test 全绿（t6 EXIT=0）
      - 核实主 agent trajectory 包装顺序（`main.go:112/133` `openai.New()` 与 TrajectoryRecorder 的先后关系），确认主 agent LLM 调用是否入 trajectory
      - 核实主 agent 在换装/hot-reload 场景是否命中 `wiring.go` resolveAgentModel 的 modelOverrides early-return 分支
      - 清理 `tagent.yaml` L20 全局 `api_endpoint` 死配置（被 L36 providers.deepseek 覆盖）及误导注释（L21「与 zhipu 同端点」）；处理方式随坑4决策（回退 deepseek → 死配置随之消失；保留 deepseek → 删除死配置+修正注释）
      - 将 `main.go:112/133` 的 `openai.New()` 硬编码迁至 resolveAgentModel 工厂统一解析（或经评审确认保留双链路的正当理由并成文）
      - 证据口径：gofmt/go build/go vet/go test 全绿 + 前后 diff + git commit hash
- [x] 5.2 坑3：观测盲区修复（depends_on: 5.1 链路统一结论）
      - 依据 5.1 核实结论：若主 agent 命中 early-return 跳过 TrajectoryRecorder 包装 → 移除 early-return 或在其分支内补包装
      - 若主 agent 走 `main.go` 独立链路未包装 → 在 `openai.New()` 产物外补 TrajectoryRecorder
      - 证据口径：换装后 trajectory dump 中可见主 agent LLM 调用条目（路径 + 条目数）
- [x] 5.3 坑4：zhipu→deepseek 切换决策（用户决策点）——已裁决 A：回退 glm-5.3-flash；commit 65aff87（19:26）+ 换装 887174（20:02）实证生效：wiring.go:156 resolved "glm-5.3-flash" via "zhipu"；决策记录见 decision-log.md
      - 决策选项 A：回退 cc21241，恢复用户 9-11 指令的「全部改 glm-5.3-flash」
      - 决策选项 B：保留 deepseek 切换，补齐观测（LLM 调用入 trajectory）与 provider 健康监控后再确认
      - 决策需用户明确拍板；决策结果记录于本 change 目录 `decision-log.md`
      - 决策后执行：A 路线 → 执行回退 + tagent.yaml 模型名修正 + 回归；B 路线 → 实施观测补齐 + 偂康监控
- [ ] 5.4 坑5：plan 子 agent 空响应诊断（depends_on: 5.3，因空响应与供应商链路相关）
      - 复现确认：zhipu glm-5.3-flash 空响应在 plan 子 agent（model=glm-5.3, messages=56, thinking_enabled=true）是否复现
      - 逐一排查：thinking 参数（thinking_enabled=true 对 56 条消息上下文的兼容性）、流式/非流式、超时、请求体参数
      - 建议先采集：一次带完整请求体的失败调用日志（或本地 curl 复现）作为诊断基线
      - 产出：诊断结论（根因归类：thinking/流式/超时/其他）+ 修复或规避方案；若与供应商端点相关，反馈进 5.3 决策
- [ ] 5.6 knowledge 子 agent 换 deepseek-flash + reasoning effort=max（用户 2026-09-13 20:2x 指令；**门控：5.1/5.2 修复完成且换装生效后执行**）
      - 改点：tagent.yaml agents.knowledge 段显式 provider: deepseek / model: deepseek-flash + effort 字段（先实证 effort 配置 schema：grep ReasoningEffort 消费链，勿猜）
      - 深浅注意：deepseek 走 api.deepseek.com/v1，与全局 zhipu 并存；改后需换装重启（全局/agent 级模型不在热更指纹范围——5.1 修复前）
      - 验证：wiring.go:82/156 resolved 日志 + knowledge 实调 trajectory 确认 model=deepseek-flash

- [ ] 5.5 设计文档 canonical 归位核查（轻量）
      - `reincarnation-expectations.md` 已在 canonical change 目录（此前核查已达成）→ 本任务降级为：随 §3 设计产出更新后，确认文档内容与 §6/§7 实现对齐（设计→实现同步核查）
      - 无需移动文件动作

- [ ] 5.7 热更换在途回合安全（2026-09-13 21:5x 事故修复，阻塞 5.6 重落）：org-hotreload 指纹变更触发的 executor 换装在回合中途执行，在途回合消息序列丢失（system-only 请求 → zhipu 400/1214 ×3 @21:51:21/21:51:37/21:55:57）。要求：换装前 quiesce 检查（无在途 LLM 调用才 swap）或换装时在途状态迁移；证据口径：yaml 变更期间在途回合 LLM 调用不受影响（trajectory 无 system-only 请求、无 400）
      - 注：5.6（knowledge→deepseek-flash+max）已于 21:50 首次上线即触发本事故，用户回滚（worktree=0c136b5，main 已复位）；重落前置 = 5.7 完成 + 改走重启部署 + 预检 deepseek 端点对 effort=max 的接受度
## 6. WAL fallback 重建实现（depends_on: §3.2 设计成文）
- [x] 6.1 测试先行：为 `projection_rebuild.go` 补 `latestCompactionKey()==0` 场景单测（新世空投影=失忆），先红后绿
- [x] 6.2 实现 fallback 重建：`latestCompactionKey()==0` 时改走 `fetchTailEvents(0)` + 设计文档五类过滤 + cap 截断
- [x] 6.3 回归：gofmt/go build/go vet/go test ./agent/... 全绿；补充全量投影重建正确性对比（有/无 compaction 快照两路径结果一致性）
- [x] 6.4 commit

## 7. nil-probe 任务双通道回收实现（depends_on: §3.3 设计成文）
- [x] 7.1 实现 reconcileZombies 豁免条款 + isTerminalExpired 对 suspect 的处理 + dedup key 挡 re-spawn 的解除条件
- [x] 7.2 实测：构造 nil-probe plan/subagent 任务，验证回收链路（标记→回收→re-spawn 不再被挡）
- [x] 7.3 回归全绿 + commit

## 8. 验收与换装演练（depends_on: 5/6/7 全部完成）
- [ ] 8.1 全量回归：go build/vet/test ./... 全绿
- [x] 8.2 换装部署演练：走 restart-tagent.sh 或既有换装流程，留存部署证据（二进制 sha/size、restart.log、healthz）
- [ ] 8.3 转世演练验收：模拟未压缩 WAL 重启 → 投影 fallback 重建成功（新世可见上一世尾部事件）；模拟 nil-probe 任务 → 回收链路生效；主 agent LLM 调用入 trajectory 可见
- [ ] 8.4 验收矩阵全绿 + 归档准备（报账后由 plan 执行 archive）
