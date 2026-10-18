// 契约: docs/wiki/memory/memory-architecture.md#read-paths
//
// The offline binding seam of vector generation for recall: an embedder is
// bound per generation and borrowed by the async pipeline that runs after the
// event is already stored, so the event path never waits for a vector and a
// runtime without an embedder configured behaves exactly as it did before the
// seam existed. The real embedding endpoint and the recall quality run belong
// to the resource-gated acceptance work; this file only carries the seam.
package memory

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/SpellingDragon/tagent/event"
)

// DefaultVectorQueue is the queue bound a vector seam runs under when its
// caller names none: every seam is bounded, so a slow embedder can never grow
// the queue without limit.
const DefaultVectorQueue = 64

// Embedder generates the embedding vector of one text under the binding of the
// generation that borrowed it. A seam never owns the endpoint: it holds the
// binding and calls through it, and a generation's jobs run against the binding
// that was current when the job was submitted, never against one swapped in
// later.
//
// The redline list (3 rules):
// MUST generate the vector under the binding of the generation that borrowed it.
// MUST report a named failure rather than a zero vector.
// MUST NOT own an endpoint or run a job against a binding swapped in after the job was submitted.
type Embedder interface {
	// Embed returns the embedding vector of text, or a named failure.
	Embed(ctx context.Context, text string) ([]float32, error)
}

// vectorJob is one queued vector generation: the event key it is for, the text
// to embed, and the generation binding borrowed at submit time.
type vectorJob struct {
	key     event.EventID
	text    string
	binding Embedder
}

// VectorSeam is the async vector generation pipeline beside the event store:
// submitting an event never waits for its vector, the vector becomes ready
// later under the binding that generation borrowed, and an unconfigured seam
// is a no-op rather than a degraded behaviour.
//
// The seam holds vectors only. It never touches event originals, tool
// declarations or any governing counter, so vector generation cannot erode
// what it runs beside.
type VectorSeam struct {
	mu       sync.Mutex
	binding  Embedder
	queue    chan vectorJob
	dropped  atomic.Int64
	vectors  map[event.EventID][]float32
	done     chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

// NewVectorSeam returns a seam with the given queue bound (a non-positive
// bound runs under DefaultVectorQueue) and no embedder bound: an unconfigured
// seam accepts nothing and changes nothing.
func NewVectorSeam(queueSize int) *VectorSeam {
	if queueSize <= 0 {
		queueSize = DefaultVectorQueue
	}
	seam := &VectorSeam{
		queue:   make(chan vectorJob, queueSize),
		vectors: map[event.EventID][]float32{},
		done:    make(chan struct{}),
	}
	seam.wg.Add(1)
	go seam.run()
	return seam
}

// Bind installs the embedder binding of the current generation, or clears it
// when nil: jobs already submitted keep the binding they borrowed, so a later
// generation can never rewrite the terms an earlier one runs under.
func (s *VectorSeam) Bind(embedder Embedder) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.binding = embedder
}

// Submit offers one event for async vector generation and returns whether the job was accepted.
//
// - An unconfigured seam (no embedder bound) accepts nothing and moves nothing: no job is queued, no embedder is called and the drop counter stays where it is, so a runtime without embeddings behaves exactly as it does without the seam.
// - A full queue drops the job by name and moves the drop counter, and the event itself is stored regardless: submission is non-blocking by contract, never a back-pressure wait on the store path.
// - A job whose generation later fails is dropped the same observable way, so a lost vector is always counted and never silently absent.
func (s *VectorSeam) Submit(key event.EventID, text string) bool {
	s.mu.Lock()
	binding := s.binding
	s.mu.Unlock()
	if binding == nil {
		return false
	}
	job := vectorJob{key: key, text: text, binding: binding}
	select {
	case s.queue <- job:
		return true
	default:
		s.dropped.Add(1)
		return false
	}
}

// Dropped returns how many event vectors were dropped so far: refused by a
// full queue or lost to a failed generation. A negative result is recorded,
// not hidden.
func (s *VectorSeam) Dropped() int64 {
	return s.dropped.Load()
}

// Vector returns the embedding vector generated for key and whether it is
// ready. A vector is never served before its generation completed, and its
// absence is reported as absence rather than as an error.
func (s *VectorSeam) Vector(key event.EventID) ([]float32, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	vector, ready := s.vectors[key]
	if !ready {
		return nil, false
	}
	return append([]float32(nil), vector...), true
}

// Close ends the pipeline: no further submission is accepted and the worker
// returns. It repeats as a no-op.
func (s *VectorSeam) Close() {
	s.stopOnce.Do(func() {
		close(s.done)
		s.wg.Wait()
	})
}

// run consumes the queue under the binding each job borrowed, so the pipeline
// serves every generation on its own terms.
func (s *VectorSeam) run() {
	defer s.wg.Done()
	for {
		select {
		case <-s.done:
			return
		case job := <-s.queue:
			vector, err := job.binding.Embed(context.Background(), job.text)
			if err != nil {
				s.dropped.Add(1)
				continue
			}
			s.mu.Lock()
			s.vectors[job.key] = append([]float32(nil), vector...)
			s.mu.Unlock()
		}
	}
}
