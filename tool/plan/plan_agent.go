// Package plan implements the PlanAgent — a TagentAgent wrapper with
// custom Run that bypasses the LLM for progress queries.
//
// Design: follows the prototype's "Run is replaceable" pattern.
// PlanAgent embeds *tagentagent.TagentAgent and overrides Run to intercept
// action=progress requests, handling them via direct file I/O
// instead of the full ReAct loop.
package plan

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	tagentagent "github.com/SpellingDragon/tagent/agent"
	tagentevent "github.com/SpellingDragon/tagent/event"
	trpcagent "trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// ---------------------------------------------------------------------------
// PlanAgent — dual-mode agent wrapper
// ---------------------------------------------------------------------------

// PlanAgent wraps a TagentAgent with a custom Run method.
// For action=="progress", it bypasses the LLM and directly reads
// openspec/changes/ to return a progress summary.
// For all other actions, it delegates to the standard TagentAgent.Run.
type PlanAgent struct {
	*tagentagent.TagentAgent
	openSpecDir string
}

// NewPlanAgent creates a PlanAgent wrapping the given TagentAgent.
// openSpecDir is the root directory containing the openspec/ folder.
func NewPlanAgent(inner *tagentagent.TagentAgent, openSpecDir string) *PlanAgent {
	if openSpecDir == "" {
		openSpecDir = "."
	}
	return &PlanAgent{
		TagentAgent: inner,
		openSpecDir: openSpecDir,
	}
}

// Run implements agent.Agent. It inspects the action field from the
// invocation message and routes progress queries to direct file I/O.
func (pa *PlanAgent) Run(ctx context.Context, inv *trpcagent.Invocation) (<-chan *event.Event, error) {
	action := extractAction(inv)

	if action == "progress" {
		log.Debugf("[PlanAgent] progress query — bypassing model")
		return pa.runProgressQuery(ctx, inv)
	}

	// Standard ReAct path
	return pa.TagentAgent.Run(ctx, inv)
}

// extractAction parses the action field from the invocation message.
// The message content is expected to be JSON: {"action":"progress", ...}
// or plain text containing the action. Falls back to "" if not found.
func extractAction(inv *trpcagent.Invocation) string {
	content := inv.Message.Content
	if content == "" {
		return ""
	}

	// Try JSON parse first
	var fields map[string]interface{}
	if err := json.Unmarshal([]byte(content), &fields); err == nil {
		if a, ok := fields["action"].(string); ok {
			return a
		}
	}

	// Fallback: check if content starts with a known action keyword
	lower := strings.ToLower(strings.TrimSpace(content))
	for _, a := range []string{"progress", "create", "update", "archive"} {
		if strings.HasPrefix(lower, a) {
			return a
		}
	}

	return ""
}

// ---------------------------------------------------------------------------
// Progress query — direct file I/O, no LLM
// ---------------------------------------------------------------------------

// runProgressQuery reads openspec/changes/ for the active change,
// parses tasks.md checkboxes, and returns a progress summary event.
// This does NOT create EventBus, does NOT start runEventLoop, does NOT call LLM.
func (pa *PlanAgent) runProgressQuery(ctx context.Context, inv *trpcagent.Invocation) (<-chan *event.Event, error) {
	summary := pa.buildProgressSummary()

	ch := make(chan *event.Event, 1)
	ch <- buildProgressEvent(summary)
	close(ch)
	return ch, nil
}

// buildProgressSummary scans for active changes and returns a progress summary string.
func (pa *PlanAgent) buildProgressSummary() string {
	changes := pa.scanActiveChanges()

	if len(changes) == 0 {
		return "当前没有活跃的工作计划。"
	}

	if len(changes) > 1 {
		var names []string
		for _, c := range changes {
			names = append(names, c)
		}
		return fmt.Sprintf("存在多个活跃的工作计划: %s\n请指定要查询的计划。", strings.Join(names, ", "))
	}

	// Exactly one active change
	changeName := changes[0]
	tasks, err := pa.parseTasksMd(changeName)
	if err != nil {
		return fmt.Sprintf("读取计划 %q 的 tasks.md 失败: %v", changeName, err)
	}
	if len(tasks) == 0 {
		return fmt.Sprintf("计划 %q 中没有任务。", changeName)
	}

	completed := 0
	for _, t := range tasks {
		if t.Done {
			completed++
		}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## 计划: %s (%d/%d 完成)\n\n", changeName, completed, len(tasks)))
	for _, t := range tasks {
		marker := "⏳"
		if t.Done {
			marker = "✓"
		}
		if t.ID != "" {
			sb.WriteString(fmt.Sprintf("%s %s %s\n", marker, t.ID, t.Title))
		} else {
			sb.WriteString(fmt.Sprintf("%s %s\n", marker, t.Title))
		}
	}
	return sb.String()
}

// TaskItem represents a single task parsed from tasks.md.
type TaskItem struct {
	ID    string
	Title string
	Done  bool
}

// scanActiveChanges scans openspec/changes/ (excluding archive/) for active directories.
func (pa *PlanAgent) scanActiveChanges() []string {
	changesDir := filepath.Join(pa.openSpecDir, "openspec", "changes")
	entries, err := os.ReadDir(changesDir)
	if err != nil {
		return nil
	}

	var active []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == "archive" {
			continue
		}
		active = append(active, name)
	}
	return active
}

// taskCheckboxRegex matches "- [ ] task" or "- [x] task".
var taskCheckboxRegex = regexp.MustCompile(`^-\s*\[([ xX])\]\s*(.+)$`)

// parseTasksMd reads and parses tasks.md from an active change directory.
func (pa *PlanAgent) parseTasksMd(changeName string) ([]TaskItem, error) {
	tasksPath := filepath.Join(pa.openSpecDir, "openspec", "changes", changeName, "tasks.md")
	data, err := os.ReadFile(tasksPath)
	if err != nil {
		return nil, err
	}

	var tasks []TaskItem
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		matches := taskCheckboxRegex.FindStringSubmatch(line)
		if matches == nil {
			continue
		}
		done := matches[1] == "x" || matches[1] == "X"
		rest := strings.TrimSpace(matches[2])
		taskID, title := parseTaskID(rest)
		tasks = append(tasks, TaskItem{ID: taskID, Title: title, Done: done})
	}
	return tasks, nil
}

// parseTaskID extracts a leading numeric ID (like "1.1", "2.3") from text.
var taskIDRegex = regexp.MustCompile(`^(\d+(?:\.\d+)*)\s+(.+)$`)

func parseTaskID(text string) (id, title string) {
	matches := taskIDRegex.FindStringSubmatch(text)
	if matches != nil {
		return matches[1], strings.TrimSpace(matches[2])
	}
	return "", text
}

// buildProgressEvent constructs an event.Event containing a final response
// with the progress summary. This mimics what the LLM would produce,
// but without any model call.
func buildProgressEvent(summary string) *event.Event {
	return &event.Event{
		Response: &model.Response{
			Choices: []model.Choice{
				{
					Message: model.Message{
						Role:    model.RoleAssistant,
						Content: summary,
					},
				},
			},
		},
		Tag: tagentevent.TypeAgentOutput,
	}
}
