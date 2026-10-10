// meditation_gate_coldstart_test 钉住 冥想门控的冷启动与重启形态：执行水位为零（首次运行或锚文件缺失）时节奏门直通，
// 第一个 tick 即完成存量通读；锚文件恢复出非零水位则按执行间下限正常判定。
//
// - 外部策展形态的回合源只有冥想自身：没有前一次执行就没有区间可量，任何以启动时刻或回合锚为下界的写法都会让门永久关死。
// - 直通只免掉节奏门：新鲜度门照常把关，观察面为空、没接事实链、读取失败都不开。
// - 重启形态由水位本身区分：非零锚=正常节奏，缺失锚=直通一次，消费后各自结账。
// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/reliability"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMeditationGate_ColdStartFirstTickFires 钉住 冷启动的第一个 tick 就触发（ticker 驱动，无回合锚、无执行史）。
// - 存量事实比 min_gap 还老，等的必须是新鲜度而不是间隔。
// - 触发不等于执行：水位直到批被消费仍为零。
func TestMeditationGate_ColdStartFirstTickFires(t *testing.T) {
	reader := &fakeNoveltyReader{}
	reader.add("recall", time.Now().Add(-time.Minute), "user", "循环窗口里的一条真事")
	inj := &mockMessageInjector{}
	mgr := NewMeditationManager(MeditationConfig{
		Interval:           10 * time.Millisecond,
		MinGap:             time.Hour,
		PromptText:         "meditation",
		ObservedNamespaces: []string{"recall"},
	}, inj)
	mgr.SetNoveltyReader(reader)

	mgr.Start()
	time.Sleep(80 * time.Millisecond)
	mgr.Stop()

	assert.GreaterOrEqual(t, len(inj.messages), 1,
		"零水位的节奏门直通：外部策展形态的第一次反思不需要任何前置回合")
	if len(inj.messages) > 0 {
		assert.Zero(t, mgr.lastMeditation.Load(), "触发只注入，执行水位靠消费推进")
	}
}

// TestMeditationGate_ColdStartWithoutNoveltyStaysClosed 钉住 直通免的是节奏门，新鲜度门照常把关。
// - 观察面上只有自管产出：冷启动也无可反思之事。
// - 水位为零不构成“有新鲜度”的证据。
func TestMeditationGate_ColdStartWithoutNoveltyStaysClosed(t *testing.T) {
	reader := &fakeNoveltyReader{}
	reader.add("recall", time.Now().Add(-time.Minute), "meditation", "自管产出")
	inj := &mockMessageInjector{}
	mgr := NewMeditationManager(MeditationConfig{
		MinGap:             time.Hour,
		PromptText:         "meditation",
		ObservedNamespaces: []string{"recall"},
	}, inj)
	mgr.SetNoveltyReader(reader)

	for i := 0; i < 3; i++ {
		mgr.checkAndMeditate()
	}

	assert.Empty(t, inj.messages, "观察面上没有非自管事件，直通也开不了门")
	assert.Zero(t, mgr.lastMeditation.Load())
}

// TestMeditationGate_RestoredAnchorResumesRhythm 钉住 重启恢复非零水位即按执行间下限判定。
// - 锚里刚执行过（30min 前）+ min_gap=1h：本轮不开，哪怕新鲜度已点亮。
// - 锚里执行于 2h 前：下限已过，同一份新鲜度照常触发。
func TestMeditationGate_RestoredAnchorResumesRhythm(t *testing.T) {
	path := filepath.Join(t.TempDir(), "anchors.json")
	writeAnchor(t, path, time.Now().Add(-30*time.Minute).UnixMilli())
	mgr, inj := coldStartManager(path)

	mgr.checkAndMeditate()
	assert.Empty(t, inj.messages, "重启后按 min_gap 的正常节奏判定，不是无条件直通")

	writeAnchor(t, path, time.Now().Add(-2*time.Hour).UnixMilli())
	mgr2, inj2 := coldStartManager(path)
	mgr2.checkAndMeditate()
	assert.Len(t, inj2.messages, 1, "锚里的执行时刻足够老，下限已过即触发")
}

// TestMeditationGate_MissingAnchorPassesThroughOnce 钉住 锚文件缺失=零水位直通一次，消费后按水位自锁。
// - 首次启动没有历史锚也不阻断：直通就是这个缺失形态的唯一正确结论。
// - 消费落盘后重启，恢复出的水位接管的仍是节奏判定。
func TestMeditationGate_MissingAnchorPassesThroughOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "anchors.json")
	mgr, inj := coldStartManager(path)

	mgr.checkAndMeditate()
	require.Len(t, inj.messages, 1, "没有锚文件就是零水位，直通节奏门")
	injectAt := mgr.pendingSince
	mgr.NoteMeditationBatchOutcome(true)
	assert.Equal(t, injectAt, mgr.lastMeditation.Load())

	restarted, inj2 := coldStartManager(path)
	assert.Equal(t, injectAt, restarted.lastMeditation.Load(), "执行水位跨重启恢复")
	restarted.checkAndMeditate()
	assert.Empty(t, inj2.messages, "恢复的水位之后没有新事，重启不会重复通读")
}

func coldStartManager(path string) (*MeditationManager, *mockMessageInjector) {
	reader := &fakeNoveltyReader{}
	reader.add("recall", time.Now().Add(-time.Minute), "user", "等着被反思的存量事实")
	inj := &mockMessageInjector{}
	mgr := NewMeditationManager(MeditationConfig{
		MinGap:             time.Hour,
		PromptText:         "meditation",
		ObservedNamespaces: []string{"recall"},
	}, inj)
	mgr.SetNoveltyReader(reader)
	as, err := reliability.NewAnchorStore(path)
	if err == nil {
		mgr.SetAnchorStore(as)
	}
	return mgr, inj
}

func writeAnchor(t *testing.T, path string, lastMeditation int64) {
	t.Helper()
	require.NoError(t, os.WriteFile(path,
		[]byte(`{"last_meditation":`+itoa(lastMeditation)+`}`), 0o644))
}
