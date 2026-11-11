// 契约: docs/wiki/rl/rl-architecture.md#live-benchmark
package runners

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/bench"

	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	trpcranner "trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

// baselineSystemPrompt fixes the one thing the baseline owns: how the loop asks
// the model to work. Every other runner contributes its own head prompt.
const baselineSystemPrompt = `You are a coding agent working alone in an isolated workspace.
Use the provided tools when the task needs them. Run shell commands only through run_command.
When you are done, reply with exactly what the task asks for: no preamble, no summary.`

// baselineRunner drives one plain trpc-agent-go agent loop over the task.
type baselineRunner struct {
	endpoint string
	apiKey   string
	model    string
}

// NewBaseline returns a runner over a plain trpc-agent-go llmagent loop.
func NewBaseline(endpoint, apiKeyEnv, modelName string) (runner, error) {
	if endpoint == "" || modelName == "" || apiKeyEnv == "" {
		return nil, fmt.Errorf("bench baseline: endpoint, api key env and model are required")
	}
	apiKey := os.Getenv(apiKeyEnv)
	if apiKey == "" {
		return nil, fmt.Errorf("bench baseline: %s is not set", apiKeyEnv)
	}
	return &baselineRunner{endpoint: endpoint, apiKey: apiKey, model: modelName}, nil
}

// Run executes the task's prompts in one session and answers the result envelope.
func (b *baselineRunner) Run(ctx context.Context, task bench.Task, workspace string) ([]byte, error) {
	if err := validateTask(task); err != nil {
		return nil, err
	}
	if workspace == "" {
		return nil, fmt.Errorf("bench baseline: workspace is empty")
	}
	stats := &modelStats{}
	client := openai.New(
		b.model,
		openai.WithAPIKey(b.apiKey),
		openai.WithBaseURL(b.endpoint),
	)
	counting := &countingModel{inner: client, stats: stats}
	agentValue := llmagent.New(
		"bench-baseline",
		llmagent.WithModel(counting),
		llmagent.WithInstruction(baselineSystemPrompt),
		llmagent.WithTools(baselineTools(workspace)),
		llmagent.WithMaxToolIterations(12),
		llmagent.WithGenerationConfig(model.GenerationConfig{
			Temperature: floatPointer(relayTemperature),
			MaxTokens:   intPointer(relayMaxTokens),
		}),
	)
	appRunner := trpcranner.NewRunner("bench-baseline", agentValue)
	defer appRunner.Close()
	sessionID := fmt.Sprintf("bench-baseline-%s-%d", task.ID, time.Now().UnixNano())
	taskStart := time.Now()
	output := ""
	delivered := false
	var failure string
	for _, prompt := range task.Prompts {
		events, err := appRunner.Run(ctx, "bench", sessionID, model.NewUserMessage(prompt))
		if err != nil {
			failure = err.Error()
			break
		}
		turnOutput, turnErr := consumeAgentEvents(events)
		if turnErr != "" {
			failure = turnErr
			break
		}
		output = turnOutput
		delivered = turnOutput != ""
	}
	stats.finish(time.Now())
	firstSamples, hotSamples, completeMS := stats.samples(taskStart)
	status := "ok"
	if failure != "" || !delivered {
		status = "failed"
	}
	return encodeResult(result{
		Runner:           "baseline",
		TaskID:           task.ID,
		Status:           status,
		Output:           output,
		Delivered:        delivered,
		APICalls:         stats.requests(),
		PromptTokens:     stats.promptTokens(),
		CompletionTokens: stats.completionTokens(),
		FirstTokenMS:     firstSamples,
		CompleteMS:       completeMS,
		Hot:              hotSamples,
		Error:            failure,
	})
}

// consumeAgentEvents returns the last completed assistant body and a failure
// text, if the loop surfaced one.
func consumeAgentEvents(events <-chan *event.Event) (string, string) {
	output := ""
	failure := ""
	for evt := range events {
		if evt == nil || evt.Response == nil {
			continue
		}
		if evt.Response.Error != nil {
			failure = evt.Response.Error.Message
		}
		if evt.IsRunnerCompletion() || evt.Response.IsPartial || len(evt.Choices) == 0 {
			continue
		}
		message := evt.Choices[0].Message
		if message.Role == model.RoleAssistant && message.Content != "" {
			output = message.Content
		}
	}
	return output, failure
}

// baselineTools builds the three function tools the baseline advertises.
func baselineTools(workspace string) []tool.Tool {
	return []tool.Tool{
		function.NewFunctionTool(
			func(_ context.Context, args readFileArgs) (readFileOutput, error) {
				full, err := bench.WorkspaceReadPath(workspace, args.Path)
				if err != nil {
					return readFileOutput{}, err
				}
				content, err := os.ReadFile(full)
				if err != nil {
					return readFileOutput{}, err
				}
				return readFileOutput{Content: string(content)}, nil
			},
			function.WithName("read_file"),
			function.WithDescription("Read a file inside the workspace and return its text."),
		),
		function.NewFunctionTool(
			func(_ context.Context, args writeFileArgs) (writeFileOutput, error) {
				full, err := bench.WorkspaceWritePath(workspace, args.Path)
				if err != nil {
					return writeFileOutput{}, err
				}
				if err := os.WriteFile(full, []byte(args.Content), 0o644); err != nil {
					return writeFileOutput{}, err
				}
				return writeFileOutput{Path: args.Path, Bytes: len(args.Content)}, nil
			},
			function.WithName("write_file"),
			function.WithDescription("Create or replace a file inside the workspace."),
		),
		function.NewFunctionTool(
			func(ctx context.Context, args runCommandArgs) (runCommandOutput, error) {
				if args.Command == "" {
					return runCommandOutput{}, fmt.Errorf("command is empty")
				}
				runCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
				defer cancel()
				command := exec.CommandContext(runCtx, "sh", "-c", args.Command)
				command.Dir = workspace
				raw, err := command.CombinedOutput()
				exitCode := 0
				if err != nil {
					if exitErr, ok := err.(*exec.ExitError); ok {
						exitCode = exitErr.ExitCode()
					} else {
						return runCommandOutput{}, err
					}
				}
				return runCommandOutput{Output: string(raw), ExitCode: exitCode}, nil
			},
			function.WithName("run_command"),
			function.WithDescription("Run one shell command with the workspace as its working directory and return its output and exit code."),
		),
	}
}

