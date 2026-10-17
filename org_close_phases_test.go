// 契约: docs/wiki/platform/org-hot-reload.md#close-drain
package tagent

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

type orgCloseResource func() error

func (c orgCloseResource) Close() error { return c() }

// TestOrgClose_StopsHiddenDeclarersBeforeWaiting checks shutdown crosses declared ownership in both directions.
func TestOrgClose_StopsHiddenDeclarersBeforeWaiting(t *testing.T) {
	var released [3]atomic.Int32
	makeOwner := func(name string, index int, tools ...tool.Tool) *agent.TagentAgent {
		t.Helper()
		owner, err := agent.NewTagentAgent(&agent.TagentConfig{
			Name: name, Model: &stubModel{name: "close-model"}, Tools: tools,
			MemoryStore:     memory.NewInMemoryStore(),
			MemStoreRelease: func() error { released[index].Add(1); return nil },
		})
		require.NoError(t, err)
		return owner
	}
	child := makeOwner("a-child", 0)
	entry := makeOwner("entry", 1, agent.NewAgentToolWrapper(child, "child", nil, nil))
	hidden := makeOwner("z-hidden", 2, agent.NewAgentToolWrapper(entry, "entry", nil, nil))
	owners := map[string]*agent.TagentAgent{"a-child": child, "entry": entry, "z-hidden": hidden}
	topology := agent.NewResidentTopology(owners)
	cms := make(map[string]*agent.ContextManager, len(owners))
	for name, owner := range owners {
		owner.SetResidentTable(topology)
		cms[name] = owner.ContextManager()
	}
	agent.WireOrgGeneration(cms, nil)
	require.Positive(t, agent.BindingHolders("entry", []*agent.TagentAgent{hidden, entry, child}))
	entry.RegisterCloser(&orgOwnerCloser{rc: &runtimeConfig{resident: topology}, self: "entry"})
	var resourceClosed atomic.Int32
	entry.RegisterCloser(orgCloseResource(func() error {
		for name, owner := range map[string]*agent.TagentAgent{"child": child, "hidden": hidden} {
			select {
			case <-owner.CloseDone():
			default:
				return fmt.Errorf("shared resource closed before %s stopped", name)
			}
		}
		resourceClosed.Add(1)
		return nil
	}))
	lease := hidden.ContextManager().AcquireLease(agent.LeaseBackground)
	defer lease.Release()
	closed := make(chan error, 1)
	go func() { closed <- entry.Close() }()
	require.Eventually(t, func() bool {
		return child.CloseStarted() && entry.CloseStarted() && hidden.CloseStarted()
	}, time.Second, time.Millisecond)
	select {
	case <-entry.CloseDone():
		t.Fatal("organization final shutdown ignored a hidden owner's live execution")
	default:
	}
	require.Zero(t, released[1].Load())
	require.Zero(t, resourceClosed.Load())
	lease.Release()
	select {
	case err := <-closed:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("declared owners waited on another owner's unstarted close")
	}
	require.Equal(t, int32(1), resourceClosed.Load())
	for index, owner := range []*agent.TagentAgent{child, entry, hidden} {
		select {
		case <-owner.CloseDone():
		default:
			t.Fatal("organization close returned before every owner finished its final exit")
		}
		require.Equal(t, int32(1), released[index].Load())
		require.Empty(t, owner.ContextManager().UnconvergedRefs())
	}
}
