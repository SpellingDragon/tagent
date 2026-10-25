package exec // import "github.com/SpellingDragon/tagent/tool/exec"

Package exec declares the external-execution face of the runtime: the
declaration one external command runs under, the identity of the resource it
occupies, and the executor that owns that resource for the whole of its life.

The tmux mechanism itself stays outside this package. How a session is started,
watched, re-attached and killed belongs to the executor behind the Executor port
declared in stop.go.

The state face of the execution resource manager: the responsibility range a
resource is tracked for, the lifecycle events the manager observes after a call
returned, and the optional observer that hands those events to the runtime.
The face stays inside this package's own vocabulary: the root assembly supplies
an OwnerResolver instead of the package importing the task or agent owners,
so the dependency edge of the golden architecture holds.

CONSTANTS

const DefaultLeaseTTL = 30 * time.Minute
    DefaultLeaseTTL is the floor lease of an execution resource whose caller
    declared no TTL. No acquisition runs without a lease deadline.

const KindTmux = "tmux"

    KindTmux names the tmux session family of execution resources: the one
    family whose availability rests on the real tmux binary being reachable on
    PATH.

const PresetName = "exec"
    PresetName is the declaration id that grants the execution face:
    the declaration names it once, and the surface answers with the run and the
    release tool under their own frozen names.

VARIABLES

var DefaultMonitorConfig = MonitorConfig{
	Interval:                  30 * time.Second,
	StableDuration:            60 * time.Second,
	InteractiveStableDuration: 90 * time.Second,
	FakeDeadDuration:          120 * time.Second,
}
    DefaultMonitorConfig is the rhythm an acquisition runs under when its caller
    declared no monitor configuration.

var ErrAdoptUnconfirmed = errors.New("exec: the resource behind that key was not observed alive")
    ErrAdoptUnconfirmed reports an adoption refused because the resource behind
    the key was not observed alive. An undeterminable probe is not an absence.

var ErrKeyMissing = errors.New("exec: the declaration names no resource key")
    ErrKeyMissing reports a declaration that names no resource key, so no
    resource identity could be compared or held.

var ErrLeaseExpired = errors.New("exec: the lease reached its deadline before this renewal")
    ErrLeaseExpired reports a renewal refused because the lease of the resource
    reached its deadline before the renewal. An expired lease belongs to the
    explicit stop handling of a sweep, so a late renewal never revives it beside
    a settlement already in flight.

var ErrLeaseInvalid = errors.New("exec: the lease must not be negative")
    ErrLeaseInvalid reports a renewal or acquisition over a negative lease,
    which no resource runs under.

var ErrResourceHeld = errors.New("exec: the resource is already held under that key")
    ErrResourceHeld reports an acquisition refused because the resource is
    already held under that key, so the key stays owned by its single holder.

var ErrResourceUnknown = errors.New("exec: no execution resource is held under that key")
    ErrResourceUnknown reports a lifecycle call over a key no resource is held
    under, so there is nothing the call could observe or free.

var ErrTmuxUnavailable = errors.New("exec: the tmux capability is not available on this system")
    ErrTmuxUnavailable reports an acquisition refused because the declared kind
    needs the tmux capability and the system does not provide it.

var ErrUnwired = errors.New("exec: the deployment wires no execution resource manager")
    ErrUnwired reports a call beside no execution resource manager: the
    declaration granted the execution face while the assembly wires no ledger to
    hold it, so the call is refused by name instead of starting anything.

FUNCTIONS

func IsTmuxAvailable() bool
    IsTmuxAvailable reports whether a tmux binary is reachable on PATH. The
    verdict comes from a real PATH lookup at every call, so a deployment without
    tmux reads as unavailable and no construction can stand in for the probe.

func NewPreset(manager *Manager, rhythm MonitorConfig, opts ...RunToolOption) []trpctool.Tool
    NewPreset returns the tools one declared execution face grants, in the
    order the request carries them: the run tool under the monitor rhythm the
    declaration fixes beside the explicit release tool. A preset built beside no
    manager still builds; each call answers ErrUnwired. The options configure
    the run tool of the preset, so an assembly that resolves the responsibility
    range of a call passes its resolver here.

