// 契约: docs/wiki/agent/agent-architecture.md#framework-boundary
//
// Package model is the runtime's single entry point to the locked tRPC model
// protocol.
//
// Every exported name in this package is a thin, unwrapped alias of the
// corresponding symbol in the locked framework package
// trpc.group/trpc-go/trpc-agent-go/model, so one import gives the runtime the
// native model, request, response and message types without building a mirror
// schema or a second tool loop. The aliases preserve the native method sets and
// field layouts exactly: this package adds no wrapper types and no behaviour of
// its own, and no alias copies or reshapes a native field.
package model

import trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"

// Model is an alias for the locked tRPC model interface, which streams responses
// over a channel and reports system-level failures through its error return.
type Model = trpcmodel.Model

// IterModel is an alias for the locked tRPC iterator extension of Model, which
// streams responses in the caller goroutine instead of over a channel.
type IterModel = trpcmodel.IterModel

// Seq is an alias for the locked tRPC callback-based response sequence yielded by
// IterModel.GenerateContentIter.
type Seq[T any] = trpcmodel.Seq[T]

// Info is an alias for the locked tRPC model descriptor returned by Model.Info.
type Info = trpcmodel.Info

// Request is an alias for the locked tRPC model request, which carries the native
// conversation history and generation configuration.
type Request = trpcmodel.Request

// Response is an alias for the locked tRPC model response, which is the unit of
// the single terminal reduction performed by this package.
type Response = trpcmodel.Response

// Message is an alias for the locked tRPC conversation message, preserving the
// native role, content parts, tool calls and reasoning fields.
type Message = trpcmodel.Message

// Role is an alias for the locked tRPC message author role.
type Role = trpcmodel.Role

// RoleSystem, RoleUser, RoleAssistant and RoleTool are aliases for the locked
// tRPC message author roles.
const (
	// RoleSystem is the system role.
	RoleSystem = trpcmodel.RoleSystem
	// RoleUser is the user role.
	RoleUser = trpcmodel.RoleUser
	// RoleAssistant is the assistant role.
	RoleAssistant = trpcmodel.RoleAssistant
	// RoleTool is the tool response role.
	RoleTool = trpcmodel.RoleTool
)

// ToolCall is an alias for the locked tRPC tool call carried by an assistant
// message, preserving the native call identifier and function arguments.
type ToolCall = trpcmodel.ToolCall

// FunctionDefinitionParam is an alias for the locked tRPC function definition and
// JSON-encoded arguments of a ToolCall.
type FunctionDefinitionParam = trpcmodel.FunctionDefinitionParam

// Choice is an alias for the locked tRPC completion choice held by a Response.
type Choice = trpcmodel.Choice

// Usage is an alias for the locked tRPC token usage record carried by a Response,
// including the usage tail packet that ends a stream.
type Usage = trpcmodel.Usage

// ResponseError is an alias for the locked tRPC API-level error carried by a
// Response.
type ResponseError = trpcmodel.ResponseError

// ContentPart is an alias for the locked tRPC multimodal content part held by a
// Message.
type ContentPart = trpcmodel.ContentPart
