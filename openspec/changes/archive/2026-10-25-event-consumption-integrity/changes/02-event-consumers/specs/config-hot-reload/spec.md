## ADDED Requirements

### Requirement: 模型替换的幂等与在途稳定

模型替换 MUST 以租约版本为动作身份：请求替换的版本与当前生效版本相同时，替换 MUST NOT 再次重建发布链、MUST NOT 推进 generation、MUST NOT 重新安装执行器；结果 SHALL 可判定为 no-op 或具名结果（`ErrLeaseVersionHeld` 只是现行实现的一种语义，不得反推公共 `Reload` 必须报错）。替换候选的重建或发布失败时，生效绑定 MUST 保持旧版本，回执 SHALL 携带具名原因与失败阶段；MUST NOT 部分安装新模型，MUST NOT 把失败记为已生效。替换成功后，已开始的模型流 SHALL 保持其开始时的版本绑定直至终态，MUST NOT 被迁移或半途换绑；替换完成后新发起的请求 SHALL 取新生效版本。版本判定 MUST NOT 以「最近一次替换时间」或模型名内容比较代替租约版本。

#### Scenario: 同版本重复替换不重复发布/安装（失败）

- **WHEN** 对同一租约版本重复发起替换
- **THEN** 第二次替换 MUST NOT 产生第二次重建、第二次安装或第二条发布记录，generation 与生效绑定保持不变
- **AND** 结果 SHALL 可判定（no-op 或具名结果），且新发起请求的版本绑定 MUST NOT 因重复替换而改变

#### Scenario: 替换失败不安装（失败）

- **WHEN** 新版本候选在重建或发布阶段失败
- **THEN** 生效绑定 MUST 保持旧版本，回执 SHALL 给出具名失败原因
- **AND** 后续请求 MUST 继续由旧版本服务，不得出现半安装态

#### Scenario: 在途流保持开始版本

- **WHEN** 一条模型流在替换前开始、替换成功后才结束
- **THEN** 该流 MUST 全程使用其开始时的版本绑定
- **AND** 替换后新发起的请求 SHALL 取新生效版本

#### Scenario: 替换后新请求取新版本

- **WHEN** 替换成功且 generation 推进后发起新请求
- **THEN** 实际请求 MUST 绑定新生效版本，并以请求侧可见证据断言
- **AND** MUST NOT 以替换回执自证代替请求侧证据