TYPES

type AcquireOption func(*Acquisition)
    AcquireOption configures one acquisition at its call.

func WithMonitorConfig(rhythm MonitorConfig) AcquireOption
    WithMonitorConfig binds the monitor rhythm to the generation one acquisition
    opens, so the rhythm travels with the call that consumes it.

func WithTTL(ttl time.Duration) AcquireOption
    WithTTL sets the lease of one acquisition. A zero TTL takes the manager
    floor, and a negative TTL is refused.

type Acquisition struct {
	// Has unexported fields.
}
    Acquisition configures one acquisition at its call.

type DegradeError struct {
	// Key is the resource the acquisition was refused for.
	Key ResourceKey
	// Rhythm is the monitor rhythm the refused call carried.
	Rhythm MonitorConfig
	// Reason is the readable cause of the refusal.
	Reason string
}
    DegradeError is the named refusal of an acquisition whose declared kind
    needs a system capability the system does not provide. The monitor rhythm
    of the refused call rides the refusal, so a configuration reaches its reader
    even on the degrade path instead of disappearing with the call.

func (e *DegradeError) Error() string
    Error reports the refusal beside the resource it applies to.

func (e *DegradeError) Unwrap() error
    Unwrap reports the capability absence behind the refusal, so a caller
    classifies it with errors.Is.

type ExecutionDecl struct {
	// Command is the program to run, named exactly as the caller declared it.
	Command string

	// Args are the arguments handed to Command, in order.
	Args []string

	// Dir is the working directory the command runs in. An empty Dir is a caller
	// that declared no directory, which is not the same as a directory that does
	// not apply.
	Dir string

	// Resource is the declared identity of the execution resource this
	// declaration occupies.
	Resource ResourceKey
}
    ExecutionDecl is the declaration one external execution is started under:
    the command, its arguments, the directory it runs in and the resource
    identity it occupies.

    Every field is a plain value, so a declaration recorded before a restart
    replays as the same execution after it. Handles, connections, locks and
    leases are not part of a declaration: the lease of a resource belongs to the
    executor that holds the resource.

type Executor interface {
	// Start begins the external execution declared by decl and returns the
	// canonical key of the resource it now holds.
	Start(ctx context.Context, decl ExecutionDecl) (ResourceKey, error)

	// Probe reports the state one probe observed for key.
	//
	// A state the probe cannot determine is reported as state, not as an error
	// and not as death, so a caller escalates diagnosis without reading
	// uncertainty as an exit.
	Probe(ctx context.Context, key ResourceKey) (ProbeResult, error)

	// Capture returns the output recorded for key: the frame observed while the
	// execution ran, not a partial read that overwrote it.
	Capture(ctx context.Context, key ResourceKey) ([]byte, error)

	// Stop asks the execution behind key to terminate and returns the evidence of
	// what actually stopped.
	//
	// Asking for a termination and observing one are two different facts, and the
	// evidence reports the observation: a stop that was only requested is
	// reported as unconfirmed.
	Stop(ctx context.Context, key ResourceKey) (StopEvidence, error)
}

    Executor is the port through which the runtime starts, probes, captures
    from and stops one external execution, and through which it reports what was
    observed rather than what was requested.

    The redline list (4 rules): MUST own the lease of every resource it holds.
    MUST report what a probe or a stop observed, never what was requested.
    MUST NOT release a resource whose stop it could not confirm. MUST NOT report
    an unconfirmed exit as a clean one.

