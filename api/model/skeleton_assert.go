package model

import trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"

// 契约: docs/wiki/agent/agent-architecture.md#framework-boundary
//
// This file holds compile-time assertions only. It proves that the aliases
// declared in aliases.go are thin, unwrapped re-exports of the locked tRPC model
// package rather than wrappers or distinct defined types.
//
// A pointer assignment type-checks only when the alias is identical to its
// native type: a distinct defined type or a wrapper struct would require an
// explicit conversion and would fail to assign. These assertions therefore fail
// the build if a later edit turns an alias into a wrapper.
var (
	_ *trpcmodel.Request                 = (*Request)(nil)
	_ *trpcmodel.Response                = (*Response)(nil)
	_ *trpcmodel.Message                 = (*Message)(nil)
	_ *trpcmodel.ToolCall                = (*ToolCall)(nil)
	_ *trpcmodel.FunctionDefinitionParam = (*FunctionDefinitionParam)(nil)
	_ *trpcmodel.Choice                  = (*Choice)(nil)
	_ *trpcmodel.Usage                   = (*Usage)(nil)
	_ *trpcmodel.TimingInfo              = (*TimingInfo)(nil)
	_ *trpcmodel.ResponseError           = (*ResponseError)(nil)
	_ *trpcmodel.ContentPart             = (*ContentPart)(nil)
	_ *trpcmodel.Info                    = (*Info)(nil)
)

// The interface aliases are asserted by mutual satisfiability, because an
// interface value carries no pointer identity.
var (
	_ trpcmodel.Model     = (Model)(nil)
	_ Model               = (trpcmodel.Model)(nil)
	_ trpcmodel.IterModel = (IterModel)(nil)
	_ IterModel           = (trpcmodel.IterModel)(nil)
)

// IterModel embeds Model, so every IterModel value satisfies Model.
var _ Model = (IterModel)(nil)

// Seq is a generic alias; these assertions check that the type parameters and
// the element type resolve to the native definitions.
var (
	_ trpcmodel.Seq[*trpcmodel.Response] = (Seq[*Response])(nil)
	_ Seq[*Response]                     = (trpcmodel.Seq[*trpcmodel.Response])(nil)
)

// The role constants are the native constants.
var (
	_ Role = RoleSystem
	_ Role = RoleUser
	_ Role = RoleAssistant
	_ Role = RoleTool
)
