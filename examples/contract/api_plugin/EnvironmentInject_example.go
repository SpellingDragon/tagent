package pluginexample

import (
	"context"

	"github.com/SpellingDragon/tagent/api/plugin"
)

// environmentBlocks is the canonical environment face: it renders the dynamic material of
// one request assembly as separate blocks, each its own product of that request.
type environmentBlocks struct{}

// Name reports the stable name of the element.
func (environmentBlocks) Name() string { return "environment-blocks" }

// Port reports the request->environment port the element serves.
func (environmentBlocks) Port() plugin.Port {
	return plugin.Port{Reads: plugin.MaterialRequest, Produces: plugin.ProductEnvironment}
}

// InjectEnvironment renders the environment material of one assembly: the clock and the
// roster are two blocks, and the material that renders no text is answered as the empty
// text it is, never merged into the block beside it.
func (environmentBlocks) InjectEnvironment(_ context.Context, request plugin.EnvironmentRequest) ([]string, error) {
	return []string{"clock=noon", "", "roster=" + string(request.Agent)}, nil
}

// silentEnvironment is the face that holds no material for the assembly: it answers the
// empty answer, which is the empty face stated as empty rather than refused.
type silentEnvironment struct{}

// Name reports the stable name of the element.
func (silentEnvironment) Name() string { return "silent-environment" }

// Port reports the request->environment port the element serves.
func (silentEnvironment) Port() plugin.Port {
	return plugin.Port{Reads: plugin.MaterialRequest, Produces: plugin.ProductEnvironment}
}

// InjectEnvironment answers no block and no error for the assembly it holds nothing for.
func (silentEnvironment) InjectEnvironment(context.Context, plugin.EnvironmentRequest) ([]string, error) {
	return nil, nil
}

var (
	_ plugin.Instance          = environmentBlocks{}
	_ plugin.EnvironmentInject = environmentBlocks{}
	_ plugin.Instance          = silentEnvironment{}
	_ plugin.EnvironmentInject = silentEnvironment{}
)

// ExampleEnvironmentInject renders the environment blocks of one request assembly: the
// multi-block face answers each block as its own product in the declared order, the text
// it renders empty contributes no block, the empty face beside it refuses nothing, and
// every block traces back to the one request the pass was dispatched with.
//
// 契约: docs/wiki/plugin/plugin-architecture.md#overview
func ExampleEnvironmentInject() {
	pipeline, err := plugin.NewFlow([]plugin.Instance{environmentBlocks{}, silentEnvironment{}})
	if err != nil {
		return
	}
	blocks, err := pipeline.InjectEnvironment(context.Background(), plugin.EnvironmentRequest{Agent: "agent-a"})
	if err != nil || len(blocks) != 2 {
		return
	}
	if blocks[0] != "clock=noon" || blocks[1] != "roster=agent-a" {
		return
	}
}
