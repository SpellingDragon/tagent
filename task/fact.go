package task

import (
	"fmt"

	"github.com/SpellingDragon/tagent/event"
)

// FactKey returns the notice's stable identity as a fact key: the four-tuple
// TaskID+Round+Kind+Seq the notice's own identity stays at, synthesised into the
// one canonical spelling "%s:%d:%s:%d". The event reference the notice points at
// never replaces that identity, so a retried delivery answers the same key. The
// runtime mints task identities as "task-<n>" and every kind is one of the closed
// NoticeKind words, so the colon join stays injective over the notices this
// package produces.
//
// 契约: docs/FACT-VOCABULARY.md#families
func (n Notice) FactKey() string {
	return fmt.Sprintf("%s:%d:%s:%d", n.TaskID, n.Round, n.Kind, n.Seq)
}

// FactType returns the notification category as the fact type: the category is
// part of the notice's own identity, so a repeated delivery keeps its type.
func (n Notice) FactType() string { return string(n.Kind) }

// The notice family satisfies the fact category contract.
var _ event.Fact = Notice{}
