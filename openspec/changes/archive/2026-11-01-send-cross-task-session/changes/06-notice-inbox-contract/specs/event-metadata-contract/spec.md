# event-metadata-contract Specification（delta：逐消息来源入 Metadata）

> 本文件为 MODIFIED delta。键词表扩展一条：通知消息的原始来源键（`MetaKeyNoticeSource` 语义），与回合级 `trigger_source` 分账（R4，design D11）。

## Purpose

每条入箱消息的原始来源随消息进持久事实 Metadata；"这条事实从哪里来"与"当前回合由谁触发"是两个键，互不覆盖。

## MODIFIED Requirements

### Requirement: 逐消息来源可查

通知/内部消息 SHALL 在其持久事实 Metadata 携带原始来源（`reincarnation`/`system_alert`/`receipt` 等），键名走 `MetaKey*` 单点定义；回合级 `trigger_source` MUST NOT 因混合输入被改写或丢失。运维通知与普通用户消息同批进入时，各自来源 SHALL 可从持久事实查询而无需解析正文。

#### Scenario: 同批各自可查

- **WHEN** 运维通知与用户消息同批进入并落事实
- **THEN** 各自 Metadata 来源可分辨；`trigger_source` 仍反映回合触发者

#### Scenario: 键同源

- **WHEN** 写入与解析通知来源
- **THEN** 键名引用 `MetaKey*` 唯一来源，无硬编码字符串
