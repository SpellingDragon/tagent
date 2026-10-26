package rl

import (
	"context"

	"github.com/SpellingDragon/tagent/api/model"
)

// 契约: docs/wiki/rl/rl-architecture.md#swappable-model
//
// LeaseModel is the model handle a binding installs for one leased model
// reference: every call borrows the version in effect when the call begins and
// delegates the whole stream to it. The handle is stable across replacements,
// so the binding of a leased agent is published once and a replacement installs
// the version a request that begins afterwards borrows without republishing the
// generation.
//
// A request already in flight keeps the version it began on: the borrowed
// VersionedModel covers the whole returned stream, so a replacement never swaps
// the model under a live request.
type LeaseModel struct {
	lease *ModelLease
}

var (
	_ model.Model     = LeaseModel{}
	_ model.IterModel = LeaseModel{}
)

// NewLeaseModel returns the stable model handle of one lease: the handle a
// leased agent's binding installs beside the lease it delegates to.
func NewLeaseModel(lease *ModelLease) LeaseModel { return LeaseModel{lease: lease} }

// Info reports the descriptor of the version in effect: the model name of the
// current identity beside the context window of the pinned instance. The name
// stays the model name across versions; the version a response actually served
// is declared on that response by the version handle.
func (m LeaseModel) Info() model.Info {
	if m.lease == nil {
		return model.Info{}
	}
	return m.lease.Current().Info()
}

// GenerateContent borrows the version in effect when the call begins and
// delegates the request to it under the version's own stream lease.
func (m LeaseModel) GenerateContent(ctx context.Context, request *model.Request) (<-chan *model.Response, error) {
	if m.lease == nil {
		return nil, ErrLeaseModelMissing
	}
	return m.lease.Current().GenerateContent(ctx, request)
}

// GenerateContentIter borrows the version in effect when the call begins and
// delegates the iterator to it, keeping the pinned version's iterator
// capability rather than bridging it behind a channel.
func (m LeaseModel) GenerateContentIter(ctx context.Context, request *model.Request) (model.Seq[*model.Response], error) {
	if m.lease == nil {
		return nil, ErrLeaseModelMissing
	}
	return m.lease.Current().GenerateContentIter(ctx, request)
}
