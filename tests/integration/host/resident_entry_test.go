package host

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
)

// TestRunInputsUseResidentConsumer proves that the public Run entries share the
// address Consumer instead of directly driving a second loop beside it.
func TestRunInputsUseResidentConsumer(t *testing.T) {
	for _, entry := range []string{"invoke", "inject_event"} {
		t.Run(entry, func(t *testing.T) {
			scene := newHostDriveScene(t)
			ctx, cancel := context.WithTimeout(context.Background(), hostDriveWindow)
			defer cancel()
			body := "the public Run entry " + entry
			var err error
			if entry == "invoke" {
				_, err = scene.host.Run().Invoke(ctx, assembly.InvokeRequest{
					InvocationID: event.InvocationID("inv-run-" + entry),
					Address:      scene.peer,
					Input:        []model.Message{{Role: model.RoleUser, Content: body}},
				})
			} else {
				_, _, err = scene.host.Run().InjectEvent(ctx, event.Event{
					Type:    event.TypeExternalInput,
					Message: model.Message{Role: model.RoleUser, Content: body},
					Owner:   scene.peer,
					Source:  scene.peer,
					Target:  scene.peer,
				}, event.InvocationID("inv-run-"+entry))
			}
			if err != nil {
				t.Fatalf("public Run %s returned %v", entry, err)
			}
			input := publicInputByBody(t, scene, body)
			residentOutputAfter(t, scene, input)
		})
	}

	t.Run("empty_invoke_uses_resident_consumer", func(t *testing.T) {
		scene := newHostDriveScene(t)
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		turn, err := scene.host.Run().Invoke(ctx, assembly.InvokeRequest{
			InvocationID: "inv-run-empty",
			Address:      scene.peer,
		})
		if err != nil {
			t.Fatalf("empty Run.Invoke returned %v", err)
		}
		if turn.DecisionCount == 0 {
			t.Fatal("empty Run.Invoke served no model decision")
		}
		var inputs []event.Event
		for _, fact := range scene.host.Run().SessionFacts(scene.peer) {
			if fact.Type == event.TypeExternalInput && fact.InvocationID == "inv-run-empty" {
				inputs = append(inputs, fact)
			}
		}
		if len(inputs) != 1 {
			t.Fatalf("empty invocation facts = %d, want exactly one", len(inputs))
		}
		residentOutputAfter(t, scene, inputs[0])
	})

	t.Run("cancellation_returns_error", func(t *testing.T) {
		scene := newHostDriveScene(t)
		scene.provider.parkModel(hostDrivePeerModel)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		returned := make(chan error, 1)
		go func() {
			_, err := scene.host.Run().Invoke(ctx, assembly.InvokeRequest{
				InvocationID: "inv-run-cancel",
				Address:      scene.peer,
				Input:        []model.Message{{Role: model.RoleUser, Content: "cancel this public Run invocation"}},
			})
			returned <- err
		}()
		scene.provider.awaitParked(t)
		cancel()
		var err error
		select {
		case err = <-returned:
		case <-time.After(hostDriveWindow):
			t.Fatal("cancelled Run.Invoke did not return")
		}
		scene.provider.release()
		if err == nil {
			t.Fatal("cancelled Run.Invoke returned nil, want the cancellation error")
		}
	})

	t.Run("rejected_input_starts_no_driver", func(t *testing.T) {
		scene := newHostDriveScene(t)
		ctx, cancel := context.WithTimeout(context.Background(), hostDriveWindow)
		defer cancel()
		_, err := scene.host.Run().Invoke(ctx, assembly.InvokeRequest{
			Address: scene.peer,
			Input:   []model.Message{{Role: model.RoleUser, Content: "missing invocation identity"}},
		})
		if err == nil {
			t.Fatal("Run.Invoke accepted an input without invocation identity")
		}
		if facts := scene.host.Run().SessionFacts(scene.peer); len(facts) != 0 {
			t.Fatalf("facts after rejected Run.Invoke = %d, want none", len(facts))
		}
	})
}

// publicInputByBody finds one committed input through the public chain face.
func publicInputByBody(t *testing.T, scene *hostDriveScene, body string) event.Event {
	t.Helper()
	var found []event.Event
	for _, fact := range scene.host.Run().SessionFacts(scene.peer) {
		if fact.Type == event.TypeExternalInput && strings.Contains(fact.Message.Content, body) {
			found = append(found, fact)
		}
	}
	if len(found) != 1 {
		t.Fatalf("public inputs carrying %q = %d, want exactly one", body, len(found))
	}
	return found[0]
}
