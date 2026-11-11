# recovery-boundary-guards Specification

## ADDED Requirements

### Requirement: recovery-boundary-guards 契约

F0 SHALL 含边界规则：链外持久物未经 R3 登记或未按内容寻址校验即红；规则 SHALL 带正反样例自测；测试 SHALL 覆盖正锚/负锚/消费者锚/症状锚四类且不追认缩水。

#### Scenario: 主路径

- **WHEN** 新增一个链外持久文件但未登记票据
- **THEN** F0 规则红并具名指出未登记面
