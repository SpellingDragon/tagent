# error-handling-consistency Specification

> **同步注记（2026-11-21 契约同步核验，10.R5e）**：原三条要求里，第二条的检查面（键值仓的 KVPut／KVGet／KVDelete／KVScan、关系仓、墓碑集的 MarkTombstone）在全仓 Go 树 0 命中，本次正名为现行的存储提交与提交屏障、原文读写、关系父子链读写；第三条的迁移对象（事件的 ParentKey 字段与「Phase 1 event view transformation」）在全树 0 命中、其点名的两个源文件已不在树，迁移已完成，故该条整体退役并并入本条注记。第一条（日志包统一）仍在位、原文不动。对已退役符号的引用自此只在本注记与 design 的义项表内出现，不作现行命令。

## Purpose

本规范定义 error-handling-consistency 能力。所有 Go 源文件 SHALL 使用 trpc-agent-go 的 log 包记录日志，MUST NOT 使用标准库 log 包；存储提交与提交屏障、原文读写、关系父子链读写的错误返回值 SHALL 被检查并按上下文报告，MUST NOT 以静默吞错冒充成功。

## Requirements

### Requirement: Log package is unified

All Go source files in tagent SHALL use `trpc-agent-go/log` for logging. The standard library `log` package SHALL NOT be used.

#### Scenario: 组合根后台循环记录一条消息

- **WHEN** 组合根的后台消费循环或请求路径记录一条消息
- **THEN** 它经 `trpc.group/trpc-go/trpc-agent-go/log` 的入口输出，而不是标准库 `log.Printf`

### Requirement: Error return values are checked

存储提交与提交屏障、原文读写、关系父子链读写的错误返回值 SHALL 被检查：静默吞错（`_ = err`）SHALL 以至少一条带上下文的日志，或一个包装后的具名错误替代，MUST NOT 让失败伪装成「没有父链」「没有原文」或「提交成功」。

#### Scenario: 关系父链读失败不伪装成无父

- **WHEN** 召回工具读父链接返回错误
- **THEN** 该失败 SHALL 以具名原因或包装错误呈现给调用方，MUST NOT 呈现为「该事件没有父」

#### Scenario: 提交屏障未完成不报成功

- **WHEN** 存储提交的屏障没有完成
- **THEN** 调用方 SHALL 收到具名错误，MUST NOT 得到耐久成功的答复

#### Scenario: 原文读的三类结果不折叠

- **WHEN** 按身份读原文分别遇到窗口外过期、该身份没有原文、底层读失败
- **THEN** 三种结果 SHALL 各自具名可判定，MUST NOT 折叠成同一个答复
