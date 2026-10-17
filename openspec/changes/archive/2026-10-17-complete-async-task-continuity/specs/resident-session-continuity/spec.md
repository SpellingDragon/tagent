## MODIFIED Requirements

### Requirement: 启动重挂与任务重关联

冷启动 SHALL 在任务与私有调用登记后重挂native资源，核验TaskID及不可变resource identity，恢复原producer round/seq、输出字节基线和资源phase。恢复旧无identity材料时 SHALL 安全取得并提交identity后启用观察。相同session名的替换资源 MUST NOT 被当作旧任务续行；已消失资源 SHALL 明确结算失败。未资源化intent只经原intentprepare/绑定后激活，已有资源MUST NOT重复执行命令。

#### Scenario: 名称相同但资源已替换
- **WHEN** 当前native session名称与历史相同而resource identity不同
- **THEN** 不发送输入、不采样或kill该替换资源，旧任务明确失败或需对账

#### Scenario: 原资源仍活
- **WHEN** 原identity和producer round可核验
- **THEN** 恢复原基线与seq并真实接通信号链，不从round 1重采样

### Requirement: 长驻会话无年龄死刑

孤儿清扫 SHALL 先尝试收养，MUST NOT 仅按初次spawn年龄清理仍有owner或任务引用的resident资源；只对仍无人持有且超过orphan grace的资源清理。该孤儿规则 MUST NOT 给受管任务的统一TTL提供豁免：已登记Task仍在自己的绝对TTL到期时由owner取消。

#### Scenario: 老服务被正常收养
- **WHEN** 原资源仍有合法任务/owner且其Task TTL尚未到期
- **THEN** 不因出生年龄被孤儿清扫误杀

#### Scenario: 受管服务TTL到期
- **WHEN** resident Task的统一TTL到期
- **THEN** 仍由任务owner请求终止，不能用孤儿清扫豁免绕过TTL

### Requirement: 重挂后信号链真实接通

恢复 SHALL 绑定原Task producer round并证明真实watch/settle到达；tracked=true、看板running或同名session不足以证明恢复。terminal只能ACK-only绑定原receipt，receiver全部就绪后才放行pending交付。活执行保持原资源；新意图与实际producer换代原子记录，旧捕获不能改新轮。

#### Scenario: 终局待受理恢复
- **WHEN** 原terminal receipt已保存但receiver尚未受理
- **THEN** 使用原round/seq/time/body恢复ACK链，不重采样或提前清理证据
