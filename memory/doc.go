// 契约: docs/wiki/memory/memory-architecture.md#event-shape
//
// Package memory is the runtime's durable original-text layer: it names one
// stored original and the reference metadata that scopes it, commits those bytes
// and returns them byte for byte, and carries the ordered projection and the
// retention guard built beside them.
//
// The package speaks the fact vocabulary of the event package and nothing above
// it, so the durable side cannot be reversed into the intelligent or governing
// side. The storage unit itself lives in memory/ledger and the replay unit in
// memory/replay; this package keeps the read face (queries, segment reads), the
// projection and the assembly seam over them.
package memory
