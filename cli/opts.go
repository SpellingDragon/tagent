package cli

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
