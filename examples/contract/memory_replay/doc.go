// Package memoryreplayexample holds the canonical example of the fact chain a replay scans
// and a projection rebuilds: the committed facts in their EventKey write order, read
// forward from an identity and by the key one fact is quoted by.
//
// The file is the minimal implementation beside the Example function that drives it
// through its real caller.
//
// 契约: docs/wiki/memory/memory-architecture.md#read-paths
package memoryreplayexample
