// 契约: docs/wiki/memory/memory-architecture.md#read-paths
//
// Package search is the retrieval port of the memory side: it names the
// authorized scope a recall query may read in, returns the reference set a
// query hit, and fuses the vector leg and the keyword leg into that one set.
//
// The port speaks references, not facts. The index behind it is a derived,
// replaceable structure, so a hit names the stored original and the text stays
// behind the original store: the index is never the source of truth, and a hit
// is never served as a second copy of the text. This is the phase 1 skeleton:
// it declares the port and holds no behaviour.
package search

import (
	"context"
	"time"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
)

// Searcher is the authorized-scope query port: it answers one recall query
// inside the scope the caller is authorized to read and returns the references
// the query hit.
//
// The scope is default-nothing. An empty authorized set authorizes no partition
// at all, so a query carrying one scans nothing and returns an empty reference
// set rather than widening to the whole store: an unauthorized read comes back
// as a miss and never as a silent full-store scan.
type Searcher interface {
	// Search returns the reference set of q, ranked by the retrieval rule the
	// query runs under and bounded by the page the query asks for.
	Search(ctx context.Context, q Query) (Result, error)
}

// Query is one retrieval request: the authorized read scope it may run in and
// the time, type and relation filters that narrow that scope.
//
// A filter only narrows what the scope already admits and never widens it, and
// the scope itself is default-nothing rather than a fallback to the whole
// store.
type Query struct {
	// Partitions is the authorized partition set this query may read. An empty
	// set authorizes nothing and is never read as every partition.
	Partitions []string
	// Types restricts the query to the given frozen event types; it is empty
	// when no type restriction applies.
	Types []string
	// Since and Until bound the query to the events recorded inside that
	// window. A zero Since leaves the lower bound open, and a zero Until the
	// upper one.
	Since time.Time
	Until time.Time
	// Relations restricts the query to the events standing in a causal relation
	// to the given keys; it is empty when no relation restriction applies.
	Relations []event.EventID
	// Limit caps how many references the result carries, so an oversized match
	// set is consumed by page rather than whole; a non-positive Limit asks for
	// no page bound.
	Limit int
}

// Result is the reference set one retrieval returned: the references the query
// hit, ordered from the best hit down.
//
// It carries references, not text. The index that produced it is not the source
// of truth, so the caller reads the text behind a hit through the original path
// and a hit is never served as a second copy of it. An empty set is a legal
// answer: both a miss and a scope that authorizes nothing produce one.
type Result struct {
	// Refs is the reference set of the result, ordered from the best hit to the
	// worst.
	Refs []memory.OriginalRef
}
