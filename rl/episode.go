package rl

import "github.com/SpellingDragon/tagent/event"

// 契约: docs/wiki/rl/rl-architecture.md#training-export
//
// DependencyKind classifies why one episode's records depend on another's: a
// sub-call inside a parent episode, a handoff to another receiver, or a stored
// history that two episodes both read.
type DependencyKind string

const (
	// DependencyChildCall marks a child call whose data cannot be split away from
	// the episode that made it.
	DependencyChildCall DependencyKind = "child_call"
	// DependencyHandoff marks a handoff, where the receiving episode continues the
	// work of the episode that handed it over.
	DependencyHandoff DependencyKind = "handoff"
	// DependencySharedHistory marks two episodes reading one stored history, so
	// their records stay on the same side of a split.
	DependencySharedHistory DependencyKind = "shared_history"
)

// EpisodeDependency declares that one episode's records share data with another's
// and must therefore not be split away from it.
//
// A dependency that could not be established is marked unknown rather than
// dropped, because a missing dependency is what a leakage free split is decided
// on.
type EpisodeDependency struct {
	// EpisodeID names the episode that depends on another.
	EpisodeID event.EpisodeID
	// DependsOn names the episode it depends on.
	DependsOn event.EpisodeID
	// Kind classifies the dependency.
	Kind DependencyKind
	// Lineage states how certain the declared dependency is.
	Lineage Lineage
}

// EpisodeGroup is one leakage free split unit: an episode together with every
// episode it shares an ancestor, a candidate set or a history dependency with.
//
// A group is a split unit and not a session: an episode may span sessions and a
// session may hold several episodes, and neither equality is forced.
type EpisodeGroup struct {
	// GroupID identifies the group.
	GroupID string
	// RootID names the shared ancestor of the members; it is empty for a group
	// holding no common ancestor.
	RootID event.EpisodeID
	// Members lists the episode identifiers the group holds.
	Members []event.EpisodeID
	// Basis lists the declared dependencies that hold the members together.
	Basis []EpisodeDependency
	// Lineage states how certain the grouping is.
	Lineage Lineage
}
