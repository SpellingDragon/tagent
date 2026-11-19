// 契约: docs/adr/0004-feature-mounting-is-a-plugin-registry.md
package assembly

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// declaredHostSummaryBudget is the summary budget the host anchors declare: a
// quarter of the policy default, so a summary that answers the declaration is
// shorter than one that answers the default.
const declaredHostSummaryBudget = 512

// writeHostDocument writes one host document over a fresh temporary root and
// answers the path it was written to.
func writeHostDocument(t *testing.T, name, agentBlock string) string {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, name)
	document := generationDocument(root, "http://127.0.0.1:1/v1", agentBlock)
	if err := os.WriteFile(path, []byte(document), 0o644); err != nil {
		t.Fatalf("write the document: %v", err)
	}
	return path
}

// TestHostDocumentCarriesTheSummaryBudget pins the summary budget the host document hands the fold.
//   - declaration: a declared summary_budget_bytes is the bound the mapped agent spec carries
//   - fold window: the window the fold reads off that spec is the same declared bound
//   - undeclared: a document naming no budget maps no budget of its own, and the fold reads the declaration default
func TestHostDocumentCarriesTheSummaryBudget(t *testing.T) {
	path := writeHostDocument(t, "tagent.yaml",
		fmt.Sprintf("    replay_window: 128000\n    summary_budget_bytes: %d\n", declaredHostSummaryBudget))
	doc, err := loadHostDocument(path)
	if err != nil {
		t.Fatalf("load the document: %v", err)
	}
	decl, err := doc.Declaration()
	if err != nil {
		t.Fatalf("declare the document: %v", err)
	}
	spec := specNamed(t, decl, "user")
	if spec.SummaryBudgetBytes != declaredHostSummaryBudget {
		t.Fatalf("the mapped declaration carries the budget %d, want the declared %d",
			spec.SummaryBudgetBytes, declaredHostSummaryBudget)
	}
	if got := foldWindowOf(spec).SummaryBudgetBytes; got != declaredHostSummaryBudget {
		t.Fatalf("the fold window reads the budget %d, want the declared %d", got, declaredHostSummaryBudget)
	}

	undeclared, err := loadHostDocument(writeHostDocument(t, "tagent-undeclared.yaml", "    replay_window: 128000\n"))
	if err != nil {
		t.Fatalf("load the document naming no budget: %v", err)
	}
	undeclaredDecl, err := undeclared.Declaration()
	if err != nil {
		t.Fatalf("declare the document naming no budget: %v", err)
	}
	bare := specNamed(t, undeclaredDecl, "user")
	if bare.SummaryBudgetBytes != 0 {
		t.Fatalf("the mapping filled the budget %d the document never declared", bare.SummaryBudgetBytes)
	}
	if got := foldWindowOf(bare).SummaryBudgetBytes; got != config.DefaultSummaryBudgetBytes {
		t.Fatalf("the fold window reads the budget %d beside no declaration, want the declaration default %d",
			got, config.DefaultSummaryBudgetBytes)
	}
}

// TestHostDocumentRefusesAMisspelledSummaryBudget pins the strict decode of the budget key.
//   - the misspelled key is refused by the field an operator searches for, not read past
//   - the refusal names the field it landed on, so the misspelling is read off the error
func TestHostDocumentRefusesAMisspelledSummaryBudget(t *testing.T) {
	path := writeHostDocument(t, "tagent.yaml",
		fmt.Sprintf("    replay_window: 128000\n    summary_budget_byte: %d\n", declaredHostSummaryBudget))
	_, err := loadHostDocument(path)
	if err == nil {
		t.Fatalf("the document carrying the misspelled summary_budget_byte was read, want the strict decode to refuse it")
	}
	if !errors.Is(err, config.ErrUnknownConfigKey) {
		t.Fatalf("the refusal of the misspelled budget key = %v, want it classified as a key outside the schema", err)
	}
	if !strings.Contains(err.Error(), "agents.user.summary_budget_byte") {
		t.Fatalf("the refusal = %v, want it naming the misspelled agents.user.summary_budget_byte", err)
	}
}
