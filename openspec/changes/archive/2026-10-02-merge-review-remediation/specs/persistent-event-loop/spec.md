## ADDED Requirements

### Requirement: 模型入口错误以失败极性呈现

回合执行中模型入口的任何失败——执行凭据校验拒绝、迭代器创建失败、流通道创建失败或返回 nil 流——MUST 以携带错误信息的失败 Response 在事件流中呈现，并使回合归约为 failed turn、形成 failed completion；MUST NOT 静默零产出使回合被归约为 completed、以 completed 口径冻结 completion 并 ack 持久输入。上游框架契约（迭代器创建失败可经 error 返回、流内错误编码进 Response.Error）SHALL 在所有模型包装层被同构兑现。

#### Scenario: 迭代器创建失败归约 failed turn

- **WHEN** 执行门包装的模型在迭代器创建时返回 error
- **THEN** 事件流收到带 Response.Error 的失败响应，回合归约 turnFailed 并形成 failed completion，持久输入不被以 completed 口径 ack

#### Scenario: nil 流不挂死不伪成功

- **WHEN** inner 模型返回 (nil, nil)
- **THEN** 同样以失败响应呈现并归约 failed turn，不永久阻塞、不归约 completed

#### Scenario: 凭据校验拒绝可见

- **WHEN** 执行凭据 verify 失败拒绝模型调用
- **THEN** 失败以带错误信息的失败响应进入事件流（含日志），回合失败可观测
