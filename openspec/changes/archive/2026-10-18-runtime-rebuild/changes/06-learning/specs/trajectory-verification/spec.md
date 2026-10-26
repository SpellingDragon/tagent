# trajectory-verification（06-learning 子变更 delta）

> 本文件为二级子变更 `06-learning` 对既有主 spec `openspec/specs/trajectory-verification/spec.md` 的 delta（主 spec **只读**）。
> 既有 requirement 为「重启点身份配对/截尾/动态系统段/失败可见」；本波**新增**真实消费验收的可见失败面（原型面全部继承，未列出者不默认改写）。

## Purpose

定义轨迹与**训练消费**验收的可见失败：以可判定分类与非零退出码暴露 labels/mask 不一致、目标样本为空、拒绝账遗漏等，**缺凭据/模板不支持/SKIP 不计为完成**。

## ADDED Requirements

### Requirement: 真实消费验收失败可见
真实 SFT 消费验收 MUST 在 **labels/mask 不一致、目标样本为空、应拒样本未入拒绝账** 时输出明确分类与非零退出码；缺凭据、模板不支持或 `SKIP` MUST NOT 计为完成，MUST NOT 声称权重训练收益。

#### Scenario: 目标样本为空（失败）
- **WHEN** 导出数据中目标 assistant 既无正文又无 `tool_calls`
- **THEN** 验收 MUST 失败并分类为 `EMPTY_TARGET`，退出码非零。

#### Scenario: mask 不一致（失败）
- **WHEN** labels 与动作/观察 mask 不匹配
- **THEN** MUST 判 `MASK_MISMATCH` 并计入失败汇总，退出码非零。

#### Scenario: 拒绝账遗漏（失败）
- **WHEN** 存在模板不支持或多模态未支持样本，但拒绝账为空
- **THEN** MUST 失败并报告遗漏的拒绝分类，MUST NOT 静默降级为纯文本。

#### Scenario: SKIP 不算通过（失败）
- **WHEN** 消费用例因缺凭据被 `t.Skip`
- **THEN** MUST NOT 计为完成，汇总 SHALL 标注「未验证」并保持非通过。

#### Scenario: 重启后复跑一致（重启）
- **WHEN** 验收器在同一输入上重启后复跑
- **THEN** 分类与退出码 MUST 与首次一致，MUST NOT 因重启翻转结论。

#### Scenario: 并发消费不互相掩盖（并发）
- **WHEN** 多个消费批次并发校验
- **THEN** 各批次分类 MUST 独立汇总，MUST NOT 因并发丢失失败计数。