type Explanation struct {
	// Key is the resource the explanation reports on.
	Key ResourceKey
	// Held reports whether the resource is still held under its key.
	Held bool
	// Rhythm is the monitor rhythm the generation of this resource runs under.
	Rhythm MonitorConfig
	// Deadline is the lease deadline the resource is held to.
	Deadline time.Time
	// Renewed counts the lease renewals the resource received.
	Renewed int
	// Adopted reports whether the resource was reattached rather than started.
	Adopted bool
	// ConsecutiveUnknowns counts the undecidable probes since the last probe
	// that did decide.
	ConsecutiveUnknowns int
	// Escalations counts the diagnosis escalations raised against the resource.
	Escalations int
	// LastStop is the stop observation the resource was last seen under.
	LastStop *StopEvidence
	// Obligations are the open obligations kept beside the lease.
	Obligations []string
}
    Explanation is the retained state of one held resource: the lease, the
    monitor rhythm of its generation, the probe unknowns recorded against it and
    the obligations kept open beside it.

type Manager struct {
	// Has unexported fields.
}
    Manager owns the whole life of the execution resources one runtime holds:
    the acquisition under a declared key, the lease that keeps the resource
    held, the probes that observe it and the release that frees it beside a
    confirmed stop. A resource already held under a key is never opened a second
    time, and a stop the executor cannot confirm leaves the resource held under
    its key.

    The manager reports what it observed rather than what a caller requested:
    a probe that cannot decide reports unknown, an unconfirmed stop keeps the
    lease and the open obligations of the resource, and every retained state
    answers an Explain with the facts it is held on.

func NewManager(backend Executor, opts ...ManagerOption) *Manager
    NewManager returns the execution resource manager over one executor: every
    acquisition, probe, sweep and release runs through the executor behind it.

func (m *Manager) Acquire(ctx context.Context, decl ExecutionDecl, opts ...AcquireOption) (ResourceKey, error)
    Acquire starts the execution declared by decl and returns the canonical key
    of the resource it now holds, or returns the key of the resource already
    held under that identity without starting a second one. A held resource
    whose last stop stayed unconfirmed refuses the acquisition instead, so its
    key is never handed to a second writer beside an unresolved stop. A resource
    whose system capability is missing is refused beside the degrade report of
    that refusal, and the monitor rhythm of the call rides that report.

func (m *Manager) Acquired() int
    Acquired reports how many resources the manager started.

func (m *Manager) Adopt(ctx context.Context, key ResourceKey, opts ...AcquireOption) error
    Adopt binds a lease to the resource already running behind key without
    starting a second execution, so a runtime that re-attaches a session takes
    over what it found. The adoption is refused beside the evidence of what the
    probe observed, and a probe that could not decide is not an absence.

func (m *Manager) Adopted() int
    Adopted reports how many resources the manager reattached.

func (m *Manager) Capture(ctx context.Context, key ResourceKey) ([]byte, error)
    Capture returns the output the executor recorded for one held resource.

func (m *Manager) Close(ctx context.Context) []SweepReport
    Close ends the life of one manager: every post-return tracker of it stops
    and is joined, the expired leases are settled through the sweep of this
    manager, and every resource still held is released through the executor.
    A resource whose stop stays unconfirmed stays held and is reported by name,
    so the close never announces an ending it did not observe. A repeated close
    reports nil and releases nothing a second time; the manager is final once it
    closed.

func (m *Manager) Escalated() int
    Escalated reports how many diagnosis escalations undecidable probes raised.

func (m *Manager) Explain(key ResourceKey) (Explanation, error)
    Explain reports the retained state of one held resource beside the facts it
    is held on. A key nothing is held under answers as not held.

func (m *Manager) Held() int
    Held reports how many resources the manager currently holds.

func (m *Manager) Observe(ctx context.Context, key ResourceKey) (ProbeResult, error)
    Observe reports what one probe observed about the held resource. A probe
    that could not decide extends the unknown run of the resource and raises one
    diagnosis escalation: unknown observations never free the resource and never
    decide its death. A probe that does decide resets the unknown run.

func (m *Manager) Release(ctx context.Context, key ResourceKey) (ReleaseEvidence, error)
    Release frees one held resource beside a confirmed stop and reports the
    evidence behind the outcome. A stop the executor could not confirm keeps the
    resource held under its key beside its open obligations, and the evidence
    reports that retention rather than an ending. The stop is observed under its
    own context, so a caller context already cancelled neither stops nor frees
    anything.

