// Command recover-kv rebuilds the independent bodies a journal root's references
// name but whose blob: keys are absent, the state the original store's load
// refuses by name as ErrSnapshotCorrupt.
//
// The chain holds the same text: assembly's persistFact commits
// body.Message.Content as the blob and the whole encoded EventBody as the segment
// record of one key, so a missing body is rebuilt from the event of that key and
// accepted only when its sha256 digest and byte length equal the ones the
// reference recorded. The scan is a dry run unless --restore is given. It only
// adds missing bodies: no reference is deleted, no chain record is touched, and a
// body the chain cannot answer for is named with its owner and exits nonzero.
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
// reference was accepted under and the completeness information its body carries.
// One event key names one body, so every record of a key states one digest and
// length and the first record answers for all of them.
type storedRef struct {
	OwnerAgent   string `json:"owner_agent"`
	OwnerSession string `json:"owner_session"`
	Digest       string `json:"digest"`
	Length       int64  `json:"length"`
}

// orphan is one reference whose independent body the root does not hold, named
// with the owner the reference was registered under.
type orphan struct {
	// Key is the event key the reference and the missing body share.
	Key string
	// OwnerAgent is the agent the reference was accepted for.
	OwnerAgent string
	// OwnerSession is the session of that agent the reference was accepted for.
	OwnerSession string
}

// report is one scan of a journal root: every orphan reference classified by what
// the chain holds for its key, each list in key order.
type report struct {
	// Recoverable holds the orphans whose body the chain rebuilds, as a dry run
	// reports them.
	Recoverable []orphan
	// Restored holds the orphans whose rebuilt body was committed.
	Restored []orphan
	// TrulyLost holds the orphans whose key the chain holds no event for.
	TrulyLost []orphan
	// Mismatch holds the orphans whose chain body is not the body the reference
	// names.
	Mismatch []orphan
}

// exitCode answers 0 when every orphan of the scan is rebuildable and 1 when any
// is not, so a root that still cannot boot is reported as a failure.
func (r report) exitCode() int {
	if len(r.TrulyLost) > 0 || len(r.Mismatch) > 0 {
		return 1
	}
	return 0
}

func main() {
	root := flag.String("root", "", "live journal root to repair")
	restore := flag.Bool("restore", false, "commit the rebuilt bodies; without it the scan writes nothing")
	flag.Parse()
	if *root == "" {
		fail(fmt.Errorf("--root is required"))
	}
	scanned, err := recoverOrphanOriginals(context.Background(), *root, *restore)
	if err != nil {
		fail(err)
	}
	printReport(*root, *restore, scanned)
	if scanned.exitCode() != 0 {
		fail(fmt.Errorf("%d truly lost and %d mismatched references leave %s unable to boot", len(scanned.TrulyLost), len(scanned.Mismatch), *root))
	}
}

