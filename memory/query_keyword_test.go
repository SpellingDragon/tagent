package memory

import "testing"

// TestMatchesKeyword locks the term-split ANY-match semantics
// (2026-08-26 wechat-bot incident: "最近对话 任务 讨论" returned zero on a
// literal whole-string match while "任务" alone had hits).
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

// TestQueryEvents_MultiTermKeyword is the store-level regression: a
// space-separated keyword list must hit events containing any term.
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
