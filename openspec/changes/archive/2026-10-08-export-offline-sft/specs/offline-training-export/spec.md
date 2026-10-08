# offline-training-export Specification

## ADDED Requirements

### Requirement: 授权的只读事实导出
导出 SHALL 仅扫描显式授权分区并在父键读取前后核验权限；空授权必须拒绝。MUST NOT 改TTL/墓碑、打开第二生产writer或将导出快照用作运行真源。

#### Scenario: 跨分区父键与缺父
- **WHEN** feedback指向未授权或已不可取得的父事件
- **THEN** 分别返回forbidden或expired_or_missing，不猜原因、不越权补读。

### Requirement: 精确关联不等同自动奖励
feedback SHALL 经parent及call关联证据导出；无证据/冲突必须保留状态，不用trace存在与否决定业务身份，不自动把任务终态映射奖励。

#### Scenario: 多反馈与未绑定任务
- **WHEN** 同一call有多条反馈或某task无精确call映射
- **THEN** 保留反馈数组与unbound/unrated，不强制聚合或丢弃。

### Requirement: 工具感知SFT与可靠loss边界
strict转换 SHALL 保留tools、toolcall ID/参数、tool结果及无正文assistant；用本地模板整体分词，仅目标assistant可靠边界置loss。MUST NOT 平文拼role或分别encode猜边界。

#### Scenario: 工具调用无正文
- **WHEN** 目标assistant只有tool_calls
- **THEN** 生成有效非空目标mask样本；历史和tool结果mask为0；末条预测动作不要求尚未发生的工具结果，历史配对错误仍拒绝。

#### Scenario: 模板或媒体不支持
- **WHEN** 无可靠assistant mask或未支持多模态部件
- **THEN** 拒绝并给原因，不静默降级为纯文本。

### Requirement: 数据集完整性与隔离
strict样本 SHALL 核验capture/export manifest并按root session分组切分；缺失、格式错误、legacy未知质量必须进入拒绝/状态清单。隐私策略与输出格式必须显式选择，不能覆盖已有数据集。

#### Scenario: 子调用与旧记录
- **WHEN** 数据包含同session子调用及无v2字段的旧JSONL
- **THEN** 同组不跨train/test，legacy不伪装严格完整。

### Requirement: 真实本地消费验收
交付 SHALL 包含真实模型工具往返到本地真实tokenizer和collator batch的证据；缺凭据、模板不支持或SKIP不能计完成，不声称权重训练收益。

#### Scenario: 最小可用数据集
- **WHEN** 两个隔离session完成真实工具使用并导出
- **THEN** 必需用例PASS、目标token非空、labels/mask一致、分割无泄漏、无生产私密数据。
