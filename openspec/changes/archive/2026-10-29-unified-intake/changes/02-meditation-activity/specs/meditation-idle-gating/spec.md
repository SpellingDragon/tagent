# meditation-idle-gating（MODIFIED delta）

## MODIFIED Requirements

### Requirement: 冥想触发采用双闸门判定

冥想触发 SHALL 采用双门判定：novelty 门（观察面内存在 `Timestamp > lastMeditation` 且非自管谱系的事件）与节奏门（`now - lastMeditation >= min_gap`，两次**执行**之间的下限；`lastMeditation` 为零时直通）。节奏门 SHALL 再叠两侧结构性活跃判据：①novelty 扫描中最近一条非自管新鲜事实（含触发本次评估的提交本身）距 now 不足 `min_gap` → 不 fire（冷却自最后用户事件重置）；抑制 SHALL 保持本次 episode（推迟非放弃：水位不动、`pending` MUST 保持为空），其评估机会 SHALL 为下一次既有重评节拍（窗口过后重评）；②最近一条非自管新鲜事实所属地址的既有决策席被在途决策占用 → 不 fire（推迟非放弃：水位不动、`pending` MUST 保持为空）。自管谱系回合（结算摘要/回执注入）SHALL NOT 重置冷却。让路 SHALL 由注入时刻的批次合并规则承担（见「混合批次中丢弃冥想事件」），MUST NOT 以任意回合的时间门槛预先判定忙闲。

#### Scenario: 节奏门按执行间隔判定

- **WHEN** 上一次冥想执行距今不足 min_gap 且观察面有新事实
- **THEN** 本 tick 不触发；间隔满足后的下个 tick 可触发

#### Scenario: 活跃 session 抑制

- **WHEN** 同 session 有用户事件且距其不足 min_gap
- **THEN** 冥想 SHALL NOT fire；冷却自最后用户事件起算

#### Scenario: 活跃窗口过后的重评

- **WHEN** 判据①抑制了一轮评估，此后会话安静，既有重评节拍到达时该事实距 now 已满 min_gap
- **THEN** 冥想 SHALL fire（同一 episode 内重评，无需新的 host-facing 提交；`min_gap>0` 时生产因此可 fire）；期间持续提交 SHALL 继续抑制

#### Scenario: 决策席在途抑制

- **WHEN** 该 session 决策席被在途 turn 占用
- **THEN** 冥想 SHALL NOT fire，水位不动；评估机会 SHALL 为下一次 host-facing 提交（安静会话不自行恢复）

#### Scenario: 家务回合不阻塞

- **WHEN** 观察面所属属主的 loop 持续运行自管谱系回合且观察面有非自管新事实
- **THEN** 节奏门不受这些回合影响（不重置冷却），到点即评
