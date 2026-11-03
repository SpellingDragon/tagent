# unified-task-semantics Specification（delta：接纳/确认三层延伸到启动通知）

> 本文件为 MODIFIED delta。原条款的三层分离（受理回执≠存储提交≠投递接纳）延伸到启动通知链：通知的"入箱"是接纳，"消费确认"才完成标记（R1）。

## Purpose

启动通知与任务同享"接纳≠交付≠确认"语义：标记完成的唯一依据是消费确认，重投同身份幂等。

## MODIFIED Requirements

### Requirement: 通知接纳与消费确认分离

启动通知 SHALL 先入稳定身份的持久接纳记录；`.notified`/`.consumed` 标记 SHALL 仅在消费确认（输入提交成功）后完成。入箱、取走 MUST NOT 被当作完成。重投 SHALL 复用同一接纳身份，MUST NOT 产生重复有效处理。

#### Scenario: 确认后才标记

- **WHEN** 通知入箱但输入提交失败
- **THEN** 标记不完成，通知在册可重投；恢复后恰被消费一次

#### Scenario: 重复恢复幂等

- **WHEN** 进程在标记完成后再次重启
- **THEN** 同身份通知不产生第二次有效处理
