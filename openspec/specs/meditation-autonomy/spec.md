# meditation-autonomy Specification

## Purpose
定义冥想自主面：状态机持有激活规则与状态锚点，激活即真实事件并携带因果，状态积累只由外部事件推进，自管产出不计入状态积累；对外行为遵守用户让路、锚点重启安全与影响他人只经 send 等不变量。

## Requirements
### Requirement: 冥想自主整理面

配置冥想插件的 agent（缺省宿主自身，反思会话如 `tagent-session:tagent:reflection`）MUST 具备 file 读、exec（受既有安全门与世代校验）与 recall 三种整理能力；整理产物 MUST 经既有工具与事实路径落盘并可审计；节奏语义（事件驱动＋空闲门）MUST NOT 因能力扩展改变。

#### Scenario: 空闲窗自主整理

- **WHEN** 空闲门放行冥想轮
- **THEN** 该 agent 可用 file＋exec＋recall 整理材料，产物经既有路径落盘且可回读

#### Scenario: 越权被拒

- **WHEN** 冥想轮的工具调用越过既有白名单或安全门
- **THEN** 调用被既有门拒绝并记日志，不新增冥想专属放行

### Requirement: 激活即真实事件并携带因果

冥想激活 MUST 是既有 `external_input` 事件（真实事件，不是虚投影），其元数据 MUST 携带完整因果：激活规则、积累窗与来源；状态持久化 MUST 经 `AnchorStore` 扩展并可审计；MUST NOT 为冥想新立事实族。

#### Scenario: 激活事件可审计

- **WHEN** 状态达到激活并触发冥想轮
- **THEN** 链上出现一条既有类型的激活输入，读者可据其元数据还原"哪条规则、哪个积累窗、哪个来源"

#### Scenario: 不新立事实族

- **WHEN** 审阅冥想实现新增的事实类型
- **THEN** 不存在冥想专属事实族；状态只经既有 `AnchorStore` 持久面保存

### Requirement: 状态积累只由外部事件推进

状态积累 MUST 只由外部事件推进；激活 MUST 复位状态；自管谱系的产出 MUST NOT 计入状态积累（防自持）。定时 MUST 只是激活检查方式之一，MUST NOT 成为唯一激活源。

#### Scenario: 自管产出不计入

- **WHEN** 一轮冥想由自管谱系触发并产出新事实
- **THEN** 该产出不给状态积累增值，激活阈值只由外部事件推进

#### Scenario: 激活复位

- **WHEN** 状态达到激活
- **THEN** 状态复位，下一次激活仍需新的外部事件积累

### Requirement: 冥想对外不变量

冥想 MUST 保持对外四条边界：用户让路（宿主回合在飞时 MUST NOT 激活）；锚点重启安全（重启后 MUST NOT 重复消费或丢失激活）；影响他人只经 send（MUST NOT 直写他会话）；失败走既有失败观察（MUST NOT 新增冥想专属失败通道）。

#### Scenario: 用户让路

- **WHEN** 宿主用户回合正在执行
- **THEN** 冥想不激活、不插队，回合结束后才按状态判定

#### Scenario: 重启安全

- **WHEN** 进程在状态积累中途重启
- **THEN** 激活不重复、不丢失，锚点可审计

#### Scenario: 影响他人只经 send

- **WHEN** 冥想需要让另一个会话看到结论
- **THEN** 只经既有 send 工具与四道门，不直写目标会话链

