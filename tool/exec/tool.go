package exec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// ErrUnwired reports a call beside no execution resource manager: the
// declaration granted the execution face while the assembly wires no ledger to
// hold it, so the call is refused by name instead of starting anything.
var ErrUnwired = errors.New("exec: the deployment wires no execution resource manager")

// 契约: docs/wiki/tool/tool-architecture.md#action-tool
//
// RunTool is the callable tool face of one execution run: it acquires the
// execution resource its arguments declare — reusing the one already held under
// that key — and answers with what the execution recorded. The monitor rhythm
// of the call rides the acquisition it opens, so each borrowed generation
// carries its own rhythm. The lease the call holds is renewed at the cadence of
// that rhythm for as long as the execution runs behind it, so a long execution
// is never swept for having taken long while it is still working.
type RunTool struct {
	manager *Manager
	monitor MonitorConfig
}

// ReleaseTool is the callable tool face of one explicit release: it stops the
// resource behind the declared key and reports the evidence of that stop. A
// stop the executor could not confirm keeps the resource held, and the answer
// reports the retention rather than an ending.
type ReleaseTool struct {
	manager *Manager
}

// RunToolOption fixes one piece of the run tool beside the manager it acquires
// through.
type RunToolOption func(*RunTool)

// WithRunMonitor binds the monitor rhythm the declaration fixes to every
// acquisition the tool opens: a call that names no monitor key of its own runs
// under this rhythm instead of the framework default.
func WithRunMonitor(rhythm MonitorConfig) RunToolOption {
	return func(t *RunTool) { t.monitor = rhythm }
}