func (m *Manager) Released() int
    Released reports how many resources were freed beside a confirmed stop.

func (m *Manager) Renew(_ context.Context, key ResourceKey, ttl time.Duration) error
    Renew extends the lease of one held resource to a fresh deadline. A renewal
    over a key nothing is held under is refused by name, and so is a renewal
    over a lease that already reached its deadline: expiry hands the resource to
    the explicit stop handling of a sweep, so a late renewal never revives it.

func (m *Manager) Reused() int
    Reused reports how many acquisitions answered from a resource already held
    instead of starting a second one.

func (m *Manager) Sweep(ctx context.Context) []SweepReport
    Sweep drives the explicit stop handling of every resource whose lease
    reached its deadline and reports the outcome of each. Expiry frees nothing
    on its own: each expired resource is stopped through the executor, and one
    whose stop stays unconfirmed stays held beside the report that says so.

type ManagerOption func(*Manager)
    ManagerOption configures one manager at construction.

func WithAvailability(available func(ResourceKey) bool) ManagerOption
    WithAvailability sets the capability probe an acquisition consults before it
    starts a resource of any kind.

func WithClock(now func() time.Time) ManagerOption
    WithClock sets the time source the leases and sweeps read.

func WithDefaultTTL(ttl time.Duration) ManagerOption
    WithDefaultTTL sets the floor lease of an acquisition that declares no TTL.

func WithStateObserver(observer StateObserver) ManagerOption
    WithStateObserver attaches the lifecycle observer of one manager. Only a
    manager carrying an observer starts the post-return tracker; a manager
    beside none keeps the call-scoped renewal and the query face of the resource
    exactly as they were.

type Monitor struct {
	// Has unexported fields.
}
    Monitor reads the silence of monitored executions through the executor
    behind them: one poll captures the recorded frame, probes the execution and
    classifies the quiet it has stood in against the monitor rhythm. A caller
    polls each execution at the interval of its rhythm.

func NewMonitor(executor Executor, rhythm MonitorConfig) *Monitor
    NewMonitor returns the monitor of the executions behind one executor,
    judging silence under the given rhythm. A rhythm field the caller leaves at
    zero takes the default of that field.

func (m *Monitor) Poll(ctx context.Context, key ResourceKey) (MonitorStatus, error)
    Poll observes one execution once and reports the silence classification of
    that observation. The first poll anchors the observed frame; a frame that
    changed since the previous poll resets the silence. A stopped execution is
    never suspect.

func (m *Monitor) Status(key ResourceKey) (MonitorStatus, bool)
    Status reports the last classification of one execution and whether it was
    observed at all.

type MonitorConfig struct {
	// Interval is the base polling rhythm of the monitor.
	Interval time.Duration
	// StableDuration is the quietness threshold of a plain session.
	StableDuration time.Duration
	// InteractiveStableDuration is the quietness threshold of a TUI session.
	InteractiveStableDuration time.Duration
	// FakeDeadDuration is the threshold beyond which a quiet pane with a live
	// process is treated as unresponsive.
	FakeDeadDuration time.Duration
}
    MonitorConfig is the monitor rhythm one execution generation runs under:
    the polling interval beside the quietness thresholds the monitor judges
    output against.

func MonitorConfigFrom(values map[string]any) (MonitorConfig, error)
    MonitorConfigFrom reads the monitor rhythm out of a configuration map
    over the default rhythm: the four supported keys take duration strings,
    and a key outside them — the retired tool-level compress section among them
    — is ignored rather than consumed or refused. A supported key that carries
    no duration is reported by name beside the default rhythm, so a malformed
    setting is visible instead of silently replaced.

func MonitorConfigFromBase(interval, stable, interactive, fakeDead time.Duration) MonitorConfig
    MonitorConfigFromBase returns the rhythm the four declared durations name:
    a duration the declaration leaves at zero keeps the default of that field,
    so a declaration naming only an interval still carries the whole rhythm.

