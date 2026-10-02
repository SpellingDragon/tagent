// Package task 提供"把一次工作交给后台去跑"的任务层，管的对象是任务本身，而不是它跑在哪个执行面上。
//
// - TaskManager：登记任务、跟踪终态、按 TTL 统一回收（生命周期锚点是派生时刻，重入发送或 resume 会刷新它），并把状态变化作为事实通知出去。
// - task_board：渲染在途面板，供模型在同一回合里判断"该等还是该再派"。
// - 结算检测器（NewFuncSettleDetector／NewManualDetector／NewManualDetectorDetach）：由宿主决定一次后台工作何时算完成，框架不猜。
// - DetachAfter／LifetimeOf：把"多久可脱离前台""还能活多久"这类策略交给声明方。
// - TaskControllerFromContext／TaskSpawnerFromContext：工具在调用上下文里取回本轮可用的委派入口，取不到就退化为同步执行。
// - 三条边界：不决定是否投影到会话（在 memory 与 agent/compress），不决定进程如何启动与回收（在 tool/action），不为省事把完成通知在入口拦掉——拦截会误杀合法的完成信号。
// - 任何"已执行但未纳入任务层管理"的情形都必须向调用方如实说明。
// 契约: docs/wiki/agent/task-lifecycle.md
package task
