package event

import "trpc.group/trpc-go/trpc-agent-go/model"

// TypeWFReceived wf.* 是 workflow 运行时的事实链记录类型：接收、活动意图、活动结果、信号、
// 转移（检查点）、终态、安全移除证据。事实链是这些状态的唯一真源，检查点适配器、
// 活动索引、任务板等视图都由它折叠而来。
//
// 引擎已撤回，这些类型现存的唯一作用是让历史 wf.* 记录继续被投影、召回与嵌入排除；
// 因此注册必须保持 TTLDays 为 0 —— 任何正值都会静默缩短既有记录的保留期。
//
// 契约: docs/wiki/event/event-architecture.md#internal-retention
const (
	// TypeWFReceived 记录一个输入批次成员已被持久接收。
	TypeWFReceived = "wf.received"
	// TypeWFIntent 在活动体执行前声明其身份三元组 {lineage,node,attempt}。
	TypeWFIntent = "wf.intent"
	// TypeWFResult 在活动推进前持久化其结果。
	TypeWFResult = "wf.result"
	// TypeWFSignal 承载外部持久信号（settle、send/resume、cancel、TTL 续期与到期）。
	TypeWFSignal = "wf.signal"
	// TypeWFTransition 是检查点转移事实，携带 {lineage,step,node}、前驱检查点引用、活动句柄与输出摘要。
	TypeWFTransition = "wf.transition"
	// TypeWFFinalized 是 workflow 终态事实：框架的 Done 本身不构成终态，只有此事实构成。
	TypeWFFinalized = "wf.finalized"
	// TypeWFSafeRemoved 是受保护材料已完成移除的证据事实；保留租约只在**已完成**的移除上释放，而非已发起。
	TypeWFSafeRemoved = "wf.safe_removed"
)

// WFExcludedTypes 返回全部 wf.* 类型名，供诊断与守卫断言使用；投影/召回/嵌入的排除
// 判定不依赖此列表，而统一走 IsNonProjectionRecord 与注册表。
func WFExcludedTypes() []string {
	return []string{
		TypeWFReceived,
		TypeWFIntent,
		TypeWFResult,
		TypeWFSignal,
		TypeWFTransition,
		TypeWFFinalized,
		TypeWFSafeRemoved,
	}
}

func init() {
	for _, name := range WFExcludedTypes() {
		RegisterEventType(EventTypeSpec{
			Name:          name,
			Role:          model.RoleUser,
			Skeleton:      false,
			TTLDays:       0,
			Embeddable:    false,
			Recallable:    false,
			NonProjection: true,
		})
	}
}

// MetaKeyWFLineage wf_* 是 workflow 运行时溯源在事实链上的唯一权威身份键，写入 FullEvent.Metadata；
// 消费方只经这些常量解析，不得使用字面量。
const (
	MetaKeyWFLineage = "wf_lineage"
	MetaKeyWFNode    = "wf_node"
	MetaKeyWFAttempt = "wf_attempt"
	MetaKeyWFStep    = "wf_step"
	MetaKeyWFKind    = "wf_kind"
	MetaKeyWFWriter  = "wf_writer"
	MetaKeyWFExtra   = "wf_extra"
)
