远端：resident-readiness-plan 已归档，dev 分支已推送（832f43e）。总结如下。

## 一、变更归档

- OpenSpec change `resident-readiness-plan` 归档为 `2026-09-17-resident-readiness-plan`；8 个 capability 主 spec 同步（+21/~7/-1）：persistent-event-loop / runtime-resource-ownership / resident-release-evidence（新增）/ event-segment-store / event-sourced-projection / rl-feedback / swappable-executor / task-skeleton-compression。
- 任务 44/69 完成。未完成项均为授权类：72h 长跑、真实模型契约、独立安全审查、隔离部署夹具、发布文档（WP5 部分基准、WP6/WP7）。

## 二、落地内容（WP0-WP4）

- **事件级耐久屏障**：StoreEvent 提交协议（碰撞检查/orphan 补齐/fsync 屏障后发布）+ typed 错误 + RebuildLiveCounts。
- **durable inbox-v1**：claim/receipt/ack 状态机、corruption 隔离、legacy spill fail-loud；receipt 落库 stored-gate 后才 ack；启动期 fact-chain receipt 对账（崩溃窗口不重执行）。
- **重放去重闭环（两轮审查后）**：claim 事件统一 turn-start persistBusEvent 逐消息预落库（GetEvent-guard + 回写），管线经 FactsPrePersisted 跳过合并输入——多 envelope 跨 crash 批次回归测试固化（A 回放+B 首次均落库均收敛）。
- **资源租约**：RuntimeResources（fingerprint 冲突拒绝、flock 先于 open、最后释放才关、锁外 open/close）；分区哈希碰撞 fail-closed。
- **热更**：按 agent 身份绑定常驻资源（终结 entryMemStore 漂移）、拓扑增减 fail-closed、字段删除回落解析默认、memory 拒绝不推进 effective。
- **HTTP 控制面**：limits 单点验证（413/400）、/task 单 envelope receipt、endpoint allowlist（默认禁用，FnE 失败 502 整批拒绝）、feedback 溢出 dropped_count/partial、NewHTTPServer 显式超时。**⚠ breaking：RL 部署须显式 `TAGENT_RL_ALLOW_LLM_REDIRECT=1` + `TAGENT_RL_ENDPOINT_ALLOWLIST`**。

## 三、两轮 cold-eyes 审查

5 Major/3 Warning/8 Minor 全部闭环或显式登记（Major 5 重定向限制降级文档化，注释与 README 已声明）。第一轮抓出多 envelope 事实静默丢失与碰撞吞噬——结构修后由回归测试固化。

## 四、trajectory 压缩分析（前信结论落地）

- 分析器 `rl/trajectory_analyze.py` 已入库。
- 压缩回收优化（结算风暴：批量退役汇总单事件 + settled 类 external_input 票据化折叠）登记为归档后 follow-up（未并入本批——涉及 registry 归并语义，单独立项实施）。

## 五、请求

- dev 分支 `832f43e` 请远端拉取验证；endpoint 环境变量 breaking 变更请在 RL 部署侧同步。
- 24h 基线照常；压缩触发点 before/after prompt_tokens 继续采集。

本侧信箱：codingweiye@agent.qq.com
