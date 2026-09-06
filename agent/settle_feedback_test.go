package agent

import (
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/compress"
	"github.com/SpellingDragon/tagent/memory"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// TestOnSettle_WritesDeterministicFeedback (2.3, design-report-closeout):
// a task_settled event persisted through persistBusEvent must auto-bind a
// feedback event with a deterministic verdict — completed→positive,
// failed→negative; suspect/alive-detached write NOTHING (only deterministic
// verdicts, so guardrail is not polluted by ambiguous settles). Anchor: the
// task_settled event itself (settlement record IS the task output; its
// bundle_id stamp makes it attributable).
func TestOnSettle_WritesDeterministicFeedback(t *testing.T) {
	cases := []struct {
		status      string
		wantVerdict string // "" = no feedback expected
	}{
		{"completed", "positive"},
		{"failed", "negative"},
		{"suspect", ""},
		{"alive-detached", ""},
	}
	for _, tc := range cases {
		t.Run(tc.status, func(t *testing.T) {
			store := memory.NewInMemoryStore()
			cm := &ContextManager{
				name:        "tagent",
				memStore:    store,
				projection:  compress.NewSessionProjection(),
				partitionID: 7,
			}
			evt := &AgentEvent{
				Type:      "external_input",
				Source:    SourceTask,
				Timestamp: time.Now(),
				Message:   &model.Message{Role: model.RoleUser, Content: "[task settled] done"},
				Metadata:  map[string]any{"settle_status": tc.status},
			}
			cm.persistBusEvent(evt)

			refs, err := store.QueryEvents(memory.QueryOptions{
				PartitionIDs: []int{7}, EventTypes: []string{"feedback"}, Limit: 10,
			})
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantVerdict == "" {
				if len(refs) != 0 {
					t.Fatalf("status %q must not write feedback, got %d", tc.status, len(refs))
				}
				return
			}
			if len(refs) != 1 {
				t.Fatalf("status %q: expected 1 feedback, got %d", tc.status, len(refs))
			}
			fb, err := store.GetEvent(refs[0].EventKey)
			if err != nil || fb == nil {
				t.Fatal(err)
			}
			if !contains(fb.Content, `"verdict":"`+tc.wantVerdict+`"`) {
				t.Fatalf("verdict missing in content: %s", fb.Content)
			}
			if fb.Metadata["subtype"] != "task_settle" {
				t.Fatalf("subtype = %q", fb.Metadata["subtype"])
			}
		})
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
