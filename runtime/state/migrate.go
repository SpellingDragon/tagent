package state

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/task"
)

const (
	// ArchiveSourcePrefix opens every identity this package derives from an old
	// archive: a migrated task or original carries its source in the identity
	// itself, so imported material stays marked and apart from the live identity
	// space whatever the archive holds.
	ArchiveSourcePrefix = "archive:"
	// ArchiveScope is the isolation scope the migrated addresses, instances and
	// reference metadata register under.
	ArchiveScope = "archive-import"
	// ArchiveOriginalFormat is the original format the migrated dumps register
	// under.
	ArchiveOriginalFormat = "archive-import-v1"
)

// ErrArchiveUnreadable reports an archive root whose checkpoint cannot be read
// or decoded, so neither an import nor a rollback can classify anything under
// it.
var ErrArchiveUnreadable = errors.New("state: archive checkpoint cannot be read")

// GapKind names why one archive item was refused by the migration.
type GapKind string

const (
	// GapMissingExternalFile names an external dump a checkpoint record points
	// at and the archive root does not hold: the byte source is gone, so the
	// record depending on it is refused beside the named dump.
	GapMissingExternalFile GapKind = "missing-external-file"
	// GapUnrecoverableTask names a task whose outcome or ownership the
	// checkpoint does not determine: an in-flight status, an unknown status
	// spelling or an owner the carrying agent does not match, each refused
	// rather than settled by guess.
	GapUnrecoverableTask GapKind = "unrecoverable-task"
)

// MigrationGap names one archive item the migration refused, with the kind of
// the refusal and its named cause, so nothing is skipped silently.
type MigrationGap struct {
	// Kind is the classification of the refusal.
	Kind GapKind
	// Name names the refused item: the relative dump path or the task identity.
	Name string
	// Detail carries the named cause of the refusal.
	Detail string
}

// MigratedTask is one task the migration stored in the new path: the
// deterministic identity its fact landed under, the checkpoint record it came
// from, the terminal status it carries and the mutation identity its fact is
// idempotent under.
type MigratedTask struct {
	// TaskID is the deterministic task identity the fact landed under.
	TaskID event.TaskID
	// Source names the checkpoint record the task was imported from.
	Source string
	// Status is the terminal status the fact carries.
	Status task.Status
	// MutationID is the mutation identity the fact is idempotent under.
	MutationID task.MutationID
}

// MigratedOriginal is one external dump the migration stored as an original:
// the key its bytes landed under, the archive file they came from, and the
// digest and length the stored bytes carry.
type MigratedOriginal struct {
	// Key is the original identity the bytes landed under.
	Key event.EventID
	// Source names the archive file the bytes were read from.
	Source string
	// Digest is the canonical digest text of the stored bytes.
	Digest string
	// Length is the byte length of the stored bytes.
	Length int64
}

// MigrationReport is the report one import returns: the digest of the archive
// root it read, every task and every original it stored in the new path, and
// every gap it refused by name.
type MigrationReport struct {
	// SourceDigest is the tree digest of the archive root the import read.
	SourceDigest string
	// Tasks lists every task fact the import stored.
	Tasks []MigratedTask
	// Originals lists every external original the import stored.
	Originals []MigratedOriginal
	// Gaps names every item the import refused.
	Gaps []MigrationGap
}

// RollbackItem is one migrated item's state in the new path as a rollback pass
// observes it: whether the new path still reads the item back, and whether that
// read-back matches what the archive carries.
type RollbackItem struct {
	// Name names the item: the task identity or the archive file it came from.
	Name string
	// Present reports whether the new path still reads the item back.
	Present bool
	// Verified reports whether the read-back matches the archive bytes or the
	// deterministic fact content; it is false whenever Present is false.
	Verified bool
}

// RollbackReport is the report one rollback returns: the digest of the archive
// observed under read-only access and the state of every migrated item in the
// new path, residue named as residue and absence named as absence.
type RollbackReport struct {
	// SourceDigest is the tree digest of the archive root the rollback read.
	SourceDigest string
	// Items accounts every migrated item the rollback looked for.
	Items []RollbackItem
}

