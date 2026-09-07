package evolution

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
)

// initGitRepo 建 tempdir git 仓并生成初始 commit（K3：裸环境无 git config——
// 被测代码每命令 -c user.* 注入，此处初始 commit 同样处理）。
func initGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		out, err := gitCmd(dir, args...)
		require.NoError(t, err, "git %v: %s", args, out)
	}
	run("init", "-q")
	run("commit", "-q", "--allow-empty", "-m", "init")
	_ = os.MkdirAll(filepath.Join(dir, "resources/prompts"), 0o755)
	_ = os.MkdirAll(filepath.Join(dir, "workspace"), 0o755)
	return dir
}

func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
}

// TestGitRegister_TaggedCommitIsolated（2.4）：register 产生行首标记 commit 且
// 不携带工作区其他改动（只 add 显式受控路径）。
func TestGitRegister_TaggedCommitIsolated(t *testing.T) {
	dir := initGitRepo(t)
	writeFile(t, dir, "resources/prompts/SOUL.md", "v1")
	writeFile(t, dir, "workspace/user-notes.txt", "用户的无关改动") // 不应被卷入

	sha, msg, err := (func() (string, string, error) {
		g := NewGitEvolution(GitEvolutionConfig{WorkDir: dir})
		return g.Register([]string{"resources/prompts/SOUL.md"}, "痛点→产物→收益")
	})()
	require.NoError(t, err)
	require.NotEmpty(t, sha)
	require.NotContains(t, msg, "nothing to commit")

	// 标记 commit 存在且 message 行首带 tag。
	ok, full, err := GitCommitHasTag(dir, sha)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, sha, full)

	// 无关文件未被 commit（仍在工作区未暂存——porcelain 对 untracked 目录取目录级展示）。
	st, err := gitCmd(dir, "status", "--porcelain")
	require.NoError(t, err)
	require.Contains(t, st, "workspace/", "无关改动不得被卷入改进 commit")

	// LogFiltered 能列出（行首锚定）。
	infos, err := GitLogFiltered(dir, 10)
	require.NoError(t, err)
	require.Len(t, infos, 1)
	require.Contains(t, infos[0].Subject, "痛点→产物→收益")
}

// TestGitRegister_NothingToCommit（N4）：无改动 → result 语义（ErrNothingToCommit）。
func TestGitRegister_NothingToCommit(t *testing.T) {
	dir := initGitRepo(t)
	writeFile(t, dir, "resources/prompts/SOUL.md", "v1")
	g := NewGitEvolution(GitEvolutionConfig{WorkDir: dir})
	_, _, err := g.Register([]string{"resources/prompts/SOUL.md"}, "first")
	require.NoError(t, err)
	// 再次登记（无改动）→ 明确 result 非报错。
	_, msg2, err2 := g.Register([]string{"resources/prompts/SOUL.md"}, "again")
	require.NoError(t, err2)
	require.Contains(t, msg2, "无改动")
}

// TestGitRegister_OutOfPathRejected：越界路径拒绝（受控三目录外）。
func TestGitRegister_OutOfPathRejected(t *testing.T) {
	dir := initGitRepo(t)
	g := NewGitEvolution(GitEvolutionConfig{WorkDir: dir})
	_, _, err := g.Register([]string{"workspace/evil.txt"}, "x")
	require.Error(t, err)
	require.Contains(t, err.Error(), "越界")
}

// TestGitRegister_NotARepo：非 git 仓 → 明确错误（改文件仍生效的降级如实呈现）。
func TestGitRegister_NotARepo(t *testing.T) {
	g := NewGitEvolution(GitEvolutionConfig{WorkDir: t.TempDir()})
	_, _, err := g.Register([]string{"resources/prompts/SOUL.md"}, "x")
	require.Error(t, err)
	require.Contains(t, err.Error(), "git 仓")
}

