package evolutionmeditationexample

import (
	"context"
	"strings"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/evolution/meditation"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/ledger"
)

// The plugin pipeline satisfies the compression port as it stands, so a meditation tick
// dispatches to the plugin surface without either package importing the other.
var _ meditation.CompressPhase = plugin.Pipeline{}

// tickCompress is the compress-trigger element a meditation tick is wired to: it records
// the identity of every request it was handed, so the port's dispatch is observable.
type tickCompress struct {
	order *[]string
}

// Name reports the stable name of the element.
func (tickCompress) Name() string { return "tick-compress" }

// Phase reports the one phase the element serves.
func (tickCompress) Phase() plugin.Phase { return plugin.PhaseCompressTrigger }

// TriggerCompress records the request it was handed and answers it without an error.
func (p tickCompress) TriggerCompress(_ context.Context, request event.CompressRequest) error {
	*p.order = append(*p.order, "phase:"+string(request.ID))
	return nil
}

var (
	_ plugin.Plugin          = tickCompress{}
	_ plugin.CompressTrigger = tickCompress{}
)

// ExampleCompressPhase drives one meditation tick through the plugin pipeline that
// satisfies the port: the request the tick read is handed to the pipeline face before the
// pass it drives commits its product, the face answers the request as filed, and the tick
// retires it only after the products of its compression are committed.
//
// 契约: docs/wiki/memory/memory-architecture.md#curation
func ExampleCompressPhase() {
	ctx := context.Background()
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	cards := []meditation.MeditationCard{
		{Key: "cp-1", Type: event.TypeExternalInput, Summary: "需求原文", RecordedAt: now.Add(-2 * time.Hour)},
		{Key: "cp-2", Type: event.TypeAgentOutput, Summary: "实现摘要", RecordedAt: now.Add(-time.Hour), Links: 1},
	}

	var order []string
	pipeline, err := plugin.NewPipeline([]plugin.Plugin{tickCompress{order: &order}})
	if err != nil {
		return
	}
	retention := ledger.NewRetention(ledger.NewMemoryGuard())
	lifecycle, err := ledger.NewLifecycle(
		ledger.LifecycleConfig{GlobalTTLDays: -1},
		retention,
		ledger.NewMemoryLifecycleStore(ledger.LifecycleCapabilities{}),
		ledger.LifecycleCapabilities{},
	)
	if err != nil {
		return
	}
	lifecycle.RecoveryComplete()

	requests := memory.NewCompressRequests()
	curation, err := meditation.NewMeditation(
		meditation.MeditationPolicy{MaxCards: 1, Keep: 1, MinScore: 1},
		retention,
		lifecycle,
		nil,
		meditation.WithCompressRequests(requests),
		meditation.WithMeditationCards(func(context.Context) ([]meditation.MeditationCard, error) { return cards, nil }),
		meditation.WithCompressPhase(pipeline),
		meditation.WithMeditationProducts(func(_ context.Context, artifact meditation.MeditationArtifact) error {
			order = append(order, "product:"+string(artifact.Key))
			return nil
		}),
	)
	if err != nil {
		return
	}
	request, filed, err := curation.Judge(ctx, now, cards)
	if err != nil || !filed {
		return
	}

	tick, err := curation.Tick(ctx, now)
	if err != nil || len(tick.Failures) != 0 || len(tick.Read) != 1 || len(tick.Products) != 1 {
		return
	}
	if len(order) != 2 || order[0] != "phase:"+string(request.ID) || !strings.HasPrefix(order[1], "product:") {
		return
	}
	if pending, err := requests.Pending(ctx); err != nil || len(pending) != 0 {
		return
	}
}