// readFileArgs, writeFileArgs and runCommandArgs are the baseline tool schemas.
type readFileArgs struct {
	Path string `json:"path"`
}

type readFileOutput struct {
	Content string `json:"content"`
}

type writeFileArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type writeFileOutput struct {
	Path  string `json:"path"`
	Bytes int    `json:"bytes"`
}

type runCommandArgs struct {
	Command string `json:"command"`
}

type runCommandOutput struct {
	Output   string `json:"output"`
	ExitCode int    `json:"exit_code"`
}

// modelStats collects the request timings and token usage of one attempt.
type modelStats struct {
	mu         sync.Mutex
	starts     []time.Time
	firsts     []time.Time
	completes  []time.Time
	prompt     int
	completion int
}

// addUsage records one completed model request's usage.
func (s *modelStats) addUsage(prompt, completion int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prompt += prompt
	s.completion += completion
}

// startRequest records a new request start and returns its ordinal.
func (s *modelStats) startRequest() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.starts = append(s.starts, time.Now())
	s.firsts = append(s.firsts, time.Time{})
	s.completes = append(s.completes, time.Time{})
	return len(s.starts) - 1
}

// firstToken marks the first token of a request.
func (s *modelStats) firstToken(index int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if index >= 0 && index < len(s.firsts) && s.firsts[index].IsZero() {
		s.firsts[index] = time.Now()
	}
}

// finishRequest marks a request complete.
func (s *modelStats) finishRequest(index int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if index >= 0 && index < len(s.completes) {
		s.completes[index] = time.Now()
	}
}

// finish records the attempt end for requests that never closed.
func (s *modelStats) finish(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for index := range s.completes {
		if s.completes[index].IsZero() {
			s.completes[index] = now
		}
	}
}

// samples returns the cold first-token time, the hot samples and the total
// attempt completion time.
func (s *modelStats) samples(taskStart time.Time) (float64, []hotSample, float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cold := 0.0
	if len(s.firsts) > 0 && !s.firsts[0].IsZero() {
		cold = milliseconds(s.firsts[0].Sub(taskStart))
	}
	hot := make([]hotSample, 0, max(0, len(s.starts)-1))
	for index := 1; index < len(s.starts); index++ {
		first := 0.0
		if !s.firsts[index].IsZero() {
			first = milliseconds(s.firsts[index].Sub(s.starts[index]))
		}
		complete := 0.0
		if !s.completes[index].IsZero() {
			complete = milliseconds(s.completes[index].Sub(s.starts[index]))
		}
		hot = append(hot, hotSample{FirstTokenMS: first, CompleteMS: complete})
	}
	complete := 0.0
	if last := len(s.completes) - 1; last >= 0 && !s.completes[last].IsZero() {
		complete = milliseconds(s.completes[last].Sub(taskStart))
	}
	return cold, hot, complete
}

func (s *modelStats) requests() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.starts)
}

func (s *modelStats) promptTokens() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.prompt
}

func (s *modelStats) completionTokens() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.completion
}

// countingModel times each GenerateContent stream and marks its first token.
type countingModel struct {
	inner model.Model
	stats *modelStats
}

func (m *countingModel) Info() model.Info { return m.inner.Info() }

func (m *countingModel) GenerateContent(ctx context.Context, request *model.Request) (<-chan *model.Response, error) {
	index := m.stats.startRequest()
	stream, err := m.inner.GenerateContent(ctx, request)
	if err != nil {
		m.stats.finishRequest(index)
		return nil, err
	}
	out := make(chan *model.Response)
	go func() {
		defer close(out)
		for response := range stream {
			if response != nil && response.Usage != nil {
				m.stats.addUsage(response.Usage.PromptTokens, response.Usage.CompletionTokens)
			}
			if response != nil && responseHasOutput(response) {
				m.stats.firstToken(index)
			}
			out <- response
		}
		m.stats.finishRequest(index)
	}()
	return out, nil
}

// responseHasOutput reports whether one response carries model output.
func responseHasOutput(response *model.Response) bool {
	for _, choice := range response.Choices {
		if choice.Message.Content != "" || choice.Message.ReasoningContent != "" || len(choice.Message.ToolCalls) > 0 {
			return true
		}
	}
	return false
}

func milliseconds(duration time.Duration) float64 {
	return float64(duration.Microseconds()) / 1000
}

func floatPointer(value float64) *float64 { return &value }
func intPointer(value int) *int           { return &value }
