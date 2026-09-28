package memory

import (
	"sort"
	"testing"

	"github.com/SpellingDragon/tagent/event"
	"github.com/stretchr/testify/require"
)

// TestMatchesKeyword 钉住 locks the term-split ANY-match semantics
//
// 契约: docs/wiki/memory/memory-architecture.md#typed-errors
func TestMatchesKeyword(t *testing.T) {
	cases := []struct {
		name    string
		text    string
		keyword string
		want    bool
	}{
		{"single term hit", "部署完成，健康检查通过", "部署", true},
		{"single term miss", "部署完成", "会议", false},
		{"single term case-insensitive", "Deploy OK", "deploy", true},
		{"multi-term any hit (space)", "我们讨论了任务分配", "最近对话 任务 讨论", true},
		{"multi-term all miss", "今天天气不错", "部署 会议", false},
		{"multi-term cjk punct split", "任务已下发", "回顾：任务，讨论", true},
		{"identifier stays one term", "session=tagent-1787749144", "tagent-1787749144", true},
		{"underscore identifier intact", "api_key rotated", "api_key", true},
		{"empty keyword matches all", "anything", "", true},
		{"full sentence still works when literal", "回顾之前所有对话历史", "回顾之前所有对话历史", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchesKeyword(tc.text, tc.keyword); got != tc.want {
				t.Errorf("matchesKeyword(%q, %q) = %v, want %v", tc.text, tc.keyword, got, tc.want)
			}
		})
	}
}

// TestQueryEvents_MultiTermKeyword 钉住 空格分隔的多关键词按"任一词命中"召回，而不是要求整串匹配。
func TestQueryEvents_MultiTermKeyword(t *testing.T) {
	s := NewInMemoryStore()
	if err := s.StoreEvent(144, FullEvent{
		EventKey: 100, PartitionID: 144, EventType: "external_input",
		EventSummary: "我们讨论了任务分配", Timestamp: 1710000000000,
	}); err != nil {
		t.Fatal(err)
	}

	evts, err := s.QueryEvents(QueryOptions{
		PartitionIDs: []int{144}, Keyword: "最近对话 任务 讨论",
		OrderBy: "timestamp_desc",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(evts) != 1 {
		t.Fatalf("space-separated keyword list must hit via ANY term, got %d events", len(evts))
	}
}

// TestQueryEvents_MinEventKey 钉住 写入序键下界在两种存储上都按 EventKey 生效（严格大于），故晚到但键更小的记录不会漏切。
func TestQueryEvents_MinEventKey(t *testing.T) {
	const pid = 7
	base := int64(1_700_000_000_000)
	keys := []int64{
		NewSnowflakeEventKey(pid, base),
		NewSnowflakeEventKey(pid, base+5_000),
		NewSnowflakeEventKey(pid, base+10_000),
		NewSnowflakeEventKey(pid, base+15_000),
	}

	stores := map[string]func(t *testing.T) MemoryStore{
		"InMemoryStore": func(t *testing.T) MemoryStore { return NewInMemoryStore() },
		"FileSegmentStore": func(t *testing.T) MemoryStore {
			s, err := NewFileSegmentStore(newMockKV(), nil, ":memory:", 100)
			require.NoError(t, err)
			return s
		},
	}

	keySet := func(refs []EventReference) []int64 {
		out := make([]int64, 0, len(refs))
		for _, r := range refs {
			out = append(out, r.EventKey)
		}
		sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
		return out
	}

	for name, mk := range stores {
		t.Run(name, func(t *testing.T) {
			store := mk(t)
			for i, k := range keys {
				ts := base + int64(i)*1_000
				if i == len(keys)-1 {
					ts = base - 60_000
				}
				err := store.StoreEvent(k, FullEvent{
					EventType:    event.TypeExternalInput,
					EventSummary: "evt",
					Timestamp:    ts,
					Content:      "content",
				})
				require.NoError(t, err)
			}

			refs, err := store.QueryEvents(QueryOptions{PartitionID: pid, MinEventKey: keys[1]})
			require.NoError(t, err)
			require.Equal(t, keys[2:], keySet(refs), "MinEventKey must cut on EventKey (strictly greater), keeping the old-Timestamp straggler")

			refs, err = store.QueryEvents(QueryOptions{PartitionID: pid})
			require.NoError(t, err)
			require.Equal(t, keys, keySet(refs), "MinEventKey=0 must not filter")

			refs, err = store.QueryEvents(QueryOptions{PartitionID: pid, StartTime: base})
			require.NoError(t, err)
			require.Equal(t, keys[:3], keySet(refs), "semantic-time filter drops the straggler (why tails must not use StartTime)")
		})
	}
}
