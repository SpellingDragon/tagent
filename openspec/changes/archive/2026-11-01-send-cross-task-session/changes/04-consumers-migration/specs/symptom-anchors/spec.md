# symptom-anchors Specification（delta：锚随 send 新语义更新）

> 本文件为 MODIFIED delta。既有症状锚体系不变；本变更的九则直译锚（S1–S3+N1–N6）登记为本变更验收第一行，锚的工具名/语义随 send 新契约更新（04 域承载迁移）。

## Purpose

消费面迁移的验收契约：症状锚不被工程锚替换、迁移后仍钉用户视角场景。

## MODIFIED Requirements

### Requirement: 锚迁移不缩水

既有 send/deliver_summary/resume_task 相关症状锚 SHALL 迁移到新语义（工具名/调用形态更新），其用户视角断言 MUST NOT 被弱化或删除；新增九则直译锚 SHALL 第一天红着并随域完成转绿。

#### Scenario: 迁移后仍钉场景

- **WHEN** 完成 04 域迁移
- **THEN** 每则锚仍断言用户视角端到端场景（收到几条/看到什么），非工程重述

#### Scenario: 工程绿症状红以症状为准

- **WHEN** 工程锚全绿而症状锚红
- **THEN** 以症状为准，不得归档