// OriginalReader is the read face of the migrated originals: the bytes one key
// holds and whether the tier holds any under it. Its signature carries built-in
// and event types only, so the memory tier's archive face satisfies it
// structurally and no package edge is required in either direction.
//
// The redline list (3 rules):
// MUST return the bytes stored under key byte for byte.
// MUST report whether the tier holds any bytes under that key.
// MUST NOT answer an absent key as an empty original.
type OriginalReader interface {
	// GetOriginal returns the bytes stored under key and whether the tier holds
	// any under it.
	GetOriginal(ctx context.Context, key event.EventID) ([]byte, bool, error)
}

// OriginalMigrator is the write face beside that read face: the bytes one
// archive dump lands under, with the reference metadata the import marks it
// by.
//
// The redline list (3 rules):
// MUST store body under key beside the reference metadata the import names.
// MUST keep the reference metadata of one import effective after a later Put.
// MUST NOT expose a reference whose original bytes are not stored.
type OriginalMigrator interface {
	OriginalReader

	// PutOriginal stores body under key beside the reference metadata the
	// import names.
	PutOriginal(ctx context.Context, key event.EventID, owner event.Address, format, retention, digest string, length int64, body []byte) error
}

// ArchiveTaskWriter is the write face of the new path's task chain: the
// admission material one migrated task declares beside the fact it commits.
//
// The redline list (3 rules):
// MUST store the admission material one migrated task declares beside the fact it commits.
// MUST recognise a repeated migrated fact as the same fact, so a replayed restore records no second one.
// MUST NOT report a migrated fact as stored before its commit result is committed.
type ArchiveTaskWriter interface {
	// Declare stores the admission material of one migrated task.
	Declare(ctx context.Context, taskID event.TaskID, spec task.Spec) error
	// Commit stores the deterministic fact of one migrated task.
	Commit(ctx context.Context, record task.CommitRecord) (task.CommitResult, error)
}

// ArchiveTaskReader is the read face of the new path's task chain: the tasks it
// holds facts for and the fact chain of one of them.
//
// The redline list (3 rules):
// MUST return every task the chain holds facts for.
// MUST report the fact chain of one task in its recorded order.
// MUST NOT answer a failed read as an empty chain.
type ArchiveTaskReader interface {
	// TaskIDs returns every task the chain holds facts for.
	TaskIDs() []event.TaskID
	// Records returns the fact chain of one task.
	Records(ctx context.Context, taskID event.TaskID) ([]task.TaskRecord, error)
}

