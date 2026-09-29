package tagent

import (
	"sync/atomic"

	"github.com/SpellingDragon/tagent/agent"

	"trpc.group/trpc-go/trpc-agent-go/log"
)

// candidateTxn is the ordered responsibility table of ONE candidate
// (S-B/2.3「事务」)：每次资源 acquire／owner 登记／agent 构成后**立即**入表
// （先记后判错——失败父的登记也在册可回退），早于下一个可失败动作；discard
// 按获取**逆序**展开（部分登记撤销→建成者 Close→指纹复位），清理顺序来自实际
// 获取证据而非 ownedAgentNames 差集推断或 map 遍历序（design 核心簇：废除
// 差集推断与 map 顺序假设）。reload 与 rollback 共用同一结构。
type candidateTxn struct {
	rc     *runtimeConfig
	order  []string                      // 责任表：获取序（append-only）
	agents map[string]*agent.TagentAgent // name → 建成实例；nil = 仅登记未建成（失败父）
}

func newCandidateTxn(rc *runtimeConfig) *candidateTxn {
	return &candidateTxn{rc: rc, agents: map[string]*agent.TagentAgent{}}
}

// acquire records a responsibility the candidate has taken on: a fully built
// agent (a != nil) or a partial owner registration whose build failed midway
// (a == nil — its store lease was already released by buildAgentDFS's own
// buildOK defer, so discard only revokes the registration). Idempotent per
// name: a dependency acquired by an earlier top is not re-recorded.
func (tx *candidateTxn) acquire(name string, a *agent.TagentAgent) {
	if _, ok := tx.agents[name]; ok {
		return
	}
	tx.agents[name] = a
	tx.order = append(tx.order, name)
}

// discard unwinds every responsibility in REVERSE acquisition order and
// returns that order (recorded into the TEST-only probe). Per item:
// store-owner registration revoked, residentMemFP entry reset, built agent
// Closed (its own Close releases the store lease; partial registrations have
// no lease left to release). The published online face is never touched —
// discard only ever runs before the single commit point.
func (tx *candidateTxn) discard() []string {
	if tx == nil {
		return nil
	}
	order := make([]string, 0, len(tx.order))
	for i := len(tx.order) - 1; i >= 0; i-- {
		n := tx.order[i]
		order = append(order, n)
		a := tx.agents[n]
		delete(tx.rc.residentMemFP, n)
		// store 将被关闭/已失败：留着 pid 登记会让日后复用同一回收堆地址的新
		// agent 被陈旧条目假阳性拒绝为 partition collision。
		tx.rc.unRegisterStoreOwner(n)
		if a != nil {
			if cerr := a.Close(); cerr != nil {
				log.Warnf("[org-hotreload] refused-candidate cleanup: close hot-added %q: %v", n, cerr)
			}
		}
		delete(tx.agents, n)
	}
	recordDiscardOrder(order)
	return order
}

// lastDiscardOrder is the TEST-only probe for the reverse-acquisition-order
// contract (same introspection family as agent.TagentAgentsConstructed): no
// production path reads it. Map-iteration cleanup order was UNOBSERVABLE —
// which is exactly why it survived review — so the contract needs a witness.
var lastDiscardOrder atomic.Value // []string

func recordDiscardOrder(order []string) {
	if len(order) == 0 {
		return
	}
	lastDiscardOrder.Store(order)
}
