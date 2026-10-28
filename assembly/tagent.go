// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
//
// Package tagent is the composition root of the runtime: the single point where
// a pure organization declaration becomes a live runtime, and the only
// dependency-injection site of the process. Assemble is the sole assembly entry
// and the Run it returns is the face the host drives; no second assembly entry
// sits beside it.
//
// The root establishes the shared faces before any candidate binding is prepared
// and assembles the execution face on top of them, and it keeps no second copy of
// the live scene: the effective bindings belong to the state owner, the
// obligations it admits belong to the task layer, and the readings it serves are
// derived from the facts those owners already hold. The one piece of the scene
// the run holds in its own right is the session transcript, which is the
// execution scene's read-out face and not a copy of anything an owner holds; the
// serializable scene is its facts beside its configuration, never a snapshot of
// the conversation body, so a later process recovers the body by replaying the
// durable fact chain instead of restoring a body snapshot kept beside the facts.
package assembly
