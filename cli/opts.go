package cli

import (
	"net/http"

	tagent "github.com/SpellingDragon/tagent"
)

// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
//
// CLIOption configures the host shell at construction.
type CLIOption func(*CLI)

// WithRequestCredential configures the request credential the host HTTP face
// authenticates every request subject with. The credential is compared in
// constant time and never logged or echoed back.
func WithRequestCredential(credential string) CLIOption {
	return func(c *CLI) { c.credential = credential }
}

// WithFeedbackEndpoint mounts the RL feedback face at POST /feedback behind the
// single authentication gate of the host face. The endpoint is reached only for an
// authenticated request subject, and it rechecks the same credential at its own
// point, so a feedback submission has no unauthenticated path into the runtime.
func WithFeedbackEndpoint(endpoint http.Handler) CLIOption {
	return func(c *CLI) { c.feedback = endpoint }
}

// WithDiagnostics configures the diagnostic switches the host serves its
// optional faces with: the unified runtime map keeps its own route, and each
// switch opens the reflection, training or observation face beside it. The
// start command reads the same switches from the process environment.
func WithDiagnostics(switches tagent.DiagnosticSwitches) CLIOption {
	return func(c *CLI) { c.diagnostics = switches }
}
