## ADDED Requirements

### Requirement: 任务关闭等待真实观察与受理尾部

TaskManager.Close(ctx) SHALL 有界停止新准入、观察和retry并请求所有已纳管detector取消；CloseDone SHALL 只在操作、watch、callback、worker及实际producer Stopped均退出后关闭。回调返回、通道关闭、kill请求或Close超时本身 MUST NOT 伪造Stopped。Cancel先于Start时，activatable producer MUST NOT 再启动业务。超时后同一尾部仍负责最终收敛。

#### Scenario: callback和producer分别延迟退出
- **WHEN** Close截止到达而callback仍阻塞或producer未确认停止
- **THEN** Close返回deadline错误且CloseDone保持未关闭；两者真正退出后才完成
