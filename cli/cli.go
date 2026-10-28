// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
//
// Package cli is the host shell beside the composition root: the thin command
// shell (invoke/query/start) over one assembled run, and the host face a host
// process drives that run through. The shell sits above package tagent and never
// below it — the runtime imports nothing from here — so a host that embeds the
// assembled run directly drops this package whole instead of carrying a second
// runtime beside the one Assemble returned.
package cli

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/features/rl"
	"github.com/SpellingDragon/tagent/memory/query"
	"github.com/SpellingDragon/tagent/runtime/agent"
)

// httpPortEnv and defaultHTTPPort are the interface constants of the host
// shell: the environment name and the default of the HTTP port the start
// command serves on.
const (
	httpPortEnv     = "TAGENT_HTTP_PORT"
	defaultHTTPPort = "8089"
)

// serveShutdownGrace is the span the start command gives the connection
// shutdown and, separately, the run's own close: one budget each, so an
// exhausted connection drain never cuts the close off before it settles.
const serveShutdownGrace = 5 * time.Second

// ErrDiagnosticNotOpen reports a diagnostic request on a host shell beside no
// request authentication: the diagnostic face stays closed until a request
// credential is configured for it.
var ErrDiagnosticNotOpen = errors.New("tagent: the diagnostic endpoints are not open: no request authentication is configured")

// ErrRequestNotAuthenticated reports a request whose subject did not
// authenticate: the credential is missing, malformed or unmatched, and this
// named refusal is the only answer the endpoint serves for it.
var ErrRequestNotAuthenticated = errors.New("tagent: the request subject is not authenticated")

// CLI is the command shell of one assembled run beside the authorization scope
// its caller already holds. The shell widens no authorization of its own: every
// reading it serves runs under the very scope it was constructed with. The
// request subject of every HTTP request authenticates at one gate ahead of the
// route resolution, so no endpoint opens a bypass around it.
type CLI struct {
	run         *assembly.Run
	scope       query.AuthorizationScope
	credential  string
	feedback    http.Handler
	diagnostics assembly.DiagnosticSwitches
}

// NewCLI returns the thin command shell over one assembled run, reading under
// the authorization scope the caller already holds and carrying the options
// the host configures it with.
func NewCLI(run *assembly.Run, scope query.AuthorizationScope, opts ...CLIOption) *CLI {
	c := &CLI{run: run, scope: scope}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Main dispatches one command against the assembled run and reports the exit
// code: "invoke" drives one invocation, "query" serves one diagnostic reading
// and "start" runs the host HTTP service until the context is cancelled or the
// process is asked to terminate. An unknown command or a malformed argument is
// refused with a usage line and a non-zero exit code.
func (c *CLI) Main(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: tagent <invoke|query|start> [flags]")
		return 2
	}
	switch args[0] {
	case "invoke":
		return c.invoke(ctx, args[1:], stdout, stderr)
	case "query":
		return c.query(ctx, args[1:], stdout, stderr)
	case "start":
		return c.start(ctx, args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "tagent: unknown command %q\n", args[0])
		return 2
	}
}

// invoke drives one invocation through the assembled run and reports the
// terminal the turn reduced to.
func (c *CLI) invoke(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("invoke", flag.ContinueOnError)
	flags.SetOutput(stderr)
	agentID := flags.String("agent", "", "the agent the invocation runs")
	session := flags.String("session", "", "the session the invocation belongs to")
	invocation := flags.String("invocation", "", "the responsibility range identity")
	text := flags.String("text", "", "the native input text")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *agentID == "" || *session == "" || *invocation == "" {
		fmt.Fprintln(stderr, "usage: tagent invoke -agent A -session S -invocation I -text T")
		return 2
	}
	turn, err := c.run.Invoke(ctx, assembly.InvokeRequest{
		InvocationID: event.InvocationID(*invocation),
		Address:      event.Address{AgentID: event.AgentID(*agentID), SessionID: event.SessionID(*session)},
		Input:        []model.Message{{Role: model.RoleUser, Content: *text}},
	})
	if err != nil {
		fmt.Fprintln(stderr, "tagent: invoke:", err)
		return 1
	}
	return writeJSON(stdout, stderr, struct {
		InvocationID event.InvocationID `json:"invocation_id"`
		Terminal     model.Kind         `json:"terminal"`
		Decisions    int                `json:"decisions"`
	}{InvocationID: turn.InvocationID, Terminal: turn.Terminal, Decisions: len(turn.Decisions)})
}

