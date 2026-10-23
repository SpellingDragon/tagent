# design：event-turning

## D1 SettleDriver 归根包（否决：runtime/agent）
驱动需要 Host.Invoke＋deliverFresh＋用户地址记忆——全是宿主装配面。核心 Run 用户（CLI）在
自己的回合读通知，宿主（Boot）才需要主动转。

## D2 随动回合空输入（否决：注入合成用户消息）
InvokeRequest 无 Input＋TriggerSource="task"——Drive 头 progressNotes 增量读通知即是
回合输入；合成消息会污染会话语义。

## D3 防循环（与冥想同构）
触发面＝任务终结通知种类闭表；随动回合产出为普通回合事实（非 host-facing 戳）——
冥想计时/折叠可正常响应，SettleDriver 自身不再触发。

## D4 并发
A（核心：settle_turn.go＋host.go 接线＋Host.fireNoticeTurn）与 B（旅程套件全部文件）
零相交；B 的 J2/J6 按合同先写（A 落地前红＝拦截证据），终态由编排者联合验证。