// TestGitRevertSafe_GuardsAndConflicts：拒绝非标记 commit；冲突返回详情。
func TestGitRevertSafe_GuardsAndConflicts(t *testing.T) {
	dir := initGitRepo(t)
	// 用户 commit（无标记）。
	writeFile(t, dir, "workspace/u.txt", "u")
	_, err := gitCmd(dir, "add", "--", "workspace/u.txt")
	require.NoError(t, err)
	_, err = gitCmd(dir, "commit", "-m", "user work")
	require.NoError(t, err)
	// 改进 commit ①。
	writeFile(t, dir, "resources/prompts/SOUL.md", "improved")
	g := NewGitEvolution(GitEvolutionConfig{WorkDir: dir})
	sha1, _, err := g.Register([]string{"resources/prompts/SOUL.md"}, "improve1")
	require.NoError(t, err)
	// 用户后续改同一文件并 commit（制造 revert 冲突基底）。
	writeFile(t, dir, "resources/prompts/SOUL.md", "user-edit-after")
	_, err = gitCmd(dir, "add", "--", "resources/prompts/SOUL.md")
	require.NoError(t, err)
	_, err = gitCmd(dir, "commit", "-m", "user follow-up")
	require.NoError(t, err)

	// 拒绝 revert 用户 commit：定位 user follow-up 的 sha。
	out, err := gitCmd(dir, "log", "-1", "--format=%H", "--grep=^user follow-up")
	require.NoError(t, err)
	userSha := strings.TrimSpace(out)
	_, err = GitRevertSafe(dir, userSha)
	require.Error(t, err)
	require.Contains(t, err.Error(), "仅可回滚")

	// revert 冲突路径：user follow-up 改了同文件，revert sha1 应冲突并返回详情。
	out2, err := GitRevertSafe(dir, sha1)
	require.Error(t, err, "应产生冲突")
	require.NotEmpty(t, out2, "冲突详情应返回给 agent 处置")
	// 清理冲突状态（revert 冲突会留 index 脏）。
	_, _ = gitCmd(dir, "revert", "--abort")
}

// TestMatchProtectedPaths_Segments（2.2）：段匹配 + 三态路径归一。
func TestMatchProtectedPaths_Segments(t *testing.T) {
	cwd := "/work/app"
	pats := []string{"resources/prompts/**", "skills/**", "scripts/**"}
	ok, bad := MatchProtectedPaths(cwd, []string{
		"resources/prompts/SOUL.md",      // 相对 cwd
		cwd + "/skills/deploy/README.md", // 绝对
		"./scripts/batch.py",             // ./ 前缀
	}, pats)
	require.True(t, ok)
	require.Empty(t, bad)

	ok, bad = MatchProtectedPaths(cwd, []string{"workspace/x.txt", "main.go"}, pats)
	require.False(t, ok)
	require.Len(t, bad, 2)
}

// fakeGuard 是 Guardrail 测试替身。
type fakeGuard struct {
	breach bool
	why    string
}

func (f fakeGuard) Breach(string) (bool, string) { return f.breach, f.why }

// TestGitEvolution_EvaluationEvent（4.2 核心）：劣化只产 evaluation 事件（建议式），
// 不执行 git revert、无消息注入。fail-before：bundle 时代 Submit 内直接 rm.rollback。
func TestGitEvolution_EvaluationEvent(t *testing.T) {
	dir := initGitRepo(t)
	writeFile(t, dir, "resources/prompts/SOUL.md", "v2")
	store := memory.NewInMemoryStore()
	pid := memory.PartitionIDFromName("tagent")
	g := NewGitEvolution(GitEvolutionConfig{WorkDir: dir})
	g.BindRuntime(store, pid,
		nil, // 无 judge
		fakeGuard{breach: true, why: "负反馈率 0.8 超阈"}, // guardrail 恒劣化
	)
	sha, _, err := g.Register([]string{"resources/prompts/SOUL.md"}, "risky change")
	require.NoError(t, err)

	g.EvaluateNow(sha, 0) // 立即评估（跳过 delay）

	// evaluation 事件落库且带建议文案。
	evals := g.Evaluations()
	require.Len(t, evals, 1)
	ev := evals[sha]
	require.Equal(t, "degraded", ev.Verdict)
	require.Contains(t, ev.Advice, "refine rollback")

	// git 未被框架动手：HEAD 仍是 register 的 commit（无 revert）。
	head, err := gitCmd(dir, "rev-parse", "HEAD")
	require.NoError(t, err)
	require.Equal(t, sha, strings.TrimSpace(head), "P4：框架不得自动 revert")
}

// TestGitEvolution_LatestShaStamp（4.4）：章缓存——register 后 LatestSha 即新 sha；
// 重启（新实例同 store）惰性恢复最新 improvement。
func TestGitEvolution_LatestShaStamp(t *testing.T) {
	dir := initGitRepo(t)
	store := memory.NewInMemoryStore()
	pid := memory.PartitionIDFromName("tagent")
	g := NewGitEvolution(GitEvolutionConfig{WorkDir: dir})
	g.BindRuntime(store, pid, nil, nil)
	require.Empty(t, g.LatestSha(), "无改进→空串不盖章")

	writeFile(t, dir, "resources/prompts/SOUL.md", "v3")
	sha, _, err := g.Register([]string{"resources/prompts/SOUL.md"}, "stamp me")
	require.NoError(t, err)
	require.Equal(t, sha, g.LatestSha())

	// 模拟重启：新实例（缓存空）从事件惰性恢复。
	g2 := NewGitEvolution(GitEvolutionConfig{WorkDir: dir})
	g2.BindRuntime(store, pid, nil, nil)
	require.Equal(t, sha, g2.LatestSha(), "重启经 improvement 事件恢复版本章")
}
