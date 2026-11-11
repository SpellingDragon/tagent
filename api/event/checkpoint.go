// 契约: docs/FACT-VOCABULARY.md#families
//
// The framework checkpoint family: one fact carrying every recoverable state
// slice of the runtime, stated at one partition-sequence cut. A checkpoint is
// the framework's own fact and never material of a session chain, so it names
// no message the model reads and carries the state of every consumer that
// delegated its state to the framework beside the offset that state corresponds
// to. One family, one offset semantics: the recovery of any consumer is "read
// the newest checkpoint, restore the slices, replay the facts past the offset".
package event

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// TypeCheckpoint is the frozen type of a framework checkpoint fact.
const TypeCheckpoint = "checkpoint"

// CheckpointVersion is the payload layout version of a checkpoint fact: a
// payload of another version is refused by name, because restoring state under a
// layout the reader does not know would invent the slices it cannot read.
const CheckpointVersion = 1

// CheckpointOffset is the partition-sequence watermark one checkpoint covers:
// for every partition, the sequence slot the partition had committed when the
// cut was taken. It is the partition sequence the landing path numbers commits
// with, never a process-local counter: a process-local number is neither
// monotonic across restarts nor comparable between partitions, so it can never
// stand for "everything up to here is covered".
type CheckpointOffset map[int]int64

// CheckpointSlice is the recoverable state one registered consumer delegated to
// the framework: the consumer name it is keyed by beside the consumer's own
// payload. The payload is the consumer's business — the fact family carries the
// name and the bytes, never a structure a reader of the fact could interpret as
// state of its own.
type CheckpointSlice struct {
	// Name is the consumer name the slice is keyed by.
	Name string `json:"name"`
	// Payload is the consumer's own recoverable state, as JSON.
	Payload json.RawMessage `json:"payload"`
}

// Checkpoint is one framework checkpoint fact: the content-addressed identity,
// the cut it states and the state slices restored from it. The writing moment is
// the commit stamp of the fact, so it is not stated twice.
type Checkpoint struct {
	// Key is the fact key the checkpoint commits under: the partition-sequence
	// slot it was taken at, so the key of every checkpoint of a partition is
	// monotone past the key of the one before it and no two cuts share one.
	Key string
	// ID is the content-addressed identity of the checkpoint: two cuts stating
	// the same state answer the same identity.
	ID EventID
	// Offset is the partition-sequence watermark the state corresponds to.
	Offset CheckpointOffset
	// Slices is the state of every registered consumer, in registration order.
	Slices []CheckpointSlice
}

// CheckpointKey is the fact key one checkpoint is filed under: the frozen
// `checkpoint:<partitionSeq>` spelling, whose sequence slot is the partition
// sequence the cut advanced to.
func CheckpointKey(seq int64) string { return checkpointKeyPrefix + strconv.FormatInt(seq, 10) }

// checkpointKeyPrefix marks a checkpoint fact key: the family is read back by
// the frozen event type and never by a key's spelling.
const checkpointKeyPrefix = "checkpoint:"

// CheckpointPayload is the payload layout of one checkpoint fact: the layout
// version, the identity the payload content-addresses to, the cut and the state
// slices — Projection, Delivery, Mailbox and Tasks — in registration order.
type CheckpointPayload struct {
	Version int               `json:"version"`
	ID      EventID           `json:"id"`
	Offset  CheckpointOffset  `json:"offset"`
	Slices  []CheckpointSlice `json:"slices"`
}

// CheckpointFor returns the checkpoint fact for one cut: the identity is
// content-addressed over the layout version, the offset and the slices, so a
// cut that states the very state a filed checkpoint already states keeps one
// identity instead of stating a second copy of it.
func CheckpointFor(offset CheckpointOffset, slices []CheckpointSlice) (Checkpoint, error) {
	body, err := json.Marshal(CheckpointPayload{Version: CheckpointVersion, Offset: offset, Slices: slices})
	if err != nil {
		return Checkpoint{}, fmt.Errorf("event: render the checkpoint payload: %w", err)
	}
	sum := sha256.Sum256(body)
	return Checkpoint{
		ID:     EventID("ckp-" + hex.EncodeToString(sum[:8])),
		Offset: offset,
		Slices: slices,
	}, nil
}

