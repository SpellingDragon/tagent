package capabilities

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/replay"
	"github.com/SpellingDragon/tagent/tool/recall"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// recallSurface is the four-tool surface one deployment assembles over a real
// store chain: the chain the tools read and the tools keyed by declared name.
type recallSurface struct {
	chain *realStoreChain
	tools map[string]trpctool.CallableTool
}

// newRecallSurface commits the corpus into a real file tier, lands every event
// into the observation table, wires the four tools over it inside the alpha
// partition, and records the two parent links the causal walk follows. The
// embedder is the capability handed to the tools, while the retrieval chain
// keeps its own hash embedder, so a call refused for want of an embedding
// capability is refused by the tool and not by a chain that cannot vectorize.
func newRecallSurface(t *testing.T, wired memory.Embedder) *recallSurface {
	t.Helper()
	chain := newRealStoreChain(t, memory.NewHashEmbedder(0))
	chain.landStore(t)
	relations := replay.NewMemoryRelations()
	if err := relations.SetParent("0a02", "0a01"); err != nil {
		t.Fatalf("SetParent(0a02 -> 0a01): %v", err)
	}
	if err := relations.SetParent("0a03", "0a02"); err != nil {
		t.Fatalf("SetParent(0a03 -> 0a02): %v", err)
	}
	tools := make(map[string]trpctool.CallableTool, 4)
	for _, tool := range recall.New(recall.Reads{
		Searcher:  chain.searcher,
		Embedder:  wired,
		Table:     chain.source,
		Originals: chain.scoped,
		Trace:     relations,
	}, []string{realAlphaPartition}) {
		tools[tool.Declaration().Name] = tool
	}
	return &recallSurface{chain: chain, tools: tools}
}

// originalOf returns the original text the corpus committed under key.
func (c *realStoreChain) originalOf(key event.EventID) string {
	for _, ev := range c.events {
		if ev.key == key {
			return ev.original
		}
	}
	return ""
}

// callRecallTool runs one call of tool with the given argument text and returns
// the decoded answer, or the error the call was refused with.
func callRecallTool(t *testing.T, tool trpctool.CallableTool, args string) (map[string]any, error) {
	t.Helper()
	name := tool.Declaration().Name
	answer, err := tool.Call(context.Background(), []byte(args))
	if err != nil {
		return nil, err
	}
	text, ok := answer.(string)
	if !ok {
		t.Fatalf("%s answered %T, want the JSON text of its answer", name, answer)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		t.Fatalf("%s answered unparsable JSON %q: %v", name, text, err)
	}
	return decoded, nil
}

// answeredKeysOf returns the event keys the page of one answer carries, in order.
func answeredKeysOf(t *testing.T, answer map[string]any) []event.EventID {
	t.Helper()
	items, ok := answer["items"].([]any)
	if !ok {
		t.Fatalf("answer carries no items page: %v", answer)
	}
	keys := make([]event.EventID, 0, len(items))
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("answer item = %v, want one event rendering", item)
		}
		key, ok := entry["key"].(string)
		if !ok {
			t.Fatalf("answer item carries no key: %v", entry)
		}
		keys = append(keys, event.EventID(key))
	}
	return keys
}

// answeredField reads one field of an answer as the type the answer declares.
func answeredField[T any](t *testing.T, answer map[string]any, field string) T {
	t.Helper()
	value, ok := answer[field].(T)
	if !ok {
		t.Fatalf("answer field %q = %v, want a %T", field, answer[field], value)
	}
	return value
}

