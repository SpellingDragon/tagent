// 契约: docs/adr/0004-feature-mounting-is-a-plugin-registry.md
package assembly

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/SpellingDragon/tagent/api/config"
)

// specNamed reports the declared agent spec of one declaration.
func specNamed(t *testing.T, decl *config.Declaration, id string) config.AgentSpec {
	t.Helper()
	for _, spec := range decl.Agents {
		if spec.ID == id {
			return spec
		}
	}
	t.Fatalf("the declaration carries no agent %q", id)
	return config.AgentSpec{}
}

// TestHostDocumentCarriesTheDeclaredPluginMount pins the declaration surface end to end.
//   - the host dialect reads the plugins key
//   - the mapping writes that key into the agent spec
func TestHostDocumentCarriesTheDeclaredPluginMount(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "tagent.yaml")
	block := "    replay_window: 128000\n    plugins: [\"-age-fold\", age-fold]\n"
	if err := os.WriteFile(path, []byte(generationDocument(root, "http://127.0.0.1:1/v1", block)), 0o644); err != nil {
		t.Fatalf("write the document: %v", err)
	}
	doc, err := loadHostDocument(path)
	if err != nil {
		t.Fatalf("load the document: %v", err)
	}
	decl, err := doc.Declaration()
	if err != nil {
		t.Fatalf("declare the document: %v", err)
	}
	spec := specNamed(t, decl, "user")
	want := []string{"-age-fold", "age-fold"}
	if len(spec.Plugins) != len(want) {
		t.Fatalf("the mapped declaration carries %v, want %v", spec.Plugins, want)
	}
	for i, entry := range want {
		if spec.Plugins[i] != entry {
			t.Fatalf("the mapped declaration carries %v, want %v", spec.Plugins, want)
		}
	}
}
