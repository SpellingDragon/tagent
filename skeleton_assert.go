package tagent

import (
	"context"
	"errors"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/plugin"
	"github.com/SpellingDragon/tagent/query"
	"github.com/SpellingDragon/tagent/state"
	"github.com/SpellingDragon/tagent/task"
	"github.com/SpellingDragon/tagent/telemetry"
)

// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
//
// Skeleton compile-time assertions for the phase 1 public face. Every entry is a
// frozen stub, so the assertions pin what the skeleton proves on its own: the
// shared face each assembly field holds, and the exact signature of every public
// entry and of the constructor.
var (
	_ state.Store  = Runtime{}.store
	_ task.Manager = Runtime{}.manager
	_ agent.Agent  = Runtime{}.agent

	_ func(*Runtime, context.Context, event.Address) error     = (*Runtime).CloseSession
	_ func(*Runtime, context.Context) error                    = (*Runtime).Shutdown
	_ func(*Runtime, query.ListFilter) (query.Snapshot, error) = (*Runtime).Query
	_ func(Deps) (*Runtime, error)                             = Build
)

// The cross-boundary ports Deps declares keep their documented shape.
var (
	_ memory.OriginalStore = Deps{}.Memory
	_ telemetry.Observer   = Deps{}.Telemetry
	_ []plugin.Plugin      = Deps{}.Plugins
)

// errNotImplemented is the sentinel every frozen stub returns while the phase 2
// implementation is absent.
var errNotImplemented = errors.New("phase 2: not implemented")
