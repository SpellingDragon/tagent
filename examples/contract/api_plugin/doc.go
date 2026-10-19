// Package pluginexample holds the canonical example of every narrow face the plugin
// pipeline dispatches: one file per interface, each a minimal implementation beside the
// Example function that drives it through its real caller.
//
// The observation bypass faces sit in the same directory because they are the same
// package surface: the post-commit observer, the bounded recorder that carries its records
// to a writer, and the audit sink those records land in, one file per interface.
//
// 契约: docs/wiki/plugin/plugin-architecture.md#overview
package pluginexample