func MonitorConfigWith(base MonitorConfig, values map[string]any) (MonitorConfig, error)
    MonitorConfigWith reads the monitor rhythm out of a configuration map over
    the base rhythm its caller declares: a key the map does not name keeps the
    base of that generation, and a key it names replaces exactly that one field.

type MonitorStatus string
    MonitorStatus is the silence classification of one monitored execution:
    the watch a fresh frame is held under, the stable a quiet frame reaches at
    the stable threshold, and the suspect a live but silent execution reaches at
    its fake-dead threshold.

const (
	// MonitorWatch is the state of an execution whose observed frame has not
	// stood quiet for the stable threshold.
	MonitorWatch MonitorStatus = "watch"
	// MonitorStable is the state of an execution whose observed frame has stood
	// quiet for the stable threshold. A stopped execution's final frame is stable.
	MonitorStable MonitorStatus = "stable"
	// MonitorSuspect is the state of a live execution whose observed frame has
	// stood quiet for the fake-dead threshold.
	MonitorSuspect MonitorStatus = "suspect"
)
type Owner struct {
	// Address is the agent session address the acquiring call belongs to.
	Address event.Address
	// TaskID is the task identity the acquiring call was admitted under.
	TaskID event.TaskID
}
    Owner is the responsibility range one execution resource is tracked for:
    the explicit address a lifecycle event is delivered to and the task identity
    of the call that acquired the resource. It is a plain value, so the root
    assembly resolves it from the call frame it already holds without this
    package depending on the task owner.

type OwnerResolver func(context.Context) (Owner, bool)
    OwnerResolver reports the responsibility range one execution call belongs to
    at the moment the call runs. A call beside no resolvable frame answers false
    and is tracked without an owner.

type ProbeResult struct {
	// State is the state this probe observed.
	State ProbeState

	// Detail carries the observation behind a state that is not alive: the probe
	// command that failed, or the evidence of absence.
	Detail string
}
    ProbeResult is what one probe observed about an execution: alive, dead,
    or not determinable.

    A probe that cannot tell reports an unknown state instead of guessing.
    Unknown is a diagnostic escalation and never a verdict: no run of unknown
    probes, however long, is a death decision, and death is reported only for an
    execution confirmed absent.

type ProbeState int
    ProbeState is the three-valued outcome of one liveness probe. The zero value
    is ProbeUnknown, so a result that was never set reads as undetermined rather
    than as alive.

const (
	// ProbeUnknown is the zero value: the probe could not determine the state. It
	// escalates diagnosis and never decides death.
	ProbeUnknown ProbeState = iota

	// ProbeAlive reports an execution observed to be alive.
	ProbeAlive

	// ProbeDead reports an execution confirmed absent.
	ProbeDead
)
type ReleaseEvidence struct {
	// Resource is the resource identity the release was driven for.
	Resource ResourceKey

	// Released reports whether the resource was freed beside a confirmed stop.
	Released bool

	// Stop is the stop evidence the release observed.
	Stop StopEvidence

	// Reason carries why the resource stayed held where Released is false.
	Reason string
}
    ReleaseEvidence is the settling proof one release produced: whether the
    resource was actually freed, beside the stop evidence observed for it and
    the reason a retained resource stayed held.

    Released is false for a resource whose stop stayed unconfirmed, so a release
    never claims an ending the executor did not observe.

type ReleaseTool struct {
	// Has unexported fields.
}
    ReleaseTool is the callable tool face of one explicit release: it stops the
    resource behind the declared key and reports the evidence of that stop.
    A stop the executor could not confirm keeps the resource held, and the
    answer reports the retention rather than an ending.

func NewReleaseTool(manager *Manager) *ReleaseTool
    NewReleaseTool returns the release tool over the given execution resource
    manager.

func (t *ReleaseTool) Call(ctx context.Context, jsonArgs []byte) (any, error)
    Call stops the execution resource behind the declared key and reports the
    stop evidence observed for it. A confirmed stop answers with the freed
    resource beside its exit code; an unconfirmed stop answers with the retained
    resource beside the reason it stayed held.

