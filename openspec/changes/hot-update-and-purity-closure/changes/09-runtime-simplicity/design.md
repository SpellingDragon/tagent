# Design: 09-runtime-simplicity（统一原子存储接口重定）

## D1 架构裁决（用户 2026-11-08 裁定，取代"journal 并列档/升默认退役 localfile"两案）

存储不是并列两套机制，而是**一个公开原子接口 + 其后可互换引擎**。引擎差异不得泄漏到调用方与测试；换引擎零适配。

- 唯一公开接口（导出，提升/取代内部 kvPort）`AtomicStore`：
  - `Commit(ctx, batch)` 全有或全无落盘（事实+索引一个原子单元）；
  - `Read/Get` 只读已落盘，禁止 resident-unlanded 读；
  - 屏障失败 → 具名 `CommitUncertain`（调用方重读判定，不静默成功/失败）；
  - `Sync/Barrier` 语义单一，每次提交恰一次屏障。
- 引擎实现该接口：localfile-shard / journal / 未来 sqlite。`OpenTier` 按 config 选引擎但**返回接口类型**；调用方/测试只依赖接口。
- **故障注入在接口边界**：port decorator（fail 第 n 次 Commit / fail Barrier / 慢写），引擎无关。原 7 处分片级写失败注入全部改为 port 注入；"事实写失败而账本可写"用双 port（事实 port 失败、账本 port 正常）表达；`ErrSegmentBodyMissing` 经独立 OriginalStore 保留可达。
- 分片概念下沉：kvStaged/kvUnstage/writeShard/readShardSnapshot 进 localfile 引擎内部或删除；engine-agnostic 层（FileSegmentStore/Tier）不留分片词。
- 退役 localfile = 删一个引擎实现，调用方/测试零改动（都走接口+port 注入）。journal 复用 09 已写帧/CRC/单屏障代码作为一个引擎，非并列新档。

## D2 原子边界（用户裁定）

**write-ahead body + 原子事实提交**：原文 body 内容寻址、write-ahead 先落 OriginalStore；事实+索引原子提交后引用 body；提交失败孤儿 body 可 GC（内容寻址、无悬空引用）。读者视角等效原子：事实可见 ⇒ body 已 durable。OriginalStore 保持独立接口，不纳入事实提交单元。

## D3 迁移与回退

- 存量 localfile root：提供一次性迁移器（localfile 只读 → journal 引擎写新 root），迁移期 localfile 仅作只读迁移源；迁移完成前不删 localfile 引擎。
- 接口落地后引擎可互换；"回退零 diff" standing 规则在引擎删除阶段由用户显式 override（记 ADR）。

## D4 被否方案

- journal 作并列 StorageType 新档（否：第二套机制、引擎差异泄漏到测试/调用方）。
- 就地切 journal 默认+重建 journal 专属注入面（否：注入面仍绑引擎，换引擎仍需适配）。
- body 纳入同一原子单元（否：引擎重、OriginalStore 独立性丧失、跨引擎迁移搬 body）。

## 风险与回退

接口重构爆炸面=memory/ledger 全层+7 测试注入面+examples；以 port 注入契约测试钉住引擎无关性（同一套契约测试跑两个引擎都绿）作为放行门。回退：接口层独立可回滚；引擎删除单独一步。