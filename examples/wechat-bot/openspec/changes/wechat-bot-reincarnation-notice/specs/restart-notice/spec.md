## Purpose

让 wechat-bot 在保险链自替换换装后，新进程能感知"转世"事实并把结构化通报注入自身事件流，消除换装导致的记忆断层。

## ADDED Requirements

### Requirement: 换装档案落盘

保险链脚本在成功完成换装（healthz 探活通过）时，SHALL 将换装详情写入 `run/REINCARNATION_NOTICE` 文件，内容 MUST 包含：换装时间、旧 PID、新 PID、触发原因、构建指纹（二进制 sha256 前 12 位）、指向 restart.log 的指针。写入失败 MUST NOT 影响换装主流程（restart.done 照常写、脚本照常 exit 0）。

#### Scenario: SUCCESS 分支写档案

- **WHEN** 保险链完成 build → 换装 → healthz 200
- **THEN** `run/REINCARNATION_NOTICE` 存在且含 reincarnated_at / old_pid / new_pid / reason / build_sha / log_pointer 字段，restart.log 记录写入动作

#### Scenario: 档案写失败不阻断

- **WHEN** REINCARNATION_NOTICE 因磁盘或权限原因写入失败
- **THEN** restart.done 仍写入、脚本仍以 SUCCESS 结束，失败仅记入 restart.log

### Requirement: 新进程启动期转世检测

wechat-bot 进程启动时 SHALL 检测 `run/restart.done`：若文件修改时间距当前 <10 分钟且文件内 PID ≠ 自身 PID，MUST 判定为"刚被换装"。检测 MUST 在事件循环启动后执行，检测与注入结果 MUST 落日志。文件不存在、无法解析或超过新鲜度阈值时 MUST 静默跳过（不注入、不报错退出）。

#### Scenario: 命中换装

- **WHEN** 进程启动时 restart.done mtime 距今 <10min 且 PID ≠ 自身
- **THEN** 判定为换装，读取 REINCARNATION_NOTICE 并进入通报注入流程

#### Scenario: 冷启动不误报

- **WHEN** restart.done 不存在，或 mtime 距今 ≥10min，或 PID 等于自身 PID
- **THEN** 跳过通报，进程照常启动，日志记录检测未命中

### Requirement: 通报注入内部事件流

判定换装成立后，进程 SHALL 将转世通报（含时间、旧/新 PID、触发原因、档案指针）注入自身事件流，MUST 复用框架现有内部事件注入通道（非 user 触发源，不干扰 meditation 新奇门与用户输入谱系）。REINCARNATION_NOTICE 缺失时 MUST 生成降级通报（时间+PID 来自 restart.done，注明档案缺失）。通报注入成功后 MUST 将 restart.done 改名为消费标记以防同进程重复通报。

#### Scenario: 通报真实抵达事件流

- **WHEN** 换装判定成立且 NOTICE 可读
- **THEN** 事件流出现该通报（日志可见 meditation 触发源收到转世通报内容），restart.done 被改名为消费标记

#### Scenario: 档案缺失降级

- **WHEN** 换装判定成立但 REINCARNATION_NOTICE 缺失或不可读
- **THEN** 注入降级通报（含可得的 PID 与时间信息、注明 NOTICE 缺失），进程不退出

### Requirement: 部署门禁与自替换验证

代码变更 MUST 通过 build/vet/test 门禁并提交推送 dev 分支；部署 MUST 经保险链自身换装完成（dogfood），并实测验证通报抵达事件流。

#### Scenario: 门禁后提交

- **WHEN** main.go 与 restart-maintenance.sh 变更完成
- **THEN** `go build ./...`、`go vet ./...`、`go test ./...` 全绿后 commit 并 push dev

#### Scenario: dogfood 自替换验证

- **WHEN** 变更合入后经保险链触发真实换装
- **THEN** 新进程上线后日志出现转世通报注入与消费证据，run/REINCARNATION_NOTICE 与消费标记文件均存在
