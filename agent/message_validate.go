package agent

import (
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// repairToolPairing enforces valid tool_call/tool-result pairing on the message
// list about to be sent to the model (conversation-self-heal L2). It is a pure
// function over the input slice — it does NOT touch the persistent projection.
//
// Conservative repair (only provably-broken items are removed):
//   - a tool result whose tool_id was never declared by a preceding assistant
//     tool_call → orphan → dropped;
//   - a second+ tool result for a tool_id already answered → duplicate → dropped
//     (this is the case that produced the observed API 4xx: a duplicated
//     role=tool message in history).
//
// Returns the (possibly new) slice and the number of repairs (0 = unchanged).
func repairToolPairing(msgs []model.Message) ([]model.Message, int) {
	declared := make(map[string]bool) // tool_call IDs declared by assistant messages seen so far
	consumed := make(map[string]bool) // tool_call IDs already answered by a tool result
	repairs := 0
	out := make([]model.Message, 0, len(msgs))

	for _, m := range msgs {
		switch m.Role {
		case model.RoleAssistant:
			for _, tc := range m.ToolCalls {
				if tc.ID != "" {
					declared[tc.ID] = true
				}
			}
			out = append(out, m)
		case model.RoleTool:
			id := m.ToolID
			if id == "" || !declared[id] {
				repairs++
				log.Warnf("[msgvalidate] drop orphan tool result: tool_id=%q (no preceding tool_call)", id)
				continue
			}
			if consumed[id] {
				repairs++
				log.Warnf("[msgvalidate] drop duplicate tool result: tool_id=%q", id)
				continue
			}
			consumed[id] = true
			out = append(out, m)
		default:
			out = append(out, m)
		}
	}
	return out, repairs
}
