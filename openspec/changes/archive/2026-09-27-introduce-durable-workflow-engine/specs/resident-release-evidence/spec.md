## ADDED Requirements

### Requirement: 编排热更以生产委派变化验收

本变更的编排热更 SHALL 以宿主生产入口验收：修改现有 YAML 后，新开始请求的实际工具声明与子调用目标随之变化；进行中请求及其重试、嵌套子调用、ACK 后后台执行保持原绑定；常驻 store/session/projection/TaskManager 身份不漂移；旧绑定与资源在实际执行停止后安全退役。候选失败、回滚、agent 增删、同名重入与并发发布交叉场景须有真实行为断言，不以指针、hash 或代际日志编号代替。

性能 SHALL 分报候选构造/发布成本、请求获取版本开销与退役回收成本。真实 LLM、掉电、72h、真实渠道和生产存储单列授权；历史完成勾选不得继承为本目标完成证据。

#### Scenario: 生产入口切代验证

- **WHEN** R1 在 G1（A→B）开始执行后发布 G2（A→C），随后 R2 开始
- **THEN** R1 的模型声明与实际调用保持 B 直至结束（含后台），R2 调用 C；两者均不以日志编号替代行为断言

#### Scenario: 常驻身份与任务板不漂移

- **WHEN** 连续多次编排发布/回滚并伴随在途请求
- **THEN** 既有 resident 契约测的存储身份、会话续写与任务板断言原样通过；被替代的拓扑冻结断言按新规格迁移并有记录

### Requirement: 安全验收不由统计或豁免替代

短锁 SHALL 用阻塞构建／阻塞清理的屏障验证，owner 有界性 SHALL 覆盖不断更换名字后的收敛，实际停止 SHALL 覆盖取消后仍存活的生产者。平均耗时、有限次数 race 未命中、固定几个名字发布多代均 MUST NOT 替代相应安全断言。

本变更 race 验收 SHALL 保留全部失败，包括全上游访问栈；family 和 broad framework 分类只能用于诊断，不得吞掉子进程错误。命令记录原始退出码。完整补丁的必要未跟踪文件 SHALL 进入交付清单，HEAD-only 基线结果不能冒充新补丁验证。

#### Scenario: 纯上游 race 也阻断验收

- **WHEN** 子进程只报告纯上游栈 DATA RACE，或同时含断言失败／panic
- **THEN** 验收保持失败并保留原输出和退出码，不因分类器识别旧家族或没有 tagent 帧而通过

#### Scenario: 慢构建与历史名字分别验证

- **WHEN** 构建／回收停在屏障，同时持续发布不同名字并令无引用旧资源收敛
- **THEN** 请求获取不等长操作，已结束 owner 数量回落；性能样本单独记录，不用机器平均延迟替代行为证据

### Requirement: 验收覆盖完整装配链与复杂度收敛

最终证据 SHALL 覆盖热增后的真实存储／热参消费、回滚共享依赖、多级重入、remote-only 热更、ACK 后父 turn 结束的后台回流、Close 超时后无新活动的最终资源退出。MUST NOT 把 resident getter 测与独立 shell 单测、inline 返回与 ACK 返回、远端重试与本地跨发布分别通过拼成整链已通过。

性能 SHALL 分开配置读取、完整候选构建、已准备候选提交、租约获取和具体回收；计时不混入编辑、探针、断言、日志或轮询等待，若测试测的是端到端则明确标注。所有未发布候选与测试资源 SHALL 关闭。复杂度验收 SHALL 展示重复热更 shell／状态／双事务／广播和角色专用执行旁路已移除，构造数量随真正完整 tagent 和实际上下文而非发布次数增长；MUST NOT 以全组织只剩一个 TaskManager 或删掉子 agent 的 bus 作为优化证据。

#### Scenario: ACK 后的跨发布与回流

- **WHEN** 宿主已收到 ACK 且父 turn 结束，后台 producer 仍停在屏障，此间发布 G2
- **THEN** 后台按 G1 完成，实际资源随后释放，原 TaskManager 的 task_settled 唤起 G2 新 turn；dense 窗内 inline 返回不能替代此测试

#### Scenario: 完整补丁的最终门禁

- **WHEN** 准备标记全部任务完成
- **THEN** 先列所有修改及必要未跟踪文件的测试映射，root build/vet/short 包含 tests 子包，wechat-bot 独立执行 build/vet/short，全部受影响包与跨发布矩阵零豁免 race；保存真实退出码、命中过滤器及未跑范围，工件 strict 不能替代代码验收

#### Scenario: 资源测试路径不跨用例共享

- **WHEN** 同一进程重复运行不同热更／恢复测试
- **THEN** 路径由 testing.TB 生命周期管理，重启模拟只在同一测试内显式共享根；先关闭资源再结束临时根，无工作树 fallback，缺少前后快照时不声称未新增用户数据
