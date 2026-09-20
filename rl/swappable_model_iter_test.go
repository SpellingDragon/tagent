package rl

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// iterCapableBase implements BOTH model.Model and model.IterModel, recording which
// entry point is used AND whether the base is touched before iteration begins (laziness).
type iterCapableBase struct {
	iterEntry int32 // GenerateContentIter invoked
	iterStart int32 // returned Seq actually invoked
	chanEntry int32 // GenerateContent invoked
	responses []*model.Response
}

func (b *iterCapableBase) Info() model.Info { return model.Info{Name: "iter-base"} }

func (b *iterCapableBase) GenerateContent(_ context.Context, _ *model.Request) (<-chan *model.Response, error) {
	atomic.AddInt32(&b.chanEntry, 1)
	ch := make(chan *model.Response, len(b.responses))
	for _, r := range b.responses {
		ch <- r
	}
	close(ch)
	return ch, nil
}

func (b *iterCapableBase) GenerateContentIter(_ context.Context, _ *model.Request) (model.Seq[*model.Response], error) {
	atomic.AddInt32(&b.iterEntry, 1)
	return func(yield func(*model.Response) bool) {
		atomic.AddInt32(&b.iterStart, 1)
		for _, r := range b.responses {
			if !yield(r) {
				return
			}
		}
	}, nil
}

// channelOnly exposes ONLY model.Model (no IterModel) over a base — the case where the
// decorator must genuinely bridge the channel, not fake an iterator.
type channelOnly struct{ inner model.Model }

func (c channelOnly) Info() model.Info { return c.inner.Info() }

func (c channelOnly) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	return c.inner.GenerateContent(ctx, req)
}

func mkIterResp(text string) *model.Response {
	return &model.Response{ID: "r", Done: true,
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: text}}}}
}

// §4.5B: SwappableModel must NOT hide a base IterModel — it delegates to the base's
// GenerateContentIter (fast path, no goroutine) and stays lazy (creating the iterator
// does not touch the base); the in-flight lease is released when iteration finishes.
func TestSwappableModel_PreservesIterModel(t *testing.T) {
	var _ model.IterModel = (*SwappableModel)(nil) // compiles ⟹ capability not hidden

	base := &iterCapableBase{responses: []*model.Response{mkIterResp("a"), mkIterResp("b")}}
	sw := NewSwappableModel(base)

	seq, err := sw.GenerateContentIter(context.Background(), &model.Request{})
	require.NoError(t, err)
	require.NotNil(t, seq)
	require.Equal(t, int32(0), atomic.LoadInt32(&base.iterEntry), "creating the iterator must not call the base")
	require.Equal(t, int32(0), atomic.LoadInt32(&base.chanEntry), "creating the iterator must not downgrade to channel")
	require.Equal(t, int64(0), sw.inFlight.Load(), "no lease before iteration starts")

	var got []string
	seq(func(r *model.Response) bool {
		if r != nil && len(r.Choices) > 0 {
			got = append(got, r.Choices[0].Message.Content)
		}
		return true
	})
	require.Equal(t, []string{"a", "b"}, got)
	require.Equal(t, int32(1), atomic.LoadInt32(&base.iterEntry), "must delegate to the base iterator (fast path)")
	require.Equal(t, int32(0), atomic.LoadInt32(&base.chanEntry), "must NOT downgrade an iterator base to channel")
	require.Equal(t, int64(0), sw.inFlight.Load(), "iteration lease must be released when the Seq finishes")
}

// §4.5B: when the base is channel-only, SwappableModel.GenerateContentIter must
// genuinely bridge (all responses delivered), not fake an unsupported interface.
func TestSwappableModel_BridgesChannelBase(t *testing.T) {
	base := &iterCapableBase{responses: []*model.Response{mkIterResp("x"), mkIterResp("y")}}
	sw := NewSwappableModel(channelOnly{base}) // inner is model.Model, NOT IterModel

	seq, err := sw.GenerateContentIter(context.Background(), &model.Request{})
	require.NoError(t, err)
	var got []string
	seq(func(r *model.Response) bool {
		if r != nil && len(r.Choices) > 0 {
			got = append(got, r.Choices[0].Message.Content)
		}
		return true
	})
	require.Equal(t, []string{"x", "y"}, got, "channel base must be bridged into the iterator")
	require.Equal(t, int64(0), sw.inFlight.Load(), "bridge lease released on completion")
}

// §4.5B: early-stop (yield false) must still free the lease and not wedge the producer.
func TestSwappableModel_IterEarlyStopReleasesLease(t *testing.T) {
	base := &iterCapableBase{responses: []*model.Response{mkIterResp("1"), mkIterResp("2"), mkIterResp("3")}}
	sw := NewSwappableModel(base)
	seq, err := sw.GenerateContentIter(context.Background(), &model.Request{})
	require.NoError(t, err)
	count := 0
	seq(func(*model.Response) bool {
		count++
		return false // stop after first
	})
	require.Equal(t, 1, count)
	require.Equal(t, int64(0), sw.inFlight.Load(), "early-stop must release the lease")
}
