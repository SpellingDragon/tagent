package state

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// snapshotPrefix locates the recovery snapshot blobs on the same log the facts
// live in: one blob per digest, the bytes a recovery ticket names. The blob is
// referenced material, not a second truth — the ticket fact on the chain is the
// truth, a blob that no ticket names is unreferenced, and a read that fails the
// ticket's digest falls back to the full replay.
const snapshotPrefix = "tagent/task/v1/snapshot:"

// ticketPrefix locates the recovery ticket facts on the log.
const ticketPrefix = "tagent/task/v1/ticket:"

// recoverySchema is the payload layout this build writes and reads. A blob
// carrying any other schema version is not guessed at: the load falls back to
// the full replay instead.
const recoverySchema = 1

// ErrRecoveryPayloadRefused names a snapshot payload this build cannot restore.
var ErrRecoveryPayloadRefused = errors.New("state: the recovery payload is not a layout this build restores")

// noticeKeyString renders one notice identity as the map key of the serialized
// tables, so the payload carries no unexported key types.
func noticeKeyString(key noticeKey) string {
	return string(key.task) + "\x00" + strconv.FormatInt(key.round, 10) + "\x00" + string(key.kind) + "\x00" + strconv.FormatInt(key.seq, 10)
}

// noticeKeyOfString parses one serialized notice identity back.
func noticeKeyOfString(text string) (noticeKey, bool) {
	parts := strings.Split(text, "\x00")
	if len(parts) != 4 {
		return noticeKey{}, false
	}
	round, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return noticeKey{}, false
	}
	seq, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil {
		return noticeKey{}, false
	}
	return noticeKey{task: event.TaskID(parts[0]), round: round, kind: task.NoticeKind(parts[2]), seq: seq}, true
}

// recoveryTables is the serializable image of the carrier's eight tables. The
// maps marshal under encoding/json's sorted-key order, so the same carrier
// state renders the same bytes every time.
type recoveryTables struct {
	SchemaVersion int                            `json:"schema_version"`
	Revisions     map[string]int64               `json:"revisions"`
	Facts         map[string][]task.CommitRecord `json:"facts"`
	Specs         map[string]task.Spec           `json:"specs"`
	Owed          map[string]task.OutboxNotice   `json:"owed"`
	Consumed      []string                       `json:"consumed"`
	Attempts      map[string]SettleAttempt       `json:"attempts"`
	Rejected      map[string]Rejection           `json:"rejected"`
	Mutations     map[string]task.CommitRecord   `json:"mutations"`
	Cursor        string                         `json:"cursor"`
}

// snapshotBytes renders the deterministic image of the carrier's tables beside
// the cursor: the fact key of the last committed record the image covers. The
// image is taken under the carrier lock: a compression may land while other
// goroutines commit, and an unlocked read of the tables is a fatal map race.
func (d *DurableCommit) snapshotBytes() ([]byte, string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	image := recoveryTables{
		SchemaVersion: recoverySchema,
		Revisions:     map[string]int64{},
		Facts:         map[string][]task.CommitRecord{},
		Specs:         map[string]task.Spec{},
		Owed:          map[string]task.OutboxNotice{},
		Consumed:      []string{},
		Attempts:      map[string]SettleAttempt{},
		Rejected:      map[string]Rejection{},
	}
	cursor := ""
	for taskID, revision := range d.revisions {
		image.Revisions[string(taskID)] = revision
	}
	for taskID, records := range d.facts {
		ordered := append([]task.CommitRecord(nil), records...)
		sort.Slice(ordered, func(i, j int) bool { return ordered[i].ExpectedRevision < ordered[j].ExpectedRevision })
		image.Facts[string(taskID)] = ordered
		for _, record := range ordered {
			if key := factKey(taskID, record.MutationID); key > cursor {
				cursor = key
			}
		}
	}
	for taskID, spec := range d.specs {
		image.Specs[string(taskID)] = spec
	}
	for key, notice := range d.owed {
		image.Owed[noticeKeyString(key)] = notice
	}
	for key := range d.consumed {
		image.Consumed = append(image.Consumed, noticeKeyString(key))
	}
	sort.Strings(image.Consumed)
	for key, attempt := range d.attempts {
		image.Attempts[noticeKeyString(key)] = attempt
	}
	for key, rejection := range d.rejected {
		image.Rejected[noticeKeyString(key)] = rejection
	}
	image.Mutations = map[string]task.CommitRecord{}
	for key, record := range d.mutations {
		image.Mutations[string(key.task)+"\x00"+string(key.mutation)] = record
	}
	image.Cursor = cursor
	body, err := json.Marshal(image)
	if err != nil {
		return nil, "", err
	}
	return body, cursor, nil
}

