// 契约: docs/wiki/rl/rl-architecture.md#live-benchmark
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	tagentmodel "github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/bench/runners"
	tagenttask "github.com/SpellingDragon/tagent/runtime/task"

	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"
)

// mainStart anchors the cold timing: process start to the first model token.
var mainStart = time.Now()

// protocolVersion is the line protocol the runner and the bridge share.
const protocolVersion = "bench-runner-v1"

func main() {
	wechatConfig := flag.String("wechat-config", "", "path to the wechat-bot tagent.yaml")
	upstream := flag.String("upstream", "", "OpenAI-compatible upstream the relay forwards to")
	apiKeyEnv := flag.String("api-key-env", "", "environment variable holding the upstream API key")
	modelName := flag.String("model", "", "model id the host samples")
	describe := flag.Bool("describe", false, "print the advertised tool face and exit")
	flag.Parse()

	if *describe {
		if err := printDescribe(*wechatConfig); err != nil {
			fail(err)
		}
		return
	}
	if *wechatConfig == "" || *upstream == "" || *apiKeyEnv == "" || *modelName == "" {
		fail(fmt.Errorf("bench bridge: --wechat-config, --upstream, --api-key-env and --model are required"))
	}
	relay, err := runners.NewModelRelay(*upstream)
	if err != nil {
		fail(err)
	}
	defer relay.Close()
	promptDir, err := os.MkdirTemp("", "bench-bridge-prompts-*")
	if err != nil {
		fail(fmt.Errorf("bench bridge: prompt dir: %w", err))
	}
	defer os.RemoveAll(promptDir)
	if err := os.WriteFile(filepath.Join(promptDir, "bench_action.md"), []byte(benchPrompt), 0o644); err != nil {
		fail(fmt.Errorf("bench bridge: write prompt: %w", err))
	}
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(trimSpace(line)) == 0 {
			continue
		}
		var req request
		if err := json.Unmarshal(line, &req); err != nil {
			fail(fmt.Errorf("bench bridge: decode request: %w", err))
		}
		answer := handle(req, bridgeConfig{
			workspace:    req.Workspace,
			wechatConfig: *wechatConfig,
			promptDir:    promptDir,
			relayURL:     relay.URL(),
			apiKeyEnv:    *apiKeyEnv,
			model:        *modelName,
		}, relay)
		encoded, err := json.Marshal(answer)
		if err != nil {
			fail(fmt.Errorf("bench bridge: encode response: %w", err))
		}
		fmt.Println(string(encoded))
	}
	if err := scanner.Err(); err != nil {
		fail(fmt.Errorf("bench bridge: read stdin: %w", err))
	}
}

// printDescribe advertises the granted tool face read out of the production
// wechat-bot declaration beside the registered capability map.
func printDescribe(wechatConfig string) error {
	if wechatConfig == "" {
		return fmt.Errorf("bench bridge: --wechat-config is required with --describe")
	}
	tools, err := declaredTools(wechatConfig)
	if err != nil {
		return err
	}
	var mapping struct {
		Tagent map[string][]string `json:"tagent"`
	}
	if err := json.Unmarshal(runners.CapabilityMap(), &mapping); err != nil {
		return fmt.Errorf("bench bridge: capability map: %w", err)
	}
	encoded, err := json.Marshal(describeResponse{
		Protocol:     protocolVersion,
		Runner:       "tagent",
		Tools:        tools,
		Capabilities: mapping.Tagent,
	})
	if err != nil {
		return fmt.Errorf("bench bridge: encode describe: %w", err)
	}
	fmt.Println(string(encoded))
	return nil
}

