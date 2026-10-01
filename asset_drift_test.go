package tagent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// writeAsset 在 wd 下写入相对路径内容（自动建目录）。
func writeAsset(t *testing.T, wd, rel, content string) {
	t.Helper()
	p := filepath.Join(wd, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
}

func newTestAuditor(t *testing.T, wd string, report func([]AssetChange)) *AssetAuditor {
	t.Helper()
	patterns := []string{"resources/prompts/**", "skills/**"}
	return NewAssetAuditor(wd, patterns, nil, report)
}

// TestAssetDriftCatchesRunningEdit 钉住运行中直改→下个比对周期产变更（D1 主场景）。
func TestAssetDriftCatchesRunningEdit(t *testing.T) {
	wd := t.TempDir()
	writeAsset(t, wd, "resources/prompts/action_tool_desc.md", "original")

	var got [][]AssetChange
	a := newTestAuditor(t, wd, func(c []AssetChange) { got = append(got, c) })
	a.scanAndReport(true) // 建基线，无变更 → 零事件
	require.Empty(t, got, "首次建基线不应产事件")

	// 直改内容
	writeAsset(t, wd, "resources/prompts/action_tool_desc.md", "poisoned methodology")
	a.scanAndReport(false)

	require.Len(t, got, 1, "改动应产一批变更")
	require.Len(t, got[0], 1)
	require.Equal(t, "resources/prompts/action_tool_desc.md", got[0][0].File)
	require.NotEmpty(t, got[0][0].OldHash)
	require.NotEmpty(t, got[0][0].NewHash)
	require.NotEqual(t, got[0][0].OldHash, got[0][0].NewHash, "内容变则 hash 必变")
}

// TestAssetDriftCatchesShutdownWindowEdit 钉住停机窗口修改被启动比对捕获：
// 上一代快照持久化 → 改文件 → 新实例启动比对即产漂移事件。
func TestAssetDriftCatchesShutdownWindowEdit(t *testing.T) {
	wd := t.TempDir()
	writeAsset(t, wd, "skills/foo/SKILL.md", "v1")

	a1 := newTestAuditor(t, wd, nil)
	a1.scanAndReport(true) // 建基线并落盘
	_, err := os.Stat(filepath.Join(wd, ".tagent", assetSnapshotName))
	require.NoError(t, err, "快照必须跨重启持久化")

	// 模拟"停机期间被改"
	writeAsset(t, wd, "skills/foo/SKILL.md", "v2 tampered")

	var got [][]AssetChange
	a2 := newTestAuditor(t, wd, func(c []AssetChange) { got = append(got, c) })
	a2.scanAndReport(true) // 启动比对：读上一代快照 → 发现漂移

	require.Len(t, got, 1, "启动比对必须捕获停机窗口修改")
	require.Equal(t, "skills/foo/SKILL.md", got[0][0].File)
}

// TestAssetDriftNoChangeZeroNoise 钉住无变更零事件零噪声。
func TestAssetDriftNoChangeZeroNoise(t *testing.T) {
	wd := t.TempDir()
	writeAsset(t, wd, "resources/prompts/a.md", "stable")

	var got [][]AssetChange
	a := newTestAuditor(t, wd, func(c []AssetChange) { got = append(got, c) })
	a.scanAndReport(true)

	a.scanAndReport(false)
	a.scanAndReport(false)
	require.Empty(t, got, "内容未变必须零事件")
}

// TestAssetDriftAddAndDelete 钉住文件新增/删除都算变更。
func TestAssetDriftAddAndDelete(t *testing.T) {
	wd := t.TempDir()
	writeAsset(t, wd, "resources/prompts/keep.md", "keep")
	writeAsset(t, wd, "resources/prompts/doomed.md", "doomed")

	var got [][]AssetChange
	a := newTestAuditor(t, wd, func(c []AssetChange) { got = append(got, c) })
	a.scanAndReport(true)

	// 删一个 + 增一个
	require.NoError(t, os.Remove(filepath.Join(wd, "resources/prompts/doomed.md")))
	writeAsset(t, wd, "resources/prompts/brand_new.md", "new")

	a.scanAndReport(false)
	require.Len(t, got, 1)
	byFile := map[string]AssetChange{}
	for _, c := range got[0] {
		byFile[c.File] = c
	}
	require.Contains(t, byFile, "resources/prompts/brand_new.md")
	require.Empty(t, byFile["resources/prompts/brand_new.md"].OldHash, "新增文件 OldHash 为空")
	require.Contains(t, byFile, "resources/prompts/doomed.md")
	require.Empty(t, byFile["resources/prompts/doomed.md"].NewHash, "删除文件 NewHash 为空")
	require.NotContains(t, byFile, "resources/prompts/keep.md", "未变文件不产变更")
}

// TestAssetDriftCatchesBypassWrite 钉住不变量：绕过文本匹配规则的写入
// （变量拼路径、base64 等对审批脚手架不可见的写法）仍被 hash 捕获。
func TestAssetDriftCatchesBypassWrite(t *testing.T) {
	wd := t.TempDir()
	writeAsset(t, wd, "resources/prompts/tool_desc.md", "clean")

	var got [][]AssetChange
	a := newTestAuditor(t, wd, func(c []AssetChange) { got = append(got, c) })
	a.scanAndReport(true)

	// 模拟"绕过审批规则"的写入：路径由变量拼接、内容经编码——审计不看文本，只看 hash。
	p := filepath.Join(wd, "resources/prompts", "tool_desc.md")
	require.NoError(t, os.WriteFile(p, []byte("\x00\x01encoded-payload"), 0o644))

	a.scanAndReport(false)
	require.Len(t, got, 1, "任何写入方都无法逃避 hash 不变量")
	require.Equal(t, "resources/prompts/tool_desc.md", got[0][0].File)
}

// TestAssetDriftConcurrentNoCorruption 钉住并发比对不崩溃、不漏报驻留变更。
func TestAssetDriftConcurrentNoCorruption(t *testing.T) {
	wd := t.TempDir()
	writeAsset(t, wd, "skills/x.md", "base")

	var got [][]AssetChange
	a := NewAssetAuditor(wd, []string{"skills/**"}, nil, func(c []AssetChange) {
		// report 在 mu 保护内被调用，安全追加。
		got = append(got, c)
	})
	a.scanAndReport(true)

	done := make(chan struct{})
	go func() {
		for i := 0; i < 20; i++ {
			a.scanAndReport(false)
		}
		close(done)
	}()
	for i := 0; i < 20; i++ {
		writeAsset(t, wd, "skills/x.md", string(rune('a'+i%26)))
		a.scanAndReport(false)
	}
	<-done
	// 至少应捕获到驻留的最后一次改动（并发写者最终态与基线不同）
	require.NotEmpty(t, got, "并发下驻留变更不得漏报")
}

// TestAssetDriftStartStopLifecycle 钉住 Start 起后台 goroutine、Close 幂等停 ticker
// （初始比对已移入 goroutine，不阻塞构造；Close 关通道，二次 Close 不 panic）。
func TestAssetDriftStartStopLifecycle(t *testing.T) {
	wd := t.TempDir()
	writeAsset(t, wd, "resources/prompts/a.md", "x")
	a := newTestAuditor(t, wd, nil)

	require.NoError(t, a.Start()) // 异步：不阻塞
	require.NoError(t, a.Close())
	require.NoError(t, a.Close()) // 幂等
}

// TestDiffAssetSnapshotsPure 钉住比对引擎是纯函数且顺序稳定（字典序）。
func TestDiffAssetSnapshotsPure(t *testing.T) {
	prev := map[string]FileEntry{
		"b.md":    {Hash: "1", Size: 1},
		"a.md":    {Hash: "2", Size: 2},
		"gone.md": {Hash: "3", Size: 3},
	}
	cur := map[string]FileEntry{
		"b.md":   {Hash: "1", Size: 1}, // 未变
		"a.md":   {Hash: "9", Size: 9}, // 改
		"new.md": {Hash: "5", Size: 5}, // 增
	}
	changes := DiffAssetSnapshots(prev, cur)
	require.Len(t, changes, 3, "改 1 + 增 1 + 删 1")
	require.Equal(t, []string{"a.md", "gone.md", "new.md"}, []string{changes[0].File, changes[1].File, changes[2].File},
		"必须按文件名字典序稳定输出")

	// 纯函数：同输入两次调用结果一致
	same := DiffAssetSnapshots(prev, cur)
	require.Equal(t, changes, same)
}

// TestAssetDriftCorruptSnapshotRebuilds 钉住损坏快照不整体失败：Warn 后重建基线，
// 下个周期恢复正常比对。
func TestAssetDriftCorruptSnapshotRebuilds(t *testing.T) {
	wd := t.TempDir()
	writeAsset(t, wd, "resources/prompts/a.md", "content")
	require.NoError(t, os.MkdirAll(filepath.Join(wd, ".tagent"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(wd, ".tagent", assetSnapshotName), []byte("{not json"), 0o644))

	var got [][]AssetChange
	a := newTestAuditor(t, wd, func(c []AssetChange) { got = append(got, c) })
	a.scanAndReport(true) // 损坏快照 → 静默重建基线，不产全体漂移
	require.Empty(t, got, "损坏快照不得误判为全体漂移")

	// 基线已重建，正常比对恢复
	writeAsset(t, wd, "resources/prompts/a.md", "changed")
	a.scanAndReport(false)
	require.Len(t, got, 1, "重建后必须恢复漂移捕获")
}