// query serves one bounded diagnostic reading of the assembled run under the
// scope the shell was constructed with.
func (c *CLI) query(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("query", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var kinds kindList
	flags.Var(&kinds, "kind", "the object kinds to read (repeatable)")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	snapshot, err := c.run.Query(ctx, query.ListFilter{Scope: c.scope, Kinds: kinds})
	if err != nil {
		fmt.Fprintln(stderr, "tagent: query:", err)
		return 1
	}
	return writeJSON(stdout, stderr, snapshot)
}

// start runs the host HTTP service on the port the environment names and keeps
// the run's own close on the way out: the standard library's server is held by
// this shell, a cancellation of the context or a termination signal stops it,
// and the close drains through the SDK close entry. The shell runs no
// uncontrolled goroutine beside the server's own accept loop.
func (c *CLI) start(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("start", flag.ContinueOnError)
	flags.SetOutput(stderr)
	addr := flags.String("addr", defaultListenAddr(), "the address the HTTP service listens on")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	switches, err := assembly.DiagnosticSwitchesFromEnv()
	if err != nil {
		fmt.Fprintln(stderr, "tagent: start:", err)
		return 2
	}
	c.diagnostics = switches
	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		fmt.Fprintln(stderr, "tagent: start:", err)
		return 1
	}
	fmt.Fprintf(stdout, "tagent: serving on %s\n", listener.Addr())
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := c.Serve(ctx, listener); err != nil {
		fmt.Fprintln(stderr, "tagent: start:", err)
		return 1
	}
	return 0
}

// Serve runs the host HTTP service on the given listener until the context is
// cancelled or the process is asked to terminate, then closes the run through
// the SDK close entry and reports its outcome. The server is held here (not by
// the SDK), so a host embeds the same handler under its own server instead of
// getting a second one from the runtime. The connection shutdown and the run's
// close read one budget each, so a drain that spent its shutdown span never
// hands the close an expired context.
func (c *CLI) Serve(ctx context.Context, listener net.Listener) error {
	server := &http.Server{Handler: c.handler()}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	select {
	case err := <-served:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), serveShutdownGrace)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return err
	}
	<-served
	closeCtx, closeCancel := context.WithTimeout(context.Background(), serveShutdownGrace)
	defer closeCancel()
	deadline := time.Now().Add(serveShutdownGrace)
	report, err := c.run.Close(closeCtx, agent.CloseRequest{Deadline: deadline})
	if err != nil {
		return err
	}
	if !report.Drained {
		return fmt.Errorf("tagent: the close did not drain: %d obligations stayed open", len(report.Unsettled))
	}
	return nil
}

// authenticate judges the request subject of one HTTP request at the single
// authentication point of the host face: the gate runs ahead of the route
// resolution, so every endpoint including the liveness probe answers through
// it and none opens a bypass. A shell beside no configured credential keeps
// its diagnostic face closed by name; the bearer scheme is matched case
// independently and the token is compared in constant time and never logged.
func (c *CLI) authenticate(r *http.Request) error {
	if c.credential == "" {
		return ErrDiagnosticNotOpen
	}
	scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return ErrRequestNotAuthenticated
	}
	if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(token)), []byte(c.credential)) != 1 {
		return ErrRequestNotAuthenticated
	}
	return nil
}

// handler returns the host HTTP face of the run: a liveness probe, the unified
// runtime reading, the diagnostic reading of every optional face and, when the
// host mounts one, the RL feedback endpoint, all served under the scope the
// shell already holds. The request subject of every request authenticates at one
// gate ahead of the route resolution, and an unauthenticated request is refused
// with the named denial and the 401 status before any route runs. The handler
// grants no authorization of its own, so the host keeps the request boundary it
// deploys behind.
func (c *CLI) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "ok")
	})
	mux.HandleFunc("GET /query", func(w http.ResponseWriter, r *http.Request) {
		snapshot, err := c.run.Query(r.Context(), query.ListFilter{Scope: c.scope})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(snapshot)
	})
	mux.HandleFunc("GET /diagnostics", func(w http.ResponseWriter, r *http.Request) {
		report, err := c.run.Diagnostics(r.Context(), query.ListFilter{Scope: c.scope}, c.diagnostics)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if source, ok := c.feedback.(interface{ Attribution() rl.FeedbackAttribution }); ok {
			attribution := source.Attribution()
			report.Training.Feedback = &attribution
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(report)
	})
	if c.feedback != nil {
		mux.Handle("POST /feedback", c.feedback)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := c.authenticate(r); err != nil {
			w.Header().Set("WWW-Authenticate", `Bearer realm="tagent"`)
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

// defaultListenAddr reports the address the start command serves on by default:
// the port the host environment names, and the interface-constant default when
// it names none.
func defaultListenAddr() string {
	port := os.Getenv(httpPortEnv)
	if port == "" {
		port = defaultHTTPPort
	}
	if _, err := strconv.Atoi(port); err != nil {
		port = defaultHTTPPort
	}
	return ":" + port
}

// kindList collects the repeatable -kind flag into the projection's kind
// filter.
type kindList []query.ObjectKind

// String reports the collected kinds for the flag package's diagnostics.
func (k kindList) String() string {
	parts := make([]string, 0, len(k))
	for _, kind := range k {
		parts = append(parts, string(kind))
	}
	return strings.Join(parts, ",")
}

// Set collects one named kind.
func (k *kindList) Set(value string) error {
	*k = append(*k, query.ObjectKind(value))
	return nil
}

// writeJSON writes one value as JSON and reports the shell's exit code.
func writeJSON(stdout, stderr io.Writer, value any) int {
	if err := json.NewEncoder(stdout).Encode(value); err != nil {
		fmt.Fprintln(stderr, "tagent: encode:", err)
		return 1
	}
	return 0
}
