# 01-vocabulary 提案

## Why

项目术语「转世」与代码标识符 reincarnation 不表意，且此前被误用的「读透视图/链权威答案」并非用户认可的项目词汇。

## What Changes

中文词汇统一为「重启恢复」；代码标识符简化规范化；撤销伪词条用法改用代码实名；契约值显式 delta 不混改。

## 边界与依赖

依赖：无实现依赖，仅文档与标识符；被依赖方：02（用新词写观测名）；接口面：GLOSSARY.md、docs/wiki/platform/reincarnation-notice.md、wechat_notice.go 标识符、F1 契约面；禁止事项：MUST NOT 改 MetaKeyNoticeSource 值、事实键、冻结类型。