// WriteRecoveryTicket files one snapshot of the carrier's tables beside the
// ticket fact that names it: the blob lands first behind its own barrier, the
// ticket after it behind a second barrier, so a ticket always names bytes the
// log already holds. The caller must hold no carrier lock and keep the carrier
// quiet for the duration of the write.
//
// 契约: docs/FACT-VOCABULARY.md#families
func (d *DurableCommit) WriteRecoveryTicket(ctx context.Context) error {
	body, cursor, err := d.snapshotBytes()
	if err != nil {
		return err
	}
	sum := sha256.Sum256(body)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	if err := d.log.Put(ctx, snapshotPrefix+digest, body); err != nil {
		return err
	}
	if err := d.log.Sync(ctx); err != nil {
		return err
	}
	ticket := event.RecoveryTicketFor(recoverySchema, cursor, digest, int64(len(body)))
	if err := d.log.Put(ctx, ticketPrefix+ticket.FactKey(), ticket.Payload()); err != nil {
		return err
	}
	return d.log.Sync(ctx)
}

// latestTicket reads one recovery ticket the log holds - the one whose
// cursor sorts last, a lexical pick that does not promise the fullest
// coverage; if its blob is gone the load falls to the full replay. A log
// holding none answers ok=false.
func (d *DurableCommit) latestTicket(ctx context.Context) (ticketCursor string, digest string, length int64, schema int, ok bool, err error) {
	tickets, err := d.log.Scan(ctx, ticketPrefix)
	if err != nil {
		return "", "", 0, 0, false, err
	}
	for key, body := range tickets {
		var filed struct {
			SchemaVersion int    `json:"schema_version"`
			Digest        string `json:"digest"`
			Length        int64  `json:"length"`
			Cursor        string `json:"cursor"`
		}
		if err := json.Unmarshal(body, &filed); err != nil {
			continue
		}
		if !ok || filed.Cursor > ticketCursor {
			ticketCursor, digest, length, schema, ok = filed.Cursor, filed.Digest, filed.Length, filed.SchemaVersion, true
		}
		_ = key
	}
	return ticketCursor, digest, length, schema, ok, nil
}

// restoreTables loads one snapshot image into the carrier's tables. A payload
// this build does not restore is refused by name, and a partial restore never
// happens: the tables are filled only after the whole image parsed.
func (d *DurableCommit) restoreTables(body []byte) error {
	var image recoveryTables
	if err := json.Unmarshal(body, &image); err != nil {
		return err
	}
	if image.SchemaVersion != recoverySchema {
		return ErrRecoveryPayloadRefused
	}
	consumed := map[noticeKey]struct{}{}
	for _, keyText := range image.Consumed {
		key, ok := noticeKeyOfString(keyText)
		if !ok {
			return ErrRecoveryPayloadRefused
		}
		consumed[key] = struct{}{}
	}
	owed := map[noticeKey]task.OutboxNotice{}
	for keyText, notice := range image.Owed {
		key, ok := noticeKeyOfString(keyText)
		if !ok {
			return ErrRecoveryPayloadRefused
		}
		owed[key] = notice
	}
	attempts := map[noticeKey]SettleAttempt{}
	for keyText, attempt := range image.Attempts {
		key, ok := noticeKeyOfString(keyText)
		if !ok {
			return ErrRecoveryPayloadRefused
		}
		attempts[key] = attempt
	}
	rejected := map[noticeKey]Rejection{}
	for keyText, rejection := range image.Rejected {
		key, ok := noticeKeyOfString(keyText)
		if !ok {
			return ErrRecoveryPayloadRefused
		}
		rejected[key] = rejection
	}
	facts := map[event.TaskID][]task.CommitRecord{}
	for taskText, records := range image.Facts {
		ordered := append([]task.CommitRecord(nil), records...)
		sort.Slice(ordered, func(i, j int) bool { return ordered[i].ExpectedRevision < ordered[j].ExpectedRevision })
		facts[event.TaskID(taskText)] = ordered
	}
	specs := map[event.TaskID]task.Spec{}
	for taskText, spec := range image.Specs {
		specs[event.TaskID(taskText)] = spec
	}
	revisions := map[event.TaskID]int64{}
	for taskText, revision := range image.Revisions {
		revisions[event.TaskID(taskText)] = revision
	}
	mutations := map[mutationKey]task.CommitRecord{}
	for keyText, record := range image.Mutations {
		parts := strings.SplitN(keyText, "\x00", 2)
		if len(parts) != 2 {
			return ErrRecoveryPayloadRefused
		}
		mutations[mutationKey{task: event.TaskID(parts[0]), mutation: task.MutationID(parts[1])}] = record
	}
	d.revisions, d.facts, d.specs = revisions, facts, specs
	d.mutations = mutations
	d.owed, d.consumed, d.attempts, d.rejected = owed, consumed, attempts, rejected
	return nil
}

// loadWithTicket is the recovery split: a ticket whose blob verifies restores
// the tables directly and merges only what the log gained after the cursor;
// anything else — no ticket, a missing or corrupted blob, a schema this build
// does not restore — falls back to the full replay, so a restart without a
// usable ticket rebuilds exactly as it always did.
func (d *DurableCommit) loadWithTicket(ctx context.Context) error {
	_, digest, length, schema, ok, err := d.latestTicket(ctx)
	if err != nil || !ok {
		return d.load(ctx)
	}
	body, err := d.readSnapshot(ctx, digest, length)
	if err != nil || schema != recoverySchema {
		return d.load(ctx)
	}
	if err := d.restoreTables(body); err != nil {
		return d.load(ctx)
	}
	if err := d.mergeDelta(ctx); err != nil {
		d.resetTables()
		return d.load(ctx)
	}
	d.usedTicket = true
	return nil
}

