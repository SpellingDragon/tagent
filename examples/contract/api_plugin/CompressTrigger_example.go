package pluginexample

import (
	"context"
	"errors"
	"strings"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/plugin"
)

// passingCompress is the canonical compress-trigger face: it records the identity of the
// request it was handed and starts the compression that answers it.
type passingCompress struct {
	started *[]event.EventID
}

// Name reports the stable name of the element.
func (passingCompress) Name() string { return "passing-compress" }

// Port reports the fact->compression port the element serves.
func (passingCompress) Port() plugin.Port {
	return plugin.PortFactCompression
}

// TriggerCompress answers the request it was handed without an error, so the pass carries
// the request forward into the compression.
func (p passingCompress) TriggerCompress(_ context.Context, request event.CompressRequest) error {
	*p.started = append(*p.started, request.ID)
	return nil
}

// refusingCompress is the face that refuses the request it was handed: the pass stops at
// that face by name, so no later face runs and the request is never counted compressed.
type refusingCompress struct {
	started *[]event.EventID
	refusal error
}

// Name reports the stable name of the element.
func (refusingCompress) Name() string { return "refusing-compress" }

// Port reports the fact->compression port the element serves.
func (refusingCompress) Port() plugin.Port {
	return plugin.PortFactCompression
}

// TriggerCompress answers the refusal it was built with, which refuses the whole pass.
func (p refusingCompress) TriggerCompress(_ context.Context, request event.CompressRequest) error {
	*p.started = append(*p.started, request.ID)
	return p.refusal
}

var (
	_ plugin.Instance        = passingCompress{}
	_ plugin.CompressTrigger = passingCompress{}
	_ plugin.Instance        = refusingCompress{}
	_ plugin.CompressTrigger = refusingCompress{}
)

// ExampleCompressTrigger drives both outcomes of the compress-trigger face through the
// pass: a face that answers starts the chain for the request as filed, while a face that
// refuses refuses the pass by name, no later face runs, and the request stays pending.
//
// 契约: docs/wiki/plugin/plugin-architecture.md#overview
func ExampleCompressTrigger() {
	ctx := context.Background()
	request := event.CompressRequestFor("memory.meditation", "sequence-1", "over-bound")
	refusal := errors.New("compress-trigger: the compression surface is at capacity")

	var refusedSeen, laterSeen []event.EventID
	refusing, err := plugin.NewFlow([]plugin.Instance{
		refusingCompress{started: &refusedSeen, refusal: refusal},
		passingCompress{started: &laterSeen},
	})
	if err != nil {
		return
	}
	err = refusing.TriggerCompress(ctx, request)
	if !errors.Is(err, refusal) || !strings.Contains(err.Error(), "refusing-compress") {
		return
	}
	if len(refusedSeen) != 1 || refusedSeen[0] != request.ID || len(laterSeen) != 0 {
		return
	}

	var passedSeen []event.EventID
	passing, err := plugin.NewFlow([]plugin.Instance{passingCompress{started: &passedSeen}})
	if err != nil {
		return
	}
	if err := passing.TriggerCompress(ctx, request); err != nil {
		return
	}
	if len(passedSeen) != 1 || passedSeen[0] != request.ID {
		return
	}
}
