// 本文件负责一条机器不变量：包内测试永不使用进程默认的 resident meta 目录，也永不递归删它。
// 契约: docs/wiki/tool/tmux-action.md#restart-takeover
package action

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestResidentMetaDirHygiene 钉住 包内测试永不落在进程默认 resident meta 目录上，也永不递归删它。
// - 裸零值 ActionTool 会回落到默认 meta 目录，于是删除动作波及同读该路径的其它进程。
// - 只匹配赋值形式，且本文件跳过自己：它是唯一允许写出被禁形状的地方。
func TestResidentMetaDirHygiene(t *testing.T) {
	if _, err := os.Stat("resident_recovery_test.go"); err != nil {
		t.Skip("static source scan needs the package directory as working directory")
	}
	defaultDir := filepath.Join(os.TempDir(), "tagent-resident-meta")
	files, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatalf("glob test files: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no test files found; the scan would pass vacuously")
	}
	for _, f := range files {
		if f == "resident_meta_hygiene_test.go" {
			continue
		}
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		for i, line := range strings.Split(string(raw), "\n") {
			if strings.Contains(line, ":= &ActionTool{}") {
				t.Errorf("%s:%d: bare &ActionTool{} uses the process-default meta dir %s; give it residentMetaDirOverride: t.TempDir()", f, i+1, defaultDir)
			}
			if strings.Contains(line, "RemoveAll(") && strings.Contains(line, "metaDir()") {
				t.Errorf("%s:%d: a test must not RemoveAll a meta directory; t.TempDir() cleans itself up", f, i+1)
			}
		}
	}
}
