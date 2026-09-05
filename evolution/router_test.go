package evolution

import "testing"

// TestDiffRiskRouter 验证发布道路由（T-E/T-F 调和判据）：模型/参数变更→慢道，仅提示词→快道。
func TestDiffRiskRouter(t *testing.T) {
	r := NewDiffRiskRouter()
	cases := []struct {
		name string
		diff BundleDiff
		want Lane
	}{
		{"仅提示词改→快道", BundleDiff{PromptsChanged: []string{"system"}}, LaneFast},
		{"提示词增→快道", BundleDiff{PromptsAdded: []string{"new"}}, LaneFast},
		{"提示词删→快道", BundleDiff{PromptsRemoved: []string{"old"}}, LaneFast},
		{"模型改→慢道", BundleDiff{ModelChanged: true}, LaneSlow},
		{"参数改→慢道", BundleDiff{ParamsChanged: true}, LaneSlow},
		{"模型+提示词→慢道", BundleDiff{ModelChanged: true, PromptsChanged: []string{"system"}}, LaneSlow},
		{"参数+提示词→慢道", BundleDiff{ParamsChanged: true, PromptsChanged: []string{"system"}}, LaneSlow},
		{"空diff→快道", BundleDiff{}, LaneFast},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := r.Route(tc.diff); got != tc.want {
				t.Errorf("Route=%s 期望 %s", got, tc.want)
			}
		})
	}
}

// TestDiffRiskRouter_ImplementsRiskRouter 验证 DiffRiskRouter 满足 RiskRouter 接口（编译期契约）。
func TestDiffRiskRouter_ImplementsRiskRouter(t *testing.T) {
	var _ RiskRouter = NewDiffRiskRouter()
}
