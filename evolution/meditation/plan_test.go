// plan_test 钉住声明解析的红线：一个文档解析出一条定时反射——观察者、节奏、提示词、
// 观察面、投递白名单与锚点记录；每条不一致的声明在装配期按名拒绝，而不是被驱动成
// 声明没说的反射。
// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
package meditation

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/document"
	"github.com/SpellingDragon/tagent/memory/ledger"
)

// reflectionPrompt is the static prompt every resolved plan carries here.
const reflectionPrompt = "本轮只看业务分区有没有值得回流的新结论。\n"

// planDocument builds one document whose meditator observes the business agent's
// partition and delivers its reflection back to it: the meditator reads across
// that partition and both agents share one storage tier, so the anchor record
// resolves beside the facts it observed.
func planDocument(t *testing.T) (*document.Document, string, string) {
	t.Helper()
	promptDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(promptDir, "reflection.md"), []byte(reflectionPrompt), 0o644); err != nil {
		t.Fatalf("write the meditation prompt: %v", err)
	}
	storageRoot := t.TempDir()
	tier := config.MemorySpec{Type: string(ledger.StorageLocalFile), Path: storageRoot}
	doc := &document.Document{
		Entry: "tagent",
		Agents: map[string]config.AgentSpec{
			"tagent": {Model: "test-model", Memory: tier},
			"meditator": {
				Model:  "test-model",
				Memory: config.MemorySpec{Type: tier.Type, Path: tier.Path, ReadNamespaces: []string{"tagent"}},
				Meditation: config.MeditationSpec{
					Enabled:            true,
					Interval:           time.Minute,
					PromptFile:         "reflection.md",
					ObservedNamespaces: []string{"tagent"},
					DeliverTo:          []string{"tagent", "tagent"},
				},
			},
		},
	}
	return doc, promptDir, storageRoot
}

// TestPlanOfResolvesTheOneEnabledSection pins the plan one declared reflection resolves to.
//   - the observing agent and its declared cadence
//   - the prompt body the declaration names
//   - the observation face and the read scope it sits inside
//   - the delivery white list, deduplicated in declaration order
//   - the anchor record beside the shared storage tier
func TestPlanOfResolvesTheOneEnabledSection(t *testing.T) {
	doc, promptDir, storageRoot := planDocument(t)
	plan, err := PlanOf(doc, promptDir, storageRoot)
	if err != nil {
		t.Fatalf("resolve the plan: %v", err)
	}
	if plan == nil {
		t.Fatal("the document enables meditation but resolves no plan")
	}
	if got := plan.Agent(); got != "meditator" {
		t.Fatalf("agent = %q, want the declaring meditator", got)
	}
	if got := plan.Interval(); got != time.Minute {
		t.Fatalf("interval = %s, want the declared one minute", got)
	}
	if got := plan.MinGap(); got != 0 {
		t.Fatalf("min gap = %s, want no floor beside an undeclared one", got)
	}
	if got := plan.Prompt(); got != reflectionPrompt {
		t.Fatalf("prompt = %q, want the declared body %q", got, reflectionPrompt)
	}
	if got := plan.ObservedNamespaces(); len(got) != 1 || got[0] != "tagent" {
		t.Fatalf("observed namespaces = %v, want the declared tagent face", got)
	}
	if got := plan.ReadNamespaces(); len(got) != 2 || got[0] != "meditator" || got[1] != "tagent" {
		t.Fatalf("read namespaces = %v, want the own partition first and the granted read after it", got)
	}
	if got := plan.DeliverTo(); len(got) != 1 || got[0] != "tagent" {
		t.Fatalf("delivery white list = %v, want the one declared target deduplicated", got)
	}
	want := filepath.Join(storageRoot, "meditation", "meditator", "anchors.json")
	if got := plan.AnchorsPath(); got != want {
		t.Fatalf("anchors path = %q, want %q", got, want)
	}
}

// TestPlanOfDefaultsAnUndeclaredCadence pins the cadence a plan runs at beside no declared interval.
func TestPlanOfDefaultsAnUndeclaredCadence(t *testing.T) {
	doc, promptDir, storageRoot := planDocument(t)
	entry := doc.Agents["meditator"]
	entry.Meditation.Interval = 0
	doc.Agents["meditator"] = entry

	plan, err := PlanOf(doc, promptDir, storageRoot)
	if err != nil {
		t.Fatalf("resolve the plan: %v", err)
	}
	if plan == nil {
		t.Fatal("the document enables meditation but resolves no plan")
	}
	if got := plan.Interval(); got != DefaultInterval {
		t.Fatalf("interval = %s, want the default %s", got, DefaultInterval)
	}
}

