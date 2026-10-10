// meditation_gate_coldstart_test 钉住空闲门对"从无回合记录"的取证：冷启动以管理器
// 启动时刻为忙的下界，而非永久关门。
//
// - 外部策展形态的回合源只有冥想自身：零值锚=先迈出第一步才有第一步，循环不可达。
// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
package agent

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestMeditationColdStart_FirstFireWithoutAnyTurn(t *testing.T) {
	reader := &fakeNoveltyReader{}
	reader.add("recall", time.Now().Add(-time.Minute), "user", "循环窗口里的一条真事")
	inj := &mockMessageInjector{}
	mgr := NewMeditationManager(MeditationConfig{
		Interval:           10 * time.Millisecond,
		MinGap:             time.Millisecond,
		PromptText:         "meditation",
		ObservedNamespaces: []string{"recall"},
	}, inj)
	mgr.SetNoveltyReader(reader)

	mgr.Start()
	time.Sleep(80 * time.Millisecond)
	mgr.Stop()

	assert.GreaterOrEqual(t, len(inj.messages), 1,
		"真实的外部策展冷启动（不播回合锚，回合唯一来源是冥想自身）：零值锚须回退启动时刻下界，不得永久关门")
}

func TestMeditationColdStart_StillWaitsMinGap(t *testing.T) {
	reader := &fakeNoveltyReader{}
	reader.add("recall", time.Now().Add(-time.Minute), "user", "循环窗口里的一条真事")
	inj := &mockMessageInjector{}
	mgr := NewMeditationManager(MeditationConfig{
		Interval:           time.Hour,
		MinGap:             time.Hour,
		PromptText:         "meditation",
		ObservedNamespaces: []string{"recall"},
	}, inj)
	mgr.SetNoveltyReader(reader)

	mgr.checkAndMeditate()

	assert.Zero(t, len(inj.messages), "cold start is treated as just-active: the first fire still waits out min_gap")
}
