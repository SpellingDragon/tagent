// Package rl provides reinforcement learning utilities for tagent agents.
//
// - Components: trajectory recording for offline training, runtime model swapping, and the HTTP API for external RL systems such as AReaL.
// - The AgentLoop interface decouples rl from agent so the HTTP API can drive a TagentAgent without importing the agent package.
// 契约: docs/wiki/rl/rl-architecture.md#http-api
package rl

import (
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// AgentLoop is the interface that decouples rl/ from agent/.
// It defines the minimal contract needed by HTTPAPI to interact
// with a TagentAgent instance.
type AgentLoop interface {
	InjectMessage(msg model.Message)
	InjectMessageWithSource(source string, msg model.Message)
	StartLoop(userID, sessionID string) (<-chan *event.Event, error)
	StopLoop()
	IsLoopActive() bool
}