// resetTables empties every carrier table, the state a fallback needs before
// the full replay fills them again: the replay appends, so a half-restored
// image would double every record it already held.
func (d *DurableCommit) resetTables() {
	d.revisions = map[event.TaskID]int64{}
	d.facts = map[event.TaskID][]task.CommitRecord{}
	d.specs = map[event.TaskID]task.Spec{}
	d.mutations = map[mutationKey]task.CommitRecord{}
	d.owed = map[noticeKey]task.OutboxNotice{}
	d.consumed = map[noticeKey]struct{}{}
	d.attempts = map[noticeKey]SettleAttempt{}
	d.rejected = map[noticeKey]Rejection{}
}

// readSnapshot reads one snapshot blob and verifies it against the digest and
// length the ticket names: a truncated or substituted blob is a failed read,
// never a restored carrier.
func (d *DurableCommit) readSnapshot(ctx context.Context, digest string, length int64) ([]byte, error) {
	body, held, err := d.log.Get(ctx, snapshotPrefix+digest)
	if err != nil || !held {
		return nil, fmt.Errorf("state: the recovery blob %s is not held", digest)
	}
	if int64(len(body)) != length {
		return nil, fmt.Errorf("state: the recovery blob %s is %d bytes, want %d", digest, len(body), length)
	}
	sum := sha256.Sum256(body)
	if "sha256:"+hex.EncodeToString(sum[:]) != digest {
		return nil, fmt.Errorf("state: the recovery blob %s does not match its digest", digest)
	}
	return body, nil
}

// mergeDelta folds only what the log gained after the snapshot: the four
// record kinds merge by identity (a key the image already holds changes
// nothing), and the commit facts merge by revision (a record at or below the
// revision the image reached for its task replays nothing new).
func (d *DurableCommit) mergeDelta(ctx context.Context) error {
	specs, err := d.scan(ctx, specPrefix)
	if err != nil {
		return err
	}
	for key, body := range specs {
		taskID := event.TaskID(key[len(specPrefix):])
		if _, held := d.specs[taskID]; held {
			continue
		}
		var spec task.Spec
		if err := json.Unmarshal(body, &spec); err != nil {
			return err
		}
		d.specs[taskID] = spec
	}
	receipts, err := d.scan(ctx, consumePrefix)
	if err != nil {
		return err
	}
	for _, body := range receipts {
		var receipt consumeRecord
		if err := json.Unmarshal(body, &receipt); err != nil {
			return err
		}
		key := noticeKey{task: receipt.TaskID, round: receipt.Round, kind: receipt.Kind, seq: receipt.Seq}
		d.consumed[key] = struct{}{}
		delete(d.owed, key)
	}
	attempts, err := d.scan(ctx, attemptPrefix)
	if err != nil {
		return err
	}
	for _, body := range attempts {
		var record attemptRecord
		if err := json.Unmarshal(body, &record); err != nil {
			return err
		}
		d.attempts[noticeKey{task: record.TaskID, round: record.Round, kind: record.Kind, seq: record.Seq}] = record.Attempt
	}
	rejections, err := d.scan(ctx, rejectPrefix)
	if err != nil {
		return err
	}
	for _, body := range rejections {
		var rejection Rejection
		if err := json.Unmarshal(body, &rejection); err != nil {
			return err
		}
		key := noticeKeyOf(rejection.Notice)
		d.rejected[key] = rejection
		delete(d.owed, key)
	}
	facts, err := d.scan(ctx, factPrefix)
	if err != nil {
		return err
	}
	byTask := map[event.TaskID][]task.CommitRecord{}
	for _, body := range facts {
		record, err := task.DecodeTaskRecord(body)
		if err != nil {
			return err
		}
		byTask[record.Fact.TaskID] = append(byTask[record.Fact.TaskID], record.Fact)
	}
	for taskID, records := range byTask {
		sort.Slice(records, func(i, j int) bool { return records[i].ExpectedRevision < records[j].ExpectedRevision })
		for _, record := range records {
			if record.ExpectedRevision < d.revisions[taskID] {
				continue
			}
			d.facts[taskID] = append(d.facts[taskID], record)
			d.mutations[mutationKey{task: taskID, mutation: record.MutationID}] = record
			if revision := record.ExpectedRevision + 1; revision > d.revisions[taskID] {
				d.revisions[taskID] = revision
			}
			for _, envelope := range record.Outbox {
				key := noticeKey{task: envelope.Notice.TaskID, round: envelope.Notice.Round, kind: envelope.Notice.Kind, seq: envelope.Notice.Seq}
				if _, consumed := d.consumed[key]; consumed {
					continue
				}
				if _, rejected := d.rejected[key]; rejected {
					continue
				}
				d.owed[key] = task.OutboxNotice{Notice: envelope, Delivery: task.DeliveryPending}
			}
		}
	}
	return nil
}
