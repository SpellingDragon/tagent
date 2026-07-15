package rl

import (
	"context"

	"trpc.group/trpc-go/trpc-agent-go/model"
)

// mockModel is a simple mock implementation of model.Model for testing.
type mockModel struct {
	info      model.Info
	responses []*model.Response
	err       error
}

func (m *mockModel) GenerateContent(ctx context.Context, request *model.Request) (<-chan *model.Response, error) {
	ch := make(chan *model.Response, 1)
	// If no responses configured, send a default response with the model name
	if len(m.responses) == 0 {
		ch <- &model.Response{
			Model: m.info.Name,
			Choices: []model.Choice{
				{
					Index: 0,
					Message: model.Message{
						Role:    model.RoleAssistant,
						Content: "mock response",
					},
				},
			},
		}
	} else {
		for _, resp := range m.responses {
			ch <- resp
		}
	}
	close(ch)
	if m.err != nil {
		return nil, m.err
	}
	return ch, nil
}

func (m *mockModel) Info() model.Info {
	return m.info
}
