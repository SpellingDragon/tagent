// 契约: docs/wiki/agent/execution-generations.md#reentry-resolution
package task

import "context"

// CommandRelauncher starts a command through the initiating execution face.
type CommandRelauncher func(context.Context, Declarative) (SpawnResult, error)

type commandRelauncherContextKey struct{}

// WithCommandRelauncher binds command reentry to the selected execution face.
func WithCommandRelauncher(ctx context.Context, relaunch CommandRelauncher) context.Context {
	return context.WithValue(ctx, commandRelauncherContextKey{}, relaunch)
}

// CommandRelauncherFromContext returns the command reentry authority of this call.
func CommandRelauncherFromContext(ctx context.Context) (CommandRelauncher, bool) {
	relaunch, ok := ctx.Value(commandRelauncherContextKey{}).(CommandRelauncher)
	return relaunch, ok && relaunch != nil
}
