package agent

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// R06／§2.1（D2「不可变执行配置与受限运行句柄」）契约测。
//
// 缺陷：ExecutorConfig() 直接 return cm.execCfg（浅拷贝）。返回的 Tools 与在线面
// 共享 backing array、Thinking/Reasoning 三个值指针共享同一个被指对象。调用方
// 于是可以「绕过发布」原地改写在线执行面：face := cm.ExecutorConfig();
// face.Tools[0] = evil 或 *face.ThinkingEnabled = false 都会写穿到 cm.execCfg。
//
// 合同要求：getter 与发布入口都返回/持有**私有可变面**（新 Tools 容器 + 独立值指
// 针），而真实 model/store/session/tool 运行对象按身份复用、不盲目深拷贝。修改构造
// 输入或 getter 返回值后，真实声明、目标解析与在线执行不变。

// declarationNames lists what a tool slice advertises (the model-visible surface).
func declarationNames(tools []trpctool.Tool) []string {
	var out []string
	for _, tl := range tools {
		if tl == nil {
			continue
		}
		if d := tl.Declaration(); d != nil {
			out = append(out, d.Name)
		}
	}
	return out
}

// A：getter 返回的 Tools 容器私有——覆盖元素不得写穿在线面，在线执行仍见原工具。
func TestExecutorConfig_ToolsContainerIsPrivate(t *testing.T) {
	capture := &requestCapturingModel{resp: gateOKResp()}
	keep := &pinEchoTool{name: "keep"}
	cm := newTestContextManager("iso-tools", capture, []trpctool.Tool{keep}, make(chan *event.Event, 16), nil)

	out, err := cm.currentRunner().Run(context.Background(), "u1", "s1", model.NewUserMessage("first"))
	require.NoError(t, err)
	drainRunner(out, 5*time.Second)
	require.Equal(t, []string{"keep"}, lastRequestToolNames(capture), "precondition: gen1 advertises keep")

	face := cm.ExecutorConfig()
	require.Len(t, face.Tools, 1)
	face.Tools[0] = &pinEchoTool{name: "evil"} // write through the returned snapshot

	require.Same(t, keep, cm.ExecutorConfig().Tools[0],
		"the published face must keep its original tool element (getter must not alias the live Tools array)")
	require.Equal(t, []string{"keep"}, declarationNames(cm.ExecutorConfig().Tools),
		"and still advertise the original declaration")

	out2, err := cm.currentRunner().Run(context.Background(), "u1", "s1", model.NewUserMessage("second"))
	require.NoError(t, err)
	drainRunner(out2, 5*time.Second)
	require.Equal(t, []string{"keep"}, lastRequestToolNames(capture),
		"online execution must be unaffected by mutating a returned snapshot")
}

// B：getter 返回的三个值指针独立——解引用写不得触及在线配置。
func TestExecutorConfig_ValuePointersArePrivate(t *testing.T) {
	capture := &requestCapturingModel{resp: gateOKResp()}
	cm := newTestContextManager("iso-vals", capture, nil, make(chan *event.Event, 16), nil)
	on, tok, eff := true, 128, "high"
	f0 := cm.ExecutorConfig()
	f0.ThinkingEnabled, f0.ThinkingTokens, f0.ReasoningEffort = &on, &tok, &eff
	cm.PublishExecutor(cm.NewExecutorCandidate(f0), f0)

	face := cm.ExecutorConfig()
	require.NotNil(t, face.ThinkingEnabled)
	require.NotNil(t, face.ThinkingTokens)
	require.NotNil(t, face.ReasoningEffort)
	*face.ThinkingEnabled = false
	*face.ThinkingTokens = 999
	*face.ReasoningEffort = "low"

	live := cm.ExecutorConfig()
	require.True(t, *live.ThinkingEnabled, "getter must not leak the live ThinkingEnabled pointee")
	require.Equal(t, 128, *live.ThinkingTokens, "nor ThinkingTokens")
	require.Equal(t, "high", *live.ReasoningEffort, "nor ReasoningEffort")
}

// C：发布对调用方输入取私有副本——发布后调用方继续改自己的输入不得影响在线面。
func TestPublishExecutor_DoesNotAliasCallerInput(t *testing.T) {
	capture := &requestCapturingModel{resp: gateOKResp()}
	cm := newTestContextManager("pub-alias", capture, nil, make(chan *event.Event, 16), nil)
	keep := &pinEchoTool{name: "keep"}
	input := cm.ExecutorConfig()
	input.Tools = []trpctool.Tool{keep}
	cm.PublishExecutor(cm.NewExecutorCandidate(input), input)

	input.Tools[0] = &pinEchoTool{name: "evil"} // caller mutates its own input after publish
	require.Same(t, keep, cm.ExecutorConfig().Tools[0],
		"publish must take a private copy of the caller's input face, not alias it")
}
