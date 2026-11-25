// Command recover-kv verifies that every reference a journal root holds is
// answered by the text of its original: a fact commit stages the reference alone
// and commits the text inside the event body of the same key, so the chain is the
// one place that text lives and the recall reads it back from there.
//
// The command writes nothing. For every reference key of the root it reads the
// copies the root holds of that text — the event body of the key, and the
// independent body a bare commit staged beside the reference — and compares the
// sha256 digest and byte length of each with the ones the references recorded. A
// key the root holds no text for is named truly lost, a copy whose bytes are not
// the ones the reference names is named a mismatch, and either one exits nonzero;
// a root whose every reference is answered by the text it names exits zero. The
// root is read through the one journal view that locates a key in the hot index and
// in the archive manifest alike, so a text the hot files do not carry is read from
// the archive root the operator names and an archive that cannot answer is reported
// by name.
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/ledger"
)

// storedRef is the persisted reference record this command reads: the scope the
// reference was accepted under and the completeness information of the text it
// names. Every record of a key states the completeness of the same text, so each
// is checked against the copies the root holds.
type storedRef struct {
	OwnerAgent   string `json:"owner_agent"`
	OwnerSession string `json:"owner_session"`
	Digest       string `json:"digest"`
	Length       int64  `json:"length"`
}

// reference is one key the root holds references for, named with the owner of the
// first reference registered under it.
type reference struct {
	// Key is the event key the references and the text they name share.
	Key string
	// OwnerAgent is the agent the first reference was accepted for.
	OwnerAgent string
	// OwnerSession is the session of that agent the reference was accepted for.
	OwnerSession string
}

// report is one verification of a journal root: what the open stated about the log's
// tail and the index it read, then every reference the root holds no text for and
// every reference whose text differs from the one it names, each list in key order.
type report struct {
	// TornTailBytes is the byte of the log the last frame the open read starts at, taken
	// from the open itself: the frame at that byte is torn, so the log ends there.
	TornTailBytes int64
	// TornTail reports whether the log ended in a torn frame at all, which the byte alone
	// cannot state: a log whose one frame is torn from its first byte stops at the zero a
	// log of whole frames stops at.
	TornTail bool
	// IndexNotice states what the open said about the index files of the root, and is
	// empty beside an open that read the commits past an index watermark.
	IndexNotice string
	// TrulyLost holds the references the root holds no text for: neither an event
	// body nor an independent body carries the text they name.
	TrulyLost []reference
	// Mismatch holds the references holding a copy whose digest or length is not the
	// one they recorded.
	Mismatch []reference
}

// exitCode answers 0 when every reference of the root is answered by the text it
// names and 1 when any is not, so a root whose recall cannot serve every original
// is reported as a failure.
func (r report) exitCode() int {
	if len(r.TrulyLost) > 0 || len(r.Mismatch) > 0 {
		return 1
	}
	return 0
}

func main() {
	root := flag.String("root", "", "journal root whose references to verify")
	archiveRoot := flag.String("archive-root", "", "archive root whose manifest locates the segments the hot log does not state")
	flag.Parse()
	if *root == "" {
		fail(fmt.Errorf("--root is required"))
	}
	verified, err := verifyOriginalReferences(context.Background(), *root, *archiveRoot)
	if err != nil {
		fail(err)
	}
	printReport(*root, verified)
	if verified.exitCode() != 0 {
		fail(fmt.Errorf("%d truly lost and %d mismatched references leave %s unable to serve every original", len(verified.TrulyLost), len(verified.Mismatch), *root))
	}
}

