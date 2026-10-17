## Implementation

- [x] 分离等待条件、intent与actual producer身份，冻结信号WaitFor及receipt身份。
- [x] lazy admission、accepted绑定、资源激活、拒收退避及原producer ACK。
- [x] 命令native退出、显式ready/input、resource fencing和精确字节基线。
- [x] logical continuation、私有投影、原轮结果附着及服务交接。
- [x] task_round历史、outbox和输入处置事实，恢复屏障及保留租约。
- [x] nearest-deadline TTL、真实Stopped/CloseDone与关闭竞态守卫。
- [x] 同步wiki、相关主规格delta、CHANGELOG与历史技术报告。

## Evidence

- [x] 归档前主规格strict门禁：116通过、0失败；正常archive同步后117主规格通过、0失败。
- [x] 任务核心针对性回归与race；accepted批量/补发/取消、inline恢复和裸service去重新增case通过一次race。
- [ ] 全仓short/race、独立wechat-bot模块及真实tmux矩阵由集成负责人记录最终结果。
- [ ] lint与生成API文档由集成负责人统一执行；不得以本归档替代发布门禁。
