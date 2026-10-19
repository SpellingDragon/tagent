package remote

import (
	"context"

	a2aclient "trpc.group/trpc-go/trpc-a2a-go/client"
	a2aprotocol "trpc.group/trpc-go/trpc-a2a-go/protocol"
)

// 契约: docs/wiki/tool/tool-architecture.md#十七、委派调用的取消归属与重试形状
//
// Skeleton compile-time assertions for the phase 1 adapter declarations in
// adapter.go.
//
// The phase 2 implementation types that will satisfy these interfaces do not
// exist yet, so there is no `var _ Iface = (*Impl)(nil)` to write. The
// assertions below pin what the skeleton proves on its own: every declared
// method keeps its exact shape, every declared name is an interface type, and
// the native result union still admits both shapes a remote call returns. A
// renamed method, a changed parameter or a changed result stops the method shape
// assertions from compiling.
var (
	_ func(Connector, context.Context, Target, ...a2aclient.Option) (*a2aclient.A2AClient, error)                                                            = Connector.Connect
	_ func(SessionBinder, context.Context, Target) (SessionScope, error)                                                                                     = SessionBinder.Bind
	_ func(Dispatcher, context.Context, Target, CallIdentity, a2aprotocol.SendMessageParams, ...a2aclient.RequestOption) (*a2aprotocol.MessageResult, error) = Dispatcher.Send
)

// The handoff carriage keeps its declared shape: the consumer-side seam naming
// the acceptance and the part, and the send delivering it under the identity and
// target of the call.
var (
	_ HandoffCarrier = (HandoffCarrier)(nil)

	_ func(*A2ADispatcher, context.Context, Target, CallIdentity, HandoffCarrier, a2aprotocol.SendMessageParams, ...a2aclient.RequestOption) (*a2aprotocol.MessageResult, error) = (*A2ADispatcher).SendHandoff

	_ error = ErrHandoffMissing
)

// Interface kinds, plus the native types the adapter hands back to its callers.
var (
	_ Connector     = (Connector)(nil)
	_ SessionBinder = (SessionBinder)(nil)
	_ Dispatcher    = (Dispatcher)(nil)

	_ a2aprotocol.UnaryMessageResult = (*a2aprotocol.Message)(nil)
	_ a2aprotocol.UnaryMessageResult = (*a2aprotocol.Task)(nil)
	_                                = (*a2aclient.A2AClient)(nil)
)
