package action

import (
	"strings"
	"testing"

	"github.com/SpellingDragon/tagent/agent/task"
	"github.com/stretchr/testify/require"
)

// TestDetectSmuggleBackground 钉住后台化走私命中：nohup … &、nohup … & disown。
// 契约: docs/wiki/platform/cognitive-asset-guard.md
func TestDetectSmuggleBackground(t *testing.T) {
	cases := []string{
		"nohup ./longjob > log 2>&1 &",
		"nohup python train.py & disown",
		"make -j8 > /tmp/build.log 2>&1 &",
	}
	for _, c := range cases {
		require.Equal(t, smuggleBackground, detectSmuggle(c), c)
		require.NotEmpty(t, smuggleHintFor(c), c)
	}
}

// TestDetectSmuggleNestedTmux 钉住嵌套 tmux 新会话命中。
func TestDetectSmuggleNestedTmux(t *testing.T) {
	cases := []string{
		`tmux new-session -d -s worker "python job.py > log"`,
		"tmux new -s bg 'sleep 100'",
	}
	for _, c := range cases {
		require.Equal(t, smuggleNestedTmuxC, detectSmuggle(c), c)
	}
}

// TestDetectSmuggleNormalCommandsUnaffected 钉住正常命令零命中（结果逐字节不变的前提）。
func TestDetectSmuggleNormalCommandsUnaffected(t *testing.T) {
	cases := []string{
		"ls -la",
		"echo hello",
		"git commit -m 'fix bug'",
		"go test ./...",
		"tmux ls",
		"tmux attach -t existing",
		"cat nohup.out",
	}
	for _, c := range cases {
		require.Equal(t, smuggleNone, detectSmuggle(c), c)
		require.Empty(t, smuggleHintFor(c), "正常命令必须无 hint 行: %s", c)
	}
}

// TestDetectSmuggleEchoNoFalsePositive 钉住 echo 字符串含 nohup 但无 & 配对不误报。
func TestDetectSmuggleEchoNoFalsePositive(t *testing.T) {
	c := `echo "nohup is a command"`
	require.Equal(t, smuggleNone, detectSmuggle(c))
	require.Empty(t, smuggleHintFor(c))
}

// TestSmuggleHintIdempotent 钉住检测纯函数幂等：重复检测同一命令产同一单行 hint。
func TestSmuggleHintIdempotent(t *testing.T) {
	c := "nohup ./job & disown"
	h1 := smuggleHintFor(c)
	h2 := smuggleHintFor(c)
	require.Equal(t, h1, h2, "重复检测必须返回相同 hint（幂等，不重复追加）")
	require.Equal(t, 1, strings.Count(h1, "[框架提示]"), "hint 只有一行提示头")
}

// TestBuildAckResultNoteAppend 钉住 ack 出口：走私命令 Note 尾附引导行，正常命令 Note 逐字节不变。
func TestBuildAckResultNoteAppend(t *testing.T) {
	ct := NewActionTool(WithOrphanCleanupDisabled())
	defer ct.Close()

	r := ct.buildAckResult("s1", "echo hi", &task.Task{ID: "t1"})
	require.NotEmpty(t, r.Note)
	base := r.Note
	r.Note += smuggleHintFor("echo hi")
	require.Equal(t, base, r.Note, "正常命令结果逐字节不变")

	r2 := ct.buildAckResult("s2", "nohup ./x & disown", &task.Task{ID: "t2"})
	origNote := r2.Note
	r2.Note += smuggleHintFor("nohup ./x & disown")
	require.Contains(t, r2.Note, "[框架提示]")
	require.True(t, strings.HasPrefix(r2.Note, origNote), "原说明保留，hint 追加在尾部")
}
