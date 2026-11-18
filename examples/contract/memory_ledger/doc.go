// Package memoryledgerexample holds the canonical examples of the ledger faces a reclaimer,
// a retention guard, a scoped read and a boot tail walk drive: the protection guard, the
// lifecycle store, the original-text store, its scoped read and the commit order.
//
// One file per interface, each a minimal implementation beside the Example function that
// drives it through its real caller.
//
// 契约: docs/wiki/memory/memory-architecture.md#hard-contracts
package memoryledgerexample