func (t *ReleaseTool) Declaration() *trpctool.Declaration
    Declaration returns the fixed declaration of the release tool.

type ResourceKey struct {
	// Kind names the resource family the key identifies within.
	Kind string

	// Key is the canonical identity of the resource inside its kind.
	Key string
}
    ResourceKey is the declared identity of one execution resource. Kind and Key
    together name the resource: declarations that agree on both name the same
    one, and a resource already held under a key is not opened a second time.

    Kind separates resource families whose keys may read alike. Key is the
    canonical form of the identity inside its kind. A key is an identity to be
    compared, never a location to be resolved.

func (k ResourceKey) String() string
    String renders the canonical comparison form of the key: its kind beside its
    identity, so every report names a resource the same way its ledger does.

type RunTool struct {
	// Has unexported fields.
}

    RunTool is the callable tool face of one execution run: it acquires the
    execution resource its arguments declare — reusing the one already held
    under that key — and answers with what the execution recorded. The monitor
    rhythm of the call rides the acquisition it opens, so each borrowed
    generation carries its own rhythm. The lease the call holds is renewed at
    the cadence of that rhythm for as long as the execution runs behind it,
    so a long execution is never swept for having taken long while it is still
    working.

func NewRunTool(manager *Manager, opts ...RunToolOption) *RunTool
    NewRunTool returns the run tool over the given execution resource manager
    and the rhythm its declaration fixes.

func (t *RunTool) Call(ctx context.Context, jsonArgs []byte) (any, error)
    Call acquires the declared execution resource and reports what its execution
    recorded. A resource already held under the key is reused rather than
    started a second time, and a declared kind the system cannot serve answers
    as the named degrade refusal beside the rhythm of the call. The lease
    is kept alive at the cadence of the call's own monitor rhythm until the
    execution behind it returns, and the renewing ends beside the call.

func (t *RunTool) Declaration() *trpctool.Declaration
    Declaration returns the fixed declaration of the run tool.

type RunToolOption func(*RunTool)
    RunToolOption fixes one piece of the run tool beside the manager it acquires
    through.

func WithRunMonitor(rhythm MonitorConfig) RunToolOption
    WithRunMonitor binds the monitor rhythm the declaration fixes to every
    acquisition the tool opens: a call that names no monitor key of its own runs
    under this rhythm instead of the framework default.

func WithRunOwner(resolve OwnerResolver) RunToolOption
    WithRunOwner binds the resolver that reports the responsibility range one
    execution call belongs to. The root assembly supplies it from the call frame
    and the task admission it already holds, so this package never imports the
    task or agent owners. A tool beside no resolver tracks its resources without
    an owner, and the events of those resources drive no follow-up turn.

type StateEvent struct {
	// Resource is the resource the observation belongs to.
	Resource ResourceKey
	// Phase is the lifecycle observation this event reports.
	Phase StatePhase
	// ExitCode is the exit status observed beside a confirmed stop. It is
	// meaningful only where Confirmed reports an observed ending.
	ExitCode int
	// Confirmed reports whether the stop behind this event was observed.
	Confirmed bool
	// Released reports whether the resource was freed by this event.
	Released bool
	// Detail carries the observation behind a retained or suspect event.
	Detail string
	// Owner is the responsibility range the resource is tracked for.
	Owner Owner
	// At is the observation time of the event.
	At time.Time
}
    StateEvent is one lifecycle observation of a held execution resource:
    what was observed, the evidence behind it, and the range it is tracked for.
    The payload carries what the observer needs to render the state; it never
    rewrites the resource's obligations.

    The redline list (3 rules): MUST keep an unconfirmed stop visible beside
    the reason it stayed held. MUST report an exit code only beside an observed
    stop. MUST NOT report stable or a partial frame as success or completion.

type StateObserver func(StateEvent)
    StateObserver receives every lifecycle event of the resources one manager
    holds. The observer is a bypass: it is called outside the manager's lock and
    its failure never changes whether a resource stays held or is freed, so a
    slow or panicking observer cannot turn an ending into a leak or a leak into
    an ending. An observer MUST NOT call back into the manager synchronously.

