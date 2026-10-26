## ADDED Requirements

### Requirement: 折叠的幂等与不回流

`FoldByAge` MUST 以折叠材料的内容寻址身份幂等：对同一投影、同一窗口参数重复折叠 SHALL 产出同一组折叠键或退回 no-op，MUST NOT 追加第二份同义综述、第二份 `tool_chain` 合成 ref 或第二条 compaction 判断。折叠已完成的投影（既有 fold 状态非空或已无待折材料）再次进入折叠 SHALL 短路，MUST NOT 递归折叠自身产物。`RebuildProjectionFromWAL` SHALL 只复原投影，MUST NOT 触发折叠、MUST NOT 因重建向事实链写入 compaction 事件；重建后再次装配请求 MUST NOT 产生新的折叠副作用。幂等判定 SHALL 以折叠键与事实链条目为证据，MUST NOT 以时间窗或内容相似度代替键比较。

#### Scenario: 重复折叠不追加第二份产物

- **WHEN** 对已折叠的同一投影以同一窗口参数再次调用 `FoldByAge`
- **THEN** 折叠键集合 SHALL 保持不变，投影与事实链 MUST NOT 新增同义综述或 `tool_chain` ref
- **AND** 折叠计数 SHALL 如实反映本次为 no-op，而非第二次产出

#### Scenario: 重建不触发折叠（失败）

- **WHEN** 冷启动对含折叠产物的事实链执行 `RebuildProjectionFromWAL`
- **THEN** 重建 MUST NOT 调用折叠，事实链 MUST NOT 新增 compaction 事件
- **AND** 复原后的投影渲染 SHALL 与折叠时逐字节一致（既有逐字节义务不变）

#### Scenario: 折叠产物不触发自身（失败）

- **WHEN** 折叠产物（综述 ref 或 `tool_chain` 合成 ref）再次进入同一 fold 判定
- **THEN** 折叠 SHALL 短路，MUST NOT 产生对自身的第二轮折叠或递归增长
- **AND** 被折叠原文 SHALL 仍可经既有 `[evt_key]` 票据 recall 回补（既有冷存义务不变）
