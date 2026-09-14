## ADDED Requirements

### Requirement: LocalFileKV 写路径 fsync 耐久

LocalFileKV 的 WAL 追加 SHALL 在每批 ops Flush 后执行文件 Sync（fsync）；snapshot 重命名 SHALL 对目标目录执行 Sync（平台不支持时 best-effort 并留痕）。fsync SHALL 可经配置关闭（默认开启）。耐久测试 SHALL 覆盖「Sync 后进程异常终止（不经 Close）→ 新实例可完整读回已确认写入」语义。

#### Scenario: 掉电窗口内的已确认写入

- **WHEN** KVPut 后 Sync 已返回，进程随即被杀（无优雅 Close）
- **THEN** 同目录新实例读回该键值，不因 OS 页缓存未落盘而丢失

#### Scenario: 显式关闭 fsync

- **WHEN** 配置 kv.fsync=false
- **THEN** 写路径行为与既有（仅 Flush）一致，且启动日志含一次性耐久降级告警
