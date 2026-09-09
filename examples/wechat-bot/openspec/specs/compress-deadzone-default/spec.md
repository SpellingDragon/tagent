# compress-deadzone-default Specification

## Purpose
消除上下文压缩的死区空转：分龄压缩目标默认对齐触发线，使"无配置即正确"，不再依赖 yaml 开关。同时移除已废弃的 UnifiedCompressTarget 配置面，保证配置面收敛。

## Requirements

### Requirement: 分龄压缩目标默认对齐触发线

框架在构建压缩选项（buildCompressorOpts）时，分龄压缩目标 SHALL 默认对齐触发线，无需任何配置即消除压缩死区空转。

#### Scenario: 无配置时的默认行为
- **WHEN** 配置中不含任何压缩目标相关字段（如已删除的 unified_compress_target）
- **THEN** 分龄压缩目标自动对齐触发线，不出现压缩死区空转

#### Scenario: 已删除字段不再被消费
- **WHEN** yaml 配置文件中出现 unified_compress_target 字段
- **THEN** 该字段被忽略（不再存在对应的 config 字段与消费分支），不影响启动与运行

### Requirement: UnifiedCompressTarget 配置面完全移除

框架 SHALL 移除 UnifiedCompressTarget 字段及其全部映射与消费分支，代码中不再残留任何该开关的痕迹。

#### Scenario: 构建验证
- **WHEN** 执行 go build 主框架与 wechat-bot 模块
- **THEN** 构建全部通过（全绿），无未定义引用、无死代码告警影响构建

### Requirement: tagent.yaml 干净恢复

wechat-bot 的 tagent.yaml SHALL 恢复到打补丁前的干净状态，无缩进错误行、无抢救性注释残留。

#### Scenario: yaml 恢复核对
- **WHEN** 对照 /tmp/tagent.yaml.bak.* 备份逐行核对
- **THEN** tagent.yaml 与备份一致，keep_recent_tasks: 4 处于生效态（未被注释）；yaml 可被正常解析

### Requirement: 进程拉起机制排查结论

系统 SHALL 产出"进程停止后为何未被自动拉起"的排查结论（crontab/systemd/watchdog 层面），但 SHALL NOT 修改运行中的 bot 进程。

#### Scenario: 只排查不重启
- **WHEN** 执行排查
- **THEN** 输出 crontab/systemd/watchdog 覆盖情况的结论清单；运行中的 bot（PID 702169）不发生重启，会话不中断
