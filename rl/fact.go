package rl

import "github.com/SpellingDragon/tagent/api/event"

// FactKey returns the attempt's identity as a fact key: the attempt ID the record
// is stored under. A retry hidden below the decision boundary is not a second
// attempt, so the key of one attempt stays the ID it was recorded with.
//
// 契约: docs/FACT-VOCABULARY.md#families
func (r AttemptRecord) FactKey() string { return string(r.ID) }

// FactType returns the record kind an attempt line is read as; the record family
// keeps one frozen type per record kind.
func (r AttemptRecord) FactType() string { return "rl.attempt" }

// FactKey returns the feedback's identity as a fact key: the feedback ID the
// record is stored under. A second record under one ID is either the same record
// (idempotent) or refused as a conflict on the write path, so one feedback ID
// never names two facts.
func (r FeedbackRecord) FactKey() string { return r.FeedbackID }

// FactType returns the record kind a feedback line is read as.
func (r FeedbackRecord) FactType() string { return "rl.feedback" }

// FactKey returns the reward's identity as a fact key: the instance ID a
// derivation takes, because a rule version alone never identifies a reward record
// — the same rule deriving a value from late feedback is a different record, and
// a record without an instance identity could not be cited at all.
func (r RewardRecord) FactKey() string { return r.InstanceID }

// FactType returns the record kind a reward line is read as.
func (r RewardRecord) FactType() string { return "rl.reward" }

// The three record kinds of the training observation family satisfy the fact
// category contract.
var (
	_ event.Fact = AttemptRecord{}
	_ event.Fact = FeedbackRecord{}
	_ event.Fact = RewardRecord{}
)
