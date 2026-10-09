# Design: unify-meditation-external-form

基线 `fab5c94`。用户模型：冥想=向某 session 注入特定冥想事件；形态差异=session id 与观察面；manager 自行识别。

## D1 单机制与观察面缺省（零 breaking 的关键）

动作恒为 `InjectMessageWithSource("meditation", …)` 到**本 agent 的循环 session**——in-loop 与外部的差别只是"哪个 agent、哪个 session、观察谁"。观察面解析：`observed_namespaces` 显式声明；**缺省＝[自身分区]**（不回落 read_namespaces——那会把"能读谁"悄悄变成"反思谁"，语义过宽）；显式集合 ⊆ read_namespaces ∪ {自身}（自身恒合法，他人须授权）。被否方案：observed 必填非空+具名拒启（上一版草案——破坏现有部署且逼用户理解形态概念）；缺省回落 read_namespaces（授权面≠观察面，静默扩观察范围）。

## D2 novelty 单判据（in-loop 等效性的证明）

触发 = interval ∧ 自身空闲（lastTurnEnd，任意谱系含投递/失败回合）∧ 观察面水位 novelty（存在 `Timestamp > lastMeditation` 且非自管谱系事件；水合早停；未知谱系/读失败不计入，fail-closed）。**缺省观察面下与原 in-loop 语义等价**：用户消息入库即自身分区的 user 谱系非自管事件，"有新用户输入才反思"的旧语义被"自身分区水位后有非自管事件"严格覆盖（且更准：system_alert 等非自管输入同样值得反思）。`lastUserInput` 锚与 inject.go 挂臂删除——"注入侧是最便宜真源"的旧论让位于"一个判据通吃"的统一收益。

## D3 锚点与持久化

两锚（lastTurnEnd/lastMeditation）；`MeditationAnchors.LastUserInput` 字段删除，旧文件未知键忽略（回归测试钉住）。per-agent 锚文件路径不变。

## D4 session 安排（用户上一问的落档）

| 线 | session | 说明 |
|---|---|---|
| 业务 | 宿主路由决定（wechat-bot：`TAGENT_SESSION_ID` 缺省 `wechat-session`） | 不动 |
| 入口自察反思 | =业务 session（同一循环内注入） | 共享会话上下文——自体维护的价值所在 |
| 策展反思 | 保留名 `curation`（curator agent 自己的 StartLoop） | 固定单 session：思路连续、增长交给自身阈值折叠；永不与用户路由撞名 |

**固定而非每反思换新 session**：换新=每次冷启动重建投影（浪费）且丢反思连续性；固定的增长由 curator 自己的 compress_threshold 折叠管理（单压缩权自管）。deliver_to 回流的消息带目标 session 自述头，进业务线。

## D5 digest 单覆盖面

观察面概况为主：各观察分区分谱系计数+引用/水合样本数+最近非自管活动（带 `[hex]` 事件键）。观察面仅自身时，概况即"自体近况"——旧 in-loop digest 的任务板降为可选段（挂任务层则有）。单次扫描复用（不为渲染二次扫链）。

## D6 产物与回流

三类产物（脚本/skill/prompt）与登记纪律适用于任何反思主体（入口自察或策展人）；卡片落**反思主体自己的分区**：入口自察卡片落业务分区（旧 in-loop 语义原样）；策展卡片落策展分区，经 deliver_to 回流进目标业务 session、被自然折叠吸收。自体维护的"上下文瘦身"由阈值折叠承担（本就不依赖冥想），"深度巩固"由卡片回流承担——与上一变更 D4 论证一致，无需重复建设。

## D7 测试收敛

双判据用例合并为单判据参数化（观察面={自身}/{他人}/{自身+他人}三参数组）；回切组删除（无第二判据可切）；新增"缺省观察面=自身"in-loop 等效回归（现有 wechat-bot 形态不改配置触发反思）；混合观察面用例（统一后自然获得的新能力）。e2e/真实模型用例不动（本就是单机制形态）。

## D8 文档与锚

wiki §2.14 重写为单机制（锚 `#meditation-two-forms`→`#meditation-curator`，全库 `契约:` 行同步）；README 双语（特性/配置行/边界段）；行为矩阵"in-loop 关闭态"断言改写为"缺省自察"。

## D9 验收

单测：缺省等效回归、混合观察面、锚两字段恢复旧文件兼容、注入侧符号灭绝（编译期）；集成：wechat-bot curator e2e；真实模型：TestRealModel_ExternalMeditation 不重跑（PASS 记录即证）；全量门禁同前；spec 逐 Scenario 对账（F3）。
