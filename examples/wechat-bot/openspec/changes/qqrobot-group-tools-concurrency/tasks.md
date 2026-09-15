## 1. v2 REST 直调客户端（SDK fork 替代路径）

> botgo v0.0.9 无 v2 群管理封装；fork 因 GitHub 网络不可达 DEFERRED（见第 7 节）。
> 本节按 `kb/api/qq/group_restrict_chat_setting.md` 契约在主工程内 net/http 直调 v2 REST，
> 不动 SDK。

- [x] 1.1 新增 v2 直调客户端（如 `service/qq_v2_client.go`）：复用 robot.go:90-96 启动期已建的 `token.QQBotTokenSource`（AppID/AppSecret 换短效 QQBot token，自动刷新），`POST /v2/groups/{group_openid}/restrict_chat_setting`，body `action_type`(add/del) + `member_openid`，头 `Authorization: QQBot <token>`；超时有界、401 时经 tokenSource 重取后重试一次
- [x] 1.2 错误语义透传：非 2xx 错误体解析为可区分错误（11253 白名单未开通 / 权限不足 / 非群成员等），供工具层降级；按需实现 `GET .../restrict_chat_setting` 禁言状态查询（member_openid + mute_expire_at），供到期复核
- [ ] 1.3 `bash build.sh`（go1.18 冻结工具链）编译通过 + 白名单小流量验证：测试群以机器人自身/测试号为目标发一次 add，若报 11253 则记录结论并停用实弹项（白名单未开通），工具层保留但 description 降级为口头警告优先 → ⚠️ 待证：全工程搜 TestV2MuteRealAPI/11253 零匹配，实弹证据需出示（日志/输出）后勾选

## 2. LLM 工具化群管理（mute）

- [x] 2.1 `service/react_tool.go` 按 BindReactTools 模式注册 `group_member_mute`：schema.ToolInfo + tool.BaseTool，description 含适用情形（有害信息处置）/ "禁言后需说明与安抚"话术指引（description 即 prompt 载荷）/ "短时处置，到期自动解除"；⚠️ v2 契约**无时长字段**（原"默认 60s / 上限 300s"语义不适用于直调路径），工具参数仅目标成员（group_openid + member_openid，群上下文注入），时长由平台语义决定 → 实际落点 `service/mute_tool.go`（MuteTool，名 `group_mute`，desc 三要素齐备）；`react_tool.go` L719/726 BindReactTools 挂载
- [x] 2.2 工具底层调第 1 节直调客户端；执行失败返回结构化错误供 LLM 降级（不 panic，记日志）→ `mute_tool.go` L43-53：v2c nil / 群上下文缺失 / GroupRestrictChat 错误均结构化返回（无 panic）；`v2client.go` GroupRestrictChat 在案
- [x] 2.3 mute 不进 ToolReturnDirectly，实测与 react.Agent MaxStep 交互无循环 → `react_agent.go` L167-170：MaxStep 15，ToolReturnDirectly 仅 process_clip_images

## 3. 触发与门控

- [x] 3.1 扩展触发条件：群主 @（既有 is_you 路径覆盖）+ 昵称触发（大小写不敏感、词边界匹配防子串误匹配）→ `chat_gate.go` OwnerTriggered（openid 精确/内容前缀/昵称包含）；⚠️ 遗留：robot.go L236-238 AuthorName 位传 MemberOpenID，昵称触发需实弹确认
- [x] 3.2 活跃窗口门控：群最近消息 N 分钟内活跃时昵称触发才生效；@ 触发全量处理 → `chat_gate.go` windowUntil + ActiveWindowSec（默认 300s）；robot.go L234-239 接线
- [x] 3.3 同群冷却：冷却期内（无论 @ / 昵称）不再发起 LLM 调用，静默丢弃记日志 → `chat_gate.go` LLMAllowed 冷却闸（默认 60s）
- [x] 3.4 LLM 调用预算：窗口内调用次数 / token 上限，耗尽丢弃记日志，窗口重置恢复 → `chat_gate.go` LLMAllowed 日预算（默认 100/日，日期锚重置）；config 四参数已落 entity

