# fold-provenance Specification（delta）

## ADDED Requirements

### Requirement: 折叠摘要携带时序与世代

折叠摘要行 MUST 以行首锚点携带事件时刻（至少天级+时:分）与 recorder 世代标识；世代标识 MUST 取信封 instance_id 的**完整值**（MUST NOT 截断，如只取前 8 位）；该值 MUST 可经既有投影条目读取面取得（承载字段与读取方法的实现形态见 design，MUST NOT 新增导出面）；锚点 MUST NOT 被摘要截断移除。**segment-fold 产物 MUST 在其 Content 的行首保留源行的同一前缀**（`[MM-DD HH:MM|gen:…]` 从 Content 首字符起；任何 `folded segment process:` 类前缀 MUST 移到锚点之后或移除）；**history summary 对 segment-fold 产物 MUST 改用其 Content**，保证二次折叠后源行时序与世代仍可辨。无世代信息的条目 MUST 以显式占位符标注，MUST NOT 伪造标识。

#### Scenario: 跨重启回放折叠

- **WHEN** 含跨重启历史的事件链被折叠为 history summary
- **THEN** 每行摘要以 `[MM-DD HH:MM|gen:…]` 开头，读者可分辨时序与世代

#### Scenario: 二次折叠仍可分辨

- **WHEN** 含 segment-fold 产物的历史再次被折叠为 history summary
- **THEN** 每行摘要仍以 `[MM-DD HH:MM|gen:…]` **行首**开头（断言=行首锚点，不接受前缀夹带），源行的时刻与世代可辨，不因二次折叠丢失

#### Scenario: 缺世代信息

- **WHEN** 某被折叠条目无法追溯 recorder 世代
- **THEN** 该行以 `gen:?` 占位，折叠不失败
