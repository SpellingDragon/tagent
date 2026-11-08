// 契约: docs/wiki/rl/rl-architecture.md#live-benchmark
package runners

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/SpellingDragon/tagent/bench"
)

// dshDefaultCheckout is the local DeepSeek Harness checkout the adapter drives
// when BENCH_DSH_CHECKOUT is unset.
const dshDefaultCheckout = "/Users/pengweiye/Documents/codes/deepseek-harness"

// dshNodeRelativePath is the bundled Node.js runtime inside a built checkout.
const dshNodeRelativePath = "apps/desktop/.desktop-build/targets/mac-arm64/runtime/primary-runtime/dependencies/node/bin/node"

// DSHExecutable resolves the node program and CLI argument the adapter runs.
func DSHExecutable(checkout string) (string, []string, error) {
	if checkout == "" {
		checkout = os.Getenv("BENCH_DSH_CHECKOUT")
	}
	if checkout == "" {
		checkout = dshDefaultCheckout
	}
	checkout = filepath.Clean(checkout)
	if _, err := os.Stat(checkout); err != nil {
		return "", nil, fmt.Errorf("bench dsh: checkout %s: %w", checkout, err)
	}
	cli := os.Getenv("BENCH_DSH_CLI")
	if cli == "" {
		cli = filepath.Join(checkout, "apps", "cli", "lib", "bin.js")
	}
	if _, err := os.Stat(cli); err != nil {
		return "", nil, fmt.Errorf("bench dsh: cli %s: %w", cli, err)
	}
	node := os.Getenv("BENCH_DSH_NODE")
	if node == "" {
		node = filepath.Join(checkout, dshNodeRelativePath)
	}
	if _, err := os.Stat(node); err != nil {
		resolved, lookErr := exec.LookPath("node")
		if lookErr != nil {
			return "", nil, fmt.Errorf("bench dsh: node: %w", err)
		}
		node = resolved
	}
	return node, []string{cli}, nil
}

// dshRunner drives the DeepSeek Harness headless CLI, one process per user
// turn, joined by the session identity the first process reports.
type dshRunner struct {
	program   string
	arguments []string
	endpoint  string
	apiKeyEnv string
	model     string
}

// NewDSH returns a runner over the headless CLI at program arguments.
func NewDSH(program string, arguments []string, endpoint, apiKeyEnv, modelName string) (runner, error) {
	if program == "" || endpoint == "" || apiKeyEnv == "" || modelName == "" {
		return nil, fmt.Errorf("bench dsh: program, endpoint, api key env and model are required")
	}
	if strings.ContainsRune(program, filepath.Separator) {
		if _, err := os.Stat(program); err != nil {
			return nil, fmt.Errorf("bench dsh: program %s: %w", program, err)
		}
	}
	return &dshRunner{program: program, arguments: append([]string(nil), arguments...), endpoint: endpoint, apiKeyEnv: apiKeyEnv, model: modelName}, nil
}

// Run executes the task's prompts through the headless CLI and answers the
// result envelope.
func (d *dshRunner) Run(ctx context.Context, task bench.Task, workspace string) ([]byte, error) {
	if err := validateTask(task); err != nil {
		return nil, err
	}
	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return nil, fmt.Errorf("bench dsh: workspace %s: %w", workspace, err)
	}
	relay, err := NewModelRelay(d.endpoint)
	if err != nil {
		return nil, err
	}
	defer relay.Close()
	home, err := os.MkdirTemp("", "bench-dsh-home-*")
	if err != nil {
		return nil, fmt.Errorf("bench dsh: home: %w", err)
	}
	defer os.RemoveAll(home)
	patch := filepath.Join(home, "bench.patch.yml")
	if err := os.WriteFile(patch, []byte(d.patch(relay.URL())), 0o644); err != nil {
		return nil, fmt.Errorf("bench dsh: write patch: %w", err)
	}
	runCtx, cancel := context.WithTimeout(ctx, 6*time.Minute)
	defer cancel()
	taskStart := time.Now()
	sessionID := ""
	output := ""
	delivered := true
	failure := ""
	apiCalls := 0
	promptTokens := 0
	completionTokens := 0
	coldFirst := 0.0
	firstRequest := true
	hot := make([]hotSample, 0, 4)
	for _, prompt := range task.Prompts {
		arguments := append([]string(nil), d.arguments...)
		arguments = append(arguments,
			"--profile", "headless",
			"--patch", patch,
			"--json",
			"--permission-mode", "workspace-write",
			"--approval-policy", "never")
		if sessionID != "" {
			arguments = append(arguments, "--session-id", sessionID)
		}
		arguments = append(arguments, prompt)
		command := exec.CommandContext(runCtx, d.program, arguments...)
		command.Dir = absWorkspace
		command.Env = append(os.Environ(), "DSH_HOME="+home)
		stdout, stdoutErr := command.StdoutPipe()
		if stdoutErr != nil {
			return nil, fmt.Errorf("bench dsh: stdout: %w", stdoutErr)
		}
		var stderr bytes.Buffer
		command.Stderr = &stderr
		turnStart := time.Now()
		if err := command.Start(); err != nil {
			return nil, fmt.Errorf("bench dsh: start: %w", err)
		}
		parsed, parseErr := parseDSHEvents(stdout)
		waitErr := command.Wait()
		if parseErr != nil {
			return nil, fmt.Errorf("bench dsh: read events: %w", parseErr)
		}
		if waitErr != nil {
			failure = fmt.Sprintf("headless exit: %v: %s", waitErr, tail(stderr.String(), 2000))
			delivered = false
			break
		}
		if parsed.sessionID != "" {
			sessionID = parsed.sessionID
		}
		if parsed.finalText == "" {
			failure = "headless turn produced no final text"
			delivered = false
		}
		output = parsed.finalText
		apiCalls += len(parsed.steps)
		for _, step := range parsed.steps {
			promptTokens += step.promptTokens
			completionTokens += step.completionTokens
			if firstRequest {
				firstRequest = false
				if !step.firstTokenAt.IsZero() {
					coldFirst = milliseconds(step.firstTokenAt.Sub(taskStart))
				}
			} else {
				hot = append(hot, hotSample{
					FirstTokenMS: milliseconds(step.firstTokenAt.Sub(step.startedAt)),
					CompleteMS:   milliseconds(step.completedAt.Sub(step.startedAt)),
				})
			}
		}
		_ = turnStart
		if failure != "" {
			break
		}
	}
	if coldFirst == 0.0 {
		coldFirst = milliseconds(time.Since(taskStart))
	}
	status := "ok"
	if failure != "" || !delivered {
		status = "failed"
	}
	return encodeResult(result{
		Runner:           "dsh",
		TaskID:           task.ID,
		Status:           status,
		Output:           output,
		Delivered:        delivered,
		APICalls:         apiCalls,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		FirstTokenMS:     coldFirst,
		CompleteMS:       milliseconds(time.Since(taskStart)),
		Hot:              hot,
		Error:            failure,
	})
}

