# config-liveness Specification（delta）

> 本域另含 `specs/config-hot-reload/spec.md`（MODIFIED，头逐字匹配主 spec「配置热更维度矩阵」）。ADDED 只留对外可观察的五条不变量；机制细节（hash 轮询间隔、防抖窗口、result 枚举取值、发布记录字段）在 design.md，不写进 spec。

## ADDED Requirements

### Requirement: 文件是编辑界面，链是账

配置源文件 MUST 只作为编辑界面：其内容变更 MUST 经唯一发布入口单向流入链上声明事实；任何生效配置 MUST 可从链上声明事实与发布记录回读，MUST NOT 把源文件或内存代当作账。Boot MUST 读文件后与链对账：文件指纹新于链上最新声明事实时 MUST 自动走唯一发布入口；一致时 MUST 静默（零发布）；链新于文件时 MUST 保留链上事实为真相并具名记录文件滞后，MUST NOT 用陈旧文件覆盖链。

#### Scenario: 文件较新自动发布

- **WHEN** 源文件指纹与链上最新声明指纹不同，且文件较新
- **THEN** Boot 经唯一发布入口发布新声明，声明事实与发布记录落链

#### Scenario: 一致静默

- **WHEN** 源文件指纹与链上最新声明指纹一致
- **THEN** Boot 零发布，绑定代不前进，不产生新的发布记录

#### Scenario: 链较新不倒退

- **WHEN** 链上最新声明事实比源文件新（如回滚后）
- **THEN** 以链上事实为准并记录文件滞后，不用陈旧文件覆盖链

### Requirement: 变更必经唯一发布入口

一切配置变更（手工 /reload、文件 watcher、重启对账）MUST 经同一发布入口（既有发布→Hold 流程）生效；治理裁决、基线复检、发布记录与 Hold 换代 MUST 原样保留且对该入口的每次尝试生效；MUST NOT 存在第二条生效路径（裸文件读、内存直写、旁路 setter）。

#### Scenario: 三条触发同一入口

- **WHEN** 手工 /reload、文件 watcher 或重启对账分别触发一次变更
- **THEN** 三次都经过同一发布入口，并留下同形态的发布记录

#### Scenario: 治理拒绝即不生效

- **WHEN** 发布入口的基线复检或治理裁决拒绝候选
- **THEN** 运行中绑定保持旧态，拒绝事实落链，无新代生效

### Requirement: 回执如实

/reload 回执 MUST 读链上事实如实作答：发布或安装失败 MUST 以失败回执返回，并落带 result 的失败事实；MUST NOT 在失败时返回成功，MUST NOT 以内存状态自证。失败 result MUST 至少区分装载失败、治理拒绝、安装失败三态。

#### Scenario: 安装失败回执失败

- **WHEN** 新代发布后模型重建或安装失败
- **THEN** 链上落带 result 的失败事实，/reload 回执返回失败与具名原因

#### Scenario: 装载失败不断线

- **WHEN** 变更后的配置无法解析
- **THEN** 运行中绑定保持不变，失败事实落链并可回读

### Requirement: 声明事实带载荷可审计

每次 Boot/Reload 装载成功后 MUST 提交一条带载荷的声明事实：类型 `config.declaration`（单源 `api/config` 既有 `Declaration.FactType()`，assembly 只读引用、不自立常量）、身份=规范指纹（族 4 既有）、Content=规范声明序列化；大正文 MUST 按既有内容寻址文件事实模式引用，MUST NOT 复制进事件。声明事实与发布记录 MUST 携带代号与前后关系；绑定代与模型版本两个切换点的窗口内决策组合 MUST 账面可查。

#### Scenario: 声明事实可回读

- **WHEN** 一次 Reload 成功发布
- **THEN** 链上恰有一条新声明事实，其身份=规范指纹，Content 可还原规范声明

#### Scenario: 同声明重载不双发

- **WHEN** 同一声明被重复装载
- **THEN** 同身份声明事实不产生第二条（幂等）

#### Scenario: 跨维窗口可查

- **WHEN** 一次发布同时切换绑定代与模型版本
- **THEN** 发布记录携带两者在该窗口内的生效结果，可按代号查询

### Requirement: 重启对账不漂移

发布记录 MUST 在重启后可回读（durable 账），MUST NOT 只存活于内存。重启对账 MUST 以文件指纹与链上最新声明指纹判定：一致即静默，MUST NOT 产生无变更的发布；文件滞后时 MUST NOT 回退链上已发布事实（不漂移）。

#### Scenario: 重启后账可回读

- **WHEN** 进程重启后查询发布记录
- **THEN** 记录完整可读，重启不改变已发布事实

#### Scenario: 对账不产生幽灵发布

- **WHEN** 文件与链一致时重启
- **THEN** 发布计数不增加，绑定代不前进
