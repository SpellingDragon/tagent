package rl

import (
	"context"
	"errors"
	"fmt"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
)

// ErrLeaseStaleReplacement reports a replacement fact that starts from a
// version the lease does not hold: a move judged against an older version is
// refused by name rather than applied on top of the version in effect.
var ErrLeaseStaleReplacement = errors.New("rl: the model replacement starts from a version the lease does not hold")

// ErrLeaseObserverLookupMissing reports a lease observer built beside no fact
// lookup: the observer reads the fact it applies back from the chain, and an
// unreachable chain is refused rather than read as no replacement.
var ErrLeaseObserverLookupMissing = errors.New("rl: the model lease observer was built beside no fact lookup")

// 契约: docs/wiki/rl/rl-architecture.md#swappable-model
//
// LeaseObserver is the event-driven replacement consumer of one agent's model
// lease. It observes the facts the agent's own pipeline commits, reads back the
// one a commit identity names, and applies a model replacement fact to the
// lease it serves: the rebuild, the publication and the installation run in the
// lease's existing order, so a refused rebuild or a refused publication installs
// no version and leaves the version in effect untouched.
//
// The observer serves the fact-to-observation port exactly as the reflection
// driver does: one committed fact in, one read-only observation out. A fact of
// another type, or of another agent's ownership, is not the observation this
// instance serves and is passed over.
type LeaseObserver struct {
	agent   event.AgentID
	lease   *ModelLease
	lookup  plugin.FactLookup
	rebuild func(version string) (model.Model, error)
	publish func(ModelIdentity, model.Model) error
}

var (
	_ plugin.Instance           = (*LeaseObserver)(nil)
	_ plugin.PostCommitObserver = (*LeaseObserver)(nil)
)

// NewLeaseObserver returns the replacement consumer of one agent's lease: the
// fact lookup that reads a committed replacement back, the rebuild that
// produces the requested version and the publication entry that accepts the
// rebuilt model beside the version in effect.
func NewLeaseObserver(agent event.AgentID, lease *ModelLease, lookup plugin.FactLookup, rebuild func(version string) (model.Model, error), publish func(ModelIdentity, model.Model) error) (*LeaseObserver, error) {
	if lease == nil {
		return nil, ErrLeaseModelMissing
	}
	if lookup == nil {
		return nil, ErrLeaseObserverLookupMissing
	}
	if rebuild == nil {
		return nil, ErrLeaseRebuildMissing
	}
	if publish == nil {
		return nil, ErrLeasePublishMissing
	}
	return &LeaseObserver{agent: agent, lease: lease, lookup: lookup, rebuild: rebuild, publish: publish}, nil
}

// Name reports the stable name of this plugin instance.
func (o *LeaseObserver) Name() string { return "model-lease" }

// Port reports the consumption the observer serves: one committed fact in, one
// observation out. The observation is the replacement the fact applies to the
// lease.
func (o *LeaseObserver) Port() plugin.Port {
	return plugin.PortFactObservation
}

// ObserveCommit reads the fact one commit wrote and applies a model replacement
// fact owned by this observer's agent to the lease. A fact of another type or
// another owner is passed over; a replacement whose start version is not the
// version in effect is refused by name; and the replacement itself is the
// lease's own rebuild, publication and installation, so a refused step installs
// no version.
func (o *LeaseObserver) ObserveCommit(ctx context.Context, id event.EventID) error {
	if o == nil || o.lease == nil {
		return nil
	}
	fact, err := o.lookup.CommittedFact(ctx, id)
	if err != nil {
		return err
	}
	if fact.Type != event.ModelReplacementType {
		return nil
	}
	if fact.Owner.AgentID != o.agent {
		return nil
	}
	to := fact.Meta[event.MetaKeyModelToVersion]
	if to == "" {
		return fmt.Errorf("%w: the replacement fact %s names no target version", ErrLeaseVersionMissing, id)
	}
	from := fact.Meta[event.MetaKeyModelFromVersion]
	held := o.lease.CurrentVersion()
	if from != "" && from != held {
		return fmt.Errorf("%w: the replacement fact %s starts at %q and the lease holds %q", ErrLeaseStaleReplacement, id, from, held)
	}
	_, err = o.lease.Replace(ctx, to, func() (model.Model, error) {
		return o.rebuild(to)
	}, o.publish)
	return err
}
