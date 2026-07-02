package agent

import (
	"context"
	"sync"

	"trpc.group/trpc-go/trpc-agent-go/model"
)

// mockModel records the request it receives and returns a preset response.
// This shared mock is used by both package tests and PoC tests.
type recordableMockModel struct {
	mu          sync.Mutex
	lastRequest *model.Request // captured request for verification
	response    *model.Response
}

func newRecordableMockModel(response *model.Response) *recordableMockModel {
	return &recordableMockModel{response: response}
}

func (m *recordableMockModel) GenerateContent(
	ctx context.Context,
	request *model.Request,
) (<-chan *model.Response, error) {
	m.mu.Lock()
	m.lastRequest = request
	m.mu.Unlock()

	ch := make(chan *model.Response, 1)
	if m.response != nil {
		ch <- m.response
	}
	close(ch)
	return ch, nil
}

func (m *recordableMockModel) Info() model.Info {
	return model.Info{Name: "mock-model"}
}

func (m *recordableMockModel) GetLastRequest() *model.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastRequest
}

// multiCallModel returns responses in sequence for tests that need
// multiple model invocations (e.g., tool call followed by final response).
type sequenceMockModel struct {
	responses []*model.Response
	callCount *int
	mu        sync.Mutex
}

func (m *sequenceMockModel) GenerateContent(
	ctx context.Context,
	request *model.Request,
) (<-chan *model.Response, error) {
	m.mu.Lock()
	idx := *m.callCount
	*m.callCount++
	var resp *model.Response
	if idx < len(m.responses) {
		resp = m.responses[idx]
	}
	m.mu.Unlock()

	ch := make(chan *model.Response, 1)
	if resp != nil {
		ch <- resp
	}
	close(ch)
	return ch, nil
}

func (m *sequenceMockModel) Info() model.Info {
	return model.Info{Name: "multi-call-model"}
}