// MigrateArchive imports one old archive into the new path under read-only
// access to the archive: the checkpoint and its external dumps are read, every
// task whose outcome and ownership the checkpoint determines becomes one
// deterministic terminal fact on the task chain, the external dumps become
// originals of the tier, and nothing is written under the archive root. Every
// identity the import lands carries the archive source marker, so migrated
// facts and originals stay marked and apart from the live chain whatever the
// archive holds.
//
// The import is idempotent and rerunnable: every identity and every record it
// writes derives from the archive content alone, so an interrupted run reruns
// to the report an uninterrupted run returns. A missing external dump and a
// task the checkpoint cannot determine are named in the report gaps and
// imported nowhere, so a refusal stays visible and a rerun repeats it verbatim.
//
// 契约: docs/wiki/reliability/durable-delivery.md#canonical-fact-resolution
func MigrateArchive(ctx context.Context, root string, tasks ArchiveTaskWriter, originals OriginalMigrator) (MigrationReport, error) {
	report := MigrationReport{}
	digest, err := ArchiveDigest(root)
	if err != nil {
		return MigrationReport{}, err
	}
	report.SourceDigest = digest
	items, gaps, err := planArchive(root)
	if err != nil {
		return MigrationReport{}, err
	}
	report.Gaps = gaps
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return MigrationReport{}, err
		}
		rec := item.record
		for _, dump := range item.dumps {
			key, digest := originalIdentity(dump.body)
			owner := event.Address{AgentID: event.AgentID(rec.agent), SessionID: event.SessionID(ArchiveScope)}
			if err := originals.PutOriginal(ctx, key, owner, ArchiveOriginalFormat, ArchiveScope, digest, int64(len(dump.body)), dump.body); err != nil {
				return MigrationReport{}, err
			}
			report.Originals = append(report.Originals, MigratedOriginal{Key: key, Source: dump.ticket, Digest: digest, Length: int64(len(dump.body))})
		}
		if err := tasks.Declare(ctx, rec.taskID(), rec.spec()); err != nil {
			return MigrationReport{}, err
		}
		result, err := tasks.Commit(ctx, task.CommitRecord{
			MutationID:       rec.mutationID(),
			TaskID:           rec.taskID(),
			ExpectedRevision: 0,
			Status:           item.status,
		})
		if err != nil {
			return MigrationReport{}, err
		}
		if result != task.CommitCommitted {
			return MigrationReport{}, fmt.Errorf("state: archive import of %s reported commit result %d", rec.name(), result)
		}
		report.Tasks = append(report.Tasks, MigratedTask{
			TaskID:     rec.taskID(),
			Source:     rec.name(),
			Status:     item.status,
			MutationID: rec.mutationID(),
		})
	}
	return report, nil
}

// RollbackArchive reads one archive and its migrated items back under read-only
// access to both sides: the archive bytes are read, the deterministic
// identities they migrate under are looked up on the read faces of the new
// path, and every lookup is accounted in the report beside its outcome. The
// rollback holds no write face at all, so it records nothing, rewrites nothing
// and removes nothing: an item the new path still holds is reported as residue,
// and one the new path holds nothing under is named absent rather than written
// back.
//
// 契约: docs/wiki/reliability/durable-delivery.md#canonical-fact-resolution
func RollbackArchive(ctx context.Context, root string, tasks ArchiveTaskReader, originals OriginalReader) (RollbackReport, error) {
	report := RollbackReport{}
	digest, err := ArchiveDigest(root)
	if err != nil {
		return RollbackReport{}, err
	}
	report.SourceDigest = digest
	items, _, err := planArchive(root)
	if err != nil {
		return RollbackReport{}, err
	}
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return RollbackReport{}, err
		}
		rec := item.record
		chain, err := tasks.Records(ctx, rec.taskID())
		if err != nil {
			return RollbackReport{}, err
		}
		entry := RollbackItem{Name: rec.name()}
		for _, link := range chain {
			if link.Fact.MutationID == rec.mutationID() {
				entry.Present = true
				entry.Verified = link.Fact.Status == item.status && link.Fact.ExpectedRevision == 0
			}
		}
		report.Items = append(report.Items, entry)
		for _, dump := range item.dumps {
			key, _ := originalIdentity(dump.body)
			stored, found, err := originals.GetOriginal(ctx, key)
			if err != nil {
				return RollbackReport{}, err
			}
			report.Items = append(report.Items, RollbackItem{
				Name:     dump.ticket,
				Present:  found,
				Verified: found && bytes.Equal(stored, dump.body),
			})
		}
	}
	return report, nil
}

// ArchiveDigest returns the tree digest of every regular file under root: one
// canonical digest text over each file's relative path, byte length and content
// digest, so any added, removed or altered byte changes the digest and a
// read-only pass over the tree can prove the source held still.
func ArchiveDigest(root string) (string, error) {
	h := sha256.New()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !entry.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(body)
		fmt.Fprintf(h, "%s\x00%d\x00%s\n", filepath.ToSlash(rel), len(body), hex.EncodeToString(sum[:]))
		return nil
	})
	if err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

// archiveCheckpointName is the checkpoint file the old program writes under an
// archive root.
const archiveCheckpointName = "checkpoint.json"

