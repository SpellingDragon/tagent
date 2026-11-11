// 契约: docs/wiki/rl/rl-architecture.md#live-benchmark
package bench

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Verifier 是任务产物的机读验收口：同一份实现供 bench 三方 runner 与 07 训练
// 样本的独立验收复用，成功判定不依赖人眼。
//
// 验收红线清单（2 条）：
// MUST 只依据工作区内的可复核证据（响应文本 / 文件 / 命令输出）判定
// MUST NOT 信任被测模型的自我声明，也不读取工作区以外的路径
type Verifier interface {
	// Verify accepts or rejects one task artifact. workspace is the isolated
	// task directory; output is the final response text of the run. Every
	// declared check must pass; the returned error names the failing check.
	Verify(workspace, output string) error
}

const (
	defaultCommandTimeout = 5 * time.Second
	maxCommandTimeout     = 30 * time.Second
)

type specVerifier struct {
	response *responseCheck
	file     *fileCheck
	command  *commandCheck
}

func (v *specVerifier) Verify(workspace, output string) error {
	var failures []error
	if v.response != nil {
		if err := v.response.verify(output); err != nil {
			failures = append(failures, fmt.Errorf("response: %w", err))
		}
	}
	if v.file != nil {
		if err := v.file.verify(workspace); err != nil {
			failures = append(failures, fmt.Errorf("file: %w", err))
		}
	}
	if v.command != nil {
		if err := v.command.verify(workspace); err != nil {
			failures = append(failures, fmt.Errorf("command: %w", err))
		}
	}
	return errors.Join(failures...)
}

type responseCheck struct {
	equals         *string
	mustContain    []string
	mustNotContain []string
	mustMatch      []*regexp.Regexp
}

func (c *responseCheck) verify(output string) error {
	var failures []error
	if c.equals != nil && strings.TrimSpace(output) != strings.TrimSpace(*c.equals) {
		failures = append(failures, fmt.Errorf("response want %q, got %q", *c.equals, abbreviate(output)))
	}
	for _, want := range c.mustContain {
		if !strings.Contains(output, want) {
			failures = append(failures, fmt.Errorf("response does not contain %q", want))
		}
	}
	for _, banned := range c.mustNotContain {
		if strings.Contains(output, banned) {
			failures = append(failures, fmt.Errorf("response contains forbidden %q", banned))
		}
	}
	for _, expression := range c.mustMatch {
		if !expression.MatchString(output) {
			failures = append(failures, fmt.Errorf("response does not match %q", expression.String()))
		}
	}
	return errors.Join(failures...)
}

type fileCheck struct {
	path           string
	equals         *string
	mustContain    []string
	mustNotContain []string
	mustMatch      []*regexp.Regexp
}

func (c *fileCheck) verify(workspace string) error {
	path, err := workspaceReadPath(workspace, c.path)
	if err != nil {
		return fmt.Errorf("read %s: %w", c.path, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", c.path, err)
	}
	content := string(data)
	var failures []error
	if c.equals != nil && content != *c.equals {
		failures = append(failures, fmt.Errorf("%s want exactly %q, got %q", c.path, *c.equals, abbreviate(content)))
	}
	for _, want := range c.mustContain {
		if !strings.Contains(content, want) {
			failures = append(failures, fmt.Errorf("%s does not contain %q", c.path, want))
		}
	}
	for _, banned := range c.mustNotContain {
		if strings.Contains(content, banned) {
			failures = append(failures, fmt.Errorf("%s contains forbidden %q", c.path, banned))
		}
	}
	for _, expression := range c.mustMatch {
		if !expression.MatchString(content) {
			failures = append(failures, fmt.Errorf("%s does not match %q", c.path, expression.String()))
		}
	}
	return errors.Join(failures...)
}

type commandCheck struct {
	argv           []string
	expectExit     int
	timeout        time.Duration
	stdoutContains []string
	stdoutMatch    []*regexp.Regexp
	stderrContains []string
}

func (c *commandCheck) verify(workspace string) error {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.argv[0], c.argv[1:]...)
	cmd.Dir = workspace
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("command %q timed out after %s", c.argv, c.timeout)
	}
	exit := 0
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			exit = exitErr.ExitCode()
		} else {
			return fmt.Errorf("command %q: %w", c.argv, runErr)
		}
	}
	var failures []error
	if exit != c.expectExit {
		failures = append(failures, fmt.Errorf("command %q exit %d, want %d (stderr: %q)", c.argv, exit, c.expectExit, abbreviate(stderr.String())))
	}
	out := stdout.String()
	for _, want := range c.stdoutContains {
		if !strings.Contains(out, want) {
			failures = append(failures, fmt.Errorf("command %q stdout does not contain %q (stdout: %q)", c.argv, want, abbreviate(out)))
		}
	}
	for _, expression := range c.stdoutMatch {
		if !expression.MatchString(out) {
			failures = append(failures, fmt.Errorf("command %q stdout does not match %q (stdout: %q)", c.argv, expression.String(), abbreviate(out)))
		}
	}
	errOut := stderr.String()
	for _, want := range c.stderrContains {
		if !strings.Contains(errOut, want) {
			failures = append(failures, fmt.Errorf("command %q stderr does not contain %q (stderr: %q)", c.argv, want, abbreviate(errOut)))
		}
	}
	return errors.Join(failures...)
}

func abbreviate(value string) string {
	const limit = 200
	value = strings.ReplaceAll(value, "\n", "\\n")
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "..."
}
