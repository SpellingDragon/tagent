// Command cli-host is the command-line host example of tagent: one process
// assembles the runtime once and drives one explicit session per invocation,
// reading a line from stdin (or one-shot input from -c) and writing the
// assistant reply to stdout. Its default model is the built-in mock, so the
// example starts with no credentials at all.
//
// 契约: docs/OPERATION.md#cli-host-build
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/config"
	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/evolution"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/model"
	openaimodel "trpc.group/trpc-go/trpc-agent-go/model/openai"
)

// mockModelName is the model reference the credential-free path resolves to.
const mockModelName = "mock"

// cliConfig is the host-side configuration of one process: the input mode, the
// optional durable data directory, and the model connection the flags and
// environment resolve to.
type cliConfig struct {
	input     string
	dataDir   string
	modelName string
	baseURL   string
	keyEnv    string
	agentID   string
	sessionID string
	timeout   time.Duration
}

// main runs the host and exits with the code the run reports.
func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

// run parses the flags, assembles the runtime, drives the input and closes the
// run. It is the testable entry of the command: the streams are handed in and
// no global state is read beyond the environment.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	cfg, err := parseFlags(args, stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintf(stderr, "cli-host: %v\n", err)
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	rt, err := assemble(ctx, cfg)
	if err != nil {
		fmt.Fprintf(stderr, "cli-host: assemble: %v\n", err)
		return 1
	}
	session := &cliSession{
		addr: event.Address{
			AgentID:   event.AgentID(cfg.agentID),
			SessionID: event.SessionID(cfg.sessionID),
		},
		out:     stdout,
		err:     stderr,
		timeout: cfg.timeout,
	}

	if strings.TrimSpace(cfg.input) != "" {
		if err := session.invoke(ctx, rt, cfg.input); err != nil {
			fmt.Fprintf(stderr, "cli-host: %v\n", err)
			_ = session.close(context.Background(), rt)
			return 1
		}
	} else if code := session.repl(ctx, rt, stdin, stderr); code != 0 {
		return code
	}

	if err := session.close(context.Background(), rt); err != nil {
		fmt.Fprintf(stderr, "cli-host: close: %v\n", err)
		return 1
	}
	return 0
}

// parseFlags resolves the flags and the environment into one configuration.
// The mock model is the default, so an empty environment is a valid start.
func parseFlags(args []string, stderr io.Writer) (cliConfig, error) {
	cfg := cliConfig{}
	fs := flag.NewFlagSet("cli-host", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&cfg.input, "c", "", "one-shot input; when empty, lines are read from stdin")
	fs.StringVar(&cfg.dataDir, "data", "", "optional durable data directory (localfile storage tier)")
	fs.StringVar(&cfg.modelName, "model", envOr("TAGENT_MODEL", mockModelName), "model name; \"mock\" needs no credentials")
	fs.StringVar(&cfg.baseURL, "base-url", envOr("TAGENT_BASE_URL", ""), "OpenAI-compatible endpoint of a real model")
	fs.StringVar(&cfg.keyEnv, "api-key-env", envOr("TAGENT_API_KEY_ENV", "OPENAI_API_KEY"), "environment variable holding the real model key")
	fs.StringVar(&cfg.agentID, "agent", "assistant", "agent identity this host assembles")
	fs.StringVar(&cfg.sessionID, "session", envOr("TAGENT_SESSION_ID", "cli-session"), "session identity this host drives")
	fs.DurationVar(&cfg.timeout, "timeout", 2*time.Minute, "timeout of one invocation")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: cli-host [-c input] [-data dir] [-model name] [-base-url url] [-api-key-env VAR]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return cliConfig{}, err
	}
	if fs.NArg() > 0 {
		return cliConfig{}, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	cfg.modelName = strings.TrimSpace(cfg.modelName)
	if cfg.modelName == "" {
		cfg.modelName = mockModelName
	}
	cfg.agentID = strings.TrimSpace(cfg.agentID)
	cfg.sessionID = strings.TrimSpace(cfg.sessionID)
	if cfg.agentID == "" || cfg.sessionID == "" {
		return cliConfig{}, errors.New("the agent and session identities must not be empty")
	}
	return cfg, nil
}

// assemble builds the one runtime this process runs: the declaration, the
// native model behind it and the governance every publication is decided
// under, published through the sole public assembly entry.
func assemble(ctx context.Context, cfg cliConfig) (*tagent.Run, error) {
	native, err := newNativeModel(cfg)
	if err != nil {
		return nil, err
	}
	decl := &config.Declaration{
		Entry:  cfg.agentID,
		Agents: []config.AgentSpec{{ID: cfg.agentID, Model: cfg.modelName}},
	}
	opts := []tagent.Option{
		tagent.WithGovernance(evolution.NewPolicy(evolution.NewStructuralGate())),
	}
	if cfg.dataDir != "" {
		opts = append(opts, tagent.WithStorage(memory.StorageConfig{
			Type: memory.StorageLocalFile,
			Root: cfg.dataDir,
		}))
	}
	return tagent.Assemble(ctx, decl, map[event.AgentID]tagent.NativeAgent{
		event.AgentID(cfg.agentID): {Model: native},
	}, opts...)
}