// CheckpointPayloadOf renders the canonical payload bytes of one checkpoint: the
// cut, the identity and the slices exactly as the identity was content-addressed
// over them.
func CheckpointPayloadOf(c Checkpoint) ([]byte, error) {
	return json.Marshal(CheckpointPayload{Version: CheckpointVersion, ID: c.ID, Offset: c.Offset, Slices: c.Slices})
}

// CheckpointOf reads one committed fact back as the checkpoint it carries, and
// reports whether the fact states one. A fact of another frozen type, a payload
// of another layout version, and a payload whose stated offset and slices do not
// content-address to the identity it states all answer no checkpoint: state is
// restored from the one payload that states it, and a payload that states other
// state under that identity is refused rather than restored as if it were its
// own.
func CheckpointOf(evt Event) (Checkpoint, bool) {
	if evt.Type != TypeCheckpoint {
		return Checkpoint{}, false
	}
	var wire CheckpointPayload
	if err := json.Unmarshal([]byte(evt.Message.Content), &wire); err != nil {
		return Checkpoint{}, false
	}
	if wire.Version != CheckpointVersion {
		return Checkpoint{}, false
	}
	stated, err := CheckpointFor(wire.Offset, wire.Slices)
	if err != nil || stated.ID != wire.ID {
		return Checkpoint{}, false
	}
	stated.Key = string(evt.Key)
	return stated, true
}

// FactKey returns the checkpoint's identity as a fact key: the partition
// sequence slot the checkpoint commits under.
//
// 契约: docs/FACT-VOCABULARY.md#families
func (c Checkpoint) FactKey() string { return c.Key }

// FactType returns the frozen type a checkpoint is read as.
func (c Checkpoint) FactType() string { return TypeCheckpoint }

// CheckpointSliceNames reports the names of the slices one checkpoint carries,
// sorted, so a reader that only needs to know what a checkpoint covers answers
// without decoding the state of every consumer.
func (c Checkpoint) CheckpointSliceNames() []string {
	names := make([]string, 0, len(c.Slices))
	for _, slice := range c.Slices {
		names = append(names, slice.Name)
	}
	sort.Strings(names)
	return names
}

// MetaEntries states the two metadata entries one checkpoint fact carries from
// the cut it was taken at: the partition-sequence watermark and the slice names,
// both sorted so the fact renders the same text for the same coverage.
func (c Checkpoint) MetaEntries() map[string]string {
	return map[string]string{
		MetaKeyCheckpointOffset: c.Offset.String(),
		MetaKeyCheckpointSlices: strings.Join(c.CheckpointSliceNames(), checkpointNameSeparator),
	}
}

// String renders one watermark as the sorted partition:seq pairs.
func (o CheckpointOffset) String() string {
	partitions := make([]int, 0, len(o))
	for partition := range o {
		partitions = append(partitions, partition)
	}
	sort.Ints(partitions)
	parts := make([]string, 0, len(partitions))
	for _, partition := range partitions {
		parts = append(parts, fmt.Sprintf("%d:%d", partition, o[partition]))
	}
	return strings.Join(parts, checkpointPairSeparator)
}

// parseCheckpointOffset reads one rendered watermark back: the sorted
// partition:seq pairs the metadata entry states.
func parseCheckpointOffset(rendered string) (CheckpointOffset, bool) {
	if rendered == "" {
		return CheckpointOffset{}, false
	}
	offset := CheckpointOffset{}
	for _, pair := range strings.Split(rendered, checkpointPairSeparator) {
		partition, seq, found := strings.Cut(pair, ":")
		if !found {
			return nil, false
		}
		at, err := strconv.Atoi(partition)
		if err != nil {
			return nil, false
		}
		value, err := strconv.ParseInt(seq, 10, 64)
		if err != nil {
			return nil, false
		}
		offset[at] = value
	}
	return offset, true
}

// checkpointPairSeparator and checkpointNameSeparator separate the entries of
// the two rendered metadata fields: neither a partition number nor a consumer
// name carries either, so both lists read back in their own order.
const (
	checkpointPairSeparator = ","
	checkpointNameSeparator = ","
)

// A framework checkpoint is a fact.
var _ Fact = Checkpoint{}
