# restart-env-snapshot-fallback Specification

## Purpose
保障 tagent 重启保险（v2 cron 脚本）转世时始终能以完整环境变量重启 bot，消除"env 快照用后即焚导致下次裸环境启动"的隐患。

## Requirements

### Requirement: env 快照持久兜底

restart-maintenance.sh 在重启成功后 SHALL 将 env 快照归档到 $BASE/run/env.snapshot 作为下次转世的兜底来源；构建/转世环节读取快照的顺序 SHALL 为 /tmp 快照优先、$BASE/run/env.snapshot 兜底。

#### Scenario: 重启成功后快照留存

- **WHEN** healthz 门控通过（RESTART OK）
- **THEN** /tmp 快照内容已被归档到 $BASE/run/env.snapshot，/tmp 副本可清理，下次转世仍有完整 env 可用

#### Scenario: /tmp 快照丢失时兜底

- **WHEN** 机器重启或 /tmp 清理导致 /tmp/tagent_env.snapshot 不存在，但 $BASE/run/env.snapshot 存在
- **THEN** 构建与转世使用兜底快照恢复 GOROOT/GOPATH 与完整 env，bot 不以裸环境启动

#### Scenario: 两处快照都无

- **WHEN** /tmp 与 $BASE/run 均无快照
- **THEN** 脚本按现有默认逻辑降级（GOROOT/GOPATH 取默认值，env 用 HOME/PATH 兜底），行为与现状一致，不额外失败
