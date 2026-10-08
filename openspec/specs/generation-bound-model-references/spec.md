# generation-bound-model-references Specification

## Purpose
TBD - created by archiving change align-runtime-overrides. Update Purpose after archive.
## Requirements
### Requirement: 覆盖解析与实际执行同面
配置驱动model_override SHALL 使用已选执行视图内的模型引用，验证与调用使用同一解析结果。引用 MUST NOT 越过原Tools调用边界或泄漏到后续无覆盖调用。

#### Scenario: 引用重注册与在途调用交错
- **WHEN** 一次覆盖调用已取得执行面，后续发布修改同名模型引用
- **THEN** 原调用使用原解析结果，新独立调用使用新面。

#### Scenario: 缺失引用与重入
- **WHEN** 重入所选执行面不含指定引用
- **THEN** 执行前明确拒绝，不回退父模型、不恢复持久指针。

### Requirement: 模型缓存标识完整
解析缓存 SHALL 区分实际protocol/model/endpoint与凭据来源配置名，MUST NOT 因provider别名和model相同而复用不匹配实例。

#### Scenario: 同名provider更换连接配置
- **WHEN** 候选中连接身份改变
- **THEN** 候选独立解析，失败不污染旧有效缓存。

### Requirement: 不支持的热更明确拒绝
FP/SRC/FILE之外无接线的配置变化 SHALL 返回restart_required与字段差异；混合候选 MUST 全部保持旧effective，不能伪报applied。

#### Scenario: 混合可热与不可热字段
- **WHEN** 同次保存修改prompt定义与构造期治理/采集资源配置
- **THEN** 整批拒绝且旧实例继续工作，成功revision不推进。

### Requirement: 动态性不扩张执行权
本能力 SHALL 复用既有Override、代际、重入和发布机制；MUST NOT 增加DAG、全局任务表、旁路调度或自动审批。

#### Scenario: 架构与真实请求验收
- **WHEN** 本域交付
- **THEN** 原架构护栏及实际模型覆盖/回滚用例通过，不以registry单测代替。

