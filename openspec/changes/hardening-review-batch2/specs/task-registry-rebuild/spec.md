# task-registry-rebuild Delta

## ADDED Requirements

### Requirement: 任务身份与世系跨重启保真
任务的身份、来源与路由字段（ID / Kind / Key / Desc / Origin）MUST 作为持久化事实跨重启保真：写入 task_spawned 时 MUST 深拷贝保存运行态 Spec.Origin（trigger_source 与来源事件键）；恢复时这些字段 MUST 以 WAL 持久层为准，恢复闭包工厂返回的 spec 仅提供执行能力（runner / probe / resume），MUST NOT 整体覆盖持久化身份。

#### Scenario: 冥想派生任务跨重启恢复
- **WHEN** trigger_source=meditation 的任务跨重启恢复并发生后台结算
- **THEN** 结算事件的世系 MUST 保持 meditation 来源（MUST NOT 退化为通用 task 来源），宿主投递门禁 MUST 维持扣留

#### Scenario: 历史记录缺 origin
- **WHEN** 恢复的 task_spawned 记录不含 origin 字段（旧版本写入）
- **THEN** 恢复后 Origin MUST 为 unknown，宿主侧 unknown 与内部来源 MUST 同等扣留（未知不得升级为可投递来源）
