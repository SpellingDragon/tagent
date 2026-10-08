// delivery.go 是组合根的跨 agent 投递面：外部化冥想的产出经授权、寻址与具名拒绝，
// 从投递方进入目标 agent 既有的注入入口，目标侧零新代码。
// 契约: docs/wiki/reliability/durable-delivery.md#lineage-visibility
package tagent

import (
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/SpellingDragon/tagent/agent"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"

	"trpc.group/trpc-go/trpc-agent-go/model"
)

// ErrDeliveryNotAllowed 表示投递未获授权：目标名不在投递方的 deliver_to 白名单内，空白名单拒绝一切。
var ErrDeliveryNotAllowed = errors.New("tagent: delivery is not authorized by the sender's deliver_to allowlist")

// ErrDeliveryBlindTarget 表示白名单目标的分区落在投递方观察面之外：投递方没观察过它。
var ErrDeliveryBlindTarget = errors.New("tagent: delivery target partition is outside the sender's observation surface")

// ErrUnknownDeliveryTarget 表示目标名不在本进程常驻表：寻址面只覆盖同进程装配出的 agent。
var ErrUnknownDeliveryTarget = errors.New("tagent: delivery target is not in the process resident table")

// ErrDeliveryTargetNotRunning 表示目标常驻但其持久循环未运行，错误携带目标名与循环状态。
var ErrDeliveryTargetNotRunning = errors.New("tagent: delivery target's persistent loop is not running")

// deliveryLineage 是投递进入目标 mailbox 时使用的谱系：单源常量，投递面零字面量。
const deliveryLineage = tagentevent.LineageMeditation

// deliveryHeader 是投递消息的来源头前缀，测试与宿主据此识别一条投递。
const deliveryHeader = "[delivery]"

// deliveryAuthority 是一个投递方的授权面，由装配期按实例登记。
//
//   - deliverTo 是声明的白名单目标名，空集合拒绝一切投递。
//   - observedNames 与 observed 成对登记观察面声明与其分区集，报错时点名观察面。
type deliveryAuthority struct {
	deliverTo     []string
	observedNames []string
	observed      map[int]bool
}

// deliveryAuthorities 按 agent 实例登记授权：一个进程可装配多个组合根，
// 按名字索引会把两个根的声明混成同一份授权。
var deliveryAuthorities sync.Map

// registerDeliveryAuthority 为装配出的实例登记投递授权，并挂上随实例关闭的撤销。
func registerDeliveryAuthority(ta *agent.TagentAgent, mc agent.MeditationConfig) {
	if ta == nil || len(mc.DeliverTo) == 0 {
		return
	}
	authority := &deliveryAuthority{
		deliverTo:     append([]string(nil), mc.DeliverTo...),
		observedNames: append([]string(nil), mc.ObservedNamespaces...),
		observed:      partitionSet(mc.ObservedNamespaces),
	}
	deliveryAuthorities.Store(ta, authority)
	ta.RegisterCloser(stopCloser(func() { deliveryAuthorities.Delete(ta) }))
}

// lookupDeliveryAuthority 取回实例登记的授权面；未登记等于没有白名单。
func lookupDeliveryAuthority(ta *agent.TagentAgent) (*deliveryAuthority, bool) {
	value, ok := deliveryAuthorities.Load(ta)
	if !ok {
		return nil, false
	}
	authority, typed := value.(*deliveryAuthority)
	return authority, typed
}

// validateDeliverySurface 在装配期具名拒绝盲投声明：deliver_to 的每个目标分区都要落在观察面上。
func validateDeliverySurface(name string, observed, deliverTo []string) error {
	coverage := partitionSet(observed)
	for _, target := range deliverTo {
		if target == "" {
			continue
		}
		if !coverage[memory.PartitionIDFromName(target)] {
			return fmt.Errorf(
				"agent %q: meditation.deliver_to %q is not covered by meditation.observed_namespaces %v (blind delivery)",
				name, target, observed)
		}
	}
	return nil
}

