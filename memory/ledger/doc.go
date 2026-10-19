// 契约: docs/wiki/memory/memory-architecture.md#event-shape
//
// Package ledger is the runtime's durable original-text layer: it names one
// stored original and the reference metadata that scopes it, commits those bytes
// and returns them byte for byte, and carries the retention guard, the
// lifecycle and the storage tiers built beside them.
//
// The package speaks the fact vocabulary of the event package and nothing above
// it, so the durable side cannot be reversed into the intelligent or governing
// side. Its files sit under the ledger unit by the mechanism-subpackage rule of
// CONTRIBUTING.md: the fact vocabulary (OriginalRef, Integrity), the consumer
// wiring (Retention, Lifecycle, the stores) and the per-tier strategies
// (localfile, rustviking).
package ledger
