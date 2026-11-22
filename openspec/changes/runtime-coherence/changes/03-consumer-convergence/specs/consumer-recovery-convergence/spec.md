## ADDED Requirements

### Requirement: 协调者不解释具体消费者业务

checkpoint 协调者与装配根 SHALL 只处理切片名、覆盖、恢复顺序与存储写入；MUST NOT 解析任务、审批、投影或压缩的业务载荷来决定行为。消费者间的前置依赖 SHALL 由拥有者关系或类型化声明表达，新增消费者 MUST NOT 要求修改协调者的业务分支。

#### Scenario: 新增一个消费者

- **WHEN** 一个新消费者注册切片并声明其所需历史
- **THEN** 切面与恢复 SHALL 由既有协调者完成
- **AND** 协调者代码 SHALL NOT 出现该消费者的专门分支

#### Scenario: 删除一个消费者

- **WHEN** 一个消费者被移除且无其它消费者依赖其切片
- **THEN** 协调者 SHALL 无需修改即可继续服务其余消费者

### Requirement: 投影与压缩的覆盖分离

投影切片 SHALL 只陈述投影呈现的覆盖；压缩判决与应答 SHALL 归压缩消费者自己的账户。协调者 MUST NOT 用压缩事实修正投影覆盖，也 MUST NOT 让投影进度代表压缩处理进度。两者的恢复 SHALL 各自有正式入口证据。

#### Scenario: 压缩已答而投影未覆盖

- **WHEN** 压缩账户已记录某判决被应答，而投影覆盖尚未到达该事实
- **THEN** 恢复 SHALL 分别按各自覆盖处理，MUST NOT 互相替代

### Requirement: 压缩正文副本先判唯一来源

checkpoint 或锚载荷中的正文 SHALL 先判定其是否为唯一可读来源。若可由权威事实按身份定位重建，副本 SHALL 删除并以统一读回替代；若确为唯一来源，SHALL 保留并在声明处写明理由。MUST NOT 仅以“减少一次读取”删除或保留。

#### Scenario: 副本可定位

- **WHEN** 正文能从链上产物按身份重建
- **THEN** 副本 SHALL 删除，恢复 SHALL 经统一读回得到同一文本

### Requirement: 材料声明与实际分派一致

插件声明的材料（读什么、时机、失败语义）SHALL 驱动实际分派；只声明端口的面 SHALL 走兼容读法并保持既有失败行为。六端口 SHALL 保留；`Governor` 等已声明裁决面的实例 SHALL 实际参与裁决，MUST NOT 只被收集而不生效。

#### Scenario: 声明与端口矛盾

- **WHEN** 一个面的材料时机与其端口允许的时机不符
- **THEN** 装配 SHALL 具名拒绝

#### Scenario: 兼容读法

- **WHEN** 一个面只声明端口
- **THEN** 它 SHALL 取得该端口的时机与失败策略
- **AND** 既有行为 SHALL NOT 改变

### Requirement: 删除被替代结构需替代证据

删除任一恢复特判、双解释或专用分支前 SHALL 指明替代者、覆盖原行为的 spec 场景与测试，并给出反事实（不删谁坏）。删除后原场景 SHALL 仍被新拥有者覆盖；缺替代证据的结构 MUST NOT 删除。

#### Scenario: 删除后行为丢失

- **WHEN** 删除后没有任何测试覆盖原 spec 场景
- **THEN** 该删除 SHALL 回退，结构保留并登记其独立需求
