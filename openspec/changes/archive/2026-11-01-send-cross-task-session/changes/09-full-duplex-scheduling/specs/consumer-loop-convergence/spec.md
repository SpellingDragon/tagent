# consumer-loop-convergence Specification（delta：补慢工具与合批场景）

> 本文件为 MODIFIED delta。"消费循环不等派发工具完成"条款**方向不变**；补慢工具/多来源合批的可测场景与重入输入合流条款（R7，design D13）。

## Purpose

消费循环的全双工收敛面完备化：慢工具不阻塞新输入、多来源合批不混淆责任、重入输入恰一次进决策。

## MODIFIED Requirements

### Requirement: 慢工具不阻塞新输入

慢工具在跑时，新的重要输入 SHALL 在规定边界进入下一次有效决策；工具结果 SHALL 回到正确责任范围（所属 Task），MUST NOT 因等待结果而阻塞消费循环。

#### Scenario: 慢工具在跑新输入仍进决策

- **WHEN** 一个慢工具未结束时新输入到达
- **THEN** 新输入按时进入下一次决策，慢工具结果随后回到其所属 Task

### Requirement: 多来源合批不混淆责任

同会话多来源输入 SHALL 按合批语义进一次决策，结果 SHALL 回到各自正确责任范围；合批 MUST NOT 混淆任务归属。

#### Scenario: 合批结果各归其位

- **WHEN** 同会话两个来源的输入合批处理
- **THEN** 各结果回到各自责任范围

### Requirement: 重入输入恰一次

经 `send{target:{task}}` 重入的输入 SHALL 作为消费循环普通输入进入下一次决策恰一次，MUST NOT 重复消费。

#### Scenario: 重入输入不重不漏

- **WHEN** 重入输入与普通输入同批到达
- **THEN** 重入输入恰被消费一次
