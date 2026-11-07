// 契约: docs/wiki/tool/tool-architecture.md#declaration-stability
//
// Compile-time assertions of the recall surface: the four tools stay native
// callable tools, the declared names stay the vocabulary the prompt spells, the
// table port stays satisfied by the production read face, and the scoped
// original store stays satisfied by the in-process original store.
package recall

import (
	"github.com/SpellingDragon/tagent/memory/ledger"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// The four tools are native callable tools, so every call is admitted through
// the task wrapper before the model can make it.
var (
	_ trpctool.CallableTool = (*queryTool)(nil)
	_ trpctool.CallableTool = (*getTool)(nil)
	_ trpctool.CallableTool = (*recentTool)(nil)
	_ trpctool.CallableTool = (*traceTool)(nil)
)

// The in-process original store satisfies the face the tools consume, so the
// runtime wires a production component rather than a test double.
var _ ledger.ScopedOriginalStore = (*ledger.MemoryOriginalStore)(nil)

// The construction entry and the declared refusals keep their shape.
var (
	_ func(Reads, []string) []trpctool.CallableTool = New
	_ error                                         = ErrRecallUnwired
	_ error                                         = ErrRecallKey
	_ error                                         = ErrRecallMissing
)
