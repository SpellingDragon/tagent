package assembly

import (
	"context"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/memory/replay"
)

// TestProjectionRendersTheStatedMessageRole pins the one rendering rule both
// assembly paths share: a fact states its own message role and the projection
// presents it as stated — the frozen type fills the role only beside a message
// that carries none. A delegated instruction is a system message its producer
// stated, so it must present as one however the request was assembled.
func TestProjectionRendersTheStatedMessageRole(t *testing.T) {
	projection := replay.NewReplayProjection()
	projection.Append(context.Background(), replay.WithMessageRole(replay.ProjectionEntry{
		Key:     "1-instruction",
		Type:    event.TypeExternalInput,
		Content: "review the change",
	}, string(model.RoleSystem)))
	projection.Append(context.Background(), replay.ProjectionEntry{
		Key:     "2-task",
		Type:    event.TypeExternalInput,
		Content: "the change under review",
	})
	messages, err := projectedSegment(context.Background(), nil, projection, FoldWindow{}, 2)
	if err != nil {
		t.Fatalf("project the entries: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("projected messages = %d, want the two entries", len(messages))
	}
	if messages[0].Role != model.RoleSystem {
		t.Fatalf("instruction role = %q, want the role its own message stated", messages[0].Role)
	}
	if messages[1].Role != model.RoleUser {
		t.Fatalf("task role = %q, want the frozen type's role beside a message stating none", messages[1].Role)
	}
}
