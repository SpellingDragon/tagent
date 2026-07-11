package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// ---------------------------------------------------------------------------
// PlanProgressTracker — 活跃 openspec change 进度注入
//
// 作为 BeforeModel 回调，在每次 LLM 调用前扫描 openspec/changes/ 目录，
// 如果恰好 1 个活跃 change，读取其 tasks.md，解析 checkbox 状态，
// 将进度摘要作为 system message 追加到 messages 末尾。
//
// 直接读文件系统，不走 plan 子 agent —— 进度注入是高频操作，
// 启动子 agent 开销太大。PlanProgressTracker 只读不写，
// 不破坏 plan agent 对 openspec 文件的独占写权。
//
// 遵循原型不变量 2: Compact 只修改投影 —— PlanProgressTracker 只注入
// 摘要到 messages（Layer 1 视图），不修改 MemoryStore 或 EventBus。
// ---------------------------------------------------------------------------

// PlanProgressTracker injects active openspec change progress into LLM context.
type PlanProgressTracker struct {
	openSpecDir string
}

// NewPlanProgressTracker creates a PlanProgressTracker with the given openspec root directory.
func NewPlanProgressTracker(openSpecDir string) *PlanProgressTracker {
	if openSpecDir == "" {
		openSpecDir = "."
	}
	return &PlanProgressTracker{openSpecDir: openSpecDir}
}

// TaskItem represents a single task parsed from tasks.md.
type TaskItem struct {
	ID    string // e.g., "1.1", "2.3"
	Title string // task description text
	Done  bool   // true if checkbox is [x]
}

// RegisterCallback registers the PlanProgressTracker as a BeforeModel callback.
func (t *PlanProgressTracker) RegisterCallback(cb *model.Callbacks) {
	cb.RegisterBeforeModel(func(ctx context.Context, args *model.BeforeModelArgs) (*model.BeforeModelResult, error) {
		t.InjectProgress(args)
		return nil, nil
	})
}

// InjectProgress scans for active openspec changes and injects progress summary.
func (t *PlanProgressTracker) InjectProgress(args *model.BeforeModelArgs) {
	changes := t.scanActiveChanges()
	if len(changes) != 1 {
		return
	}

	changeName := changes[0]
	tasks, err := t.parseTasksMd(changeName)
	if err != nil {
		log.Warnf("[PlanProgressTracker] failed to parse tasks.md for %q: %v", changeName, err)
		return
	}
	if len(tasks) == 0 {
		return
	}

	summary := t.buildProgressSummary(changeName, tasks)
	args.Request.Messages = append(args.Request.Messages, model.Message{
		Role:    model.RoleSystem,
		Content: summary,
	})

	completed := 0
	for _, task := range tasks {
		if task.Done {
			completed++
		}
	}
	log.Debugf("[PlanProgressTracker] active change: %s, progress: %d/%d", changeName, completed, len(tasks))
}

// scanActiveChanges scans openspec/changes/ (excluding archive/) for active change directories.
func (t *PlanProgressTracker) scanActiveChanges() []string {
	changesDir := filepath.Join(t.openSpecDir, "openspec", "changes")
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

// taskCheckboxRegex matches lines like "- [ ] 1.1 some task" or "- [x] 1.2 done task".
var taskCheckboxRegex = regexp.MustCompile(`^-\s*\[([ xX])\]\s*(.+)$`)

// parseTasksMd reads and parses tasks.md from an active change directory.
func (t *PlanProgressTracker) parseTasksMd(changeName string) ([]TaskItem, error) {
	tasksPath := filepath.Join(t.openSpecDir, "openspec", "changes", changeName, "tasks.md")
	data, err := os.ReadFile(tasksPath)
	if err != nil {
		return nil, err
	}

	var tasks []TaskItem
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		matches := taskCheckboxRegex.FindStringSubmatch(line)
		if matches == nil {
			continue
		}
		done := matches[1] == "x" || matches[1] == "X"
		rest := strings.TrimSpace(matches[2])
		// Try to extract task ID (e.g., "1.1", "2.3") from the beginning.
		taskID, title := parseTaskID(rest)
		tasks = append(tasks, TaskItem{
			ID:    taskID,
			Title: title,
			Done:  done,
		})
	}
	return tasks, nil
}

// parseTaskID extracts a leading numeric ID (like "1.1", "2.3") from the task text.
// If no ID is found, returns empty ID and the full text as title.
func parseTaskID(text string) (id, title string) {
	// Match patterns like "1.1 ", "2.3.4 ", "10. "
	idRegex := regexp.MustCompile(`^(\d+(?:\.\d+)*)\s+(.+)$`)
	matches := idRegex.FindStringSubmatch(text)
	if matches != nil {
		return matches[1], strings.TrimSpace(matches[2])
	}
	return "", text
}

// buildProgressSummary generates a progress summary string from tasks.
func (t *PlanProgressTracker) buildProgressSummary(changeName string, tasks []TaskItem) string {
	completed := 0
	for _, task := range tasks {
		if task.Done {
			completed++
		}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[active_plan] %s (%d/%d 完成)\n", changeName, completed, len(tasks)))
	for _, task := range tasks {
		marker := "⏳"
		if task.Done {
			marker = "✓"
		}
		if task.ID != "" {
			sb.WriteString(fmt.Sprintf("%s %s %s\n", marker, task.ID, task.Title))
		} else {
			sb.WriteString(fmt.Sprintf("%s %s\n", marker, task.Title))
		}
	}
	return sb.String()
}
