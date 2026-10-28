// judgment_test 钉住 冥想域自己的判断：新鲜度策略只把宿主可见的来源戳读成新素材，
// 投递策略只放行宿主可见的输出，记录性血统在两个判断之外保持恒等派生。
// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
package meditation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory"
)

// TestNoveltyPolicyJudgesFreshnessInTheDomain pins the domain's freshness vocabulary: only awaited-interaction stamps open the gate.
// - user, task, reincarnation and system_alert traffic is new material
// - reflection, consolidation, receipt, unknown, empty and absent stamps are not
func TestNoveltyPolicyJudgesFreshnessInTheDomain(t *testing.T) {
	cases := []struct {
		name   string
		stamp  string
		absent bool
		want   bool
	}{
		{name: "user traffic", stamp: "user", want: true},
		{name: "task traffic", stamp: "task", want: true},
		{name: "reincarnation traffic", stamp: "reincarnation", want: true},
		{name: "system alert traffic", stamp: "system_alert", want: true},
		{name: "reflection traffic", stamp: LineageReflection, want: false},
		{name: "consolidation wake-up", stamp: "consolidation_hint", want: false},
		{name: "delivery receipt", stamp: "delivery_receipt", want: false},
		{name: "unknown stamp", stamp: "http", want: false},
		{name: "empty stamp", stamp: "", want: false},
		{name: "absent stamp", absent: true, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			meta := map[string]string{}
			if !tc.absent {
				meta[event.MetaKeyTriggerSource] = tc.stamp
			}
			if got := (NoveltyPolicy{}).Novel(event.Event{Meta: meta}); got != tc.want {
				t.Fatalf("novel(%q) = %v, want %v", tc.stamp, got, tc.want)
			}
		})
	}
}

// TestDeliveryPolicyJudgesDeliveryInTheDomain pins the domain's delivery vocabulary: only host-visible traffic leaves through the port.
// - the four awaited-interaction stamps and the reflection input are deliverable
// - consolidation, receipt, unknown, empty and absent stamps are withheld
func TestDeliveryPolicyJudgesDeliveryInTheDomain(t *testing.T) {
	cases := []struct {
		name   string
		stamp  string
		absent bool
		want   bool
	}{
		{name: "user traffic", stamp: "user", want: true},
		{name: "task traffic", stamp: "task", want: true},
		{name: "reincarnation traffic", stamp: "reincarnation", want: true},
		{name: "system alert traffic", stamp: "system_alert", want: true},
		{name: "reflection input", stamp: LineageReflection, want: true},
		{name: "consolidation wake-up", stamp: "consolidation_hint", want: false},
		{name: "delivery receipt", stamp: "delivery_receipt", want: false},
		{name: "unknown stamp", stamp: "http", want: false},
		{name: "empty stamp", stamp: "", want: false},
		{name: "absent stamp", absent: true, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			meta := map[string]string{}
			if !tc.absent {
				meta[event.MetaKeyTriggerSource] = tc.stamp
			}
			if got := (DeliveryPolicy{}).Deliverable(event.Event{Meta: meta}); got != tc.want {
				t.Fatalf("deliverable(%q) = %v, want %v", tc.stamp, got, tc.want)
			}
		})
	}
}

// TestLineageRecordingsMirrorTheDomainJudgments pins the recording readings as the domain judgments' own derivations.
// - SelfManagedLineage is the negation of the freshness judgment for every stamp
// - DeliverableLineage answers what the delivery judgment answers
// - the reflection input is recorded as self-managed beside being deliverable
func TestLineageRecordingsMirrorTheDomainJudgments(t *testing.T) {
	stamps := []string{
		"user", "task", "reincarnation", "system_alert",
		LineageReflection, "consolidation_hint", "delivery_receipt", "http", "",
	}
	for _, stamp := range stamps {
		evt := event.Event{Meta: map[string]string{event.MetaKeyTriggerSource: stamp}}
		novel := (NoveltyPolicy{}).Novel(evt)
		deliverable := (DeliveryPolicy{}).Deliverable(evt)
		if got := SelfManagedLineage(stamp); got != !novel {
			t.Fatalf("SelfManagedLineage(%q) = %v, want the negation of the freshness judgment %v", stamp, got, novel)
		}
		if got := DeliverableLineage(stamp); got != deliverable {
			t.Fatalf("DeliverableLineage(%q) = %v, want the delivery judgment %v", stamp, got, deliverable)
		}
	}
	if !SelfManagedLineage(LineageReflection) || !DeliverableLineage(LineageReflection) {
		t.Fatalf("reflection lineage = (%v, %v), want recorded self-managed beside deliverable",
			SelfManagedLineage(LineageReflection), DeliverableLineage(LineageReflection))
	}
}

