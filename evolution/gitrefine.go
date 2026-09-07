package evolution

import (
	"fmt"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
)

// ==================== git 原生改进通道（self-evolution-git-native）====================
//
// 纯函数集（无状态、无生命周期）：git exec 包装 + 受控路径段匹配。
// 哲学四原则（design §0）：文件即真源（P2）/ 复用 git（P3）/ 零自建版本库。

// selfImproveTag 是改进 commit 的 message 行首标记——register 生成、LogFiltered
// 过滤、RevertSafe 校验，三处共享。行首锚定天然排除 revert 生成的
// `Revert "[self-improve] ..."`（K5）。
const selfImproveTag = "[self-improve]"

// gitIdentityArgs 防 K3：裸环境（CI/容器）无全局 git config 时 commit 失败——
// 每命令局部注入身份，生产同样受益（不依赖用户全局配置）。
var gitIdentityArgs = []string{"-c", "user.email=tagent@local", "-c", "user.name=tagent"}

func gitCmd(dir string, args ...string) (string, error) {
	full := append([]string{"git"}, gitIdentityArgs...)
	full = append(full, args...)
	cmd := exec.Command(full[0], full[1:]...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// GitIsRepo 报告 dir 是否在 git 工作区内（启动自检用，Warn 不阻断——闸不是墙）。
func GitIsRepo(dir string) bool {
	_, err := gitCmd(dir, "rev-parse", "--is-inside-work-tree")
	return err == nil
}

// GitAddCommit 对受控路径文件执行 add+commit（message 带改进标记），返回新 commit sha。
// 仅 add 显式 paths（绝不 -A——防混入用户工作区改动）。nothing-to-commit 由调用方
// 按 GitNothingToCommit 判定并以 result 呈现（N4，非 error）。
func GitAddCommit(dir string, paths []string, note string) (sha string, err error) {
	if len(paths) == 0 {
		return "", fmt.Errorf("git register: no paths")
	}
	args := append([]string{"add", "--"}, paths...)
	if _, err := gitCmd(dir, args...); err != nil {
		return "", err
	}
	msg := selfImproveTag + " " + note
	// M3(独立评审):--only 限定 pathspec——裸 commit 会提交整个 index,卷入用户
	// 手工暂存内容或上次失败遗留的暂存文件(revert 时连带回滚用户工作)。
	commitArgs := append([]string{"commit", "--only", "-m", msg, "--"}, paths...)
	if _, err := gitCmd(dir, commitArgs...); err != nil {
		if strings.Contains(err.Error(), "nothing to commit") ||
			strings.Contains(err.Error(), "no changes added to commit") {
			return "", ErrNothingToCommit
		}
		return "", err
	}
	out, err := gitCmd(dir, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// ErrNothingToCommit：受控文件无改动（N4）——调用方以 result 渗透，不按 error。
var ErrNothingToCommit = fmt.Errorf("nothing-to-commit")

// GitCommitInfo 是一条改进 commit 的摘要。
type GitCommitInfo struct {
	Sha     string
	Note    string
	Time    string // git 默认本地格式（status 展示用，不参与排序）
	Subject string
}

// GitLogFiltered 列出改进标记 commit（行首锚定，排除 Revert——K5）。limit<=0 取 20。
func GitLogFiltered(dir string, limit int) ([]GitCommitInfo, error) {
	if limit <= 0 {
		limit = 20
	}
	out, err := gitCmd(dir, "log", "-n", fmt.Sprint(limit),
		// 正则转义：[self-improve] 的方括号不转义会被当字符类（"init" 行首 'i' 即误命中）。
		"--grep=^\\[self-improve\\]", "--format=%H%x09%ad%x09%s", "--date=short")
	if err != nil {
		return nil, err
	}
	var infos []GitCommitInfo
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		infos = append(infos, GitCommitInfo{
			Sha: parts[0], Time: parts[1], Subject: parts[2],
			Note: strings.TrimPrefix(parts[2], selfImproveTag+" "),
		})
	}
	return infos, nil
}

// GitCommitHasTag 校验目标 commit 的 message 行首带改进标记（rollback 安全闸：
// 防误 revert 用户提交）。sha 可为前缀。
func GitCommitHasTag(dir, sha string) (bool, string, error) {
	out, err := gitCmd(dir, "show", "-s", "--format=%H %s", sha)
	if err != nil {
		return false, "", fmt.Errorf("commit %s 不存在或不可读: %w", sha, err)
	}
	line := strings.TrimSpace(out)
	fullSha, subject, _ := strings.Cut(line, " ")
	return strings.HasPrefix(subject, selfImproveTag), fullSha, nil
}

// GitRevertSafe 校验标记后执行 git revert --no-edit。冲突时返回冲突详情
// （exit!=0 的 CombinedOutput），由 agent 决定后续——建议式哲学。
func GitRevertSafe(dir, sha string) (string, error) {
	if ok, full, err := GitCommitHasTag(dir, sha); err != nil {
		return "", err
	} else if !ok {
		return "", fmt.Errorf("仅可回滚 %s 标记的改进 commit（保护用户提交不被误 revert）", selfImproveTag)
	} else {
		sha = full
	}
	out, err := gitCmd(dir, "revert", "--no-edit", sha)
	if err != nil {
		return out, fmt.Errorf("revert 冲突或失败（详情见输出，可自行处置后重试）: %w", err)
	}
	return out, nil
}

// ==================== 受控路径段匹配（N5：path.Match 不支持 **）====================

// MatchProtectedPaths 校验 paths（相对运行 cwd 归一后）全部落在 patterns 内。
// 返回 (ok, 越界路径列表)。pattern 按 / 分段：`**` 匹配任意段序列，`*` 段内通配。
func MatchProtectedPaths(cwd string, paths, patterns []string) (bool, []string) {
	var bad []string
	for _, p := range paths {
		rel := normalizeRel(cwd, p)
		matched := false
		for _, pat := range patterns {
			if matchSegments(strings.Split(filepath.ToSlash(pat), "/"), strings.Split(rel, "/")) {
				matched = true
				break
			}
		}
		if !matched {
			bad = append(bad, p)
		}
	}
	return len(bad) == 0, bad
}

// normalizeRel 三态归一（K6）：绝对 / 相对 cwd / 相对 workspace 一律 → 相对 cwd 的 clean 路径。
func normalizeRel(cwd, p string) string {
	if filepath.IsAbs(p) {
		if r, err := filepath.Rel(cwd, p); err == nil {
			return filepath.ToSlash(filepath.Clean(r))
		}
	}
	return filepath.ToSlash(filepath.Clean(p))
}

// matchSegments 递归段匹配：`**` 吃任意多段，普通段内 path.Match 语义。
func matchSegments(pat, seg []string) bool {
	if len(pat) == 0 {
		return len(seg) == 0
	}
	if pat[0] == "**" {
		// `**` 匹配 0..n 段
		for i := 0; i <= len(seg); i++ {
			if matchSegments(pat[1:], seg[i:]) {
				return true
			}
		}
		return false
	}
	if len(seg) == 0 {
		return false
	}
	ok, err := pathMatchStar(pat[0], seg[0])
	if err != nil || !ok {
		return false
	}
	return matchSegments(pat[1:], seg[1:])
}

// pathMatchStar 段内通配（复用 path.Match 的单段语义）。
func pathMatchStar(pat, s string) (bool, error) {
	ok, err := path.Match(pat, s)
	return ok && !strings.Contains(s, "/"), err
}