// recoverOrphanOriginals scans the journal root for references whose independent
// body is absent and rebuilds each one the chain answers for.
//   - The orphan keys are walked in key order, so one root reports the same classes in the same order on every run
//   - A rebuilt body is committed only when its sha256 digest and byte length equal the ones the reference recorded; a key the chain holds no event for and a body that differs are reported by name and left untouched
//   - restore=false writes nothing and reports the rebuildable keys as recoverable
//   - After a commit the root is re-read through a freshly opened engine, and a restored key whose body or reference is not held is reported as an error
//   - A reference or chain body that does not decode is reported as an error naming its key; the bodies already committed stay, each having matched its reference before it was written
func recoverOrphanOriginals(ctx context.Context, root string, restore bool) (report, error) {
	tier, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageJournal, Root: root})
	if err != nil {
		return report{}, err
	}
	blobKeys, err := tier.Store.Keys(ctx, "blob:")
	if err != nil {
		return report{}, err
	}
	refPairs, err := tier.Store.Read(ctx, ledger.Fetch{Prefix: "ref:"})
	if err != nil {
		return report{}, err
	}
	blobs := keySet(blobKeys)
	refs := map[string][]byte{}
	var keys []string
	for _, pair := range refPairs {
		key, named := strings.CutPrefix(pair.Key, "ref:")
		if !named {
			continue
		}
		refs[key] = pair.Value
		if _, paired := blobs["blob:"+key]; !paired {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	var scanned report
	for _, key := range keys {
		var records []storedRef
		if err := json.Unmarshal(refs[key], &records); err != nil {
			return scanned, fmt.Errorf("recover-kv: reference record %q: %w", key, err)
		}
		if len(records) == 0 {
			return scanned, fmt.Errorf("recover-kv: reference record %q names no body to rebuild", key)
		}
		record := records[0]
		item := orphan{Key: key, OwnerAgent: record.OwnerAgent, OwnerSession: record.OwnerSession}
		stored, held, err := tier.Segments.GetEvent(ctx, event.EventID(key))
		if err != nil {
			return scanned, err
		}
		if !held {
			scanned.TrulyLost = append(scanned.TrulyLost, item)
			continue
		}
		body, err := memory.DecodeEventBody(stored.Body)
		if err != nil {
			return scanned, fmt.Errorf("recover-kv: chain body of %q: %w", key, err)
		}
		content := []byte(body.Message.Content)
		if digestOf(content) != record.Digest || int64(len(content)) != record.Length {
			scanned.Mismatch = append(scanned.Mismatch, item)
			continue
		}
		if !restore {
			scanned.Recoverable = append(scanned.Recoverable, item)
			continue
		}
		if err := tier.Store.Commit(ctx, ledger.Batch{Puts: []ledger.Pair{{Key: "blob:" + key, Value: content}}}); err != nil {
			return scanned, fmt.Errorf("recover-kv: commit the rebuilt body of %q: %w", key, err)
		}
		scanned.Restored = append(scanned.Restored, item)
	}
	if len(scanned.Restored) > 0 {
		if err := verifyRestored(ctx, root, scanned.Restored); err != nil {
			return scanned, err
		}
	}
	return scanned, nil
}

// verifyRestored re-reads the two original namespaces of the root through a
// freshly opened engine and reports the first restored key whose reference or
// rebuilt body is not held, so a commit that did not land is named instead of
// counted.
func verifyRestored(ctx context.Context, root string, restored []orphan) error {
	store, err := ledger.OpenAtomicStore(ctx, ledger.StorageConfig{Type: ledger.StorageJournal, Root: root})
	if err != nil {
		return err
	}
	blobKeys, err := store.Keys(ctx, "blob:")
	if err != nil {
		return err
	}
	refKeys, err := store.Keys(ctx, "ref:")
	if err != nil {
		return err
	}
	blobs, refs := keySet(blobKeys), keySet(refKeys)
	for _, item := range restored {
		if _, held := blobs["blob:"+item.Key]; !held {
			return fmt.Errorf("recover-kv: the rebuilt body of %q is not held after the commit", item.Key)
		}
		if _, held := refs["ref:"+item.Key]; !held {
			return fmt.Errorf("recover-kv: the reference of %q is not held after the commit", item.Key)
		}
	}
	return nil
}

// printReport writes the summary of one scan: the count of every class, each
// reference named with its owner, and whether anything was committed.
func printReport(root string, restore bool, scanned report) {
	total := len(scanned.Recoverable) + len(scanned.Restored) + len(scanned.TrulyLost) + len(scanned.Mismatch)
	if total == 0 {
		fmt.Printf("recover-kv: %s: nothing to recover, every reference names its independent body\n", root)
		return
	}
	fmt.Printf("recover-kv: %s: %d orphan references: %d recoverable, %d restored, %d truly lost, %d mismatch\n",
		root, total, len(scanned.Recoverable), len(scanned.Restored), len(scanned.TrulyLost), len(scanned.Mismatch))
	if !restore {
		fmt.Println("recover-kv: dry run, nothing written; rerun with --restore to commit the rebuilt bodies")
	}
	printOrphans("recoverable: the chain rebuilds the body", scanned.Recoverable)
	printOrphans("restored: the rebuilt body was committed", scanned.Restored)
	printOrphans("truly lost: the chain holds no event for the key", scanned.TrulyLost)
	printOrphans("mismatch: the chain body is not the body the reference names", scanned.Mismatch)
}

// printOrphans names every reference of one class with the owner it was
// registered under, so the operator can decide what to do with it.
func printOrphans(class string, list []orphan) {
	if len(list) == 0 {
		return
	}
	fmt.Printf("recover-kv: %s\n", class)
	for _, item := range list {
		fmt.Printf("recover-kv:   %s owner agent=%s session=%s\n", item.Key, item.OwnerAgent, item.OwnerSession)
	}
}

// keySet returns the keys as a membership set.
func keySet(keys []string) map[string]struct{} {
	set := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		set[key] = struct{}{}
	}
	return set
}

// digestOf returns the canonical digest text of body, the form the original store
// records for the bytes it commits.
func digestOf(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// fail reports one recovery failure to stderr and exits nonzero.
func fail(err error) {
	fmt.Fprintln(os.Stderr, "recover-kv:", err)
	os.Exit(1)
}