// verifyOriginalReferences verifies every reference of the journal root against the
// text the root holds for its key.
//   - The reference keys are walked in key order, so one root reports the same classes in the same order on every run
//   - A key the root holds no text for — no event body and no independent body — is reported truly lost by name, and a copy whose sha256 digest or byte length is not the one the reference recorded is reported a mismatch; a reference every copy agrees with is not reported
//   - Every copy the root holds of the text is checked: the event body of the key, and the independent body a bare commit staged beside the reference, so a root holding both states a copy that drifted
//   - The open declares no write rights, so the scan reads beside the live runtime and writes no byte
//   - A reference record or an event body that does not decode is reported as an error naming its key
//   - A key the hot log does not state is read through the archive root, so a text that moved reads back instead of being reported truly lost, and a segment or record the archive cannot serve is reported by its own name
//   - What the open found at the end of the log and in the index files is taken from it, so the report states the root the scan met
func verifyOriginalReferences(ctx context.Context, root, archiveRoot string) (report, error) {
	config := ledger.StorageConfig{Type: ledger.StorageJournal, Root: root}
	config.ArchiveRoot = archiveRoot
	tier, err := ledger.OpenTier(ctx, config)
	if err != nil {
		return report{}, err
	}
	var scanned report
	scanned.TornTailBytes, scanned.TornTail = tornTailOf(tier)
	scanned.IndexNotice = indexNoticeOf(tier)
	blobPairs, err := tier.Store.Read(ctx, ledger.Fetch{Prefix: "blob:"})
	if err != nil {
		return report{}, err
	}
	refPairs, err := tier.Store.Read(ctx, ledger.Fetch{Prefix: "ref:"})
	if err != nil {
		return report{}, err
	}
	blobs := map[string][]byte{}
	for _, pair := range blobPairs {
		if key, named := strings.CutPrefix(pair.Key, "blob:"); named {
			blobs[key] = pair.Value
		}
	}
	refs := map[string][]byte{}
	var keys []string
	for _, pair := range refPairs {
		key, named := strings.CutPrefix(pair.Key, "ref:")
		if !named {
			continue
		}
		refs[key] = pair.Value
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		var records []storedRef
		if err := json.Unmarshal(refs[key], &records); err != nil {
			return scanned, fmt.Errorf("recover-kv: reference record %q: %w", key, err)
		}
		if len(records) == 0 {
			return scanned, fmt.Errorf("recover-kv: reference record %q names no text to verify", key)
		}
		item := reference{Key: key, OwnerAgent: records[0].OwnerAgent, OwnerSession: records[0].OwnerSession}
		body, held := blobs[key]
		drifted := held && differs(body, records)
		stored, onChain, err := tier.Segments.GetEvent(ctx, event.EventID(key))
		if err != nil {
			return scanned, err
		}
		if onChain {
			committed, err := memory.DecodeEventBody(stored.Body)
			if err != nil {
				return scanned, fmt.Errorf("recover-kv: chain body of %q: %w", key, err)
			}
			if differs([]byte(committed.Message.Content), records) {
				drifted = true
			}
		}
		if !held && !onChain {
			scanned.TrulyLost = append(scanned.TrulyLost, item)
			continue
		}
		if drifted {
			scanned.Mismatch = append(scanned.Mismatch, item)
		}
	}
	return scanned, nil
}

// differs reports whether body is not the text the reference records of one key
// name: they all state the completeness of that one text, so a single digest or
// length that does not match the bytes is a drift.
func differs(body []byte, records []storedRef) bool {
	digest, length := digestOf(body), int64(len(body))
	for _, record := range records {
		if record.Digest != digest || record.Length != length {
			return true
		}
	}
	return false
}

// printReport writes the summary of one verification: what the open stated about the
// log's tail and the index it read, the count of every class, and each reference
// named with its owner.
func printReport(root string, scanned report) {
	if scanned.IndexNotice != "" {
		fmt.Printf("recover-kv: %s: %s\n", root, scanned.IndexNotice)
	}
	if scanned.TornTail {
		fmt.Printf("recover-kv: %s: the log ends in a torn tail at %d, and the scan cut nothing: it read the whole frames before it\n", root, scanned.TornTailBytes)
	}
	total := len(scanned.TrulyLost) + len(scanned.Mismatch)
	if total == 0 {
		fmt.Printf("recover-kv: %s: every reference is answered by the text it names\n", root)
		return
	}
	fmt.Printf("recover-kv: %s: %d references not answered by the text they name: %d truly lost, %d mismatch\n",
		root, total, len(scanned.TrulyLost), len(scanned.Mismatch))
	printReferences("truly lost: the root holds no text for the key", scanned.TrulyLost)
	printReferences("mismatch: the text held for the key is not the text the reference names", scanned.Mismatch)
}

// printReferences names every reference of one class with the owner it was
// registered under, so the operator can decide what to do with it.
func printReferences(class string, list []reference) {
	if len(list) == 0 {
		return
	}
	fmt.Printf("recover-kv: %s\n", class)
	for _, item := range list {
		fmt.Printf("recover-kv:   %s owner agent=%s session=%s\n", item.Key, item.OwnerAgent, item.OwnerSession)
	}
}

// tornTailOf answers the byte the log the open read ends its last whole frame at and
// whether that open met a torn tail there, through the measure the journal engine states.
// A store that states no such measure answers none.
func tornTailOf(tier *ledger.Tier) (int64, bool) {
	probe, held := tier.Store.(interface{ TornTailBytesOpen() (int64, bool) })
	if !held {
		return 0, false
	}
	return probe.TornTailBytesOpen()
}

// indexNoticeOf answers what the open of tier said about the index files it read, through
// the method the journal engine states. A store that states no notice answers empty.
func indexNoticeOf(tier *ledger.Tier) string {
	probe, held := tier.Store.(interface{ IndexUnstatedNotice() string })
	if !held {
		return ""
	}
	return probe.IndexUnstatedNotice()
}

// digestOf returns the canonical digest text of body, the form the original store
// records for the bytes it commits.
func digestOf(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// fail reports one verification failure to stderr and exits nonzero.
func fail(err error) {
	fmt.Fprintln(os.Stderr, "recover-kv:", err)
	os.Exit(1)
}
