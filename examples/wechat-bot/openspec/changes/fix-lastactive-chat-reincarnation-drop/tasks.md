## 1. 代码修改（main.go）

- [x] 1.1 在 main.go 中新增 helper：persistLastActiveChat(chatID string)——原子写 run/last_active_chat（临时文件+rename），错误仅 WARN 不致命
- [x] 1.2 在 main.go 中新增 helper：seedLastActiveChat() ——启动时读取 run/last_active_chat，TrimSpace+非空校验后回种 lastActiveChat（Store "latest" -> chatID），输出 seed 日志行（含 chat_id 或明确"已回种"语义）；文件缺失静默/DEBUG，损坏 WARN 不致命
- [x] 1.3 在 lastActiveChat 声明处（main.go:312 附近）调用 seedLastActiveChat()，在 Store("latest", chatID) 处（main.go:347 附近）追加调用 persistLastActiveChat(chatID)

> **部署节奏调整（2026-09-13，用户指令）**：本计划的编译/提交/部署/验证（步骤 2-5）与
> `tagent-unify-model-call-config` 重构**合并为一次自杀换装**（单次转世、两个独立 commit：
> fix commit 先行、重构 commit 随后）。步骤 2-5 待重构 commit 就绪后合并执行。
> 另：模型切换配置（全局 deepseek-flash / summary deepseek-flash+effort low / recall effort low）
> 已先落 tagent.yaml（18 / 119-121 / 171 / 192 行）；其中 `summary_effort` 依赖重构后的
> CompressConfig 字段支持，当前非 strict 解析静默忽略——为重构必要性的实证案例。

## 2. 编译预验证

- [x] 2.1 go vet 通过（在 tagent/examples/wechat-bot 目录）——2026-09-13 报账：全模块干净 true exit
- [x] 2.2 预构建到 /tmp（如 go build -o /tmp/wechat-bot-precheck .）验证编译通过，不触碰运行中二进制——2026-09-13 报账：根+wechat-bot 双模块 build 全绿

## 3. 提交

- [x] 3.1 git commit（外科手术式最小 diff，仅 main.go + run/last_active_chat 运行态文件不入库）——已实证 commit e6fd125（.git/logs/HEAD:50）

## 4. 部署（自杀换装）

- [ ] 4.1 用 restart-tagent.sh 以 old_pid=243828 执行自杀换装部署

## 5. 转世后验证

- [ ] 5.1 新进程日志存在 seed/回种日志行（含回种 chat_id）
- [ ] 5.2 销假报告成功送达用户微信（无静默丢弃 WARN）
