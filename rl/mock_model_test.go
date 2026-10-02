package rl

import (
	"context"

	"trpc.group/trpc-go/trpc-agent-go/model"
)

// mockModel 是 model.Model 的测试替身：未配置 responses 时回一条带模型名的默认响应；
// 设置了 err 则直接失败。本文件只放桩件，用例见同包其他测试。
//
// 契约: docs/wiki/rl/rl-architecture.md#http-api
type mockModel struct {
	info      model.Info
	responses []*model.Response
	err       error
}

func (m *mockModel) GenerateContent(ctx context.Context, request *model.Request) (<-chan *model.Response, error) {
	ch := make(chan *model.Response, 1)
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
