package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	a2aprotocol "trpc.group/trpc-go/trpc-a2a-go/protocol"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

var (
	// ErrToolTargetMissing reports a tool call that names no agent to address.
	// The call is refused rather than aimed at a default peer.
	ErrToolTargetMissing = errors.New("remote: the tool call names no agent")
	// ErrDispatcherMissing reports a tool constructed without a dispatcher.
	ErrDispatcherMissing = errors.New("remote: no dispatcher is attached")
)

// IdentityResolver reports the attribution identity of the call under way, so
// the dispatch it drives is attributed to the invocation, decision and native
// call that issued it.
type IdentityResolver func(ctx context.Context) (CallIdentity, bool)

// ToolOption configures the remote call tool at construction.
type ToolOption func(*Tool)

// WithToolName sets the declared name of the remote call tool.
func WithToolName(name string) ToolOption {
	return func(t *Tool) {
		if name != "" {
			t.name = name
		}
	}
}

// callArgs is the argument set of one remote call. The agent name and the
// message are the only fields: the destination of that agent comes from the
// caller's declaration and the attribution from the resolver, so neither is
// something the arguments can choose.
type callArgs struct {
	Agent   string `json:"agent"`
	Message string `json:"message"`
}

// 契约: docs/wiki/tool/tool-architecture.md#十七、委派调用的取消归属与重试形状
//
// Tool is the callable tool face of one explicitly addressed remote agent call:
// it reads the addressed agent from its arguments, the destination from the
// caller's declaration and the attribution from the resolver, then hands one
// message to the dispatcher.
type Tool struct {
	dispatcher Dispatcher
	endpoints  *Endpoints
	identity   IdentityResolver
	name       string
}

// NewTool returns the remote call tool over the given dispatcher, declaration
// and identity resolver.
func NewTool(dispatcher Dispatcher, endpoints *Endpoints, identity IdentityResolver, options ...ToolOption) (*Tool, error) {
	if dispatcher == nil {
		return nil, ErrDispatcherMissing
	}
	tool := &Tool{dispatcher: dispatcher, endpoints: endpoints, identity: identity, name: "remote_call"}
	for _, option := range options {
		option(tool)
	}
	return tool, nil
}

// Declaration returns the fixed declaration of the remote call tool.
func (t *Tool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{
		Name:        t.name,
		Description: "send one message to an explicitly named remote agent and answer with its reply; the application declares which agents may be addressed and no destination is chosen for a name it does not carry",
		InputSchema: &trpctool.Schema{
			Type: "object",
			Properties: map[string]*trpctool.Schema{
				"agent":   {Type: "string", Description: "the declared name of the remote agent to address"},
				"message": {Type: "string", Description: "the message text handed to that agent"},
			},
			Required: []string{"agent", "message"},
		},
	}
}

// Call sends the message to the explicitly named agent and answers with the
// text its reply carries. A name the caller's declaration does not carry, and a
// call beside no attribution, are refused by name.
func (t *Tool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	var args callArgs
	if err := json.Unmarshal(jsonArgs, &args); err != nil {
		return nil, fmt.Errorf("remote: the call arguments do not decode: %w", err)
	}
	if args.Agent == "" {
		return nil, ErrToolTargetMissing
	}
	target, err := t.endpoints.Lookup(args.Agent)
	if err != nil {
		return nil, err
	}
	identity := CallIdentity{}
	if t.identity != nil {
		identity, _ = t.identity(ctx)
	}
	params := a2aprotocol.SendMessageParams{
		Message: a2aprotocol.NewMessage(a2aprotocol.MessageRoleUser, []a2aprotocol.Part{a2aprotocol.NewTextPart(args.Message)}),
	}
	result, err := t.dispatcher.Send(ctx, target, identity, params)
	if err != nil {
		return nil, err
	}
	return ResultText(result), nil
}

// ResultText reports the text one remote call result carries: the message a
// remote agent answered with, or the message of the task status it reported. A
// result carrying no text reports an empty string rather than a rendering of
// its own.
func ResultText(result *a2aprotocol.MessageResult) string {
	if result == nil || result.Result == nil {
		return ""
	}
	switch answer := result.Result.(type) {
	case *a2aprotocol.Message:
		return messageText(answer)
	case *a2aprotocol.Task:
		if answer.Status.Message == nil {
			return ""
		}
		return messageText(answer.Status.Message)
	}
	return ""
}

// messageText joins the text parts of one message in the order the message
// carries them.
func messageText(message *a2aprotocol.Message) string {
	var text string
	for _, part := range message.Parts {
		switch typed := part.(type) {
		case *a2aprotocol.TextPart:
			text += typed.Text
		case a2aprotocol.TextPart:
			text += typed.Text
		}
	}
	return text
}

var _ trpctool.CallableTool = (*Tool)(nil)