type StatePhase string
    StatePhase is the lifecycle a held execution resource reached after the call
    that acquired it returned. The phases are observations of the resource,
    never verdicts on the work the command was asked to do: stable is not
    success and a partial frame is not completion.

const (
	// StateStable reports an alive resource whose observed frame stood quiet for
	// the stable threshold. It says nothing about the outcome of the command.
	StateStable StatePhase = "stable"
	// StateSuspect reports a live resource whose frame stayed quiet past the
	// fake-dead threshold, or a probe that could not decide. It is a diagnosis
	// escalation, never a death decision.
	StateSuspect StatePhase = "suspect"
	// StateExited reports a resource observed dead and freed beside a confirmed
	// stop; ExitCode carries the observed exit status.
	StateExited StatePhase = "exited"
	// StateReleased reports a resource freed by an explicit release beside a
	// confirmed stop.
	StateReleased StatePhase = "released"
	// StateRetained reports a resource whose stop stayed unconfirmed: it is
	// still held, and the event names why instead of claiming an ending.
	StateRetained StatePhase = "retained"
)
type StopEvidence struct {
	// Resource is the resource identity this evidence was observed for.
	Resource ResourceKey

	// Confirmed reports whether the execution was observed to have stopped.
	Confirmed bool

	// ExitCode is the exit code observed for the stopped execution. It is
	// meaningful only where Confirmed reports an observed exit.
	ExitCode int

	// Reason carries why the stop is unconfirmed where Confirmed is false.
	Reason string
}
    StopEvidence is the proof a stop produced: what was observed to have
    actually ended, kept apart from the request to end it.

    A cancellation acknowledgement is not this proof. Cancellation is a signal
    the execution may not have observed yet, so an executor holding nothing but
    an acknowledgement reports the stop as unconfirmed.

type SweepReport struct {
	// Key is the resource whose lease reached its deadline.
	Key ResourceKey
	// Expired reports that the lease reached its deadline before the sweep.
	Expired bool
	// Evidence is the release outcome the sweep drove for the expired lease.
	Evidence ReleaseEvidence
}
    SweepReport is what one sweep observed for one expired resource.

type TmuxExecutor struct {
	// Has unexported fields.
}
    TmuxExecutor is the Executor over real tmux sessions: every resource key
    names one tmux session, the command of a declaration runs inside that
    session, and the pane is kept after its command exits so the recorded output
    and exit status stay observable until the resource is stopped.

func NewTmuxExecutor() (*TmuxExecutor, error)
    NewTmuxExecutor returns the executor over the tmux sessions of this system.
    A system whose PATH carries no tmux binary is refused by name, so no
    executor exists that cannot start a session. The availability verdict is the
    real PATH probe of IsTmuxAvailable.

func (e *TmuxExecutor) Capture(ctx context.Context, key ResourceKey) ([]byte, error)
    Capture returns the frame tmux records for key: the pane's history and its
    visible rows, read as one frame so a later read never replaces the output an
    earlier read observed.

func (e *TmuxExecutor) Probe(ctx context.Context, key ResourceKey) (ProbeResult, error)
    Probe reports the state one probe observed for key: a key absent from the
    session list is dead, a key whose pane ended is dead, and a probe that could
    not read the session list or the pane reports unknown rather than death.

func (e *TmuxExecutor) Start(ctx context.Context, decl ExecutionDecl) (ResourceKey, error)
    Start creates the session the declaration's resource key names and runs
    its command inside it, with the pane kept after the command exits. The call
    answers once the new session has produced its first frame, or once the
    bounded observation window passed without one. A key whose session already
    runs is refused instead of opened a second time.

func (e *TmuxExecutor) Stop(ctx context.Context, key ResourceKey) (StopEvidence, error)
    Stop ends the session behind key and reports what was observed: a pane that
    already ended answers its recorded exit status, a live pane is killed and
    the stop is confirmed by the session leaving the list, and an exit status
    the stop could not observe is reported as -1 rather than as a clean exit.