// archiveSnapshot mirrors the shape the old program's checkpoint carries: agent
// definitions beside the task records each agent holds. The import reads the
// records it migrates and ignores the rest of the checkpoint, which stays
// byte-identical under the archive root.
type archiveSnapshot struct {
	Agents []archiveAgent
}

// archiveAgent is the checkpoint record of one agent: its definition beside its
// task records.
type archiveAgent struct {
	Definition archiveDefinition
	Tasks      []archiveTask
}

// archiveDefinition is the agent definition part of a checkpoint record.
type archiveDefinition struct {
	ID string
}

// archiveTask is one old task record: its per-agent number, the owner and the
// requester it carries, the call it answers, the status it settled at and the
// output and error text beside it.
type archiveTask struct {
	ID      int
	OwnerID string
	Source  string
	Call    archiveCall
	Status  string
	Output  string
	Error   string
}

// archiveCall is the tool call one old task record answers.
type archiveCall struct {
	Name string
	Args string
}

// archiveTicketPattern matches an external dump ticket: the dump directory the
// old program writes its oversized results under, beside the file name it
// names. The file name carries no separator, so a ticket resolves under the
// archive root and never escapes it.
var archiveTicketPattern = regexp.MustCompile(`tool-output/[A-Za-z0-9._-]+`)

// archiveRecord is one checkpoint task record paired with the identity of the
// agent that carries it.
type archiveRecord struct {
	agent string
	task  archiveTask
}

// name returns the archive identity of the record: the carrying agent beside
// the per-agent task number.
func (r archiveRecord) name() string {
	return r.agent + "#" + strconv.Itoa(r.task.ID)
}

// taskID returns the deterministic task identity the record migrates under.
func (r archiveRecord) taskID() event.TaskID {
	return event.TaskID(ArchiveSourcePrefix + r.name())
}

// mutationID returns the deterministic mutation identity the record's fact is
// idempotent under.
func (r archiveRecord) mutationID() task.MutationID {
	return task.MutationID("settle:" + string(r.taskID()))
}

// spec returns the admission material the record migrates under.
func (r archiveRecord) spec() task.Spec {
	return task.Spec{
		CallID:       r.task.Call.Name,
		DedupKey:     task.DedupKey(r.taskID()),
		InvocationID: event.InvocationID(ArchiveScope),
		InstanceID:   event.InstanceID(ArchiveScope),
		Description:  "archived task " + r.name() + " (" + r.task.Call.Name + ")",
		Source:       event.Address{AgentID: event.AgentID(r.task.Source), SessionID: event.SessionID(ArchiveScope)},
		Target:       event.Address{AgentID: event.AgentID(r.agent), SessionID: event.SessionID(ArchiveScope)},
		Execution:    task.Serialization(ArchiveScope),
	}
}

// terminal returns the terminal status the record settles at and whether the
// checkpoint determines one at all.
func (r archiveRecord) terminal() (task.Status, bool) {
	switch r.task.Status {
	case "completed":
		return task.StatusSettled, true
	case "failed":
		return task.StatusFailed, true
	}
	return task.StatusUnknown, false
}

// owned reports whether the carrying agent owns the record.
func (r archiveRecord) owned() bool {
	return r.task.OwnerID != "" && r.task.OwnerID == r.agent
}

// dumps returns the external dump tickets the record references, without
// duplicates and in sorted order.
func (r archiveRecord) dumps() []string {
	seen := map[string]bool{}
	var out []string
	for _, ticket := range archiveTicketPattern.FindAllString(r.task.Output+" "+r.task.Error, -1) {
		if seen[ticket] {
			continue
		}
		seen[ticket] = true
		out = append(out, ticket)
	}
	sort.Strings(out)
	return out
}

// plannedDump is one external dump an importable record depends on: the ticket
// that names it beside the bytes the archive root holds under it.
type plannedDump struct {
	ticket string
	body   []byte
}