## 4. 消息并发化

- [x] 4.1 per-group FIFO 有界队列（同群串行，回复顺序 = 触发顺序；超限丢最旧/拒新并记日志）→ `service/group_queue.go`（per-group cap 8 + 全局 4 worker + panic recover + 队满丢弃计数）
- [x] 4.2 有界 worker 池跨群并行（池大小配置化；池满任务排队不扩 goroutine）→ `group_queue.go` GetDispatcher：4 worker 信号量；DispatchGroup(0)=永不过期语义已修
- [x] 4.3 单条处理超时 + panic recover 隔离（超时取消后同群队列继续，进程不崩）→ `group_queue.go` jobTimeoutSec=120 + recover；robot.go L263-269 群消息投递接线（频道 L347/L370 同步）
- [x] 4.4 【增·多模态】群消息附件图 URL 提取：`chat_gate.go` ParseGroupAttachments（RawMessage 解析 attachments[].url，QQ 多媒体域名前缀补全）+ robot.go L240-243 接线，`[img:URL]` token 贯通
- [x] 4.5 【增·多模态】ReactCtx.ImageURLs + userMessage() eino schema MultiContent（Text+ImageURL parts，eino@v0.5.4 L295-297/L225-240 契约）

## 5. 构建与实测验收

- [x] 5.1 `bash build.sh` 全量构建通过（主工程，go1.18 工具链；不涉及 fork）→ 构建 45373693 全绿 + go vet 干净（代码层）；二进制产物 wechat-bot（54MB）在案
- [ ] 5.2 测试群实弹禁言一次（前提：1.3 白名单验证通过）：目标为测试账号（非真人），确认到期自动解除（GET mute_expire_at 复核或平台到期行为实测）、群内出现说明与安抚话术
- [ ] 5.3 实测门控与并发：冷却丢弃 / 活跃窗口 / 预算耗尽 / 同群串行回复顺序 / 多群并行各验证一次（日志为证）
- [ ] 5.4 确认 config.yaml 敏感字段未入 git（cookie/token 脱敏，.gitignore 覆盖）

## 6. kb 归档

- [ ] 6.1 kb/INDEX 更新（新能力条目：LLM 群管理工具（v2 REST 直调）/ 门控 / 并发模型；不含 fork v0.1.0）
- [ ] 6.2 kb/timeline 更新（本特性包时间线条目，含实测结论与 SDK defer 记录）
- [ ] 6.3 归档核验：build 产物存在、kb 文件非空、DEFERRED 项已登记（第 7 节 + timeline），然后归档本计划

## 7. DEFERRED：botgo SDK v0.1.0 fork（网络恢复后另立 change，不计入本期验收）

> 侦察结论（2026-09-08）：GitHub 网络四路全败（git@ SSH 挂起、HTTPS clone/tarball 超时，仅 ssh -T 认证通），fork 工作区不可得；用户指示"SDK 拉不下来就先不优化 SDK"。
> 以下为普通列表（非 checkbox），不参与本期任务统计与归档门控；网络恢复后另立 change 执行。

- D1: 重新克隆 botgo fork 到 `src/botgo`（克隆后 ls + git log 验证非空），确认基线 v0.0.9
- D2: fork 内实现 v2 群管理封装：禁言（正确路径 `POST /v2/groups/{group_openid}/restrict_chat_setting`，⚠️ 网传 `PUT .../member_mute` 不存在，以 fork 内官方 autogen 文档为准）+ 群成员角色查询（契约未入 kb，直调无依据，故随 SDK defer）
- D3: 核对群富媒体 v1 既有封装无行为回归（编译 + 关键路径走查）
- D4: fork 保持 go1.18 兼容（无泛型 / 新标准库），`git tag v0.1.0` 并推送
- D5: 主工程 go.mod `replace github.com/tencent-connect/botgo => ../src/botgo`，`bash build.sh` 通过
- D6: mute 调用层从直调客户端切回 SDK 封装（第 2 节已隔离调用层，切换不改工具行为，回归验证）；群成员角色查询注册为 LLM 工具（同 BindReactTools 模式）
