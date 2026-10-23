// 契约: docs/wiki/tool/tool-architecture.md#recall-subtools
//
// The recent page tool of the recall surface: the newest events of the granted
// partitions, on the same declarative query semantics the memory read face
// states, newest first and cut to one page.
package recall

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/SpellingDragon/tagent/memory"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// recentArgs is the declared argument shape of one recent page: the type and
// time filters and the page it asks for.
type recentArgs struct {
	// Types restricts the page to the given frozen event types.
	Types []string `json:"types"`
	// Since is the inclusive lower time bound in Unix milliseconds.
	Since int64 `json:"since"`
	// Until is the inclusive upper time bound in Unix milliseconds.
	Until int64 `json:"until"`
	// Limit caps how many events one page carries.
	Limit int `json:"limit"`
}

// recentAnswer is the answer of one recent page: the events the page carries in
// newest-first order, the scope it ran in, and whether more events stand behind
// it than the page could carry.
type recentAnswer struct {
	// Tool is the name of the tool that answered.
	Tool string `json:"tool"`
	// Partitions is the granted partition set the page ran in.
	Partitions []string `json:"partitions"`
	// Since is the lower time bound the page ran under, in Unix milliseconds.
	Since int64 `json:"since,omitempty"`
	// Until is the upper time bound the page ran under, in Unix milliseconds.
	Until int64 `json:"until,omitempty"`
	// Count is how many items the page carries.
	Count int `json:"count"`
	// Truncated reports that the page bound cut the matching set.
	Truncated bool `json:"truncated"`
	// Items is the page, newest first.
	Items []eventItem `json:"items"`
}

// recentTool is the model-facing recent page tool.
type recentTool struct{ base }

// Declaration reports the recent page tool's declaration.
func (t *recentTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{
		Name: RecentToolName,
		Description: "取最近若干条已落地事件：没有具体指向、只想知道最近发生了什么时用它。" +
			"结果按时间从新到旧排列（事件键、类型、摘要、时间），可加类型与时间范围过滤；" +
			"条数有上限，超限即截断并在 truncated 里如实标出；“最近没有事件”与“读不到事件”不是同一个结论。",
		InputSchema: &trpctool.Schema{
			Type: "object",
			Properties: map[string]*trpctool.Schema{
				"types": {
					Type:        "array",
					Items:       &trpctool.Schema{Type: "string"},
					Description: "只保留这些事件类型；省略表示不限类型",
				},
				"since": {
					Type:        "integer",
					Description: "时间下界（含），Unix 毫秒；省略表示不设下界",
				},
				"until": {
					Type:        "integer",
					Description: "时间上界（含），Unix 毫秒；省略表示不设上界",
				},
				"limit": {
					Type:        "integer",
					Description: "本页条数上限，缺省 20，最大 100",
				},
			},
		},
	}
}

// Call answers the newest events of the granted partitions. The page asks for
// one event beyond its bound, so a full page reports whether more events stand
// behind it instead of guessing from a page it cannot see past.
func (t *recentTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	if t.reads.Table == nil {
		return nil, unwired(RecentToolName)
	}
	var args recentArgs
	if err := json.Unmarshal(jsonArgs, &args); err != nil {
		return nil, fmt.Errorf("%s: malformed arguments: %w", RecentToolName, err)
	}
	limit := clampPage(args.Limit)
	refs, err := t.reads.Table.QueryEvents(ctx, memory.QueryOptions{
		PartitionIDs: t.ids,
		EventTypes:   args.Types,
		StartTime:    args.Since,
		EndTime:      args.Until,
		Limit:        limit + 1,
		OrderBy:      memory.QueryOrderByDesc,
	})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", RecentToolName, err)
	}
	answer := recentAnswer{
		Tool:       RecentToolName,
		Partitions: t.names,
		Since:      args.Since,
		Until:      args.Until,
		Truncated:  len(refs) > limit,
		Items:      make([]eventItem, 0, limit),
	}
	if len(refs) > limit {
		refs = refs[:limit]
	}
	for _, ref := range refs {
		if !t.admits(ref.PartitionID) {
			continue
		}
		answer.Items = append(answer.Items, itemOf(ref))
	}
	answer.Count = len(answer.Items)
	return encode(RecentToolName, answer)
}
