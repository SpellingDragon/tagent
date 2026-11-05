# exec-integrity Specification

## Purpose
定义 exec 回执完整性：回执声明成功前必须证明本次命令真实执行，世代令牌绑定输出与退出码；复用路径必探活，陈旧 scrollback 与世代不匹配的退出码不得被采信，标记不得污染本次输出。

## Requirements
### Requirement: 回执与执行事实一致

`exec_run` 回执声明成功前 MUST 证明本次命令真实执行（世代令牌绑定输出与退出码）；陈旧 scrollback 与世代不匹配的退出码 MUST NOT 被采信为本次结果。**世代令牌校验失败 MUST 归为明确失败（陈旧世代不猜测、不按未知结果搁置）：处理＝清理旧会话（Release）后以全新会话重建执行；MUST NOT 把陈旧世代的输出或退出码当作本次成功。**

#### Scenario: 死 pane 复用旧缓冲

- **WHEN** 目标会话的 pane 已死且缓冲含更早命令输出
- **THEN** 回执不报 exit=0 假成功：默认重建会话并执行成功；显式失败仅当重建不可行

#### Scenario: 世代校验失败即明确失败

- **WHEN** 回执中的世代令牌校验失败（陈旧世代）
- **THEN** 该次执行判为明确失败，清理旧会话后以全新会话重建执行；MUST NOT 采信陈旧输出或把结果留作未知

### Requirement: 复用路径必探活（r2 F15 根因更正）

资源被持有的复用短路（RunTool.Call 的 held 短路与 Manager.Acquire 的命中路径）MUST 在复用前探查 pane 状态；pane_dead 时 MUST 清理旧会话后以新命令重建并执行；显式失败仅当重建不可行；旧缓冲 MUST NOT 进入本次回执；回执声明成功 MUST NOT 与可观察的外部事实矛盾（探针文件落盘读回是验收用例形态，非产品契约——任意命令不被要求写探针文件；产品契约=世代令牌绑定）。

#### Scenario: held 资源上的死 pane

- **WHEN** 同一资源键再次执行命令而其 pane 已死
- **THEN** 复用前探活发现 pane_dead，清理重建后执行，touch 探针真实落盘，回执不含旧缓冲

### Requirement: 标记不污染输出

世代标记 MUST 从用户可见输出中剥离；剥离 MUST 有负锚锁定。

#### Scenario: 正常执行

- **WHEN** 命令成功执行并捕获输出
- **THEN** 回执输出不含世代标记文本

