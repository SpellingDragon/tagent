package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// ============================================================================
// parseValuationResponse tests
// ============================================================================

func TestParseValuationResponse_ValidJSONWithSummary(t *testing.T) {
	response := `[{"event_key":123,"value_score":0.9,"processing":"keep","key_facts":"用户原始需求","reason":"高价值"}]
--- BATCH SUMMARY ---
这是一个关于用户需求的批次摘要。`

	values, summary, err := parseValuationResponse(response)
	require.NoError(t, err)
	require.Len(t, values, 1)
	assert.Equal(t, int64(123), values[0].EventKey)
	assert.Equal(t, 0.9, values[0].ValueScore)
	assert.Equal(t, Keep, values[0].Processing)
	assert.Equal(t, "用户原始需求", values[0].KeyFacts)
	assert.Equal(t, "高价值", values[0].Reason)
	assert.Equal(t, "这是一个关于用户需求的批次摘要。", summary)
}

func TestParseValuationResponse_MultipleValues(t *testing.T) {
	response := `[
{"event_key":1,"value_score":0.9,"processing":"keep","key_facts":"关键需求"},
{"event_key":2,"value_score":0.3,"processing":"summary","key_facts":"中间推理"},
{"event_key":3,"value_score":0.0,"processing":"drop","key_facts":""}
]`

	values, _, err := parseValuationResponse(response)
	require.NoError(t, err)
	require.Len(t, values, 3)
	assert.Equal(t, Keep, values[0].Processing)
	assert.Equal(t, Summary, values[1].Processing)
	assert.Equal(t, Drop, values[2].Processing)
}

func TestParseValuationResponse_MissingProcessingDefaultsToSummary(t *testing.T) {
	response := `[{"event_key":123,"value_score":0.9}]`

	values, _, err := parseValuationResponse(response)
	require.NoError(t, err)
	require.Len(t, values, 1)
	assert.Equal(t, Summary, values[0].Processing, "missing processing should default to Summary")
	assert.Equal(t, "", values[0].KeyFacts, "missing key_facts should default to empty")
}

func TestParseValuationResponse_InvalidProcessingDefaultsToSummary(t *testing.T) {
	response := `[{"event_key":123,"value_score":0.5,"processing":"unknown_strategy"}]`

	values, _, err := parseValuationResponse(response)
	require.NoError(t, err)
	require.Len(t, values, 1)
	assert.Equal(t, Summary, values[0].Processing)
}

func TestParseValuationResponse_MalformedJSON(t *testing.T) {
	response := `这不是有效的 JSON`

	_, _, err := parseValuationResponse(response)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no JSON array found")
}

func TestParseValuationResponse_JSONWrappedInText(t *testing.T) {
	response := `以下是评估结果：
[{"event_key":42,"value_score":0.7,"processing":"keyfacts","key_facts":"关键发现"}]
--- BATCH SUMMARY ---
批次总结`

	values, summary, err := parseValuationResponse(response)
	require.NoError(t, err)
	require.Len(t, values, 1)
	assert.Equal(t, int64(42), values[0].EventKey)
	assert.Equal(t, KeyFacts, values[0].Processing)
	assert.Equal(t, "批次总结", summary)
}

func TestParseValuationResponse_ValueScoreClamped(t *testing.T) {
	response := `[{"event_key":1,"value_score":1.5,"processing":"keep"},{"event_key":2,"value_score":-0.3,"processing":"drop"}]`

	values, _, err := parseValuationResponse(response)
	require.NoError(t, err)
	require.Len(t, values, 2)
	assert.Equal(t, 1.0, values[0].ValueScore, "value > 1 should be clamped to 1.0")
	assert.Equal(t, 0.0, values[1].ValueScore, "value < 0 should be clamped to 0.0")
}

// ============================================================================
// extractJSONArray tests
// ============================================================================

func TestExtractJSONArray_Simple(t *testing.T) {
	result := extractJSONArray(`[{"a":1}]`)
	assert.Equal(t, `[{"a":1}]`, result)
}

func TestExtractJSONArray_NestedBrackets(t *testing.T) {
	result := extractJSONArray(`text [{"a":[1,2]}, {"b":3}] more text`)
	assert.Equal(t, `[{"a":[1,2]}, {"b":3}]`, result)
}

func TestExtractJSONArray_NoArray(t *testing.T) {
	result := extractJSONArray(`no json here`)
	assert.Equal(t, "", result)
}

// ============================================================================
// applyValueFloors tests
// ============================================================================

func TestApplyValueFloors_ClampsBelowFloor(t *testing.T) {
	v := &LLMEventValuator{
		config: ValuationConfig{
			ValueFloors: map[string]float64{
				"external_input": 0.5,
				"agent_output":   0.4,
			},
		},
	}

	segments := []*TaskSegment{
		{Messages: []model.Message{{Role: model.RoleUser, Content: "[evt_100|external_input] hello"}}},
		{Messages: []model.Message{{Role: model.RoleAssistant, Content: "[evt_101|agent_output] world"}}},
		{Messages: []model.Message{{Role: model.RoleTool, Content: "[evt_102|action_tool] result"}}},
	}
	values := []EventValue{
		{EventKey: 100, ValueScore: 0.1}, // external_input floor = 0.5
		{EventKey: 101, ValueScore: 0.1}, // agent_output floor = 0.4
		{EventKey: 102, ValueScore: 0.1}, // no floor
	}

	v.applyValueFloors(segments, values)

	assert.Equal(t, 0.5, values[0].ValueScore, "external_input should be clamped to 0.5")
	assert.Equal(t, 0.4, values[1].ValueScore, "agent_output should be clamped to 0.4")
	assert.Equal(t, 0.1, values[2].ValueScore, "action_tool has no floor, should stay 0.1")
}

func TestApplyValueFloors_AboveFloorUnchanged(t *testing.T) {
	v := &LLMEventValuator{
		config: ValuationConfig{
			ValueFloors: map[string]float64{
				"external_input": 0.5,
			},
		},
	}

	segments := []*TaskSegment{
		{Messages: []model.Message{{Role: model.RoleUser, Content: "[evt_100|external_input] hello"}}},
	}
	values := []EventValue{
		{EventKey: 100, ValueScore: 0.9},
	}

	v.applyValueFloors(segments, values)
	assert.Equal(t, 0.9, values[0].ValueScore, "value above floor should not be changed")
}

// ============================================================================
// noopValuator tests
// ============================================================================

func TestNoopValuator_ReturnsDefaultValues(t *testing.T) {
	v := NewNoopValuator()
	segments := []*TaskSegment{
		{Messages: []model.Message{{Role: model.RoleUser, Content: "[evt_100|external_input] hello"}}},
		{Messages: []model.Message{{Role: model.RoleAssistant, Content: "[evt_101|agent_output] reply"}}},
	}

	values, summary, err := v.Evaluate(context.Background(), segments)
	require.NoError(t, err)
	assert.Equal(t, "", summary)
	require.Len(t, values, 2)
	assert.Equal(t, int64(100), values[0].EventKey)
	assert.Equal(t, int64(101), values[1].EventKey)
	assert.Equal(t, 0.5, values[0].ValueScore)
	assert.Equal(t, Summary, values[0].Processing)
}

// ============================================================================
// clamp tests
// ============================================================================

func TestClamp(t *testing.T) {
	assert.Equal(t, 0.5, clamp(0.5, 0.0, 1.0))
	assert.Equal(t, 0.0, clamp(-1.0, 0.0, 1.0))
	assert.Equal(t, 1.0, clamp(2.0, 0.0, 1.0))
}