// handle assembles one host for the task, drives its prompts and answers the
// result envelope.
func handle(req request, config bridgeConfig, relay interface {
	Reset()
	StatsJSON() ([]byte, error)
}) response {
	answer := response{
		Protocol: protocolVersion,
		Runner:   "tagent",
		TaskID:   req.TaskID,
		Status:   "failed",
	}
	if req.Protocol != protocolVersion || req.TaskID == "" || len(req.Prompts) == 0 {
		answer.Error = "bench bridge: malformed request"
		return answer
	}
	relay.Reset()
	document, err := buildHostDocument(config)
	if err != nil {
		answer.Error = err.Error()
		return answer
	}
	dir, err := os.MkdirTemp("", "bench-bridge-doc-*")
	if err != nil {
		answer.Error = err.Error()
		return answer
	}
	defer os.RemoveAll(dir)
	configPath := filepath.Join(dir, "host.yaml")
	if err := os.WriteFile(configPath, document, 0o644); err != nil {
		answer.Error = err.Error()
		return answer
	}
	sender := &recordingSender{}
	approver := &countingApprover{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	host, err := assembly.Boot(configPath, assembly.WithSender(sender), assembly.WithApprover(approver.approve))
	if err != nil {
		answer.Error = "bench bridge: boot: " + err.Error()
		return answer
	}
	defer func() { _ = host.Close(context.Background()) }()
	readyAt := time.Now()
	session := fmt.Sprintf("bench-%s-%d", req.TaskID, time.Now().UnixNano())
	failure := ""
	for index, prompt := range req.Prompts {
		turn, err := host.Invoke(ctx, assembly.InvokeRequest{
			InvocationID:  event.InvocationID(fmt.Sprintf("%s-%d", req.TaskID, index)),
			Address:       event.Address{AgentID: "action", SessionID: event.SessionID(session)},
			TriggerSource: "user",
			Input:         []trpcmodel.Message{{Role: trpcmodel.RoleUser, Content: prompt}},
		})
		if err != nil {
			failure = err.Error()
			break
		}
		if turn.Terminal == tagentmodel.KindFailed || turn.Terminal == tagentmodel.KindCancelled {
			failure = turn.Failure
			if failure == "" {
				failure = "turn terminal: " + fmt.Sprint(turn.Terminal)
			}
			break
		}
	}
	output, _ := sender.snapshot()
	answer.Output = output
	answer.Delivered = failure == "" && output != ""
	if failure != "" {
		answer.Error = failure
	}
	if answer.Delivered {
		answer.Status = "ok"
	}
	answer.ApprovalCalls = approver.count()
	stats := relayStats{}
	if raw, err := relay.StatsJSON(); err == nil {
		_ = json.Unmarshal(raw, &stats)
	}
	answer.APICalls = stats.APICalls
	answer.PromptTokens = stats.PromptTokens
	answer.CompletionTokens = stats.CompletionTokens
	answer.FirstTokenMS = milliseconds(readyAt.Sub(mainStart))
	if len(stats.Requests) > 0 {
		answer.FirstTokenMS += stats.Requests[0].FirstTokenMS
		answer.Hot = make([]sample, 0, len(stats.Requests)-1)
		for _, value := range stats.Requests[1:] {
			answer.Hot = append(answer.Hot, sample{FirstTokenMS: value.FirstTokenMS, CompleteMS: value.CompleteMS})
		}
	}
	answer.CompleteMS = milliseconds(time.Since(mainStart))
	if stats.Error != "" && answer.Error == "" {
		answer.Error = stats.Error
	}
	return answer
}

// relayStats is the relay's stats document.
type relayStats struct {
	APICalls         int      `json:"api_calls"`
	PromptTokens     int      `json:"prompt_tokens"`
	CompletionTokens int      `json:"completion_tokens"`
	Requests         []sample `json:"requests"`
	Error            string   `json:"error"`
}

// recordingSender is the outbound face: it records the last delivered body and
// the number of accepted bodies.
type recordingSender struct {
	mu    sync.Mutex
	last  string
	count int
}

func (s *recordingSender) Send(_ context.Context, _ event.Address, content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.last = content
	s.count++
	return nil
}

func (s *recordingSender) snapshot() (string, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last, s.count
}

// countingApprover admits every call and counts how often the approval channel
// was consulted.
type countingApprover struct {
	mu    sync.Mutex
	calls int
}

func (a *countingApprover) approve(_ context.Context, _ string, _ []byte) (tagenttask.Ruling, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls++
	return tagenttask.Ruling{Kind: tagenttask.RulingRun, Reason: "bench: unattended"}, nil
}

func (a *countingApprover) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.calls
}

// milliseconds renders a duration as fractional milliseconds.
func milliseconds(value time.Duration) float64 {
	return float64(value.Microseconds()) / 1000
}

// trimSpace drops ASCII space around a request line.
func trimSpace(raw []byte) []byte {
	start, end := 0, len(raw)
	for start < end && (raw[start] == ' ' || raw[start] == '\t' || raw[start] == '\r' || raw[start] == '\n') {
		start++
	}
	for end > start && (raw[end-1] == ' ' || raw[end-1] == '\t' || raw[end-1] == '\r' || raw[end-1] == '\n') {
		end--
	}
	return raw[start:end]
}

// fail reports one startup or protocol failure to stderr and exits nonzero.
func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
