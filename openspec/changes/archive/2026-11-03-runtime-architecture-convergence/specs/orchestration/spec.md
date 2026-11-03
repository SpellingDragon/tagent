# runtime-architecture-convergence Orchestration Delta

## ADDED Requirements

### Requirement: 独立领域拷问先于实施授权

每个领域 SHALL 由不带前序审计结论的独立代理阅读当前文档、主规格与代码，输出具名场景、证据和待裁 frontier。编排者 MUST 对每项分别裁定成立、反证驳回或范围外登记；W0 未完成前 MUST NOT 派发产品实现。

#### Scenario: 六域报告到齐

- **WHEN** 六个领域各自提交文档—规格—代码证据及待裁问题
- **THEN** 编排者逐项记录裁定并回写对应二级子变更
- **AND** 任何未裁关键问题都阻止相关实施单进入就绪队列

### Requirement: 公共概念只减不增的实质判据

新增导出类型、接口、状态名或读面 MUST 对应独立领域事实并说明既有概念为何不能承载；缓存、锁、围栏、补偿事务和实现索引 SHALL 默认内化。仅改名、增加别名或缩小接口方法数 MUST NOT 计为概念收敛。

#### Scenario: 修复拟新增公共机制

- **WHEN** 一个修复需要新增导出机制类型
- **THEN** 该类型必须进入概念账本并证明独立领域必要性
- **AND** 无法证明时改为非导出实现或删除，不得以测试绿豁免

### Requirement: 公共症状锚与垂直红绿证据

实施 SHALL 从用户原话对应的公共入口症状锚开始，每个孙任务 MUST 冻结正锚、负锚和生产消费者锚。完成证据 MUST 包含非空转的红→绿过程；内部 helper 或私有状态断言 MUST NOT 替代公共行为。

#### Scenario: 地址消费最小探针

- **WHEN** 通过正式 Host/send 路径向空闲地址提交输入
- **THEN** 该输入只能由该地址的常驻 Consumer 驱动并得到其自身答案
- **AND** 该行为锚在旧实现上失败、在收敛实现上通过

### Requirement: 每批修复接受新鲜独立复审

每个修复批次完成后 SHALL 由不带上一轮结论的新只读审者重扫受影响面和同类路径。审者 MUST 报告扫描范围与逐项证据；只有显式 frontier 空才可声明该批放行。

#### Scenario: 修复后再次出现关键问题

- **WHEN** 新审者在修复面或同机制路径发现关键问题
- **THEN** 编排者重新裁定并派发最小修复，不得沿用上批自证结论
- **AND** 同一机制三次修复仍出现关键缺陷时停止打补丁并升级设计裁定

### Requirement: CI 分层不得后置或丢失完整证明

CI 优化 SHALL 区分快速 PR 反馈与 merge 前完整证明，保留独立进程、矩阵身份和缺号/重号/缺片拒收。完整长程测试 MUST 在进入 main 前通过；MUST NOT 以扩大超时、放大未实测矩阵或 push 后 nightly 冒充等价优化。

#### Scenario: 长测移到 merge queue

- **WHEN** 完整六组合改为 merge_group 执行
- **THEN** 相同输入、重启、seed、window 与证据校验全部保留
- **AND** 缺少任何必需组合的变更不得进入 main
