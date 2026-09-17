## ADDED Requirements

### Requirement: 验证工具必须对底层失败保真

测试包装器 SHALL 保留底层命令状态与失败输出；编译失败、非法包、超时、普通断言失败 MUST 非零。上游 race 豁免 SHALL 只适用于登记的具体签名、版本和测试，不因栈位于依赖包就一律放行。没有 race 文本 SHALL NOT 被视为测试成功。

#### Scenario: 非法包
- **WHEN** race wrapper 收到不存在的 Go 包
- **THEN** 返回非零并显示失败，不输出成功 verdict

#### Scenario: race 豁免混合普通失败
- **WHEN** 输出含已知上游 race 和普通断言失败
- **THEN** 总结果失败，不被 race 豁免覆盖

### Requirement: 证据分层与可复现性

每个工作包 SHALL 记录基线 commit、配置、命令、结果、跳过项、失败原因、影响文件到测试的映射；真实 LLM、独立进程、掉电、长期运行证据 MUST 分开，不以 mock/优雅 Close/同进程 New 代替。历史报告的已修/部分/仍在/未验证 SHALL 明确标注，归档勾选不得单独证明修复。

#### Scenario: 只有 mock 与优雅关闭
- **WHEN** 仅完成 mock 或同进程 write→Close→New 测试
- **THEN** 报告限定证据范围，不宣称真实模型质量、掉电恢复或多天稳定性已验证

### Requirement: 双模块与真实消费者验收

CI SHALL 分别覆盖根模块与 wechat-bot 独立模块 build/vet/short，并覆盖 memory 子包、agent、plugin、rl、evolution、event、tool 的定向 race。恢复/任务/热更的集成验收 SHALL 到达实际模型请求、diagnostics、任务结算及宿主投递决定，不能以写盘、tracked=true 或 applied 日志作为终点。

#### Scenario: 恢复后实际请求
- **WHEN** 新子进程从真实 localfile 恢复并发出首次模型请求
- **THEN** 测试捕获实际消息序列、source IDs、票据、恢复状态，与死亡前基线对账

### Requirement: 常驻准出证据

发布候选 SHALL 具备确定性 30 轮独立进程重启 E2E 与 72h 隔离长跑记录。长跑 SHALL 包含并发输入、存储拒写、慢消费者、mock 模型/MCP 故障、任务重挂及热更；durable 接收项不得无解释丢失，确认项不得重复投影。资源指标按 design D7 的稳态阈值验收，失败先定因而非调宽门槛。

#### Scenario: 尚无 72h 证据
- **WHEN** 短程回归通过但未完成长跑
- **THEN** 可标实现阶段完成，不能标长期运行发布门通过；遗留明确待验

### Requirement: 发布与外部操作权限

提案/apply SHALL NOT 隐含授权实际部署、付费模型调用、提交/推送/tag/发布或对外提单。外部验证缺授权 SHALL 标为 BLOCKED/待验并说明，不伪造完成；独立代码审查失败/不可用也 SHALL 明示，不能以主执行者自查冒充独立准出。

#### Scenario: 缺少部署授权
- **WHEN** 本地实现与测试已完成但没有外部环境授权
- **THEN** 交付本地结果和待验项，不操作真实部署或打发布 tag
