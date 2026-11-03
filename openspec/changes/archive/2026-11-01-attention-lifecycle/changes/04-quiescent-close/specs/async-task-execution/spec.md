# async-task-execution Specification（delta：未消费通知的存活与结清 + 静止收束）

> 本文件为 MODIFIED delta。原"通知义务存活至逻辑消费确认"严语义**保留**；判据载体从内存义务账改为权威未消费账（`Unsettled()` 同源函数）；拒收落 feedback 事实族。词汇："义务/Obligation"→"未消费通知"。

## MODIFIED Requirements

### Requirement: 通知义务存活至逻辑消费确认

后台任务的通知义务 SHALL 存活到该通知被**逻辑消费确认**，MUST NOT 因下列任一状态提前结清：**运输层接收（EventBus、邮箱、claim）**、发送动作成功、任务终态（settled/completed/failed）、宿主 `Close` 返回 nil。义务是否结清 SHALL 以**消费方记录的已提交事实键与消费位置**为准。

判据载体 SHALL 为权威未消费账（mailbox 未消费 ∪ 持久欠通知），经同源函数 `Unsettled()`（未终态工作 ∪ 未消费通知）求值；MUST NOT 以进程内影子账本（不可从链上算回的记忆字段）作为结清判据。

#### Scenario: 运输接收不等于消费

- **WHEN** 通知已进入 EventBus 或邮箱并被 claim，但消费方尚未记录逻辑消费确认
- **THEN** 该义务 SHALL 保持未结清（`Unsettled()` 非空）

### Requirement: 拒收落事实

通知被拒收 SHALL 落 feedback 事实（携带原通知引用与拒收原因），跨重启可见；拒收是**明确的消费结果**，不构成欠账。拒收事实 SHALL 可驱动后续清理/重试决策。

#### Scenario: 拒收跨重启可见

- **WHEN** 一条通知被拒收后进程重启
- **THEN** 拒收事实仍在链上可读，且不被当作未消费通知

## ADDED Requirements

### Requirement: 静止收束三态

关闭 SHALL 表达为静止收束（quiesce）：停止注入（intake 关）→ 静止到不动点（工作收敛 ∧ 消费收敛 ∧ 写入者停止）→ 三态返回（`Quiesced` / `Cut` / `Broken`）。`Cut`（预算到点）SHALL 为正常返回：余量在链上，重启续上。`CloseRequest` 禁零 deadline。

#### Scenario: 预算到点 Cut

- **WHEN** 静止到点仍有未消费/未终态项
- **THEN** 返回 `Cut` 并逐地址报告清单（非错误），重启后续拉

#### Scenario: 拒收后仍可关闭

- **WHEN** 一条通知被拒收且未再消费
- **THEN** 关闭仍可完成（Cut 合法），拒收事实跨重启可见
