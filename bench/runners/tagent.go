// 契约: docs/wiki/rl/rl-architecture.md#live-benchmark
package runners

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/SpellingDragon/tagent/bench"
)

// BuildBridge builds the wechat-bot bench-bridge into dst and returns its path.
func BuildBridge(dst string) (string, error) {
	root, err := repositoryRoot()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return "", fmt.Errorf("bench tagent: create bridge dir: %w", err)
	}
	binary := filepath.Join(dst, "bench-bridge")
	command := exec.Command("go", "build", "-o", binary, "./cmd/bench-bridge")
	command.Dir = filepath.Join(root, "examples", "wechat-bot")
	command.Env = os.Environ()
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("bench tagent: build bridge: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return binary, nil
}

// repositoryRoot resolves the tagent checkout from this source file.
func repositoryRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("bench tagent: cannot resolve the repository root")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..")), nil
}

// tagentRunner drives the wechat-bot bench-bridge subprocess.
type tagentRunner struct {
	bridge       string
	wechatConfig string
	endpoint     string
	apiKeyEnv    string
	model        string
}

// NewTagent returns a runner over the bench-bridge subprocess.
func NewTagent(bridge, wechatConfig, endpoint, apiKeyEnv, modelName string) (runner, error) {
	if bridge == "" || wechatConfig == "" || endpoint == "" || apiKeyEnv == "" || modelName == "" {
		return nil, fmt.Errorf("bench tagent: bridge, config, endpoint, api key env and model are required")
	}
	if _, err := os.Stat(bridge); err != nil {
		return nil, fmt.Errorf("bench tagent: bridge %s: %w", bridge, err)
	}
	if _, err := os.Stat(wechatConfig); err != nil {
		return nil, fmt.Errorf("bench tagent: wechat config %s: %w", wechatConfig, err)
	}
	return &tagentRunner{bridge: bridge, wechatConfig: wechatConfig, endpoint: endpoint, apiKeyEnv: apiKeyEnv, model: modelName}, nil
}

// Run sends one task envelope to the bridge and reads the result envelope.
func (t *tagentRunner) Run(ctx context.Context, task bench.Task, workspace string) ([]byte, error) {
	if err := validateTask(task); err != nil {
		return nil, err
	}
	request, err := json.Marshal(map[string]any{
		"protocol":  protocolVersion,
		"task_id":   task.ID,
		"prompts":   task.Prompts,
		"workspace": workspace,
	})
	if err != nil {
		return nil, fmt.Errorf("bench tagent: encode request: %w", err)
	}
	runCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	command := exec.CommandContext(runCtx, t.bridge,
		"--wechat-config", t.wechatConfig,
		"--upstream", t.endpoint,
		"--api-key-env", t.apiKeyEnv,
		"--model", t.model,
	)
	command.Dir = workspace
	command.Stdin = bytes.NewReader(append(request, '\n'))
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	start := time.Now()
	runErr := command.Run()
	elapsed := milliseconds(time.Since(start))
	if runErr != nil {
		return nil, fmt.Errorf("bench tagent: bridge: %w: %s", runErr, tail(stderr.String(), 2000))
	}
	payload, err := protocolLine(stdout.Bytes())
	if err != nil {
		return nil, fmt.Errorf("bench tagent: %w: %s", err, tail(stderr.String(), 2000))
	}
	value, err := decodeResult(payload)
	if err != nil {
		return nil, err
	}
	if value.Protocol != protocolVersion || value.Runner != "tagent" || value.TaskID != task.ID {
		return nil, fmt.Errorf("bench tagent: bridge answered %s/%s/%s", value.Protocol, value.Runner, value.TaskID)
	}
	if !value.Delivered && value.Status == "ok" {
		return nil, fmt.Errorf("bench tagent: bridge answered ok without a delivery receipt")
	}
	value.CompleteMS = elapsed
	return encodeResult(value)
}

// protocolLine picks the bridge result line out of stdout, tolerating host logs
// that share the stream.
func protocolLine(raw []byte) ([]byte, error) {
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if !bytes.HasPrefix(line, []byte("{")) {
			continue
		}
		var probe struct {
			Protocol string `json:"protocol"`
		}
		if err := json.Unmarshal(line, &probe); err != nil || probe.Protocol != protocolVersion {
			continue
		}
		return append([]byte(nil), line...), nil
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("bench tagent: read bridge stdout: %w", err)
	}
	return nil, fmt.Errorf("bench tagent: bridge printed no result envelope")
}

// tail returns the last limit bytes of text.
func tail(text string, limit int) string {
	if len(text) <= limit {
		return strings.TrimSpace(text)
	}
	return strings.TrimSpace(text[len(text)-limit:])
}
