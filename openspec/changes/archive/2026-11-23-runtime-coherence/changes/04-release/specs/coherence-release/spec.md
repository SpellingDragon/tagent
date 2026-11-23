## ADDED Requirements

### Requirement: 生命周期矩阵覆盖贡献与 builtin 同规则

挂载、换代、禁用、卸载与重启 SHALL 有正式入口证据；贡献实例与 builtin SHALL 走同一规则。卸载仍有未完义务时 SHALL 具名拒绝；换代失败 SHALL 保留原对象与未结义务。

#### Scenario: 贡献随换代存活

- **WHEN** 一次 reload 发生且贡献实例未变
- **THEN** 该贡献 SHALL 新代继续生效
- **AND** 其未完义务 SHALL 不丢失

### Requirement: 完整矩阵在冻结候选上取证

十短、六长、负变异、三 module、race、smoke、openspec 与归档前拒绝 SHALL 在冻结候选上执行并记录原始退出码。失败轮 MUST NOT 作为绿证据；任一带证据字节改动后受影响结果 SHALL 重取。性能与成本 SHALL 单列，已接受线性项与未归因项分开。

#### Scenario: 失败轮混入

- **WHEN** 一组证据含失败轮
- **THEN** 该组 SHALL 被拒收，重取全绿集
- **AND** 失败样本 SHALL 留存而不是删除

### Requirement: CI 接线真实生效

CI 作业 SHALL 与本地口径一致；缺工具 SHALL 在作业中安装固定版本，证据产物 SHALL 按真实路径绑定。MUST NOT 通过跳过作业、改判据或缩小选择集使作业转绿。远端未绿前 SHALL NOT 宣称已放行。

#### Scenario: 缺 OpenSpec CLI

- **WHEN** 规格门因 CLI 缺失失败
- **THEN** 作业 SHALL 安装固定版本并重跑
- **AND** 不得以 exit 2 记为“无问题”

### Requirement: 独立审与旧账结论

放行 SHALL 经独立只读审分轮拷问，末轮输出扫描范围、未查面与 frontier 空。旧账未闭合项 SHALL 逐条给出最终结论或明确状态；MUST NOT 由本变更完成自动关闭拒并、暂缓或活义务。

#### Scenario: frontier 非空

- **WHEN** 审者仍有到期正确性、安全、成本或需求问题
- **THEN** SHALL NOT 放行，修后重审

#### Scenario: 症状锚

- **WHEN** 完整回滚本变更
- **THEN** S1（及按阶段 0 结论保留的 S2）SHALL 红
- **AND** 修复后 SHALL 绿，两次证据入 F3
