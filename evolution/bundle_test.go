package evolution

import (
	"path/filepath"
	"testing"
)

// staticGetter 是测试用的 prompt.Getter（模拟文件热载 Source 的回退）。
type staticGetter struct {
	content string
	empty   bool
}

func (s staticGetter) Get() (string, error) { return s.content, nil }
func (s staticGetter) IsEmpty() bool         { return s.empty }

func TestBundleStore_CreateImmutableActiveRollback(t *testing.T) {
	dir := t.TempDir()
	store, err := NewBundleStore(dir)
	if err != nil {
		t.Fatalf("NewBundleStore: %v", err)
	}

	// 基线物化 → active。
	base, err := store.InitBaseline(map[string]string{"system": "v0 prompt"}, BundleParams{}, ModelRef{Name: "glm-4"})
	if err != nil {
		t.Fatalf("InitBaseline: %v", err)
	}
	if store.Active() == nil || store.Active().ID != base.ID {
		t.Fatal("基线应为 active")
	}
	// InitBaseline 幂等：再次调用返回现有 active，不新建。
	again, _ := store.InitBaseline(map[string]string{"system": "different"}, BundleParams{}, ModelRef{})
	if again.ID != base.ID {
		t.Fatal("InitBaseline 应幂等（基线不可变）")
	}

	// 派生 draft（不改 active）。
	draft, err := store.Create(base, map[string]string{"system": "v1 prompt"}, BundleParams{}, ModelRef{Name: "glm-4"}, "refine", "改进")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if store.Active().ID != base.ID {
		t.Fatal("Create 不应改 active（激活必经 SetActive，报告 D1：refine 无直接生效）")
	}
	if draft.ID == base.ID {
		t.Fatal("内容不同 → 内容寻址 id 应不同")
	}

	// 激活 draft（原子切换）。
	if err := store.SetActive(draft.ID); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	if store.Active().ID != draft.ID {
		t.Fatal("SetActive 后 active 应为 draft")
	}

	// 回滚到基线（= SetActive）。
	if err := store.Rollback(base.ID); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if store.Active().ID != base.ID {
		t.Fatal("回滚后 active 应为基线")
	}

	// List 含两个 bundle。
	all, _ := store.List()
	if len(all) != 2 {
		t.Fatalf("List 应含 2 个 bundle, got %d", len(all))
	}
}

func TestBundleStore_PersistenceAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	s1, _ := NewBundleStore(dir)
	b, _ := s1.InitBaseline(map[string]string{"system": "persisted"}, BundleParams{}, ModelRef{Name: "m"})
	if err := s1.SetActive(b.ID); err != nil {
		t.Fatalf("SetActive: %v", err)
	}

	// 重开（模拟重启）→ active 从磁盘恢复。
	s2, err := NewBundleStore(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if s2.Active() == nil || s2.Active().ID != b.ID {
		t.Fatalf("重启后 active 应恢复, got %v", s2.Active())
	}
	if s2.Active().Prompts["system"] != "persisted" {
		t.Fatal("重启后 bundle 内容应完整恢复")
	}
}

func TestBundleDiff(t *testing.T) {
	from := &Bundle{Prompts: map[string]string{"system": "a", "soul": "s"}, Model: ModelRef{Name: "m1"}}
	to := &Bundle{Prompts: map[string]string{"system": "b", "tools": "t"}, Model: ModelRef{Name: "m2"}}
	d := Diff(from, to)
	if len(d.PromptsChanged) != 1 || d.PromptsChanged[0] != "system" {
		t.Errorf("changed 应=[system], got %v", d.PromptsChanged)
	}
	if len(d.PromptsAdded) != 1 || d.PromptsAdded[0] != "tools" {
		t.Errorf("added 应=[tools], got %v", d.PromptsAdded)
	}
	if len(d.PromptsRemoved) != 1 || d.PromptsRemoved[0] != "soul" {
		t.Errorf("removed 应=[soul], got %v", d.PromptsRemoved)
	}
	if !d.ModelChanged {
		t.Error("模型应标记变更")
	}
}

func TestVersionedSource_ActiveAndFallback(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewBundleStore(dir)
	provider := NewBundleProvider(store)
	base := staticGetter{content: "base file prompt"}
	vs := NewVersionedSource(provider, "system", base)

	// 无 active → 回退 base。
	if got, _ := vs.Get(); got != "base file prompt" {
		t.Fatalf("无 active 应回退 base, got %q", got)
	}
	if vs.IsEmpty() {
		t.Fatal("base 非空 → IsEmpty 应 false")
	}

	// 激活 bundle → Get 返回 bundle 内容（版本切换）。
	b, _ := store.InitBaseline(map[string]string{"system": "bundled prompt"}, BundleParams{}, ModelRef{})
	if err := store.SetActive(b.ID); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	if got, _ := vs.Get(); got != "bundled prompt" {
		t.Fatalf("active bundle 应生效, got %q", got)
	}

	// 派生新版本 + 切换 → Get 反映新 active（回合边界生效的机制）。
	b2, _ := store.Create(b, map[string]string{"system": "v2 prompt"}, BundleParams{}, ModelRef{}, "refine", "")
	_ = store.SetActive(b2.ID)
	if got, _ := vs.Get(); got != "v2 prompt" {
		t.Fatalf("切换后应读新 active, got %q", got)
	}

	// 回滚 → Get 反映回滚后的 active。
	_ = store.Rollback(b.ID)
	if got, _ := vs.Get(); got != "bundled prompt" {
		t.Fatalf("回滚后应读旧 active, got %q", got)
	}
}

func TestVersionedSource_IsEmptyNoSource(t *testing.T) {
	vs := NewVersionedSource(nil, "system", staticGetter{empty: true})
	if !vs.IsEmpty() {
		t.Fatal("无 provider 且 base 空 → IsEmpty 应 true")
	}
	// base 为 nil 也不 panic。
	vs2 := NewVersionedSource(nil, "system", nil)
	if !vs2.IsEmpty() {
		t.Fatal("无 provider 无 base → IsEmpty 应 true")
	}
	if got, err := vs2.Get(); got != "" || err != nil {
		t.Fatalf("无源 Get 应返回空, got %q err=%v", got, err)
	}
}

// TestBundleStore_ContentAddressed 验证相同内容派生同 id（内容寻址幂等）。
func TestBundleStore_ContentAddressed(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewBundleStore(filepath.Clean(dir))
	b1, _ := store.Create(nil, map[string]string{"system": "same"}, BundleParams{}, ModelRef{Name: "m"}, "manual", "")
	b2, _ := store.Create(nil, map[string]string{"system": "same"}, BundleParams{}, ModelRef{Name: "m"}, "manual", "")
	if b1.ID != b2.ID {
		t.Fatalf("相同内容应内容寻址同 id, got %s vs %s", b1.ID, b2.ID)
	}
}
