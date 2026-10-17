## MODIFIED Requirements

### Requirement: resume_task 重入原语

resume_task SHALL 在同task id下认领新intent并复用dense→ACK→结果路径。alive_detached/stable/completed/failed可尝试续行；running/suspect及并发认领者 SHALL 被拒绝，cancelled SHALL 引导relaunch。完成态仅表示可尝试新轮，不保证已死native资源可重新输入；执行器失败 SHALL 明确记录而不伪造成功。

#### Scenario: 已完成subagent显式续行
- **WHEN** subagent完成且仍在terminalTTL重入窗口内
- **THEN** Resume以新intent和持久历史创建新Run，保持task id

#### Scenario: cancelled不能重入
- **WHEN** task已cancelled
- **THEN** 返回明确错误并引导新资源relaunch，不调用ResumeFn


### Requirement: 特异出入口

命令 Resume SHALL 在原资源identity上准备输入，冻结精确字节输出基线、认领新的单调intent round，再在资源绑定提交后注入输入；TUI仍 SHALL 拒绝注入。旧/new detector wrapper 可共享底层resource，但观察者 SHALL 按producer round换代，MUST NOT 以wrapper不同为由杀死复用资源。发送失败 SHALL 记round_aborted并保留旧actual producer。

subagent Resume SHALL 新建一次显式委派Run，在可见执行面解析目标，并从持久task_round begin/finish历史恢复前序输入、结果、错误和正文票据；MUST NOT 重跑未完成逻辑目标或靠运行期map保存跨重启上下文。

#### Scenario: 原末行prompt不冒充新输入
- **WHEN** 同一行交互会话被resume
- **THEN** 只有发送之后的字节增量可满足input pattern，旧末行不能使新轮提前就绪

#### Scenario: 子任务跨重启显式续行
- **WHEN** 已完成subagent任务经显式resume追加输入
- **THEN** 新Run获得持久轮次历史，沿选定执行面解析目标并保留同task id

## ADDED Requirements

### Requirement: 意图轮与实际 producer 轮分离

每次被受理的Resume尝试 SHALL 消耗一个新intent round并保存本次owner。actual producer round/owner/seq与TTL SHALL 独立保存；失败意图 MUST NOT 复用编号、覆盖旧producer、清掉其等待或续命。round_aborted SHALL 作为内部事实提交而不伪装为旧producer终局。新生产绑定和候选TTL SHALL 先于实际Start持久化，拒绝时回滚候选资源。

#### Scenario: 失败后再次resume
- **WHEN** round 2受理后发送失败，而round 1仍在原会话中
- **THEN** 实际producer保持round 1；下一次尝试使用round 3，不复用round 2
