package rlexample

import (
	"context"

	"github.com/SpellingDragon/tagent/rl"
)

// grantingExporter is the canonical authorized export port: it authorizes before any read,
// refuses an empty partition list and reports what it wrote with its provenance manifest.
type grantingExporter struct {
	granted []string
}

func (e *grantingExporter) authorizes(partition string) bool {
	for _, granted := range e.granted {
		if granted == partition {
			return true
		}
	}
	return false
}

// Export runs one authorized export and returns the manifest of the snapshot it wrote.
func (e *grantingExporter) Export(_ context.Context, req rl.ExportRequest) (rl.ExportResult, error) {
	if len(req.PartitionIDs) == 0 {
		return rl.ExportResult{}, rl.ErrExportUnauthorized
	}
	if req.PageLimit > rl.MaxExportPageLimit {
		return rl.ExportResult{}, rl.ErrExportPageLimit
	}
	written := int64(0)
	for _, partition := range req.PartitionIDs {
		if !e.authorizes(partition) {
			continue
		}
		written++
	}
	return rl.ExportResult{
		Manifest: rl.ExportManifest{ExportID: "export-1", Complete: true},
		Written:  written,
	}, nil
}

var _ rl.Exporter = (*grantingExporter)(nil)

// ExampleExporter runs one authorized export over the granted partition and reports that a
// request naming no partition is refused before any read.
//
// 契约: docs/wiki/rl/rl-architecture.md#training-export
func ExampleExporter() {
	ctx := context.Background()
	var exporter rl.Exporter = &grantingExporter{granted: []string{"partition-1"}}

	result, err := exporter.Export(ctx, rl.ExportRequest{PartitionIDs: []string{"partition-1"}, PageLimit: rl.DefaultExportPageLimit})
	if err != nil || result.Written != 1 || !result.Manifest.Complete {
		return
	}
	if _, err := exporter.Export(ctx, rl.ExportRequest{}); err != rl.ErrExportUnauthorized {
		return
	}
	if _, err := exporter.Export(ctx, rl.ExportRequest{PartitionIDs: []string{"partition-1"}, PageLimit: rl.MaxExportPageLimit + 1}); err != rl.ErrExportPageLimit {
		return
	}
}
