// Package agentexample shows the notice feed the loop reads.
//
// 契约: docs/wiki/agent/agent-architecture.md#core-components
package agentexample

import (
	"fmt"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/task"
)

type demoFeed struct{}

func (demoFeed) NoticeBoundary(_ agent.RequestChain) (int, bool) {
	return 0, true
}

func (demoFeed) NoticesFrom(_ agent.RequestChain, _ event.EventID, cursor int) ([]agent.NoticeView, int, bool) {
	return nil, cursor, true
}

// ExampleNoticeFeed demonstrates the incremental notice read from the chain.
func ExampleNoticeFeed() {
	var feed agent.NoticeFeed = demoFeed{}
	_, pos, _ := feed.NoticesFrom(agent.RequestChain{}, "prefix", 0)
	fmt.Printf("read reached %d\n", pos)
	_ = task.Notice{TaskID: "task-1"}
}
