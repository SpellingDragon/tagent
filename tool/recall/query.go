// 契约: docs/wiki/tool/tool-architecture.md#recall-subtools
//
// The retrieval tool of the recall surface: one query runs through the
// retrieval port inside the granted partitions, and every hit is answered as
// the reference of the event it landed as, never as a copy of its text.
package recall

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/SpellingDragon/tagent/memory/search"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// queryArgs is the declared argument shape of one retrieval call: the content
// to retrieve for, the type and time filters, and the page it asks for.
type queryArgs struct {
	// Text is the retrieval content of the query.
	Text string `json:"text"`
	// Types restricts the query to the given frozen event types.
	Types []string `json:"types"`
	// Since is the inclusive lower time bound in Unix milliseconds.
	Since int64 `json:"since"`
	// Until is the inclusive upper time bound in Unix milliseconds.
	Until int64 `json:"until"`
	// Limit caps how many references one page carries.
	Limit int `json:"limit"`
	// Cursor is the continuation token a previous page answered with.
	Cursor string `json:"cursor"`
}

// queryAnswer is the answer of one retrieval call: the page the query answered
// with, the scope it ran in and how many hits the table could not render.
type queryAnswer struct {
	// Tool is the name of the tool that answered.
	Tool string `json:"tool"`
	// Partitions is the granted partition set the query ran in.
	Partitions []string `json:"partitions"`
	// Since is the lower time bound the query ran under, in Unix milliseconds.
	Since int64 `json:"since,omitempty"`
	// Until is the upper time bound the query ran under, in Unix milliseconds.
	Until int64 `json:"until,omitempty"`
	// Count is how many items the page carries.
	Count int `json:"count"`
	// Items is the reference set of the page, best hit first.
	Items []eventItem `json:"items"`
	// Dropped is how many hits the table held no reference for, or held
	// outside the granted partitions, and therefore did not render.
	Dropped int `json:"dropped"`
	// NextCursor is the continuation token of the page after this one.
	NextCursor string `json:"next_cursor,omitempty"`
}

// queryTool is the model-facing retrieval tool.
type queryTool struct{ base }

// Declaration reports the retrieval tool's declaration.
func (t *queryTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{
		Name: QueryToolName,
		Description: "按语义检索已落地的记忆事件：手里只有模糊线索（“上次聊到部署那次”）时用它。" +
			"结果只含引用（事件键、类型、摘要、时间），原文用 recall_get 按事件键读取；" +
			"时间范围用 Unix 毫秒；可读分区由宿主授予，不由参数指定。",
		InputSchema: &trpctool.Schema{
			Type: "object",
			Properties: map[string]*trpctool.Schema{
				"text": {
					Type:        "string",
					Description: "要检索的线索或问题，不能为空",
				},
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
				"cursor": {
					Type:        "string",
					Description: "上一页返回的续页标记；首页省略",
				},
			},
			Required: []string{"text"},
		},
	}
}

// Call runs one retrieval through the port and answers the page it hit. A
// deployment wiring no retrieval port or no embedding capability is refused by
// name instead of being answered through a narrower rule the caller did not
// ask for.
func (t *queryTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	if t.reads.Embedder == nil || t.reads.Searcher == nil || t.reads.Table == nil {
		return nil, unwired(QueryToolName)
	}
	var args queryArgs
	if err := json.Unmarshal(jsonArgs, &args); err != nil {
		return nil, fmt.Errorf("%s: malformed arguments: %w", QueryToolName, err)
	}
	query := t.query(args)
	if err := query.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", QueryToolName, err)
	}
	result, err := t.reads.Searcher.Search(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", QueryToolName, err)
	}
	answer := queryAnswer{
		Tool:       QueryToolName,
		Partitions: t.names,
		Since:      args.Since,
		Until:      args.Until,
		Items:      make([]eventItem, 0, len(result.Refs)),
		NextCursor: string(result.NextCursor),
	}
	for _, ref := range result.Refs {
		landed, held := t.reads.Table.Event(ref.Key)
		if !held || !t.admits(landed.PartitionID) {
			answer.Dropped++
			continue
		}
		answer.Items = append(answer.Items, itemOf(landed))
	}
	answer.Count = len(answer.Items)
	return encode(QueryToolName, answer)
}

// query maps one call onto the retrieval request it runs: the retrieval content,
// the granted partition set, the filters and the page, under the fusion rule
// that answers a query through both legs.
func (t *queryTool) query(args queryArgs) search.Query {
	query := search.Query{
		Text:       strings.TrimSpace(args.Text),
		Strategy:   search.StrategyHybrid,
		Cursor:     search.Cursor(args.Cursor),
		Partitions: t.names,
		Types:      args.Types,
		Limit:      clampPage(args.Limit),
	}
	if args.Since > 0 {
		query.Since = time.UnixMilli(args.Since)
	}
	if args.Until > 0 {
		query.Until = time.UnixMilli(args.Until)
	}
	return query
}
