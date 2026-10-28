# Orchestration 契约

## ADDED Requirements

### Requirement: 独立写权状态可反事实删除

凡不改允许操作的状态 SHALL 通过「删掉后从已有记录无歧义算回来」检验；通过者 SHALL 删除写权改派生读数。

#### Scenario: progress 事实化

- **WHEN** 进度不改允许操作
- **THEN** 进度 SHALL 以事实记录，状态位写权 SHALL 删除

### Requirement: 执行核心零训练形态

核心 SHALL 只保留执行证据（调用时固定的实际请求、输入引用、工具调用、已归约结果、因果）；训练组织 SHALL 移训练模块。

#### Scenario: 关闭训练后核心协议完整

- **WHEN** 训练/自演进关闭
- **THEN** 核心执行协议 SHALL 完整成立，实际输入 SHALL 仍调用时固定

### Requirement: 六端口闭集无组合词汇

Material×Product 组合词汇若不做真正组合推导 SHALL 删除；六具名端口窄接口 SHALL 保留且不合并。

#### Scenario: 扩展者少理解一层

- **WHEN** 实现插件
- **THEN** SHALL 只需声明一个具名端口并实现其窄接口

### Requirement: 热更新三档明示

模型/提示词/工具声明 SHALL 走新代发布；存储根/持久化格式/资源所有权变更 SHALL 要求重启；固定配置 SHALL 一次发布。

#### Scenario: 越档变更具名拒绝

- **WHEN** /reload 请求越档变更
- **THEN** SHALL 具名拒绝并指明重启要求

### Requirement: 新工具接入零补偿

接入新工具 SHALL 只需适配器提供执行、取消及恢复证据；宿主 SHALL NOT 另写通知补偿队列；核心 SHALL NOT 新增生命周期状态。

#### Scenario: 责任模型降低复杂度

- **WHEN** 一个新工具按此契约接入
- **THEN** 受理→结算→投递全链 SHALL 跑通且宿主零补偿代码
