package memory

import "sync"

// ==================== 有限 key 保留租约（§2.8）====================
//
// RetentionLease 保护「未确认恢复材料」的 durable 原文——未确认 inbox envelope 的
// prepared fact key 与 receipt key、普通 spill 待重放 key——使其在恢复 owner 能安全
// 释放之前不被 TTL 过期、容量淘汰、内容降分辨率或墓碑最终清理物理销毁。持有者是共享
// 资源 owner（FileSegmentStore），非任一 agent。
//
// 不变量（event-segment-store §2.8）：
//   - 租约是**内存守护集**，启动时由现有未确认材料（inbox envelope 文件 + spill 文件）
//     重建，MUST NOT 引入第二持久保留表或全历史去重集合。
//   - 保护期内只拒绝**销毁**；无损搬迁（压实段合并复制原文到高层段）允许。
//   - 显式删除受保护 key 返回 ErrEventProtected（不销毁）。
//   - 释放（ack 目录同步成功 / spill 安全移除）后恢复该 key 原类型的 TTL，按其原有
//     timestamp 参与年龄淘汰，绝不重新盖时间。
//
// 引用计数：同一 key 可同时被 envelope owner 与 spill owner 保护，Release 幂等地
// 递减，归零才真正解除保护。nil-safe（未接线租约的 store 表现为无保护）。
type RetentionLease struct {
	mu   sync.RWMutex
	refs map[int64]int // key -> 存活持有者数（>0 即受保护）

	// §2.8 B：就绪门控。恢复 owner 从现有未确认材料（inbox envelope 文件 + spill）重建
	// 租约后 MarkReady；生命周期扫描器在首趟破坏性扫描前等待就绪，杜绝「先淘汰后登记」
	// 重启竞跑（spec L117-119）。未挂租约（nil）的 store 无此约束，立即扫描。
	ready     chan struct{}
	readyOnce sync.Once

	// §5.8：登记屏障（hold 计数）。任一 owner 正在清点/登记（含已运行 store 的
	// late attach、组合根的 build 级汇总门）期间，遗忘发布必须暂停——扫描器等
	// HoldClear，且该等待 **不受 armGrace 回退**（宽限只兜「从未登记任何恢复
	// owner」的后备；显式屏障不能被计时静默绕过）。holds 与 ready 正交：
	// ready 是「首个 owner 完成过首次重建」的一次性事实，hold 是可重复取得的
	// 登记窗口，嵌套/并发多 owner 各自 Begin/End 配对。
	holds    int
	holdGate chan struct{} // holds==0 时处于 closed；0→1 时换新
}

// NewRetentionLease 构造空租约（未就绪：挂上它的 store 会等首次 populate 后才扫描）。
func NewRetentionLease() *RetentionLease {
	gate := make(chan struct{})
	close(gate) // 无 hold = 屏障常开
	return &RetentionLease{refs: make(map[int64]int), ready: make(chan struct{}), holdGate: gate}
}

// BeginHold 取得登记屏障：从此刻起 HoldClear 不放行，扫描器不得发布遗忘。
// 调用方 MUST 在清点+登记（含失败路径的显式阻断决策）完成后 EndHold 配对；
// 组合根对共享 store 的多个 owner 各自配对，计数嵌套安全。
func (l *RetentionLease) BeginHold() {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.holds++
	if l.holds == 1 {
		l.holdGate = make(chan struct{}) // 0→1：关闸（旧的已 closed，换新）
	}
	l.mu.Unlock()
}

// EndHold 释放一层登记屏障；最外层归零时放闸。多余的 End 是 no-op（不 panic、
// 不反向放闸未配对的 Begin）。
func (l *RetentionLease) EndHold() {
	if l == nil {
		return
	}
	l.mu.Lock()
	if l.holds > 0 {
		l.holds--
		if l.holds == 0 {
			close(l.holdGate)
		}
	}
	l.mu.Unlock()
}

// HoldClear 返回「当前无登记屏障」信号；nil 租约返回已关闭通道（不阻塞）。
// 扫描器在首趟破坏性扫描前无条件等待它（无宽限回退，见 §5.8 注释）。
func (l *RetentionLease) HoldClear() <-chan struct{} {
	if l == nil {
		closed := make(chan struct{})
		close(closed)
		return closed
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.holdGate
}

// MarkReady 登记恢复 owner 已完成首次租约重建，放行扫描器首趟。幂等。
func (l *RetentionLease) MarkReady() {
	if l == nil {
		return
	}
	l.readyOnce.Do(func() { close(l.ready) })
}

// Ready 返回就绪信号通道（供扫描器 select 等待）；nil 租约返回已关闭通道（不阻塞）。
func (l *RetentionLease) Ready() <-chan struct{} {
	if l == nil {
		closed := make(chan struct{})
		close(closed)
		return closed
	}
	return l.ready
}

// Protect 为 key 增加一个持有者（幂等注册）。零 key 忽略。
func (l *RetentionLease) Protect(key int64) {
	if l == nil || key == 0 {
		return
	}
	l.mu.Lock()
	l.refs[key]++
	l.mu.Unlock()
}

// Release 移除一个持有者；归零才解除保护。幂等（重复释放多余的不动作）。
func (l *RetentionLease) Release(key int64) {
	if l == nil || key == 0 {
		return
	}
	l.mu.Lock()
	if l.refs[key] <= 1 {
		delete(l.refs, key)
	} else {
		l.refs[key]--
	}
	l.mu.Unlock()
}

// IsProtected 报告 key 是否仍有存活持有者。nil-safe（无租约＝不保护）。
func (l *RetentionLease) IsProtected(key int64) bool {
	if l == nil {
		return false
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.refs[key] > 0
}

// Len 返回受保护的 distinct key 数（诊断/测试）。
func (l *RetentionLease) Len() int {
	if l == nil {
		return 0
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.refs)
}
