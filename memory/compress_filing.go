// 契约: docs/FACT-VOCABULARY.md#families
//
// Consumer wiring for the compress request family: the producer and the reason
// the meditation bound states when it judges material due for compression, and
// the in-process log a filed request waits in until its consumer retires it.
//
// The fact itself — who judged, why, what was judged, and the identity the
// judgment is quoted by — is event vocabulary (event.CompressRequest). What
// stands here is the memory side of that wiring: the judgment label this package
// contributes and the log that holds it.
//
// 契约: docs/wiki/agent/compression-and-telemetry.md#output-overflow
package memory

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	trpclog "trpc.group/trpc-go/trpc-agent-go/log"

	"github.com/SpellingDragon/tagent/api/event"
)

// CompressProducerMeditation names the mechanism that judges a card sequence
// against the meditation bound, and CompressReasonMeditation states why that
// judgment files a request. The producer and the reason are part of the request
// identity, so the same material judged by two mechanisms stays two facts.
const (
	// CompressProducerMeditation names the meditation bound judgment.
	CompressProducerMeditation = "memory.meditation"
	// CompressReasonMeditation states why the meditation bound judgment files a
	// request.
	CompressReasonMeditation = "the card sequence exceeds the meditation bound"
)

// CompressRequestFiling files one compress request into the log its consumer
// reads and answers the request as it stands on record, so a repeated judgment
// keeps the identity and the filing moment of the first one. A filing that fails
// is reported by name and a judgment is never dropped silently.
type CompressRequestFiling func(ctx context.Context, request event.CompressRequest) (event.CompressRequest, error)

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
	filed   []event.CompressRequest
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
func (l *CompressRequests) Produce(ctx context.Context, request event.CompressRequest) (event.CompressRequest, error) {
	if err := ctx.Err(); err != nil {
		return event.CompressRequest{}, err
	}
	if request.Producer == "" || request.Subject == "" || request.Reason == "" {
		filingDebug("tagent: compress request judged result=refused producer=%q subject=%q reason=%q",
			request.Producer, request.Subject, request.Reason)
		return event.CompressRequest{}, ErrCompressRequestUnnamed
	}
	if request.ID == "" {
		request.ID = event.CompressRequestFor(request.Producer, request.Subject, request.Reason).ID
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, filed := range l.filed {
		if filed.ID == request.ID {
			filingDebug("tagent: compress request judged result=recorded producer=%s subject=%s reason=%s id=%s pending=%d",
				filed.Producer, filed.Subject, filed.Reason, filed.ID, len(l.pending))
			return filed, nil
		}
	}
	if request.At.IsZero() {
		request.At = time.Now().UTC()
	}
	l.filed = append(l.filed, request)
	l.pending[request.ID] = true
	filingDebug("tagent: compress request judged result=filed producer=%s subject=%s reason=%s id=%s pending=%d",
		request.Producer, request.Subject, request.Reason, request.ID, len(l.pending))
	return request, nil
}

// Pending lists the requests awaiting consumption, in filing order, exactly as
// they stand on record.
func (l *CompressRequests) Pending(ctx context.Context) ([]event.CompressRequest, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]event.CompressRequest, 0, len(l.pending))
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
		filingDebug("tagent: compress request retired result=refused id=%s pending=%d", key, len(l.pending))
		return fmt.Errorf("%w: %q", ErrCompressRequestNotPending, key)
	}
	delete(l.pending, key)
	filingDebug("tagent: compress request retired result=retired id=%s pending=%d", key, len(l.pending))
	return nil
}

// filingDebug renders one line of the compress request family at DEBUG: who
// judged, why, which request identity the judgment is recorded under, whether a
// new request was filed or the record kept, and how many requests still await
// consumption. The producer, the subject and the reason are labels the
// vocabulary fixes, so one line stays bounded whatever the judged material
// holds.
//
// The redline list (3 rules):
// MUST stay a bypass: a failing logger is a missing line, never a failed filing.
// MUST keep one line per judgment, per retirement and per refusal, at DEBUG.
// MUST NOT render the judged material or any card body behind the subject.
func filingDebug(format string, args ...any) {
	if trpclog.Default == nil {
		return
	}
	defer func() { _ = recover() }()
	trpclog.Default.Debugf(format, args...)
}
