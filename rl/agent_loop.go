// Package rl provides reinforcement learning utilities for tagent agents.
//
// This package contains components for:
// - Recording agent trajectories for offline training
// - Swapping model instances at runtime
// - HTTP API for external RL systems (AReaL)
//
// The AgentLoop interface decouples rl/ from agent/, allowing HTTPAPI
// to interact with TagentAgent without importing the agent package.
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