// DeliverToAgent 把一条产出从投递方 agent 送进目标 agent 的 mailbox，谱系固定为 meditation。
//
//   - 目标名必须属于投递方的 deliver_to 白名单；白名单缺省为空，拒绝一切投递。
//   - 白名单目标的分区必须属于投递方观察面，盲投具名拒绝。
//   - 寻址只走同进程常驻表：未知目标具名拒绝，不发生任何网络调用。
//   - 目标循环未运行只返回错误：不起新 Run、不落盘等待、不静默丢弃。
//   - 成功语义是已进入 mailbox；与用户输入同批时被移除是合法结局，不构成投递失败。
//   - 消息自带来源头（来源 agent 与目标会话），目标无需回查即可理解来源。
//
// 契约: docs/wiki/reliability/durable-delivery.md#lineage-visibility
func DeliverToAgent(from *agent.TagentAgent, targetAgent, sessionID string, msg model.Message) error {
	if from == nil {
		return fmt.Errorf("%w: sender agent is nil", ErrDeliveryNotAllowed)
	}
	sender := senderName(from)
	authority, registered := lookupDeliveryAuthority(from)
	if !registered {
		return fmt.Errorf("%w: agent %q declared no deliver_to allowlist, target %q refused",
			ErrDeliveryNotAllowed, sender, targetAgent)
	}
	if !containsName(authority.deliverTo, targetAgent) {
		return fmt.Errorf("%w: agent %q allowlist %v excludes target %q",
			ErrDeliveryNotAllowed, sender, authority.deliverTo, targetAgent)
	}
	partition := memory.PartitionIDFromName(targetAgent)
	if !authority.observed[partition] {
		return fmt.Errorf("%w: agent %q observes %v, target %q partition %d is outside it",
			ErrDeliveryBlindTarget, sender, authority.observedNames, targetAgent, partition)
	}
	resident := from.ResidentTable()
	target := resident[targetAgent]
	if target == nil {
		return fmt.Errorf("%w: target %q absent, resident agents %v",
			ErrUnknownDeliveryTarget, targetAgent, sortedResidentNames(resident))
	}
	if !target.IsLoopActive() {
		return fmt.Errorf("%w: target %q loopActive=false closeStarted=%v",
			ErrDeliveryTargetNotRunning, targetAgent, target.CloseStarted())
	}
	target.InjectMessageWithSource(deliveryLineage, describeDelivery(sender, sessionID, msg))
	return nil
}

// describeDelivery 给投递消息盖来源头，并补齐缺失的角色使其能进入目标 turn。
func describeDelivery(sender, sessionID string, msg model.Message) model.Message {
	if msg.Role == "" {
		msg.Role = model.RoleUser
	}
	header := fmt.Sprintf("%s 来源 agent：%s", deliveryHeader, sender)
	if sessionID != "" {
		header += fmt.Sprintf("；目标会话：%s", sessionID)
	}
	msg.Content = header + "\n\n" + msg.Content
	return msg
}

// senderName 报告投递方在常驻拓扑里的身份名。
func senderName(ta *agent.TagentAgent) string {
	if name := ta.Info().Name; name != "" {
		return name
	}
	return "<unnamed>"
}

// containsName 在白名单里逐名比对：白名单规模由配置声明，个位数。
func containsName(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

// partitionSet 把 namespace 声明名映射到存储分区，身份轴与插件、装配面同源。
func partitionSet(names []string) map[int]bool {
	out := make(map[int]bool, len(names))
	for _, n := range names {
		if n == "" {
			continue
		}
		out[memory.PartitionIDFromName(n)] = true
	}
	return out
}

// sortedResidentNames 给出常驻表的名字清单，具名拒绝的报错保持可复现。
func sortedResidentNames(resident map[string]*agent.TagentAgent) []string {
	out := make([]string, 0, len(resident))
	for n := range resident {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
