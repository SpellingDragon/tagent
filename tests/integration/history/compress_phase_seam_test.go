package history

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/evolution/meditation"
	"github.com/SpellingDragon/tagent/memory"
)

// The plugin phase table and the meditation consumer port meet here: the pipeline the
// plugin surface hands out satisfies the port the tick dispatches to, without either
// package importing the other.
var _ meditation.CompressPhase = plugin.Pipeline{}

// seamCompress is one compress-trigger element of the plugin surface: it records the
// request it was dispatched for beside the refusal it was built with.
type seamCompress struct {
	received *[]event.EventID
	order    *[]string
	err      error
}

// Name reports the stable name of the element.
func (p seamCompress) Name() string { return "compress-seam" }

// Phase reports the one phase the element serves.
func (p seamCompress) Phase() plugin.Phase { return plugin.PhaseCompressTrigger }

// TriggerCompress records the request it was dispatched for and answers the refusal it
// was built with.
func (p seamCompress) TriggerCompress(_ context.Context, request event.CompressRequest) error {
	*p.received = append(*p.received, request.ID)
	if p.order != nil {
		*p.order = append(*p.order, "phase:"+string(request.ID))
	}
	return p.err
}

// TestCompressPhaseSeam pins the plugin face one meditation tick consumes.
// - a pipeline registered for the compress-trigger phase satisfies the meditation port as it stands
// - every request the tick reads is dispatched to the pipeline before the pass it drives commits its product
// - a pipeline whose face refused keeps the request pending instead of counting it compressed
// 契约: docs/FACT-VOCABULARY.md#families
func TestCompressPhaseSeam(t *testing.T) {
	ctx := context.Background()

	t.Run("a pipeline registered for the phase drives the tick of one request", func(t *testing.T) {
		var order []string
		var received []event.EventID
		pipeline, err := plugin.NewPipeline([]plugin.Plugin{seamCompress{received: &received, order: &order}})
		if err != nil {
			t.Fatalf("register the compress-trigger element: %v", err)
		}
		requests := memory.NewCompressRequests()
		var products []meditation.MeditationArtifact
		curation := newCompressionCuration(t,
			meditation.WithCompressRequests(requests),
			meditation.WithMeditationCards(compressionCardSource()),
			meditation.WithCompressPhase(pipeline),
			meditation.WithMeditationProducts(func(_ context.Context, artifact meditation.MeditationArtifact) error {
				order = append(order, "product:"+string(artifact.Key))
				products = append(products, artifact)
				return nil
			}),
		)
		request, filed, err := curation.Judge(ctx, compressionNow, compressionCards())
		if err != nil || !filed {
			t.Fatalf("Judge = (%+v, %v, %v), want the request the over-limit sequence files", request, filed, err)
		}

		tick, err := curation.Tick(ctx, compressionNow)
		if err != nil {
			t.Fatalf("Tick through the pipeline: %v", err)
		}
		if len(tick.Failures) != 0 || len(tick.Read) != 1 || len(products) != 1 {
			t.Fatalf("tick = (failures %v, read %d, products %d), want one request driven to its product", tick.Failures, len(tick.Read), len(products))
		}
		if len(received) != 1 || received[0] != request.ID {
			t.Fatalf("dispatched requests = %v, want the one request the tick read %q", received, request.ID)
		}
		if len(order) != 2 || order[0] != "phase:"+string(request.ID) || !strings.HasPrefix(order[1], "product:") {
			t.Fatalf("dispatch order = %v, want the phase ahead of the product of its pass", order)
		}
	})

	t.Run("a pipeline whose face refused keeps the request pending", func(t *testing.T) {
		refused := errors.New("the compression surface holds no capacity")
		var received []event.EventID
		pipeline, err := plugin.NewPipeline([]plugin.Plugin{seamCompress{received: &received, err: refused}})
		if err != nil {
			t.Fatalf("register the refusing element: %v", err)
		}
		requests := memory.NewCompressRequests()
		var products []meditation.MeditationArtifact
		curation := newCompressionCuration(t,
			meditation.WithCompressRequests(requests),
			meditation.WithMeditationCards(compressionCardSource()),
			meditation.WithCompressPhase(pipeline),
			meditation.WithMeditationProducts(func(_ context.Context, artifact meditation.MeditationArtifact) error {
				products = append(products, artifact)
				return nil
			}),
		)
		if _, filed, err := curation.Judge(ctx, compressionNow, compressionCards()); err != nil || !filed {
			t.Fatalf("Judge = (filed %v, %v), want the request the over-limit sequence files", filed, err)
		}

		tick, err := curation.Tick(ctx, compressionNow)
		if err != nil {
			t.Fatalf("Tick under a refused dispatch: %v", err)
		}
		if len(received) != 1 {
			t.Fatalf("dispatched requests = %v, want the one request the tick read", received)
		}
		if len(tick.Reports) != 0 || len(products) != 0 {
			t.Fatalf("tick = (reports %d, products %d), want no pass behind a refused request", len(tick.Reports), len(products))
		}
		if len(tick.Failures) != 1 || !errors.Is(tick.Failures[0], refused) || !strings.Contains(tick.Failures[0].Error(), "compress-seam") {
			t.Fatalf("tick failures = %v, want the refusal of the named face recorded", tick.Failures)
		}
		if pending, err := requests.Pending(ctx); err != nil || len(pending) != 1 {
			t.Fatalf("pending under a refused dispatch = (%v, %v), want the request still awaited", pending, err)
		}
	})
}
