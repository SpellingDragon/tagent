## ADDED Requirements

### Requirement: 配置严格解析

tagent.yaml 解析 SHALL 采用 strict 模式（KnownFields）：出现未知字段 SHALL 使加载失败，错误信息列出全部未知字段名；字段拼写错误 MUST NOT 被静默忽略。仓库内全部随载 yaml（示例/测试/资源）SHALL 通过 strict 解析。

#### Scenario: 配置键拼写错误

- **WHEN** yaml 含拼错的键（如 `wroking_dir`）
- **THEN** 启动报配置错误并列出该未知字段，进程不进入「配置被忽略、行为悄然偏离预期」状态

### Requirement: agent 引用环构建期检测

buildAgent 递归装配 SHALL 携带 visited 集：agent 引用构成环（A↔B 或自引用）时 SHALL 返回明确配置错误（指明环上的 agent 名），MUST NOT 以栈溢出崩溃。

#### Scenario: 配置声明了引用环

- **WHEN** yaml 中 agent a 的 tools 引用 agent b、b 又引用 a
- **THEN** 构建返回形如 `agent "a" <-> "b" reference cycle detected` 的配置错误
