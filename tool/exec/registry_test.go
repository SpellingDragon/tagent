package exec

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/runtime/state"
)

// execGenDeclaration is the declaration one candidate generation publishes: the
// named agent grants the execution face when grants is true, under the monitor
// interval it states.
func execGenDeclaration(id string, grants bool, interval time.Duration) *config.Declaration {
	spec := config.AgentSpec{ID: id}
	if grants {
		spec.Tools = []config.ToolRef{{Name: PresetName}}
	}
	spec.Exec.Monitor.Interval = interval
	return &config.Declaration{Entry: id, Agents: []config.AgentSpec{spec}}
}

// TestHotReloadExecGen is the positive anchor of the generation-scoped execution consumer.
//   - the boot manager is the template and the stable face, and every generation builds its own instance off it
//   - the instance holds the process-private ledger that dies with the generation, while the external executor is shared across generations
//   - a candidate that leaves without taking effect puts the previous face back
//
// 契约: docs/adr/0007-generation-scoped-runtime-plugin-lifecycle.md
func TestHotReloadExecGen(t *testing.T) {
	ctx := context.Background()

	t.Run("registers_under_the_preset_name", func(t *testing.T) {
		reg := GenRegistration(nil)
		if reg.Name != PresetName {
			t.Fatalf("registration name = %q, want the preset name %q", reg.Name, PresetName)
		}
		if reg.BuildGen == nil {
			t.Fatal("registration carries no generation builder")
		}
		if got := state.GenLeaseKey(state.Generation(2), reg.Name); got != "gen:2:exec" {
			t.Fatalf("exclusive key = %q, want the frozen form gen:2:exec", got)
		}
		if _, err := reg.BuildGen(ctx, execGenDeclaration("user", true, time.Second), state.Generation(2)); !errors.Is(err, ErrUnwired) {
			t.Fatalf("building over no template answered %v, want %v", err, ErrUnwired)
		}
	})

	t.Run("builds_install_one_generation_face", func(t *testing.T) {
		template, backend, _ := newLeaseTestManager(t)
		reg := GenRegistration(template)
		built, err := reg.BuildGen(ctx, execGenDeclaration("user", true, 2*time.Second), state.Generation(2))
		if err != nil {
			t.Fatalf("build the generation consumer: %v", err)
		}
		face := template.presetFace()
		if face == template {
			t.Fatal("the built generation installed no face: the template still serves the construction")
		}
		if instances := template.generationInstances(); len(instances) != 1 || instances[0] != face {
			t.Fatalf("installed instances = %d, want the one built face", len(instances))
		}
		if len(face.resources) != 0 || len(face.final) != 0 {
			t.Fatalf("the generation instance carries bookkeeping at build: %d resources, %d finals", len(face.resources), len(face.final))
		}
		refs := built.Resources.Refs()
		if len(refs) != 1 || refs[0].Kind != "exec" || !strings.HasSuffix(refs[0].Key, ":2") {
			t.Fatalf("exclusive registration = %+v, want the one generation keyed by generation 2", refs)
		}
		if built.Restore == nil {
			t.Fatal("the built consumer answers no restore")
		}

		key, err := face.Acquire(ctx, testExecutionDecl("gen-face"))
		if err != nil {
			t.Fatalf("acquire through the generation face: %v", err)
		}
		if backend.startCount() != 1 {
			t.Fatalf("executor starts = %d, want the one the generation face drove", backend.startCount())
		}
		if !face.held(key) {
			t.Fatal("the generation face does not hold the resource it acquired")
		}
		if template.held(key) {
			t.Fatal("the template ledger holds the generation's resource, want the generation-private ledger alone")
		}

		if err := built.Resources.ReleaseAll(ctx); err != nil {
			t.Fatalf("retire the generation consumer: %v", err)
		}
		if face.held(key) {
			t.Fatal("the retired generation still holds its resource")
		}
		if backend.stopCount() != 0 {
			t.Fatalf("retirement stopped %d external executions, want the durable session left running", backend.stopCount())
		}
		if err := built.Resources.ReleaseAll(ctx); err != nil {
			t.Fatalf("repeated retirement answered %v, want the once-only release", err)
		}
		if err := face.Detach(ctx); err != nil {
			t.Fatalf("repeated detach answered %v, want a no-op", err)
		}

		built.Restore()
		if template.presetFace() != template {
			t.Fatal("the restore did not put the template back as the face the construction reads")
		}
		if instances := template.generationInstances(); len(instances) != 0 {
			t.Fatalf("the restore left %d installed instances", len(instances))
		}
	})

	t.Run("refuses_an_unbuildable_declaration", func(t *testing.T) {
		template, _, _ := newLeaseTestManager(t)
		reg := GenRegistration(template)
		if _, err := reg.BuildGen(ctx, execGenDeclaration("user", true, -time.Second), state.Generation(3)); err == nil {
			t.Fatal("building a negative monitor rhythm answered success, want the fail-closed refusal")
		}
		if template.presetFace() != template {
			t.Fatal("a refused candidate installed a face")
		}
		if instances := template.generationInstances(); len(instances) != 0 {
			t.Fatalf("a refused candidate left %d installed instances", len(instances))
		}
	})

	t.Run("ignores_a_section_no_agent_reads", func(t *testing.T) {
		template, _, _ := newLeaseTestManager(t)
		reg := GenRegistration(template)
		if _, err := reg.BuildGen(ctx, execGenDeclaration("user", false, -time.Second), state.Generation(4)); err != nil {
			t.Fatalf("building a section no execution face reads answered %v, want the candidate built", err)
		}
	})
}