// TestPlanOfReportsNoPlanBesideNoEnabledSection pins a document enabling nothing to a nil plan.
func TestPlanOfReportsNoPlanBesideNoEnabledSection(t *testing.T) {
	doc, promptDir, storageRoot := planDocument(t)
	entry := doc.Agents["meditator"]
	entry.Meditation.Enabled = false
	doc.Agents["meditator"] = entry

	plan, err := PlanOf(doc, promptDir, storageRoot)
	if err != nil {
		t.Fatalf("resolve the plan: %v", err)
	}
	if plan != nil {
		t.Fatalf("plan = %+v, want nil beside no enabled section", plan)
	}
}

// TestPlanOfReportsNoPlanForNoDocument pins the nil document the assembly may hand over.
func TestPlanOfReportsNoPlanForNoDocument(t *testing.T) {
	plan, err := PlanOf(nil, "", "")
	if err != nil {
		t.Fatalf("resolve the plan: %v", err)
	}
	if plan != nil {
		t.Fatalf("plan = %+v, want nil beside no document", plan)
	}
}

// TestPlanOfRefusesInconsistentSections refuses each inconsistent declaration by name.
// - two enabled loops, a negative cadence or floor, a prompt not named or unreadable
// - an observation face outside the read scope, an undeclared or empty delivery list
// - an enabled section beside no storage tier
func TestPlanOfRefusesInconsistentSections(t *testing.T) {
	cases := []struct {
		name        string
		mutate      func(t *testing.T, doc *document.Document, promptDir string)
		dropStorage bool
		want        []error
	}{
		{
			name: "two enabled loops",
			mutate: func(_ *testing.T, doc *document.Document, _ string) {
				entry := doc.Agents["tagent"]
				entry.Meditation = doc.Agents["meditator"].Meditation
				doc.Agents["tagent"] = entry
			},
			want: []error{ErrDeclaration},
		},
		{
			name: "negative interval",
			mutate: func(_ *testing.T, doc *document.Document, _ string) {
				entry := doc.Agents["meditator"]
				entry.Meditation.Interval = -time.Minute
				doc.Agents["meditator"] = entry
			},
			want: []error{ErrDeclaration},
		},
		{
			name: "negative min gap",
			mutate: func(_ *testing.T, doc *document.Document, _ string) {
				entry := doc.Agents["meditator"]
				entry.Meditation.MinGap = -time.Second
				doc.Agents["meditator"] = entry
			},
			want: []error{ErrDeclaration},
		},
		{
			name: "no prompt file",
			mutate: func(_ *testing.T, doc *document.Document, _ string) {
				entry := doc.Agents["meditator"]
				entry.Meditation.PromptFile = " "
				doc.Agents["meditator"] = entry
			},
			want: []error{ErrDeclaration},
		},
		{
			name: "missing prompt",
			mutate: func(_ *testing.T, doc *document.Document, _ string) {
				entry := doc.Agents["meditator"]
				entry.Meditation.PromptFile = "absent.md"
				doc.Agents["meditator"] = entry
			},
			want: []error{ErrDeclaration},
		},
		{
			name: "empty prompt",
			mutate: func(t *testing.T, doc *document.Document, promptDir string) {
				blank := filepath.Join(promptDir, "blank.md")
				if err := os.WriteFile(blank, []byte(" \n"), 0o644); err != nil {
					t.Fatalf("write the blank prompt: %v", err)
				}
				entry := doc.Agents["meditator"]
				entry.Meditation.PromptFile = blank
				doc.Agents["meditator"] = entry
			},
			want: []error{ErrDeclaration},
		},
		{
			name: "observation outside the read scope",
			mutate: func(_ *testing.T, doc *document.Document, _ string) {
				entry := doc.Agents["meditator"]
				entry.Meditation.ObservedNamespaces = []string{"ops"}
				doc.Agents["meditator"] = entry
			},
			want: []error{ErrDeclaration, ErrObservationScopeOutOfBounds},
		},
		{
			name: "undeclared delivery target",
			mutate: func(_ *testing.T, doc *document.Document, _ string) {
				entry := doc.Agents["meditator"]
				entry.Meditation.DeliverTo = []string{"ghost"}
				doc.Agents["meditator"] = entry
			},
			want: []error{ErrDeclaration},
		},
		{
			name: "no delivery target",
			mutate: func(_ *testing.T, doc *document.Document, _ string) {
				entry := doc.Agents["meditator"]
				entry.Meditation.DeliverTo = nil
				doc.Agents["meditator"] = entry
			},
			want: []error{ErrDeclaration},
		},
		{
			name:        "no storage tier",
			mutate:      func(_ *testing.T, _ *document.Document, _ string) {},
			dropStorage: true,
			want:        []error{ErrDeclaration},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc, promptDir, storageRoot := planDocument(t)
			tc.mutate(t, doc, promptDir)
			if tc.dropStorage {
				storageRoot = ""
			}
			plan, err := PlanOf(doc, promptDir, storageRoot)
			if plan != nil {
				t.Fatalf("plan = %+v, want nil beside a refused declaration", plan)
			}
			for _, want := range tc.want {
				if !errors.Is(err, want) {
					t.Fatalf("resolve the plan: error %v, want %v", err, want)
				}
			}
		})
	}
}