// newNativeModel constructs the model the declared agent runs against: the
// built-in mock unless a real model name was named, in which case the key of
// the configured environment variable must be present.
func newNativeModel(cfg cliConfig) (model.Model, error) {
	if strings.EqualFold(cfg.modelName, mockModelName) {
		return &mockModel{name: mockModelName}, nil
	}
	key := strings.TrimSpace(os.Getenv(cfg.keyEnv))
	if key == "" {
		return nil, fmt.Errorf("model %q needs a key in $%s; use -model mock for the credential-free path", cfg.modelName, cfg.keyEnv)
	}
	opts := []openaimodel.Option{openaimodel.WithAPIKey(key)}
	if cfg.baseURL != "" {
		opts = append(opts, openaimodel.WithBaseURL(cfg.baseURL))
	}
	return openaimodel.New(cfg.modelName, opts...), nil
}

// cliSession is the explicit session this process drives. It remembers how much
// of the committed transcript was already printed, so one reply is written
// once even though every invocation reads the whole session history back.
type cliSession struct {
	addr      event.Address
	out       io.Writer
	err       io.Writer
	timeout   time.Duration
	seq       int
	delivered int
}

// invoke drives one user line through the assembled runtime and prints the
// assistant messages the drive added to the session transcript.
func (s *cliSession) invoke(ctx context.Context, rt *tagent.Run, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	s.seq++
	callCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	turn, err := rt.Invoke(callCtx, tagent.InvokeRequest{
		InvocationID: event.InvocationID(fmt.Sprintf("cli-%d", s.seq)),
		Address:      s.addr,
		Input:        []model.Message{{Role: model.RoleUser, Content: text}},
	})
	if err != nil {
		return fmt.Errorf("invoke: %w", err)
	}
	s.printFresh(rt.SessionHistory(s.addr))
	if turn.Terminal == model.KindFailed {
		return errors.New("the model turn failed")
	}
	return nil
}

// printFresh writes the assistant messages committed since the previous print,
// one line each. Tool-call messages and empty bodies are not part of the reply.
func (s *cliSession) printFresh(history []model.Message) {
	for ; s.delivered < len(history); s.delivered++ {
		msg := history[s.delivered]
		if msg.Role != model.RoleAssistant || len(msg.ToolCalls) > 0 {
			continue
		}
		if content := strings.TrimSpace(msg.Content); content != "" {
			fmt.Fprintln(s.out, content)
		}
	}
}

// repl reads one line at a time until stdin ends or the user types /exit.
func (s *cliSession) repl(ctx context.Context, rt *tagent.Run, stdin io.Reader, stderr io.Writer) int {
	scanner := bufio.NewScanner(stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for {
		fmt.Fprint(stderr, "> ")
		if !scanner.Scan() {
			break
		}
		line := strings.TrimSpace(scanner.Text())
		switch line {
		case "":
			continue
		case "/exit", "/quit":
			return 0
		}
		if err := s.invoke(ctx, rt, line); err != nil {
			fmt.Fprintf(stderr, "cli-host: %v\n", err)
			_ = s.close(context.Background(), rt)
			return 1
		}
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(stderr, "cli-host: read stdin: %v\n", err)
		_ = s.close(context.Background(), rt)
		return 1
	}
	return 0
}

// close drains the runtime for this session and reports what stayed open.
func (s *cliSession) close(ctx context.Context, rt *tagent.Run) error {
	closeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	report, err := rt.Close(closeCtx, agent.CloseRequest{
		Address:  s.addr,
		Deadline: time.Now().Add(10 * time.Second),
	})
	if err != nil {
		return err
	}
	if !report.Drained {
		fmt.Fprintf(s.err, "cli-host: close left %d obligation(s) unsettled\n", len(report.Unsettled))
	}
	return nil
}

// mockModel is the credential-free native model stand-in: it answers each
// request with a deterministic echo of the latest user message and never
// reaches a network endpoint. It is a real model implementation of the runtime,
// not a test double: the same assembly, loop and transcript path run.
type mockModel struct {
	name string
}

// GenerateContent echoes the latest user message as one completed response.
func (m *mockModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ch := make(chan *model.Response, 1)
	ch <- &model.Response{
		Choices: []model.Choice{{Message: model.Message{
			Role:    model.RoleAssistant,
			Content: mockReply(req),
		}}},
		Done: true,
	}
	close(ch)
	return ch, nil
}

// Info reports the mock model descriptor.
func (m *mockModel) Info() model.Info {
	return model.Info{Name: m.name}
}

// mockReply renders the answer of one request from its latest user message.
func mockReply(req *model.Request) string {
	if req != nil {
		for i := len(req.Messages) - 1; i >= 0; i-- {
			if req.Messages[i].Role == model.RoleUser {
				return "mock reply: " + req.Messages[i].Content
			}
		}
	}
	return "mock reply: (no user input)"
}

// envOr reports the trimmed environment value, or the default when it is empty.
func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
