# event-turning：事件转动完整性——终结通知驱动回合＋端到端旅程测试套件

## 裁定（用户）
settled 不转动只是发现的一部分；这类接缝断裂属于明显质量问题——没有集成完整转过，
必须由测试用例发现并充分拦截。

## 事故链（本会话全部接缝断裂，单元全绿旅程全断）
1. recall 读面 Bind 前快照——运行时发现
2. 冥想静默投递——运行时发现
3. 三龄段零接线——追问发现
4. Capture 未挂载／热换无入口——审计发现
5. 投影会话投递断路——用户运行时发现
6. **settled 通知不转动（本变更主修）**——用户运行时发现

## 会转的与不会转的
| 事件 | 驱动者 |
|---|---|
| 用户事实→冥想计时 | meditation.Driver（PostCommitObserver）✅ |
| 体积超阈→折叠 | AgeFoldPlugin ✅ |
| replacement 事实→换模型 | LeaseObserver ✅ |
| **任务终结通知→结果回合** | **无——通知只落链，等用户下一句才被读** ❌ |

## 修法

### T1 SettleDriver（终结通知驱动——与冥想同端口同模式）
- 根包新文件：SettleDriver 实现 plugin.Instance＋PostCommitObserver（name=settle-driver）
- ObserveCommit：事实为任务终结 Notice 且其会话有已记住的用户地址 → 对该会话
  主动发起一轮 headless Invoke（TriggerSource="task"，无 Input）——Drive 头自然
  增量读通知 → 模型产出结果/摘要 → deliverFresh 投递用户
- 防循环：只对任务终结类通知触发；随动回合自身若再委派→再 settle→再转（链式异步
  正确转动）；无新任务即自然停止；随动产出非 host-facing 戳不再触发冥想/自身

### T2 端到端旅程测试套件（tests/integration/journey/）
脚本化 provider＋记录型 sender，一个进程内完整走 Boot→Drive→投递：
- J1 同步旅程：消息→回复→用户收到
- J2 异步委派旅程（T1 消费者锚）：消息→受理回执→settle→**结果主动送达**（先红后绿）
- J3 冥想回流旅程：反思→deliver_summary→用户会话→下轮装配可见
- J4 热换旅程：reload→replacement→新请求新模型
- J5 压缩旅程：多轮→超阈折叠→装配仍可读
- J6 审批旅程：critical→Hold→批准消息驱动→执行→用户收到结果

## 验收
- J2 红转绿见证（T1 前必红）
- J1-J6 全绿＋37 包绿＋golden match＋lint ok
- wechat-bot 实机复验：委派完成后摘要主动送达
