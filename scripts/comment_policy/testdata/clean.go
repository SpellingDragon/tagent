// Package clean is the compliance fixture for the comment policy: every
// comment below states a contract, and the index points at a live document.
package clean

// Widget renders one widget.
//
// - The zero value is not usable; call New before Use.
// - Use is safe for concurrent callers and returns ErrClosed after Close has been called.
//
// 契约: docs/wiki/README.md
type Widget struct {
	// size is measured in bytes; 0 means unbounded.
	size int
}

// New returns a Widget with the given size in bytes, or nil when size is negative.
func New(size int) *Widget { return &Widget{size: size} }

// Use applies f to the widget. f must not retain the widget after Use returns.
func (w *Widget) Use(f func(int)) {}

// ErrClosed reports a use after Close.
var ErrClosed = err{}

type err struct{}

func (err) Error() string { return "closed" }
