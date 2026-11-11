// 契约: docs/wiki/rl/rl-architecture.md#live-benchmark
package bench

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// verifierFixture is the JSON declaration of a verifier: response, file and
// command checks compose with AND, so one task can require both a readable
// answer and a durable artifact.
type verifierFixture struct {
	Response *responseFixture `json:"response,omitempty"`
	File     *fileFixture     `json:"file,omitempty"`
	Command  *commandFixture  `json:"command,omitempty"`
}

type responseFixture struct {
	Equals         *string  `json:"equals,omitempty"`
	MustContain    []string `json:"must_contain,omitempty"`
	MustNotContain []string `json:"must_not_contain,omitempty"`
	MustMatch      []string `json:"must_match,omitempty"`
}

type fileFixture struct {
	Path           string   `json:"path"`
	Equals         *string  `json:"equals,omitempty"`
	MustContain    []string `json:"must_contain,omitempty"`
	MustNotContain []string `json:"must_not_contain,omitempty"`
	MustMatch      []string `json:"must_match,omitempty"`
}

type commandFixture struct {
	Argv           []string `json:"argv"`
	ExpectExit     *int     `json:"expect_exit,omitempty"`
	StdoutContains []string `json:"stdout_contains,omitempty"`
	StdoutMatch    []string `json:"stdout_match,omitempty"`
	StderrContains []string `json:"stderr_contains,omitempty"`
	TimeoutMS      int      `json:"timeout_ms,omitempty"`
}

func (v *verifierFixture) validate() error {
	if v.Response == nil && v.File == nil && v.Command == nil {
		return errors.New("at least one of response/file/command is required")
	}
	if v.Response != nil {
		if err := v.Response.validate("response"); err != nil {
			return err
		}
	}
	if v.File != nil {
		if err := v.File.validate(); err != nil {
			return err
		}
	}
	if v.Command != nil {
		if err := v.Command.validate(); err != nil {
			return err
		}
	}
	return nil
}

func (c *responseFixture) validate(where string) error {
	if c.Equals == nil && len(c.MustContain)+len(c.MustNotContain)+len(c.MustMatch) == 0 {
		return fmt.Errorf("%s: at least one of equals/must_contain/must_not_contain/must_match is required", where)
	}
	if err := validatePatterns(where, c.MustMatch); err != nil {
		return err
	}
	return validateNeedles(where, c.MustContain, c.MustNotContain)
}

func (c *fileFixture) validate() error {
	if _, err := safeRelativePath(c.Path); err != nil {
		return fmt.Errorf("file: %w", err)
	}
	if err := validatePatterns("file", c.MustMatch); err != nil {
		return err
	}
	return validateNeedles("file", c.MustContain, c.MustNotContain)
}

func (c *commandFixture) validate() error {
	if len(c.Argv) == 0 || strings.TrimSpace(c.Argv[0]) == "" {
		return errors.New("command: argv must name a program")
	}
	if c.TimeoutMS < 0 || c.TimeoutMS > int(maxCommandTimeout/time.Millisecond) {
		return fmt.Errorf("command: timeout_ms must be between 0 and %d", maxCommandTimeout/time.Millisecond)
	}
	if err := validatePatterns("command stdout", c.StdoutMatch); err != nil {
		return err
	}
	return validateNeedles("command", c.StdoutContains, c.StderrContains)
}

func validateNeedles(where string, groups ...[]string) error {
	for _, group := range groups {
		for _, needle := range group {
			if needle == "" {
				return fmt.Errorf("%s: empty match string", where)
			}
		}
	}
	return nil
}

func validatePatterns(where string, expressions []string) error {
	for _, expression := range expressions {
		if strings.TrimSpace(expression) == "" {
			return fmt.Errorf("%s: empty regular expression", where)
		}
		if _, err := regexp.Compile(expression); err != nil {
			return fmt.Errorf("%s: invalid regular expression %q: %w", where, expression, err)
		}
	}
	return nil
}

func (v *verifierFixture) build() (Verifier, error) {
	built := &specVerifier{}
	if v.Response != nil {
		check := &responseCheck{
			equals:         v.Response.Equals,
			mustContain:    v.Response.MustContain,
			mustNotContain: v.Response.MustNotContain,
		}
		for _, expression := range v.Response.MustMatch {
			compiled, err := regexp.Compile(expression)
			if err != nil {
				return nil, err
			}
			check.mustMatch = append(check.mustMatch, compiled)
		}
		built.response = check
	}
	if v.File != nil {
		check := &fileCheck{
			path:           v.File.Path,
			equals:         v.File.Equals,
			mustContain:    v.File.MustContain,
			mustNotContain: v.File.MustNotContain,
		}
		for _, expression := range v.File.MustMatch {
			compiled, err := regexp.Compile(expression)
			if err != nil {
				return nil, err
			}
			check.mustMatch = append(check.mustMatch, compiled)
		}
		built.file = check
	}
	if v.Command != nil {
		check := &commandCheck{
			argv:           v.Command.Argv,
			stdoutContains: v.Command.StdoutContains,
			stderrContains: v.Command.StderrContains,
			timeout:        defaultCommandTimeout,
		}
		check.expectExit = 0
		if v.Command.ExpectExit != nil {
			check.expectExit = *v.Command.ExpectExit
		}
		if v.Command.TimeoutMS > 0 {
			check.timeout = time.Duration(v.Command.TimeoutMS) * time.Millisecond
		}
		for _, expression := range v.Command.StdoutMatch {
			compiled, err := regexp.Compile(expression)
			if err != nil {
				return nil, err
			}
			check.stdoutMatch = append(check.stdoutMatch, compiled)
		}
		built.command = check
	}
	return built, nil
}
