## MODIFIED Requirements

### Requirement: 分层依赖方向可机械断言
架构检查 SHALL 以人工登记的 golden/runtime/architecture.json 为准，枚举第一方产品包及全部子包，逐一给出归属层并登记进允许依赖集（人工显式清单）。组合根 MUST 位于module根且显式登记允许边；未登记子包、未知内部边、循环依赖和缺必需包均须具名拒收（默认拒绝：新包先登记归属再合入）。政策表不得从现存残件自动学习；覆盖证据 MUST NOT 用 `tagent: *`、`tagent/...`、通配或前缀折叠掩盖未覆盖子包，每个子包 SHALL 单独可判定。纯声明不得依赖运行时，事实和任务提交不得反向依赖执行器。

#### Scenario: 未登记子包或反向依赖
- **WHEN** 新增未登记子包或memory反向import agent
- **THEN** check_runtime_architecture --implementation非零退出并标明包与边，政策模式通过不能覆盖失败

#### Scenario: 通配不得掩盖未覆盖子包
- **WHEN** 覆盖清单以 `tagent: *` 或前缀折叠声称全绿，而某子包实际未被逐一判定
- **THEN** strict 结构门判失败并具名该未登记子包，MUST NOT 以通配静默放行

#### Scenario: 包枚举命令失败
- **WHEN** 递归go list失败或依赖缺失
- **THEN** 检查保留原始退出码并拒绝结构通过，不解析残缺输出伪造成功

### Requirement: 唯一编排发布权与中性契约
组合根 SHALL 构造候选模型、原生工具及插件；state SHALL 持有和发布生效绑定，请求只借用同代绑定。执行器、Query和训练观察 MUST NOT 复制可写运行现场或反向依赖根包。共享端口由实际消费方审定，持久记录不得含指针、闭包或连接。包与边按真实职责设计，不强制复活旧agent子包机制。

#### Scenario: 候选失败或在途换代
- **WHEN** 候选构造失败或发布时存在旧请求
- **THEN** 候选独占资源释放，state生效代不受失败候选影响，在途请求保留旧代直到实际使用结束；由01.4/01.5和金样S01取证

#### Scenario: 共享接口未被消费者验证
- **WHEN** G1只有类型草案而没有生产构造链和最小原生签名探针
- **THEN** 保持design-blocked，不为避免import建立同形影子协议

## ADDED Requirements

### Requirement: 检查资产区分三类证据
规划检查、实际结构覆盖与运行行为证据 SHALL 分开输出。完成状态 MUST 具有命令、原始成功退出码、run-id和产物引用；这些字段存在仍不代表来源已认证。运行检查 MUST 核验真实测试run/终局、包终局及金样具体值，不能仅比较键名。

#### Scenario: 缺失或矛盾的运行记录
- **WHEN** 证据缺case、run、pass、包终局，或出现Skip/Fail、重复键、未知观测、错值
- **THEN** check_runtime_evidence非零退出；后补pass不能覆盖先前失败

#### Scenario: 自测负控
- **WHEN** 临时fixture分别注入未知子包、错值、缺终局和错误退出码
- **THEN** 检查器各自拒绝，unittest证明的是拒收逻辑，不勾选产品验收

### Requirement: 质量门迁移不得放宽判据
CI SHALL 覆盖next与既有集成分支，产品门覆盖根与微信两module、全部适用第一方子包race，Go版本不得低于1.24.1。旧规则引发碎片化时 MUST 先记录真实反例与正反测试，再修改规则，不提高baseline掩盖错误。

#### Scenario: 本轮只能生成检查资产
- **WHEN** 规划和新检查workflow已可运行而旧产品树仍不完整
- **THEN** 只报告检查资产完成，00.6完整迁移及09.3全量产品门保持未完成，不删除旧门
