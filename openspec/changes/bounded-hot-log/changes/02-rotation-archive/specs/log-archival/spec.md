## ADDED Requirements

### Requirement: 段界确定且索引可重建

轮转与封段 SHALL 以 `cut`（安静点提交批）为唯一时点，段界 SHALL 由 cut 序号与提交序确定，且 SHALL NOT 在 cut 提交批的 barrier 返回成功之前推进（失败 cut 的残帧留在开段）；字节阈值 SHALL 仅作为请求提前 cut 的触发器，MUST NOT 在非安静点封段。同输入段界 MUST NOT 依赖时间或调度顺序。封段索引 SHALL 覆盖段内每一提交键（键→偏移+长度），并 SHALL 可从段文件重建；重建事件 SHALL 具名上报。

#### Scenario: 索引损坏可重建

- **WHEN** 段索引损坏而段文件完好
- **THEN** 打开 SHALL 重建索引并具名报告
- **AND** 所有键的按身份读 SHALL 逐字节相等

#### Scenario: 安静点不可达时超界等待

- **WHEN** 字节超界而安静点长期不可达
- **THEN** 系统 SHALL 容忍超界并以 `hot_over_budget` 观测计数具名上报
- **AND** MUST NOT 在非安静点封段

### Requirement: 搬迁序固定且崩溃幂等

搬迁 SHALL 固定为 copy→fsync→manifest 提交→删热段→收缩热索引；热段删除 MUST NOT 先于 manifest 提交。搬迁 SHALL 仅覆盖段界 ≤ 倒数第二个 cut 的段；最新 cut 段与滚动开段 MUST NOT 被搬迁。任一切点中断后重开 SHALL 一致，重复搬迁 SHALL 幂等。

#### Scenario: 四切点

- **WHEN** copy 后、fsync 后、manifest 后、删除中各注入一次停止
- **THEN** 重开 SHALL 收敛到相同归档态
- **AND** 每条已提交事实 SHALL 恰好可读一次（热或归档其一）

### Requirement: 热索引随归档收缩

归档完成后热卷派生索引 SHALL 有界于热段键数加段级定位条目；指向归档键的逐键热索引条目 MUST NOT 保留。

#### Scenario: 收缩水位以 manifest 为准

- **WHEN** 崩溃发生在删除热段与收缩索引之间
- **THEN** 重开 SHALL 以 manifest 为准完成删除与收缩
- **AND** MUST NOT 用热索引反向修正 manifest

### Requirement: 定位读三态错误

按身份读 SHALL 区分：未陈述（热与归档都无）=`ErrFactAbsent`；manifest 陈述而段缺=`ErrArchiveMissing`；校验失败=`ErrArchiveCorrupt`。三态 MUST NOT 互相折叠；`pointReadLog` 窄口语义不变。

#### Scenario: 归档在而段缺

- **WHEN** manifest 有段条目而段文件缺失
- **THEN** 读该键 SHALL 返回 `ErrArchiveMissing`
- **AND** MUST NOT 答不存在