// patch renders the profile overlay that routes the headless profile at the
// relay and fixes the sampling contract.
func (d *dshRunner) patch(relayURL string) string {
	var out strings.Builder
	out.WriteString("- id: session-title-llm\n  disabled: true\n")
	out.WriteString("- id: llm-pi-ai\n  config:\n    providers:\n      zai:\n")
	out.WriteString("        displayName: Bench ZAI\n")
	out.WriteString("        apiKeyEnv: " + strconv.Quote(d.apiKeyEnv) + "\n")
	out.WriteString("        api: openai-completions\n")
	out.WriteString("        baseURL: " + strconv.Quote(relayURL) + "\n")
	out.WriteString("        models:\n")
	out.WriteString("          - id: " + strconv.Quote(d.model) + "\n")
	out.WriteString("            name: " + strconv.Quote(d.model) + "\n")
	out.WriteString("            contextWindow: 200000\n")
	out.WriteString("            maxTokens: " + strconv.Itoa(relayMaxTokens) + "\n")
	out.WriteString("- id: agent-default-model\n  config:\n")
	out.WriteString("    provider: zai\n")
	out.WriteString("    model: " + strconv.Quote(d.model) + "\n")
	return out.String()
}

// dshStep is one model request of one headless process.
type dshStep struct {
	startedAt        time.Time
	firstTokenAt     time.Time
	completedAt      time.Time
	promptTokens     int
	completionTokens int
}

// dshTurn is the parsed event stream of one headless process.
type dshTurn struct {
	sessionID string
	finalText string
	steps     []dshStep
}

// parseDSHEvents reads the NDJSON event stream of one headless turn.
func parseDSHEvents(stdout io.Reader) (dshTurn, error) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	turn := dshTurn{}
	stepIndex := -1
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var event struct {
			Type      string `json:"type"`
			Phase     string `json:"phase"`
			Text      string `json:"text"`
			SessionID string `json:"sessionId"`
			Usage     *struct {
				InputTokens  int `json:"inputTokens"`
				OutputTokens int `json:"outputTokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}
		switch event.Type {
		case "session":
			turn.sessionID = event.SessionID
		case "status":
			switch event.Phase {
			case "step_start":
				stepIndex++
				turn.steps = append(turn.steps, dshStep{startedAt: time.Now()})
			case "step_end":
				if stepIndex >= 0 {
					turn.steps[stepIndex].completedAt = time.Now()
					if event.Usage != nil {
						turn.steps[stepIndex].promptTokens = event.Usage.InputTokens
						turn.steps[stepIndex].completionTokens = event.Usage.OutputTokens
					}
				}
			}
		case "text", "thinking", "tool_call":
			if stepIndex >= 0 && turn.steps[stepIndex].firstTokenAt.IsZero() {
				turn.steps[stepIndex].firstTokenAt = time.Now()
			}
		case "final":
			turn.finalText = event.Text
		}
	}
	if err := scanner.Err(); err != nil {
		return dshTurn{}, err
	}
	for index := range turn.steps {
		if turn.steps[index].completedAt.IsZero() {
			turn.steps[index].completedAt = time.Now()
		}
		if turn.steps[index].firstTokenAt.IsZero() {
			turn.steps[index].firstTokenAt = turn.steps[index].completedAt
		}
	}
	return turn, nil
}
