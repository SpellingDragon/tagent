package tagent

import (
	"github.com/SpellingDragon/tagent/config"
	"github.com/SpellingDragon/tagent/evolution"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/plugin"
	"github.com/SpellingDragon/tagent/rl"
	"github.com/SpellingDragon/tagent/telemetry"
	"github.com/SpellingDragon/tagent/workspace"
)

// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
//
// Deps carries what the composition root takes from the process that embeds it:
// the pure declaration to build from and the cross-boundary ports the runtime
// does not construct on its own. The model/provider factory is deliberately absent:
// its signature is not settled, and it is decided in phase two (02.4).
type Deps struct {
	// Declaration is the normalized organization declaration the runtime is
	// built from; it describes an organization as data and holds no live object.
	Declaration *config.Declaration
	// Gate is the ordered governance front of a publication: a candidate binding
	// is published only after Eval, Benchmark and ApplyGate each answered, and a
	// missing required check refuses the publication.
	Gate evolution.StructuralGate
	// Externalizer commits an oversized result as a durable original before a
	// bounded preview of it is served.
	Externalizer workspace.Externalizer
	// Cleaner reclaims the temporary replicas the runtime materialises, bounded
	// by age and by count.
	Cleaner workspace.Cleaner
	// Capture records the model requests and attempts a real training reader
	// consumes, under a bound that never waits on the caller.
	Capture rl.Capture
	// Memory is the persistent original-text store port: an externalized result
	// is committed here before a reference to it is served.
	Memory memory.OriginalStore
	// Telemetry is the post-commit observation port: the runtime feeds committed
	// facts to it on the observation side channel, and an observation gap leaves
	// the committed fact in place.
	Telemetry telemetry.Observer
	// Plugins is the ordered plugin pipeline: each element is a registration unit
	// that reports its phase, and the runtime dispatches every phase to the
	// matching narrow interface in the declared order.
	Plugins []plugin.Plugin
}

// Build assembles the runtime from deps and returns it ready for its first
// request.
//
// The shared faces are established in a fixed order: the state store is
// established first, the task manager is injected into it as the notification
// outlet, and the execution face is assembled last over both. Assembly draws the
// cross-boundary ports from Deps — the memory original store, the telemetry
// observer and the ordered plugin pipeline among them — and dispatches each plugin
// by phase. When construction fails part way through, only the resources that
// attempt created are released,
// in the reverse of their construction order and once each, while the shared
// faces and any binding already in service are left untouched. Build is not
// implemented in the phase 1 skeleton: it always fails and returns a nil
// runtime.
func Build(deps Deps) (*Runtime, error) {
	return nil, errNotImplemented
}
