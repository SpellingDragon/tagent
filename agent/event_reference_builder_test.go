package agent

import (
	"testing"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

func TestBuildEventReference_Success(t *testing.T) {
	evt := event.New("", "test-agent")
	evt.StateDelta = map[string][]byte{
		"event_key":    []byte("123456789"),
		"partition_id": []byte("42"),
		"event_type":   []byte("external_input"),
	}
	evt.Timestamp = time.UnixMilli(1000)
	evt.Response = &model.Response{
		Choices: []model.Choice{{
			Message: model.Message{Content: "hello world"},
		}},
	}

	ref, ok := BuildEventReference(evt)
	if !ok {
		t.Fatal("expected BuildEventReference to succeed")
	}
	if ref.EventKey != 123456789 {
		t.Fatalf("expected EventKey=123456789, got %d", ref.EventKey)
	}
	if ref.PartitionID != 42 {
		t.Fatalf("expected PartitionID=42, got %d", ref.PartitionID)
	}
	if ref.EventType != "external_input" {
		t.Fatalf("expected EventType=external_input, got %s", ref.EventType)
	}
	if ref.EventSummary != "hello world" {
		t.Fatalf("expected EventSummary=hello world, got %s", ref.EventSummary)
	}
	if ref.Timestamp != 1000 {
		t.Fatalf("expected Timestamp=1000, got %d", ref.Timestamp)
	}
}

func TestBuildEventReference_MissingKey(t *testing.T) {
	evt := event.New("", "test-agent")
	evt.StateDelta = map[string][]byte{
		"partition_id": []byte("42"),
	}
	_, ok := BuildEventReference(evt)
	if ok {
		t.Fatal("expected BuildEventReference to fail when event_key missing")
	}
}

func TestBuildEventReference_NilEvent(t *testing.T) {
	_, ok := BuildEventReference(nil)
	if ok {
		t.Fatal("expected BuildEventReference to fail for nil event")
	}
}