// TestRecallToolSurface drives the four recall tools over a real file tier:
// - normal: the retrieval tool answers the landed hits inside its partition and never a hit outside it
// - normal: the exact read serves the original back in bounded segments and resumes where it stopped
// - normal: the exact read names a miss and an event outside the partition, and brings the parent summary when asked
// - normal: the recent page answers newest first and reports the page it could not carry
// - normal: the causal walk follows the parent links to a root, stops at the step bound, and refuses a start nothing holds
// - failure: without an embedding capability the retrieval tool is refused by name while the page tool still answers
// - failure: a surface wiring nothing refuses all four tools by name instead of answering an empty page
//
// 契约: docs/wiki/tool/tool-architecture.md#recall-subtools
func TestRecallToolSurface(t *testing.T) {
	alphaPartition := memory.PartitionIDFromName(realAlphaPartition)

	t.Run("normal", func(t *testing.T) {
		t.Run("the retrieval tool answers the landed hits inside its partition", func(t *testing.T) {
			surface := newRecallSurface(t, memory.NewHashEmbedder(0))
			answer, err := callRecallTool(t, surface.tools[recall.QueryToolName], fmt.Sprintf(`{"text":%q,"limit":5}`, realRecallText))
			if err != nil {
				t.Fatalf("recall_query(%q): %v", realRecallText, err)
			}
			want := []event.EventID{"0a01", "0a02", "0a03"}
			if got := answeredKeysOf(t, answer); !reflect.DeepEqual(got, want) {
				t.Fatalf("recall_query keys = %v, want %v: the vector leg recalls the context first, then its weaker neighbours, and 0b05 lies outside the granted partition", got, want)
			}
			if got := answeredField[float64](t, answer, "count"); int(got) != len(want) {
				t.Fatalf("recall_query count = %v, want %d", got, len(want))
			}
			if got := answeredField[float64](t, answer, "dropped"); got != 0 {
				t.Fatalf("recall_query dropped = %v, want 0: every hit the retrieval answered lies inside the partition scope it ran in", got)
			}
			partitions, ok := answer["partitions"].([]any)
			if !ok || len(partitions) != 1 || partitions[0] != realAlphaPartition {
				t.Fatalf("recall_query partitions = %v, want [%s]: the answer reports the scope it ran in", answer["partitions"], realAlphaPartition)
			}
			for _, item := range answer["items"].([]any) {
				if got := int(item.(map[string]any)["partition"].(float64)); got != alphaPartition {
					t.Fatalf("recall_query item partition = %d, want %d: no item leaves the granted set", got, alphaPartition)
				}
			}
		})

		t.Run("the exact read serves the original back in bounded segments", func(t *testing.T) {
			surface := newRecallSurface(t, memory.NewHashEmbedder(0))
			tool := surface.tools[recall.GetToolName]
			var walked []byte
			position := 0
			for step := 0; ; step++ {
				if step > 64 {
					t.Fatalf("the segmented read never reached EOF: it walked %d bytes of a %d byte original", len(walked), len(surface.chain.originalOf("0a01")))
				}
				answer, err := callRecallTool(t, tool, fmt.Sprintf(`{"key":"[evt_0a01|%s]","offset":%d,"max_bytes":12}`, event.TypeExternalInput, position))
				if err != nil {
					t.Fatalf("recall_get(0a01 at %d): %v", position, err)
				}
				if !answeredField[bool](t, answer, "found") {
					t.Fatalf("recall_get(0a01 at %d) answered found = false, reason %v: the original is committed and inside the scope", position, answer["reason"])
				}
				walked = append(walked, answeredField[string](t, answer, "body")...)
				if eof, _ := answer["eof"].(bool); eof {
					break
				}
				position = int(answeredField[float64](t, answer, "next"))
			}
			if got, want := string(walked), surface.chain.originalOf("0a01"); got != want {
				t.Fatalf("the segmented read walked %q, want %q byte for byte", got, want)
			}
		})

		t.Run("the exact read names a miss and an event outside the partition", func(t *testing.T) {
			surface := newRecallSurface(t, memory.NewHashEmbedder(0))
			tool := surface.tools[recall.GetToolName]
			miss, err := callRecallTool(t, tool, `{"key":"9f9f"}`)
			if err != nil {
				t.Fatalf("recall_get(9f9f): %v", err)
			}
			if found := answeredField[bool](t, miss, "found"); found {
				t.Fatalf("recall_get(9f9f) answered found = true: the table holds no such event")
			}
			if reason := answeredField[string](t, miss, "reason"); reason != "missing" {
				t.Fatalf("recall_get(9f9f) reason = %q, want missing", reason)
			}
			outside, err := callRecallTool(t, tool, `{"key":"0b05"}`)
			if err != nil {
				t.Fatalf("recall_get(0b05): %v", err)
			}
			if reason := answeredField[string](t, outside, "reason"); reason != "out_of_scope" {
				t.Fatalf("recall_get(0b05) reason = %q, want out_of_scope: the event is landed under another partition", reason)
			}
			child, err := callRecallTool(t, tool, `{"key":"0a02","include_parent":true,"max_bytes":4096}`)
			if err != nil {
				t.Fatalf("recall_get(0a02 with parent): %v", err)
			}
			parent, ok := child["parent"].(map[string]any)
			if !ok {
				t.Fatalf("recall_get(0a02) answered no parent: %v", child)
			}
			if got := parent["key"]; got != "0a01" {
				t.Fatalf("recall_get(0a02) parent = %v, want 0a01: the walk reads the link the relation ledger holds", got)
			}
		})

		t.Run("the recent page answers newest first and reports the page it could not carry", func(t *testing.T) {
			surface := newRecallSurface(t, memory.NewHashEmbedder(0))
			tool := surface.tools[recall.RecentToolName]
			page, err := callRecallTool(t, tool, `{"limit":2}`)
			if err != nil {
				t.Fatalf("recall_recent(limit 2): %v", err)
			}
			if got, want := answeredKeysOf(t, page), []event.EventID{"0a04", "0a03"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("recall_recent keys = %v, want %v: the page is newest first and carries no event of another partition", got, want)
			}
			if truncated := answeredField[bool](t, page, "truncated"); !truncated {
				t.Fatalf("recall_recent(limit 2) truncated = false, want true: two more landing events stand behind the page")
			}
			full, err := callRecallTool(t, tool, `{"limit":10}`)
			if err != nil {
				t.Fatalf("recall_recent(limit 10): %v", err)
			}
			want := []event.EventID{"0a04", "0a03", "0a02", "0a01"}
			if got := answeredKeysOf(t, full); !reflect.DeepEqual(got, want) {
				t.Fatalf("recall_recent keys = %v, want %v", got, want)
			}
			if truncated := answeredField[bool](t, full, "truncated"); truncated {
				t.Fatalf("recall_recent(limit 10) truncated = true, want false: the page carried every landing event of its partition")
			}
		})

		t.Run("the causal walk follows the parent links to a root", func(t *testing.T) {
			surface := newRecallSurface(t, memory.NewHashEmbedder(0))
			tool := surface.tools[recall.TraceToolName]
			chain, err := callRecallTool(t, tool, `{"key":"0a03"}`)
			if err != nil {
				t.Fatalf("recall_trace(0a03): %v", err)
			}
			want := []event.EventID{"0a03", "0a02", "0a01"}
			if got := answeredKeysOf(t, chain); !reflect.DeepEqual(got, want) {
				t.Fatalf("recall_trace keys = %v, want %v: the walk starts at the named key and follows the parent links back", got, want)
			}
			if complete := answeredField[bool](t, chain, "complete"); !complete {
				t.Fatalf("recall_trace(0a03) complete = false, want true: the walk reached an event with no parent link")
			}
			if capped := answeredField[bool](t, chain, "capped"); capped {
				t.Fatalf("recall_trace(0a03) capped = true, want false: three steps fit inside the default bound")
			}
			capped, err := callRecallTool(t, tool, `{"key":"0a03","max_steps":1}`)
			if err != nil {
				t.Fatalf("recall_trace(0a03 at one step): %v", err)
			}
			if got := answeredField[bool](t, capped, "capped"); !got {
				t.Fatalf("recall_trace(0a03 at one step) capped = false, want true")
			}
			if reason := answeredField[string](t, capped, "reason"); reason != "limit" {
				t.Fatalf("recall_trace(0a03 at one step) reason = %q, want limit", reason)
			}
			if got := answeredKeysOf(t, capped); len(got) != 1 || got[0] != "0a03" {
				t.Fatalf("recall_trace(0a03 at one step) keys = %v, want [0a03]: the bound cuts the walk, it does not widen it", got)
			}
			if _, err := callRecallTool(t, tool, `{"key":"9f9f"}`); !errors.Is(err, recall.ErrRecallMissing) {
				t.Fatalf("recall_trace(9f9f) error = %v, want the named refusal that the read face holds no such event", err)
			}
		})
	})

	t.Run("failure", func(t *testing.T) {
		t.Run("without an embedding capability the retrieval tool is refused by name", func(t *testing.T) {
			surface := newRecallSurface(t, nil)
			if _, err := callRecallTool(t, surface.tools[recall.QueryToolName], fmt.Sprintf(`{"text":%q}`, realRecallText)); !errors.Is(err, recall.ErrRecallUnwired) {
				t.Fatalf("recall_query without an embedder = %v, want the named refusal that the deployment wires no embedding capability", err)
			}
			page, err := callRecallTool(t, surface.tools[recall.RecentToolName], `{"limit":10}`)
			if err != nil {
				t.Fatalf("recall_recent without an embedder: %v", err)
			}
			if got := answeredKeysOf(t, page); len(got) != 4 {
				t.Fatalf("recall_recent without an embedder answered %v, want the four landing events: the page reads the table and needs no embedding capability", got)
			}
		})

		t.Run("a surface wiring nothing refuses all four tools by name", func(t *testing.T) {
			for _, tool := range recall.New(recall.Reads{}, nil) {
				name := tool.Declaration().Name
				answer, err := tool.Call(context.Background(), []byte(`{}`))
				if !errors.Is(err, recall.ErrRecallUnwired) {
					t.Fatalf("%s on an unwired surface answered %v with error %v, want the named refusal instead of an empty page", name, answer, err)
				}
			}
		})
	})
}