// TestObserveJudgesNoveltyFromTheEventItself pins each hydrated fact read through the domain policy rather than a stamp list.
// - the first fact the policy reads as new material is the pass verdict
// - unstamped and reflection traffic is recorded self-managed and never counted fresh
// - a pass of internal and unstamped traffic alone holds the gate closed
func TestObserveJudgesNoveltyFromTheEventItself(t *testing.T) {
	ctx := context.Background()
	since := time.Unix(0, 0)

	t.Run("the first host-facing fact is the newest new material", func(t *testing.T) {
		scan, err := observe(ctx, judgmentScope(), &judgmentFacts{held: []event.Event{
			judgmentFact("internal", "", since.Add(3*time.Minute)),
			judgmentFact("reflection", LineageReflection, since.Add(2*time.Minute)),
			judgmentFact("host", "user", since.Add(time.Minute)),
		}}, ObservationScanRequest{Since: since})
		if err != nil {
			t.Fatalf("observe the face: %v", err)
		}
		if !scan.Verdict.Novelty || scan.Verdict.Recent == nil || scan.Verdict.Recent.Identity.Key != "host" {
			t.Fatalf("verdict = %+v, want the oldest read fact as the newest new material", scan.Verdict)
		}
		if scan.Counts.HydratedTotal != 3 || scan.Counts.UnknownLineage != 1 || scan.Counts.Foreign != 0 || scan.Counts.Unreadable != 0 {
			t.Fatalf("counts = %+v, want three hydrated facts and one unstamped", scan.Counts)
		}
		if got := scan.Partitions[0]; got.External != 1 || got.SelfManaged != 2 {
			t.Fatalf("partition tally = %+v, want one external beside two self-managed", got)
		}
		if recording := scan.Facts[0].Attribution; !recording.SelfManaged || recording.Lineage != "" {
			t.Fatalf("unstamped recording = %+v, want self-managed beside an empty lineage", recording)
		}
		if recording := scan.Facts[1].Attribution; !recording.SelfManaged || recording.Lineage != LineageReflection {
			t.Fatalf("reflection recording = %+v, want self-managed beside the reflection stamp", recording)
		}
	})

	t.Run("internal and unstamped traffic holds the gate closed", func(t *testing.T) {
		scan, err := observe(ctx, judgmentScope(), &judgmentFacts{held: []event.Event{
			judgmentFact("unstamped", "", since.Add(4*time.Minute)),
			judgmentFact("reflection", LineageReflection, since.Add(3*time.Minute)),
			judgmentFact("hint", "consolidation_hint", since.Add(2*time.Minute)),
			judgmentFact("receipt", "delivery_receipt", since.Add(time.Minute)),
		}}, ObservationScanRequest{Since: since})
		if err != nil {
			t.Fatalf("observe the face: %v", err)
		}
		if scan.Verdict.Novelty || scan.Verdict.Recent != nil {
			t.Fatalf("verdict = %+v, want no new material without host-facing traffic", scan.Verdict)
		}
		if got := scan.Partitions[0]; got.External != 0 || got.SelfManaged != 4 {
			t.Fatalf("partition tally = %+v, want every hydrated fact self-managed", got)
		}
		if scan.Counts.HydratedTotal != 4 || scan.Counts.UnknownLineage != 1 {
			t.Fatalf("counts = %+v, want four hydrated facts and one unstamped", scan.Counts)
		}
	})
}

// TestObserveRefusesAnUnwiredFace pins the zero behavior of an unwired read face: no pass and the refusal by name.
func TestObserveRefusesAnUnwiredFace(t *testing.T) {
	scan, err := observe(context.Background(), judgmentScope(), nil, ObservationScanRequest{})
	if !errors.Is(err, ErrObservedFaceUnwired) {
		t.Fatalf("observe without a face = %v, want %v", err, ErrObservedFaceUnwired)
	}
	if scan != nil {
		t.Fatalf("scan = %+v, want no pass beside the refusal", scan)
	}
}

// judgmentScope is the one-partition observation face the judgment tests read.
func judgmentScope() ReflectionScope {
	return ReflectionScope{
		Own:        "meditator",
		Names:      []string{"tagent"},
		Partitions: []int{memory.PartitionIDFromName("tagent")},
	}
}

// judgmentFact builds one committed fact carrying the trigger-source stamp it is
// given, or none when the stamp is empty.
func judgmentFact(key event.EventID, stamp string, at time.Time) event.Event {
	meta := map[string]string{}
	if stamp != "" {
		meta[event.MetaKeyTriggerSource] = stamp
	}
	return event.Event{Key: key, Type: "answer", Timestamp: at, Meta: meta}
}

// judgmentFacts is the fact-chain face of the judgment tests: it answers the held
// facts in declaration order and refuses an identity it does not hold.
type judgmentFacts struct {
	held []event.Event
}

// QueryEvents answers one reference per held fact, in declaration order.
func (f *judgmentFacts) QueryEvents(context.Context, memory.QueryOptions) ([]memory.EventReference, error) {
	refs := make([]memory.EventReference, 0, len(f.held))
	for _, fact := range f.held {
		refs = append(refs, memory.EventReference{
			Key:         fact.Key,
			PartitionID: memory.PartitionIDFromName("tagent"),
			Timestamp:   fact.Timestamp.UnixMilli(),
		})
	}
	return refs, nil
}

// CommittedFact answers the held fact the identity names.
func (f *judgmentFacts) CommittedFact(_ context.Context, id event.EventID) (event.Event, error) {
	for _, fact := range f.held {
		if fact.Key == id {
			return fact, nil
		}
	}
	return event.Event{}, errors.New("judgment test: no committed fact behind the reference")
}
