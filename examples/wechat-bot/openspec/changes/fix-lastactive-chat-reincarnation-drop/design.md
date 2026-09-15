## Context

main.go 消费循环中，task 输出兜底路由依赖 `lastActiveChat := sync.Map{}`（main.go:312）。用户消息到达时 Store "latest" -> chatID（main.go:347）；task 输出无 meta_chat_id 时 Load 兜底（main.go:388）。纯内存结构在热换装（restart-tagent.sh 自杀换装）/重启后清零，导致转世后首条 task 输出被静默丢弃（log 35127 行 WARN 实证，案例：销假报告）。

run/ 目录为既有运行态目录（含 restart.done / env.snapshot 等），适合存放该锚点文件。改动要求外科手术式最小：仅动 main.go 中锚点的存取路径，不改消费循环其他逻辑。

## Goals / Non-Goals

**Goals:**
- 锚点跨进程存活：转世后首条无 meta_chat_id 的 task 输出正确送达
- 持久化/回种失败均为非致命（WARN），不影响主流程与启动
- 部署前编译预验证（go vet + 预构建到 /tmp），部署用已两次实战验证的自杀换装机制

**Non-Goals:**
- 不改造消费循环整体路由架构、不引入外部存储
- 不处理多会话并发的锚点语义（仍保持 "latest" 单锚点）
- 不追溯已丢失的历史消息

## Decisions

1. **持久化路径：`run/last_active_chat`，纯文本单行 chatID**
   - 与 run/ 目录既有运行态文件（restart.done、env.snapshot）同级同风格，无需新目录或格式约定
   - 备选：写入 SQLite/JSON state 文件——对单字符串过重，且违反"最小改动"约束；否决

2. **原子写：临时文件 + rename（同目录）**
   - 自杀换装场景进程可能被 kill -9，非原子写会留半行 chatID；rename 在同目录内原子
   - 备选：直接 WriteFile——半写风险；否决

3. **回种时机：main() 启动早期、消费循环初始化前后均可，先读后 Store**
   - 在 lastActiveChat 声明后立即 seed，保证任何 task 输出到达前锚点已就绪
   - 文件缺失时静默/DEBUG（首次运行无文件是正常态）；读取失败/内容空时 WARN 不致命

4. **写入时机：与 Store("latest", chatID) 同点触发（main.go:347 附近）**
   - 保证内存与磁盘锚点同步更新，不做定时刷盘（避免窗口期）
   - 备选：defer/定时持久化——引入不一致窗口；否决

## Risks / Trade-offs

- [chatID 含换行/异常内容导致锚点文件脏] → 回种时 TrimSpace + 非空校验，异常内容 WARN 并忽略
- [run/ 目录不可写（容器只读等）] → 写失败仅 WARN，功能退化为现状（内存锚点），不致命
- [磁盘锚点陈旧（用户很久未发消息）] → 转世后首条兜底可能路由到旧会话；这是"送达旧锚点"优于"静默丢弃"的明确取舍，且 meta_chat_id 存在时不受影响
- [换装窗口期竞态：写入与 kill 同时发生] → 原子写 rename 保证要么旧值要么新值，无半写态

## Migration Plan

1. 代码修改（仅 main.go：helper + 两处调用点）
2. `go vet` + 预构建到 /tmp 验证编译通过
3. git commit
4. restart-tagent.sh 自杀换装部署（old_pid=243828，机制已两次实战验证）
5. 转世后验证：新进程 seed 日志行存在 + 销假报告送达用户微信

回滚：git revert 该 commit 并再次换装即可；run/last_active_chat 残留文件无害（回种失败仅 WARN）。

## Open Questions

无。
