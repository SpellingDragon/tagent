// 契约: docs/FACT-VOCABULARY.md#families
//
// The compress request family: the judgment that material is due for
// compression, stated as the fact it is — who judged, why, what was judged and
// the identity the judgment is quoted by — and never as the instruction that
// performs the compression. A producer files the request it judged, a consumer
// reads it, compresses the material the request names and files the product it
// minted. A repeated judgment keeps the identity and the first filing moment of
// the request already on record, so one body of material carries one request
// however often it is judged.
package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/event"
)

// CompressRequestType is the frozen type of a compress request fact.
const CompressRequestType = "compress.request"

// CompressProducerCuration names the mechanism that judges a card sequence
// against the curation bound, and CompressReasonCuration states why that
// judgment files a request. The producer and the reason are part of the request
// identity, so the same material judged by two mechanisms stays two facts.
const (
	// CompressProducerCuration names the curation bound judgment.
	CompressProducerCuration = "memory.curation"
	// CompressReasonCuration states why the curation bound judgment files a
	// request.
	CompressReasonCuration = "the card sequence exceeds the curation bound"
)

// CompressRequest is one compress request fact: the judgment that material is
// over its bound. It names who judged, why, what was judged and the identity the
// request is quoted by, and it is immutable once filed — the same judgment files
// the same identity, and a retry never refiles it under a second one.
type CompressRequest struct {
	// ID is the identity of the judgment: the content-addressed key the request
	// is quoted by, derived from the producer, the subject and the reason.
	ID event.EventID
	// Producer names the mechanism that judged the material due for compression.
	Producer string
	// Reason states why that mechanism judged it due.
	Reason string
	// Subject is the identity of the material judged: the content-addressed key
	// of the sequence or result the request names.
	Subject string
	// At is the moment the judgment was filed.
	At time.Time
}

// CompressRequestFor returns the request fact one judgment files: the identity is
// content-addressed over the producer, the subject and the reason, so the same
// judgment always answers the same identity and a re-judgment adds no second
// request. The filing moment stays zero for the log to stamp on first filing.
func CompressRequestFor(producer, subject, reason string) CompressRequest {
	sum := sha256.Sum256([]byte(producer + "\x00" + subject + "\x00" + reason))
	return CompressRequest{
		ID:       event.EventID("cmp-" + hex.EncodeToString(sum[:8])),
		Producer: producer,
		Reason:   reason,
		Subject:  subject,
	}
}

// CompressRequestFiling files one compress request into the log its consumer
// reads and answers the request as it stands on record, so a repeated judgment
// keeps the identity and the filing moment of the first one. A filing that fails
// is reported by name and a judgment is never dropped silently.
type CompressRequestFiling func(ctx context.Context, request CompressRequest) (CompressRequest, error)

var (
	// ErrCompressRequestUnnamed reports a judgment that names no producer, no
	// subject or no reason: no identity is derived from it and no consumer could
	// tell what it asks for.
	ErrCompressRequestUnnamed = errors.New("memory: a compress request names no producer, subject or reason")
	// ErrCompressRequestNotPending reports a retirement of a request that does not
	// await consumption: a repeated retirement never retires a second one.
	ErrCompressRequestNotPending = errors.New("memory: no compress request awaits consumption under that identity")
)

// CompressRequests is the in-process log of compress request facts: every request
// filed, the order it was filed in and the ones still awaiting consumption. The
// log is the identity authority of the family — a filed request is answered as it
// stands on record, so one judgment keeps its identity and first filing moment —
// and a request leaves the pending set exactly once, when its consumer retires it
// after the products of its compression are committed.
type CompressRequests struct {
	mu      sync.Mutex
	filed   []CompressRequest
	pending map[event.EventID]bool
}

// NewCompressRequests returns an empty request log.
func NewCompressRequests() *CompressRequests {
	return &CompressRequests{pending: map[event.EventID]bool{}}
}

// Produce files one compress request and answers it as it stands on record: a
// judgment already filed is answered with the recorded request, so its identity
// and filing moment never move and no second entry is added. A request naming no
// producer, subject or reason is refused by name, and the filing moment of a
// request that carries none is stamped on the first filing.
func (l *CompressRequests) Produce(ctx context.Context, request CompressRequest) (CompressRequest, error) {
	if err := ctx.Err(); err != nil {
		return CompressRequest{}, err
	}
	if request.Producer == "" || request.Subject == "" || request.Reason == "" {
		return CompressRequest{}, ErrCompressRequestUnnamed
	}
	if request.ID == "" {
		request.ID = CompressRequestFor(request.Producer, request.Subject, request.Reason).ID
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, filed := range l.filed {
		if filed.ID == request.ID {
			return filed, nil
		}
	}
	if request.At.IsZero() {
		request.At = time.Now().UTC()
	}
	l.filed = append(l.filed, request)
	l.pending[request.ID] = true
	return request, nil
}

// Pending lists the requests awaiting consumption, in filing order, exactly as
// they stand on record.
func (l *CompressRequests) Pending(ctx context.Context) ([]CompressRequest, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]CompressRequest, 0, len(l.pending))
	for _, filed := range l.filed {
		if l.pending[filed.ID] {
			out = append(out, filed)
		}
	}
	return out, nil
}

// Consume retires one request once the products of its compression are
// committed. A request that does not await consumption is refused by name, so a
// retirement is never counted twice.
func (l *CompressRequests) Consume(ctx context.Context, key event.EventID) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.pending[key] {
		return fmt.Errorf("%w: %q", ErrCompressRequestNotPending, key)
	}
	delete(l.pending, key)
	return nil
}
