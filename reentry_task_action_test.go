package tagent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/task"
	tasktool "github.com/SpellingDragon/tagent/tool/task"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// §4.2 at the PRODUCTION task-action entry: the same `relaunch_task` /
// `resume_task` objects the LLM calls, driven against a generation published by
// the real reload path. Two things only this level can prove: the refusal reaches
// the host-visible tool answer (not just an internal error), and a wrapper built by
// a CANDIDATE carries a usable resident owner — without that wire every re-entry
// after the first hot update would fail closed.

// reentryYAML renders entry "a" delegating to `target` with relaunch_task and
// resume_task on the same face. `maxIters` is a fingerprint field, so changing it
// is what makes an edit publish a NEW generation while the routing shape holds.
func reentryYAML(target string, maxIters int) string {
	return fmt.Sprintf(`entry: a
agents:
  a:
    system_prompt:
      inline: "ENTRY-A-PROMPT"
    max_tool_iterations: %d
    memory:
      type: memory
    tools:
      - kind: agent
        agent: %s
        description: delegate-%s
      - kind: tool
        id: relaunch_task
      - kind: tool
        id: resume_task
  b:
    system_prompt:
      inline: "SUB-B-PROMPT"
    memory:
      type: memory
  c:
    system_prompt:
      inline: "SUB-C-PROMPT"
    memory:
      type: memory
`, maxIters, target, target)
}

func writeReentryYAML(t *testing.T, path string, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	tick := time.Now().Add(2 * time.Second)
	require.NoError(t, os.Chtimes(path, tick, tick))
}

// namedDelegModel is delegModel with the choice made BY NAME: the framework does
// not promise any ordering of the tools it hands the model, so a mock that calls
// "tools[0]" would be asserting an accident of slice order rather than the
// orchestration. This mock plays the honest LLM role: it delegates to the tool it
// was told to prefer, when that tool is really on the face it was offered.
type namedDelegModel struct {
	delegModel
	pick string // the delegation tool this caller asks for
}

func (m *namedDelegModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	label := delegLabel(delegSystemOf(req))
	tools := delegToolNames(req)

	m.mu.Lock()
	if m.perCall == nil {
		m.perCall = map[string]int{}
	}
	m.perCall[label]++
	n := m.perCall[label]
	offer := ""
	if n%2 == 1 {
		for _, t := range tools {
			if t == m.pick {
				offer = t
			}
		}
	}
	m.served = append(m.served, delegServed{System: label, Tools: tools, Answered: offer})
	m.mu.Unlock()

	ch := make(chan *model.Response, 1)
	if offer != "" {
		ch <- &model.Response{Choices: []model.Choice{{Message: model.Message{
			Role: model.RoleAssistant,
			ToolCalls: []model.ToolCall{{Type: "function", ID: "call-" + offer,
				Function: model.FunctionDefinitionParam{Name: offer, Arguments: []byte(`{"request":"work"}`)}}},
		}}}}
		close(ch)
		return ch, nil
	}
	ch <- &model.Response{Done: true, Choices: []model.Choice{{Message: model.Message{
		Role: model.RoleAssistant, Content: "served:" + label}}}}
	close(ch)
	return ch, nil
}

// subagentTask finds the task the delegation left behind (kind/key are the
// production spawn's own identity, not a test fixture).
func subagentTask(t *testing.T, tm *task.TaskManager) *task.Task {
	t.Helper()
	for _, tk := range tm.List() {
		if tk.Spec.Kind == "subagent" {
			return tk
		}
	}
	t.Fatalf("no subagent task on the board: %+v", tm.List())
	return nil
}

func relaunchArgs(t *testing.T, id string) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]string{"task_id": id})
	require.NoError(t, err)
	return b
}

