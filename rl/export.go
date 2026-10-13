package rl

import "context"

// 契约: docs/wiki/rl/rl-architecture.md#training-export
//
// Exporter is the read-only consumption port for authorized training export: it
// reads the events of the partitions a caller is authorized for, together with the
// feedback bound to them, and writes one snapshot whose provenance manifest says
// what the snapshot contains and how completely it was read.
//
// The port carries three invariants:
// - authorization runs before any read, and an empty partition list is a refusal;
// - every written row is rechecked, and a row that fails the recheck is counted;
// - a partial read is reported as partial, never as a complete snapshot.
type Exporter interface {
	// Export runs one authorized export and returns the manifest of the snapshot it
	// wrote with the counts of the run. The context bounds the run, and a cancelled
	// run reports what it read as partial.
	Export(ctx context.Context, req ExportRequest) (ExportResult, error)
}

// ExportRequest is one authorized export run: the partitions the caller may read,
// the optional call-id filter and time bounds, and the page size to read them with.
//
// The request is the authorization input: the partition list is the caller's grant,
// and no field of the request may widen that grant.
type ExportRequest struct {
	// PartitionIDs names the partitions the caller is authorized to read. An empty
	// list is refused, and the port never widens it into a whole-store scan.
	PartitionIDs []string
	// CallIDs keeps only the facts whose call id is listed when it is non-empty.
	CallIDs []string
	// Since and Until bound the export in Unix milliseconds, the unit the stored
	// events use; 0 means open.
	Since int64
	Until int64
	// PageLimit is the per-partition page size; 0 takes the default and a value
	// outside the supported range is refused rather than clamped.
	PageLimit int
}

// ExportResult is the outcome of one export run: the provenance manifest of the
// snapshot together with the counts a caller reads without opening the snapshot.
//
// A partial run returns both the manifest and a non-nil error, so a truncated
// snapshot is never mistaken for a complete one.
type ExportResult struct {
	// Manifest is the provenance manifest of the run.
	Manifest ExportManifest
	// Written counts the sample lines the run wrote.
	Written int64
	// Forbidden counts the rows the per-row authorization recheck refused.
	Forbidden int64
	// Filtered counts the facts the caller's filters removed.
	Filtered int64
	// Partial reports a run that stopped before every authorized page was read.
	Partial bool
}

// Export contract assertions pin the port and its request and result types: a
// changed signature or a renamed field stops the package from compiling.
var (
	_ Exporter = (Exporter)(nil)

	_ func(Exporter, context.Context, ExportRequest) (ExportResult, error) = Exporter.Export

	_ = ExportRequest{
		PartitionIDs: []string{},
		CallIDs:      []string{},
		Since:        0,
		Until:        0,
		PageLimit:    0,
	}

	_ = ExportResult{
		Manifest:  ExportManifest{},
		Written:   0,
		Forbidden: 0,
		Filtered:  0,
		Partial:   false,
	}
)
