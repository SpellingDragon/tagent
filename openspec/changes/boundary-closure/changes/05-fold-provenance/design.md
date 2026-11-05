# 折叠可追溯：技术要点

## 文件清单

- memory/replay/age_segments.go：historySummaryLine(:491)/historySummaryOf 与 foldProcessDetail(:419) **两处公式注入时刻+世代**；**第三处=foldProcessDetail 产物 Content 行首即锚点（`[MM-DD HH:MM|gen:…]` 在 Content 首字符；原 `folded segment process:` 前缀移到锚点之后或去掉），historySummaryLine 对 segment-fold 产物改用其 Content**；**gen=完整 InstanceID**
- **assembly/facts.go：mappedEntryOf 透传 InstanceID→ProjectionEntry**（信封→投影入口的唯一映射处）
- **memory/replay/projection.go：`ProjectionEntry` 新增未导出 `instanceID` 字段 + `Generation()` 读取方法 + `WithInstanceID` 设置方法**（不新增导出字段；沿用 `inputEvents`/`messageRole` 私有字段先例，导出面仍是五字段）
- memory/replay/age_segments_test.go（锚⑦与负锚）
- ~~AGENTS.md 注记修正~~（HEAD d6bfc363 已修正，本域仅留防回归负断言）

## 关键决策

- 时刻粒度：天级+HH:MM（随事件时间戳原文时区，不换算）；**三处公式同改**：①historySummaryLine（age_segments.go:491）对普通条目注入 `[MM-DD HH:MM|gen:…]` 前缀；②foldProcessDetail（:419）的源行同样以该前缀渲染；③historySummaryLine 对 segment-fold 产物（ProjectionTypeSegmentFold，Content 非空）改读其 Content，**且其行首即 `[MM-DD HH:MM|gen:…]` 锚点（`folded segment process:` 前缀 MUST 移到锚点之后或移除）**——缺①则 history 折叠剥时序，缺②则 segment 折叠剥时序，缺③则 segment 产物被二次折叠时源行 provenance 丢失。`TestSecondFoldKeepsProvenance` 锁定二次折叠后**行首锚点**（时序/世代仍可辨）。
- 世代标识：**完整 InstanceID**（信封 instance_id 全串，不做前 8 位截断——recorder-* 前缀会撞车）；entry 无世代信息时以占位符 `gen:?` 显式标注（不得伪造）。**承载机制（F11/F12）**：`ProjectionEntry.instanceID` 未导出，`mappedEntryOf` 经 `WithInstanceID` 复制设置、读侧只经 `Generation()`；不新增导出字段、不动 `event.Envelope`。
- 摘要行格式：`[MM-DD HH:MM|gen:<完整 InstanceID>] Type: Summary`；解析侧无消费者（纯呈现），无兼容负担。
- 锚⑦断言：行首 `gen:` 的值 MUST 与该行事实的 `Envelope.InstanceID` 逐字相等（占位仅允许 `gen:?`），不是“至少四字符”的弱形态。**`TestSecondFoldKeepsProvenance` 的断言=行首锚点**——`[MM-DD HH:MM|gen:…]` 从 Content 首字符起匹配，不接受前缀夹带。
- 注记：HEAD d6bfc363 已修正两处 AGENTS.md 注记；本域不改该文件，只留防回归断言（先注入一词证明 grep 能红，再断言原文件 0 命中）。

## 计量诚实更正（r21 裁定）

锚点字节是模型真实看到的内容，计入渲染体积是诚实计量（P11）——同阈值下折叠略提前是真实代价而非缺陷；request_fidelity 窗口 375→440 重定基已接受，本域“不改变折叠触发阈值语义”的原表述作废。

## 风险与回退

- 时刻字段增加摘要行长度，极端长 Summary 时截断 Summary 保锚点前缀（锚点在行首不可截）。
- 回退：独立域，可直接 revert。
