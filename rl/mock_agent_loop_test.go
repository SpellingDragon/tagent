package rl

import (
	"sync"

	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// mockAgentLoop is a mock implementation of AgentLoop for testing.
type mockAgentLoop struct {
	mu           sync.Mutex
	messages     []model.Message
	loopActive   bool
	sources      []string
	startCalled  int
	stopCalled   int
}

func (m *mockAgentLoop) InjectMessage(msg model.Message) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, msg)
}

func (m *mockAgentLoop) InjectMessageWithSource(source string, msg model.Message) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, msg)
	m.sources = append(m.sources, source)
}

func (m *mockAgentLoop) StartLoop(userID, sessionID string) (<-chan *event.Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.startCalled++
	m.loopActive = true
	ch := make(chan *event.Event)
	close(ch)
	return ch, nil
}

func (m *mockAgentLoop) StopLoop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopCalled++
	m.loopActive = false
}

func (m *mockAgentLoop) IsLoopActive() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.loopActive
}
