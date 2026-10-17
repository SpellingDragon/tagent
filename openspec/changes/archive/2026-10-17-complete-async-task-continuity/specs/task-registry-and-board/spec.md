## MODIFIED Requirements

### Requirement: TaskManager 内存 registry 与幂等 spawn

TaskManager SHALL 维护每个任务的声明、状态、结果、intent round/owner、actual producer round/owner、seq、等待条件及真实TTL锚点，按active Key幂等去重。生产注册 SHALL 先可靠提交意图和资源绑定，再启动业务；记录失败不得只打日志后假装纳管成功。运行态是同一事实链的fold，不随执行器换代重建。

声明式投影 SHALL 深拷贝Origin、Params、EventKeys与Overrides；恢复闭包工厂仅恢复能力，不覆盖持久身份。command原resource可重接；subagent按持久task_round历史和continuation地址恢复，显式新Run Resume使用当前合法执行面；generic仅在有可恢复工厂时恢复执行。

#### Scenario: lazy去重命中
- **WHEN** 同Key已active而第二次调用仍未prepare
- **THEN** 第二次返回原Task，factory不执行且无新业务副作用

#### Scenario: 内联结果仍可恢复
- **WHEN** dense内完成并返回结果
- **THEN** 同一信号仍形成可靠raw结果及受理证据，重启不复活为无结果suspect

#### Scenario: 子任务终态保留重入窗口
- **WHEN** subagent已结束且仍在terminalTTL内
- **THEN** 任务结果和历史仍支持显式Resume；未受理pending不会因terminalTTL被prune
