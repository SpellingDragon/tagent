// 契约: docs/wiki/tool/tool-architecture.md#action-tool
//
// The generation face of the tmux executor: the token one session generation is
// minted under, the dedicated marker line its wrapper writes into the pane, and
// the reads that verify a captured frame belongs to that generation. The session
// generation is the proof a receipt stands on: a frame that does not carry the
// token of the generation this executor started is the scrollback of an earlier
// generation, never this run's output.
package exec

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

const (
	// generationMarkerPrefix and generationMarkerSuffix frame the one dedicated
	// line a generation's wrapper writes before and after its command, so the
	// marker is told apart from command output by shape alone and stripped whole.
	generationMarkerPrefix = "<<<exec-generation:"
	generationMarkerSuffix = ">>>"
	// generationTokenBytes is the entropy of one generation token: enough that a
	// stale frame of an earlier generation cannot carry the current one.
	generationTokenBytes = 8
)

var (
	// errStaleGeneration reports a captured frame that does not carry the token
	// of the generation this executor started. The frame is another generation's
	// scrollback and is never answered as this run's output.
	errStaleGeneration = errors.New("exec: the captured frame does not carry the current generation marker")
	// errGenerationToken reports a generation token the system could not mint.
	errGenerationToken = errors.New("exec: the generation token cannot be minted")
)

// tmuxGeneration is the generation one started session runs under: the token its
// marker lines carry and whether a captured frame has already proven the token
// present. A generation is proven once, so a long output whose first lines later
// scroll out of the pane history stays trusted after its first verified frame.
type tmuxGeneration struct {
	token  string
	proven bool
}

// generationTracker is the optional executor face that keeps one generation per
// held resource. The manager consults it for the receipt's execution proof; an
// executor without it (the contract example, a test double) answers no proof
// instead of a claim it cannot make.
type generationTracker interface {
	proofOf(ResourceKey) (string, bool)
}

// setGeneration records the generation a key now runs under, replacing whatever
// generation the key carried before.
func (e *TmuxExecutor) setGeneration(key ResourceKey, generation tmuxGeneration) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.gens == nil {
		e.gens = map[ResourceKey]tmuxGeneration{}
	}
	e.gens[key] = generation
}

// generationOf reports the generation a key runs under.
func (e *TmuxExecutor) generationOf(key ResourceKey) (tmuxGeneration, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	generation, ok := e.gens[key]
	return generation, ok
}

// forgetGeneration drops the generation a key ran under: a stopped key must not
// be verified against a token another generation may carry later.
func (e *TmuxExecutor) forgetGeneration(key ResourceKey) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.gens, key)
}

// noteGenerationProof marks the generation of key proven when frame carries its
// token, and reports whether the generation stands proven afterwards. A
// generation is proven once: later frames of the same session are trusted
// without a second marker read.
func (e *TmuxExecutor) noteGenerationProof(key ResourceKey, frame []byte) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	generation, ok := e.gens[key]
	if !ok || generation.proven {
		return true
	}
	if !frameCarriesGeneration(frame, generation.token) {
		return false
	}
	generation.proven = true
	e.gens[key] = generation
	return true
}

// proofOf reports the execution proof one key's receipt may carry: the
// generation token was verified in a captured frame. A key this executor holds
// no generation for, or whose generation was never proven, answers no proof.
func (e *TmuxExecutor) proofOf(key ResourceKey) (string, bool) {
	generation, ok := e.generationOf(key)
	if !ok || !generation.proven {
		return "", false
	}
	return "generation verified", true
}

// mintGenerationToken mints the random token one session generation is bound to.
func mintGenerationToken() (string, error) {
	raw := make([]byte, generationTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("%w: %v", errGenerationToken, err)
	}
	return hex.EncodeToString(raw), nil
}

// generationMarker renders the dedicated line one generation's wrapper writes:
// a prefix and suffix keep the marker distinguishable from any command output
// and allow the whole line to be stripped before the receipt is assembled.
func generationMarker(token string) string {
	return generationMarkerPrefix + token + generationMarkerSuffix
}

// frameCarriesGeneration reports whether frame carries one complete marker line
// of the given token: the marker is its own line, so a command's output cannot
// fake it by quoting the token inside a longer line.
func frameCarriesGeneration(frame []byte, token string) bool {
	if token == "" {
		return false
	}
	want := generationMarker(token)
	for _, line := range strings.Split(strings.ReplaceAll(string(frame), "\r\n", "\n"), "\n") {
		if strings.TrimSuffix(line, "\r") == want {
			return true
		}
	}
	return false
}

// stripGenerationMarkers removes every generation marker line from a frame: the
// marker is bookkeeping for the receipt, never user-visible output, and an old
// generation's marker left in a reused frame is stripped beside the current one.
func stripGenerationMarkers(frame []byte) []byte {
	lines := strings.SplitAfter(string(frame), "\n")
	var clean strings.Builder
	clean.Grow(len(frame))
	for _, line := range lines {
		body := strings.TrimSuffix(line, "\n")
		body = strings.TrimSuffix(body, "\r")
		if strings.HasPrefix(body, generationMarkerPrefix) && strings.HasSuffix(body, generationMarkerSuffix) {
			continue
		}
		clean.WriteString(line)
	}
	return []byte(clean.String())
}
