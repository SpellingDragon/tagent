package rl

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// §4.5B/§4.5C: the recorder decorator exposes GenerateContentIter (does not hide the
// iterator capability from a flow) AND stays lazy — creating the iterator does not fire
// the model or consume a batch slot; iteration yields every response and writes exactly
// one trajectory record (recording preserved via the shared record path).
func TestTrajectoryRecorder_IteratorLazyRecords(t *testing.T) {
	tmpDir := t.TempDir()
	base := &iterCapableBase{responses: []*model.Response{mkIterResp("a"), mkIterResp("b")}}
	tr, err := NewTrajectoryRecorder(base, tmpDir, "https://x/v1")
	require.NoError(t, err)
	tr.SetSessionInfo("u", "sess-iter")

	seq, err := tr.GenerateContentIter(context.Background(), &model.Request{
		Messages: []model.Message{{Role: model.RoleUser, Content: "go"}},
	})
	require.NoError(t, err)
	// Lazy: the base is untouched until iteration starts.
	require.Equal(t, int32(0), atomic.LoadInt32(&base.chanEntry), "creating the iterator must not call the model")
	require.Equal(t, int32(0), atomic.LoadInt32(&base.iterEntry))

	var got []string
	seq(func(r *model.Response) bool {
		if r != nil && len(r.Choices) > 0 {
			got = append(got, r.Choices[0].Message.Content)
		}
		return true
	})
	require.Equal(t, []string{"a", "b"}, got, "iterator must yield every response")
	require.Equal(t, int32(1), atomic.LoadInt32(&base.chanEntry), "iteration fires the model once")

	require.NoError(t, tr.Close())
	data, err := os.ReadFile(filepath.Join(tmpDir, "sess-iter.jsonl"))
	require.NoError(t, err)
	require.Len(t, splitJSONL(data), 1, "iterator path writes exactly one trajectory record")
}

// §4.5B: the sub-agent wrapper likewise preserves the iterator entry point + laziness.
func TestTrajectoryRecorderModelWrapper_Iterator(t *testing.T) {
	tmpDir := t.TempDir()
	base := &iterCapableBase{responses: []*model.Response{mkIterResp("z")}}
	tr, err := NewTrajectoryRecorder(&iterCapableBase{}, tmpDir, "https://x/v1")
	require.NoError(t, err)
	tr.SetSessionInfo("u", "sess-wrap")
	w := NewTrajectoryRecorderModelWrapper(base, tr)

	seq, err := w.GenerateContentIter(context.Background(), &model.Request{
		Messages: []model.Message{{Role: model.RoleUser, Content: "go"}},
	})
	require.NoError(t, err)
	require.Equal(t, int32(0), atomic.LoadInt32(&base.chanEntry), "lazy: not fired at creation")

	n := 0
	seq(func(*model.Response) bool { n++; return true })
	require.Equal(t, 1, n)
	require.Equal(t, int32(1), atomic.LoadInt32(&base.chanEntry), "iteration fires the wrapped model")
	require.NoError(t, tr.Close())
}
