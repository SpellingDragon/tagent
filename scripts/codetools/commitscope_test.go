// 本文件负责提交范围一致性门的判定形状：docs 标题遇代码路径即红，纯文档与代码标题放过。
// 规格: docs/comment-gate-tooling.md#commit-scope
package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommitScopeFlagsCodeUnderDocsSubject(t *testing.T) {
	got := commitScopeFindings("docs(openspec): record findings", []string{
		"docs/wiki/spec.md", "config.go", "scripts/lint.sh", "README.md",
	})
	assert.Equal(t, []string{
		"config.go: docs-titled commit carries code path",
		"scripts/lint.sh: docs-titled commit carries code path",
	}, got)
}

func TestCommitScopePassesHonestSubjects(t *testing.T) {
	require.Empty(t, commitScopeFindings("docs(readme): sync layout", []string{"README.md", "docs/wiki/a.md"}))
	require.Empty(t, commitScopeFindings("refactor(config): move model out", []string{"config/config.go", "tagent.go"}))
}