// TestOrgReentry_RelaunchActionRefusesTargetRemovedByPublishedGeneration is 4.2's
// 「G2 删除 B 后的存量任务」at the entry: the delegation ran under G1, the operator
// then published a generation that routes c instead, and the stored task's relaunch
// — asked for through the production tool — must be REFUSED with the version reason,
// must create no new execution, and must not run the removed target again.
func TestOrgReentry_RelaunchActionRefusesTargetRemovedByPublishedGeneration(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	writeReentryYAML(t, yamlPath, reentryYAML("b", 2))

	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	m := &namedDelegModel{pick: "b"}
	entry, err := New(*cfg, WithModel(m), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	out, err := entry.StartLoop("u", "reentry-session")
	require.NoError(t, err)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range out {
		}
	}()
	t.Cleanup(func() { <-done })

	_, err = entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("first request"))
	require.NoError(t, err)
	waitFor(t, "b served the delegation", func() bool { return countServed(m.snapshot(), "SUB-B-PROMPT") > 0 })
	waitFor(t, "the entry turn closed", func() bool { return countServed(m.snapshot(), "ENTRY-A-PROMPT") >= 2 })

	// The declaration the model saw is the published face's own: the delegation and
	// both production task tools, all three offered on the real request.
	decls := entryDeclarations(m.snapshot())
	require.NotEmpty(t, decls)
	require.ElementsMatch(t, []string{"b", "relaunch_task", "resume_task"}, decls[0],
		"the entry is offered its delegation AND the production task-action tools")

	tk := subagentTask(t, entry.TaskManager())
	require.Equal(t, "b:work", tk.Spec.Key, "the board entry is the production spawn identity")

	// 换代：a 改路由 c，B 从有效面上消失（b 的常驻 owner 仍在，正是 R03 的复活风险面）。
	writeReentryYAML(t, yamlPath, reentryYAML("c", 2))
	entry.CheckOrgReload()
	require.Nil(t, entry.ContextManager().SubagentWrapper("b"),
		"precondition: the published generation really stopped routing b")

	runsBefore := countServed(m.snapshot(), "SUB-B-PROMPT")
	ctxWithTM := task.WithTaskSpawner(context.Background(), entry.TaskManager())
	res, err := tasktool.NewRelaunchTaskTool().Call(ctxWithTM, relaunchArgs(t, tk.ID))
	require.NoError(t, err, "the tool reports a refusal as an answer, not a transport error")
	text, ok := res.(string)
	require.True(t, ok)
	require.Contains(t, text, "重跑任务", "the host sees WHICH action failed: %s", text)
	require.Contains(t, text, "EFFECTIVE orchestration generation",
		"and WHY: version selection refused it, not a missing file: %s", text)

	require.Equal(t, runsBefore, countServed(m.snapshot(), "SUB-B-PROMPT"),
		"a refused relaunch must not run the removed target — not once")
	require.Equal(t, 0, countServed(m.snapshot(), "SUB-C-PROMPT"),
		"and it must not silently substitute the new target either")
}

// TestOrgReentry_RelaunchActionAfterPublishRunsCurrentGenerationTarget is the
// other side of the same entry: the target is STILL routed after a structural
// publish, so a re-entry that finds no initiating call must run it through the
// NEWLY PUBLISHED face. A wrapper built by a candidate without a resident-owner
// wire would fail closed here ("no resident owner") — which is exactly the
// wiring this test exists to falsify.
func TestOrgReentry_RelaunchActionAfterPublishRunsCurrentGenerationTarget(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	writeReentryYAML(t, yamlPath, reentryYAML("b", 2))

	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	m := &namedDelegModel{pick: "b"}
	entry, err := New(*cfg, WithModel(m), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	out, err := entry.StartLoop("u", "reentry-live-session")
	require.NoError(t, err)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range out {
		}
	}()
	t.Cleanup(func() { <-done })

	_, err = entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("first request"))
	require.NoError(t, err)
	waitFor(t, "the first delegation settled", func() bool { return countServed(m.snapshot(), "SUB-B-PROMPT") > 0 })
	waitFor(t, "the entry turn closed", func() bool { return countServed(m.snapshot(), "ENTRY-A-PROMPT") >= 2 })
	tk := subagentTask(t, entry.TaskManager())

	before := countServed(m.snapshot(), "SUB-B-PROMPT")
	// A structural edit that KEEPS a→b: max_tool_iterations is fingerprinted, so
	// this really publishes a new generation and rebuilds the wrapper.
	writeReentryYAML(t, yamlPath, reentryYAML("b", 3))
	entry.CheckOrgReload()
	require.NotNil(t, entry.ContextManager().SubagentWrapper("b"), "the new generation still routes b")

	ctxWithTM := task.WithTaskSpawner(context.Background(), entry.TaskManager())
	res, err := tasktool.NewRelaunchTaskTool().Call(ctxWithTM, relaunchArgs(t, tk.ID))
	require.NoError(t, err)
	text, ok := res.(string)
	require.True(t, ok)
	require.NotContains(t, text, "失败", "a still-routed target must relaunch through the published face: %s", text)

	waitFor(t, "the re-entered delegation ran on the current generation", func() bool {
		return countServed(m.snapshot(), "SUB-B-PROMPT") > before
	})
}
