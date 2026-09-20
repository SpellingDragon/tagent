package reliability

// §8.3 — RECEIVE-SIDE crash-window matrix, run as REAL subprocesses: the child
// drives the production write path (Enqueue → ClaimNext → PrepareFacts, all of
// which flow through writeEnvelopeFile) and hard-exits INSIDE the write-stage
// hook, so the kill lands exactly at tmp-landed / renamed for each audited
// step. The parent then reopens the inbox through a completely fresh Inbox
// instance (independent read-back — no shared memory) and reconciles identity,
// source, counts and unconfirmed material.

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	crashAtEnv  = "TAGENT_CRASH_AT"  // "<callNo>:<stage>" e.g. "1:tmp"
	crashDirEnv = "TAGENT_CRASH_DIR" // inbox dir shared with the parent
)

// crashSlots builds a fixed 2-slot batch under a request id known to BOTH the
// child and the parent (identity reconciliation needs no IPC).
func crashReqID() string { return "crash-req" }

func crashEnvelope() *Envelope {
	return &Envelope{RequestID: crashReqID(), Source: "user", State: InboxStatePending,
		Messages: []MessageSlot{
			{Slot: 0, SourceEvent: srcEvent("c0", "crash-slot-0")},
			{Slot: 1, SourceEvent: srcEvent("c1", "crash-slot-1")},
		}}
}

// TestCrashWindowChild is subprocess-only (see the §5.10 xproc pattern): the
// parent re-executes this test binary with TAGENT_CRASH_AT set. The hook counts
// writeEnvelopeFile invocations (1=enqueue, 2=claim rewrite, 3=prepare rewrite)
// and exits (NO close, NO cleanup) at the requested stage.
func TestCrashWindowChild(t *testing.T) {
	spec, dir := os.Getenv(crashAtEnv), os.Getenv(crashDirEnv)
	if spec == "" || dir == "" {
		t.Skip("crash-window child: spawned by TestCrashWindows_ReceiveSideMatrix only")
	}
	callStr, stage, ok := strings.Cut(spec, ":")
	if !ok {
		fatalChild("malformed TAGENT_CRASH_AT " + spec)
	}
	wantCall, err := strconv.Atoi(callStr)
	if err != nil {
		fatalChild("bad crash call number: " + err.Error())
	}
	in, err := NewInbox(dir, 0)
	if err != nil {
		fatalChild("child NewInbox: " + err.Error())
	}
	cur := 0
	testWriteStageHook = func(s string) {
		if s == "tmp" {
			cur++ // a new writeEnvelopeFile call reaches its tmp stage first
		}
		if cur == wantCall && s == stage {
			fmt.Printf("crashed at call %d stage %s\n", cur, s)
			os.Stdout.Sync()
			os.Exit(0) // the crash: no Close, no defers, no cleanup
		}
	}
	if _, err := in.Enqueue(crashEnvelope()); err != nil {
		fatalChild("child enqueue: " + err.Error())
	}
	env, path, err := in.ClaimNext()
	if err != nil || env == nil {
		fatalChild("child claim: " + fmt.Sprint(err))
	}
	if err := in.PrepareFacts(path, "11112222333344445555666677778888", []json.RawMessage{
		json.RawMessage(`{"prepared":"slot0"}`), json.RawMessage(`{"prepared":"slot1"}`),
	}); err != nil {
		fatalChild("child prepare: " + err.Error())
	}
	fatalChild(fmt.Sprintf("crash point %s never reached (writes seen: %d)", spec, cur))
}

func fatalChild(msg string) {
	fmt.Fprintln(os.Stderr, "CHILD FATAL:", msg)
	os.Exit(1)
}

func TestCrashWindows_ReceiveSideMatrix(t *testing.T) {
	cases := []struct {
		name       string
		spec       string // TAGENT_CRASH_AT
		wantEnvs   int    // envelopes visible after independent reopen
		wantTmp    int    // unconfirmed tmp residue count
		wantState  string // after OPEN requeue semantics (§5.7)
		wantClaims int    // Attempts survives the requeue rewrite
		wantPrep   bool
	}{
		{"receive-tmp", "1:tmp", 0, 1, "", 0, false},                        // died between tmp-landed and rename
		{"receive-renamed", "1:renamed", 1, 0, InboxStatePending, 0, false}, // landed, dir sync never confirmed
		{"claim", "2:renamed", 1, 0, InboxStatePending, 2, false},           // claim landed → open REQUEUES it (never lost)
		{"prepare", "3:renamed", 1, 0, InboxStatePending, 2, true},          // freeze landed → survives requeue intact
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			cmd := exec.Command(os.Args[0], "-test.run", "^TestCrashWindowChild$", "-test.v")
			cmd.Env = append(os.Environ(), crashAtEnv+"="+c.spec, crashDirEnv+"="+dir)
			out, err := cmd.CombinedOutput()
			require.NoError(t, err, "child must hit its crash point:\n%s", out)

			// ---- INDEPENDENT reopen: brand-new instance over the same dir ----
			in, err := NewInbox(dir, 0)
			require.NoError(t, err)
			got, err := in.Outstanding()
			require.NoError(t, err)
			require.Len(t, got, c.wantEnvs, "accepted-set count after %s crash", c.name)

			tmps, err := filepath.Glob(filepath.Join(dir, "inbox-v2", "*.tmp"))
			require.NoError(t, err)
			require.Len(t, tmps, c.wantTmp, "unconfirmed material visible after %s crash", c.name)
			if c.wantTmp > 0 {
				// The tmp orphan is REAL unconfirmed material: it carries the full
				// envelope bytes yet was never renamed — it must not be counted as
				// accepted above, and must remain visible for inspection.
				raw, rerr := os.ReadFile(tmps[0])
				require.NoError(t, rerr)
				require.Contains(t, string(raw), crashReqID())
			}
			if c.wantEnvs == 0 {
				return
			}
			env := got[0].Env
			require.Equal(t, crashReqID(), env.RequestID, "identity survives the crash")
			require.Equal(t, "user", env.Source, "source survives the crash")
			require.Len(t, env.Messages, 2, "fixed slots are never compacted")
			require.Contains(t, string(env.Messages[1].SourceEvent), "crash-slot-1", "slot-1 ORIGINAL is intact")
			require.Equal(t, c.wantState, env.State)
			require.Equal(t, c.wantClaims, env.Attempts, "the crashed claim counts as one attempt at open requeue")
			if c.wantPrep {
				require.Equal(t, "11112222333344445555666677778888", env.ReceiptKey, "reserved key landed atomically with the freeze")
				require.NotEmpty(t, env.Messages[0].PreparedFact)
				require.NotEmpty(t, env.Messages[1].PreparedFact)
			} else {
				require.Empty(t, env.ReceiptKey, "nothing reserved before prepare")
			}
		})
	}
}