// NewRunTool returns the run tool over the given execution resource manager and
// the rhythm its declaration fixes.
func NewRunTool(manager *Manager, opts ...RunToolOption) *RunTool {
	t := &RunTool{manager: manager, monitor: DefaultMonitorConfig}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// NewReleaseTool returns the release tool over the given execution resource
// manager.
func NewReleaseTool(manager *Manager) *ReleaseTool { return &ReleaseTool{manager: manager} }

// PresetName is the declaration id that grants the execution face: the
// declaration names it once, and the surface answers with the run and the
// release tool under their own frozen names.
const PresetName = "exec"

// NewPreset returns the tools one declared execution face grants, in the order
// the request carries them: the run tool under the monitor rhythm the
// declaration fixes beside the explicit release tool. A preset built beside no
// manager still builds; each call answers ErrUnwired.
func NewPreset(manager *Manager, rhythm MonitorConfig) []trpctool.Tool {
	return []trpctool.Tool{NewRunTool(manager, WithRunMonitor(rhythm)), NewReleaseTool(manager)}
}

// runArgs is the argument set of one execution run. A key outside this set —
// the retired tool-level compress section among them — is accepted and left
// unconsumed, so its presence changes nothing and refuses nothing.
type runArgs struct {
	Command  string         `json:"command"`
	Args     []string       `json:"args"`
	Dir      string         `json:"dir"`
	Kind     string         `json:"kind"`
	Resource string         `json:"resource"`
	TTL      string         `json:"ttl"`
	Monitor  map[string]any `json:"monitor"`
}

// releaseArgs is the argument set of one explicit release.
type releaseArgs struct {
	Kind     string `json:"kind"`
	Resource string `json:"resource"`
}

// Declaration returns the fixed declaration of the run tool.
func (t *RunTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{
		Name:        "exec_run",
		Description: "run one external command under a declared execution resource: command, args, dir beside the resource kind and key; kind and key reuse the resource already held under that identity; ttl is a duration string and monitor carries the monitor rhythm keys of this run",
		InputSchema: &trpctool.Schema{
			Type: "object",
			Properties: map[string]*trpctool.Schema{
				"command":  {Type: "string", Description: "the program to run"},
				"args":     {Type: "array", Items: &trpctool.Schema{Type: "string"}, Description: "the arguments handed to the command"},
				"dir":      {Type: "string", Description: "the working directory of the command"},
				"kind":     {Type: "string", Description: "the resource family the key identifies within"},
				"resource": {Type: "string", Description: "the canonical key of the execution resource"},
				"ttl":      {Type: "string", Description: "the lease of the run as a duration string"},
				"monitor":  {Type: "object", Description: "the monitor rhythm keys of this run"},
			},
			Required: []string{"command", "resource"},
		},
	}
}

// Declaration returns the fixed declaration of the release tool.
func (t *ReleaseTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{
		Name:        "exec_release",
		Description: "stop and free the execution resource behind a declared key: the answer reports the observed stop evidence, and a stop that stays unconfirmed keeps the resource held",
		InputSchema: &trpctool.Schema{
			Type: "object",
			Properties: map[string]*trpctool.Schema{
				"kind":     {Type: "string", Description: "the resource family the key identifies within"},
				"resource": {Type: "string", Description: "the canonical key of the execution resource"},
			},
			Required: []string{"resource"},
		},
	}
}

// Call acquires the declared execution resource and reports what its execution
// recorded. A resource already held under the key is reused rather than started
// a second time, and a declared kind the system cannot serve answers as the
// named degrade refusal beside the rhythm of the call. The lease is kept alive
// at the cadence of the call's own monitor rhythm until the execution behind it
// returns, and the renewing ends beside the call.
func (t *RunTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	if t.manager == nil {
		return nil, ErrUnwired
	}
	var args runArgs
	if err := json.Unmarshal(jsonArgs, &args); err != nil {
		return nil, fmt.Errorf("exec: the run arguments do not parse: %w", err)
	}
	if args.Resource == "" {
		return nil, ErrKeyMissing
	}
	decl := ExecutionDecl{
		Command:  args.Command,
		Args:     args.Args,
		Dir:      args.Dir,
		Resource: ResourceKey{Kind: args.Kind, Key: args.Resource},
	}
	rhythm, err := MonitorConfigWith(t.monitor, args.Monitor)
	if err != nil {
		return nil, err
	}
	options := []AcquireOption{WithMonitorConfig(rhythm)}
	var lease time.Duration
	if args.TTL != "" {
		parsed, err := time.ParseDuration(args.TTL)
		if err != nil {
			return nil, fmt.Errorf("exec: the run lease is not a duration: %q", args.TTL)
		}
		lease = parsed
		options = append(options, WithTTL(lease))
	}
	key, err := t.manager.Acquire(ctx, decl, options...)
	if err != nil {
		return nil, err
	}
	stopRenewing := t.renewForCall(key, lease, rhythm)
	defer stopRenewing()
	output, err := t.manager.Capture(ctx, key)
	if err != nil {
		return nil, err
	}
	return fmt.Sprintf("executed %s: %s", key, output), nil
}

// renewForCall keeps the lease of one in-flight execution alive for as long as
// the call holds it: at the monitor interval the call declares — and never
// longer than half the lease it keeps, so a short lease is renewed inside its
// own window — the execution path renews the lease it borrowed. The returned
// stop ends the renewing beside the call. A renewal refused by name ends it too:
// a lease released or already expired belongs to its settlement and its sweep,
// and the call renews nothing behind them.
func (t *RunTool) renewForCall(key ResourceKey, lease time.Duration, rhythm MonitorConfig) func() {
	span := t.manager.leaseOf(lease)
	cadence := rhythm.Interval
	if half := span / 2; cadence <= 0 || cadence > half {
		cadence = half
	}
	if cadence <= 0 {
		return func() {}
	}
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(cadence)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if err := t.manager.Renew(context.Background(), key, lease); err != nil {
					return
				}
			}
		}
	}()
	return func() {
		close(done)
		<-stopped
	}
}

// Call stops the execution resource behind the declared key and reports the
// stop evidence observed for it. A confirmed stop answers with the freed
// resource beside its exit code; an unconfirmed stop answers with the retained
// resource beside the reason it stayed held.
func (t *ReleaseTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	if t.manager == nil {
		return nil, ErrUnwired
	}
	var args releaseArgs
	if err := json.Unmarshal(jsonArgs, &args); err != nil {
		return nil, fmt.Errorf("exec: the release arguments do not parse: %w", err)
	}
	if args.Resource == "" {
		return nil, ErrKeyMissing
	}
	evidence, err := t.manager.Release(ctx, ResourceKey{Kind: args.Kind, Key: args.Resource})
	if err != nil {
		return nil, err
	}
	if !evidence.Released {
		return fmt.Sprintf("retained %s: stop unconfirmed: %s", evidence.Resource, evidence.Reason), nil
	}
	return fmt.Sprintf("released %s after confirmed stop: exit=%d", evidence.Resource, evidence.Stop.ExitCode), nil
}
