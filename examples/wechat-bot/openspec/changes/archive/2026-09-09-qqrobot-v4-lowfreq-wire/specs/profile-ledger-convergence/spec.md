## Purpose

用户画像与群级频率统计的账本收敛：频率判定状态单一事实源，画像收缩为身份/别名/备注/展示计数职责，落盘格式保持不变。

## ADDED Requirements

### Requirement: 频率状态单一事实源

频率判定所需状态（尾随 24h 发言时间戳、lifetime 计数、首见时间）SHALL 仅由群级统计账本维护；画像模块 SHALL NOT 维护频率判定专用状态。

#### Scenario: 删除画像侧频率状态

- **WHEN** 开发者检查画像模块结构
- **THEN** 不存在频率判定专用字段（滑窗时间戳列表、首见时间），仅保留身份字段（昵称/角色）、别名、备注与展示用 lifetime 计数

### Requirement: 画像落盘格式不变

画像持久化文件（profiles.json）SHALL 保持既有字段结构不变：仅含 openid/nickname/role/aliases/notes，不含任何统计性字段。

#### Scenario: 升级后落盘兼容

- **WHEN** 机器人按新代码保存画像
- **THEN** profiles.json 条目结构与升级前逐字段一致（无新增/缺失/改名），旧文件可直接加载

### Requirement: 画像展示计数保留

画像速览（list_known_users 工具）SHALL 继续展示每位用户的 lifetime 发言计数。

#### Scenario: 速览含发言数

- **WHEN** 画像库非空时请求用户速览
- **THEN** 每条用户条目含其发言次数