// plannedItem is one importable checkpoint record beside the terminal status
// and the dump bytes its migration lands.
type plannedItem struct {
	record archiveRecord
	status task.Status
	dumps  []plannedDump
}

// planArchive classifies every task record of the checkpoint under root into
// the items a migration would land beside the gaps it refuses by name, so an
// import and a rollback classify one archive the same way.
func planArchive(root string) ([]plannedItem, []MigrationGap, error) {
	records, err := readArchiveTasks(root)
	if err != nil {
		return nil, nil, err
	}
	sort.Slice(records, func(i, j int) bool { return records[i].name() < records[j].name() })
	var items []plannedItem
	var gaps []MigrationGap
	seen := map[event.TaskID]string{}
	for _, rec := range records {
		if prior, duplicate := seen[rec.taskID()]; duplicate {
			return nil, nil, fmt.Errorf("state: archive carries records %s and %s under one identity", prior, rec.name())
		}
		seen[rec.taskID()] = rec.name()
		status, terminal := rec.terminal()
		refused := false
		if !rec.owned() {
			gaps = append(gaps, MigrationGap{
				Kind:   GapUnrecoverableTask,
				Name:   rec.name(),
				Detail: "owner " + rec.task.OwnerID + " does not match the carrying agent " + rec.agent,
			})
			refused = true
		}
		if !terminal {
			gaps = append(gaps, MigrationGap{
				Kind:   GapUnrecoverableTask,
				Name:   rec.name(),
				Detail: "status " + rec.task.Status + " carries no terminal outcome",
			})
			refused = true
		}
		item := plannedItem{record: rec, status: status}
		for _, ticket := range rec.dumps() {
			body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(ticket)))
			if err != nil {
				gaps = append(gaps, MigrationGap{
					Kind:   GapMissingExternalFile,
					Name:   ticket,
					Detail: "referenced by " + rec.name(),
				})
				refused = true
				continue
			}
			item.dumps = append(item.dumps, plannedDump{ticket: ticket, body: body})
		}
		if refused {
			continue
		}
		items = append(items, item)
	}
	sort.Slice(gaps, func(i, j int) bool {
		if gaps[i].Kind != gaps[j].Kind {
			return gaps[i].Kind < gaps[j].Kind
		}
		if gaps[i].Name != gaps[j].Name {
			return gaps[i].Name < gaps[j].Name
		}
		return gaps[i].Detail < gaps[j].Detail
	})
	return items, gaps, nil
}

// readArchiveTasks returns every task record the checkpoint under root
// carries, each paired with the identity of its carrying agent.
func readArchiveTasks(root string) ([]archiveRecord, error) {
	snapshot, err := readArchiveCheckpoint(root)
	if err != nil {
		return nil, err
	}
	var out []archiveRecord
	for _, agent := range snapshot.Agents {
		for _, t := range agent.Tasks {
			out = append(out, archiveRecord{agent: agent.Definition.ID, task: t})
		}
	}
	return out, nil
}

// readArchiveCheckpoint reads and decodes the checkpoint file under root.
func readArchiveCheckpoint(root string) (archiveSnapshot, error) {
	path := filepath.Join(root, archiveCheckpointName)
	body, err := os.ReadFile(path)
	if err != nil {
		return archiveSnapshot{}, fmt.Errorf("%w: %s", ErrArchiveUnreadable, path)
	}
	var snapshot archiveSnapshot
	if err := json.Unmarshal(body, &snapshot); err != nil {
		return archiveSnapshot{}, fmt.Errorf("%w: %s", ErrArchiveUnreadable, path)
	}
	return snapshot, nil
}

// originalIdentity returns the deterministic original identity and the
// canonical digest text of one archive dump's bytes.
func originalIdentity(body []byte) (event.EventID, string) {
	sum := sha256.Sum256(body)
	text := hex.EncodeToString(sum[:])
	return event.EventID(ArchiveSourcePrefix + text), "sha256:" + text
}
