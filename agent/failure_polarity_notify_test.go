package agent

import (
	"fmt"
	"strings"
	"testing"

	"github.com/SpellingDragon/tagent/agent/task"
	"github.com/stretchr/testify/require"
)

func fpTask(id, desc string) *task.Task {
	return &task.Task{ID: id, Spec: task.TaskSpec{Kind: "command", Desc: desc}}
}

// TestSettleNotify_ExitCodePassthrough 钉住 D2：非零退出码透传到通知文本。
func TestSettleNotify_ExitCodePassthrough(t *testing.T) {
	evt := newTaskSettledEvent(fpTask("t42", "build"),
		task.SettleSignal{Kind: task.SettleCompleted, Err: fmt.Errorf("tmux session x exited with code 42"), ExitCode: 42},
		100000, "")
	require.Contains(t, evt.Message.Content, "✗", "失败极性 marker")
	require.Contains(t, evt.Message.Content, "exit_code=42")
	require.Contains(t, evt.Message.Content, "exited with code 42")
}

// TestSettleNotify_SignalDeathFormatted 钉住信号死负码格式化为 `-15 (signal)`。
func TestSettleNotify_SignalDeathFormatted(t *testing.T) {
	evt := newTaskSettledEvent(fpTask("tsig", "job"),
		task.SettleSignal{Kind: task.SettleCompleted, Err: fmt.Errorf("tmux session x exited with code -15"), ExitCode: -15},
		100000, "")
	require.Contains(t, evt.Message.Content, "✗")
	require.Contains(t, evt.Message.Content, "exit_code=-15 (signal)")
}

// TestSettleNotify_UnresolvableNoMisleadingCode 钉住探测失明：Err 携带但无假 exit_code=0；仍失败极性。
func TestSettleNotify_UnresolvableNoMisleadingCode(t *testing.T) {
	evt := newTaskSettledEvent(fpTask("tunres", "job"),
		task.SettleSignal{Kind: task.SettleCompleted, Err: fmt.Errorf("tmux session x entered error state (exit status unresolvable)"), ExitCode: 0},
		100000, "")
	require.Contains(t, evt.Message.Content, "✗", "框架失明不得伪装成功")
	require.NotContains(t, evt.Message.Content, "exit_code=0", "不可辨不得输出误导的 exit_code=0")
	require.Contains(t, evt.Message.Content, "unresolvable")
}

// TestSettleNotify_SuccessZeroNoCode 钉住干净退出（code 0、无 Err）：✓ completed，无 exit_code 噪声。
func TestSettleNotify_SuccessZeroNoCode(t *testing.T) {
	evt := newTaskSettledEvent(fpTask("tok", "echo hi"),
		task.SettleSignal{Kind: task.SettleCompleted, Output: "hi", ExitCode: 0},
		100000, "")
	require.Contains(t, evt.Message.Content, "✓")
	require.NotContains(t, evt.Message.Content, "exit_code")
}

// TestSettleNotify_BlankPayloadDegradToTicket 钉住 D5：纯空白载荷降级为单行票据，不投递空白正文。
func TestSettleNotify_BlankPayloadDegradToTicket(t *testing.T) {
	blank := strings.Repeat("\n", 25)
	evt := newTaskSettledEvent(fpTask("tblank", "quiet svc"),
		task.SettleSignal{Kind: task.SettleCompleted, Output: blank},
		100000, t.TempDir())
	content := evt.Message.Content
	// 无空白正文：不应出现被转义的 ␤ 空白串（25 个换行 → ␤×25）。
	require.NotContains(t, content, "␤␤", "空白正文必须降级，不得投递")
	require.Contains(t, content, "（无输出）", "降级为单行无输出票据")
	// 单行轨迹：不含真实换行（空白正文本会带来多行）。
	require.NotContains(t, content, "\n", "票据保持单行")
}

// TestSettleNotify_ErrWithBlankKeepsErrLine 钉住带 Err + 空输出：Err 行保留，结果段省略。
func TestSettleNotify_ErrWithBlankKeepsErrLine(t *testing.T) {
	evt := newTaskSettledEvent(fpTask("terr", "failed job"),
		task.SettleSignal{Kind: task.SettleCompleted, Err: fmt.Errorf("exit code 1"), ExitCode: 1},
		100000, "")
	require.Contains(t, evt.Message.Content, "错误: exit code 1")
	require.Contains(t, evt.Message.Content, "exit_code=1")
	require.NotContains(t, evt.Message.Content, "（无输出）", "有 Err 时不套用无输出票据")
}

// TestBatchRetiredSummary_ExitCodeAndBlank 钉住批量回收行同样携带 exit_code 与空白降级。
func TestBatchRetiredSummary_ExitCodeAndBlank(t *testing.T) {
	batch := []task.BatchRetired{
		{Task: fpTask("b1", "svc-a"), Sig: task.SettleSignal{Kind: task.SettleCompleted, Err: fmt.Errorf("boom"), ExitCode: 3}},
		{Task: fpTask("b2", "svc-b"), Sig: task.SettleSignal{Kind: task.SettleCompleted, Output: "   \n  "}},
	}
	evt := newBatchRetiredSummaryEvent(batch)
	require.NotNil(t, evt)
	content := evt.Message.Content
	require.Contains(t, content, "exit_code=3")
	require.Contains(t, content, "✗")
	require.Contains(t, content, "（无输出）", "空白条目降级")
}

// TestFormatExitCode_Table 钉住码值渲染的边界。
func TestFormatExitCode_Table(t *testing.T) {
	require.Equal(t, "", formatExitCode(0))
	require.Equal(t, " exit_code=42", formatExitCode(42))
	require.Equal(t, " exit_code=-9 (signal)", formatExitCode(-9))
}
