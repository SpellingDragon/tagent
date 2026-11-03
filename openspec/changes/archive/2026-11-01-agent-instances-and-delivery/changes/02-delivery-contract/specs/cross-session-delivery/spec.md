# cross-session-delivery Specification（delta：插件派发路径补范围门）

> 本文件为 MODIFIED delta。四道安全门与"入链+驱动一轮"条款不变；范围门从 send 对外形态扩展到**插件派发路径**（G2，D10）。

## ADDED Requirements

### Requirement: 插件派发路径的范围门

插件派发路径（终态通知驱动的跨会话投递）SHALL 经 deliver_to 类声明式白名单校验（按投递方声明的目标集），fail-closed（未声明=拒绝一切），越界 SHALL 具名拒绝且目标零感知——与 send 对外形态的白名单档同一判定语义。派发失败 SHALL 留责任记录（插件派发义务不因失败消失）。

#### Scenario: 插件越界派发被拒

- **WHEN** 插件派发指向投递方未声明的目标
- **THEN** 具名拒绝、目标零感知、派发义务留痕

#### Scenario: 未声明范围 fail-closed

- **WHEN** 投递方未声明任何派发范围
- **THEN** 一切插件派发被拒（默认关）
