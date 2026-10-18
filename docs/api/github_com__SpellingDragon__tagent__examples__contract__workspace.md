package workspaceexample // import "github.com/SpellingDragon/tagent/examples/contract/workspace"

Package workspaceexample holds the canonical examples of the workspace faces:
the temporary-replica reclaimer and the oversized-output externalizer.

One file per interface, each a minimal implementation beside the Example
function that drives it.

FUNCTIONS

func ExampleCleaner()
    ExampleCleaner reclaims this workspace's own replicas: a replica an active
    handle protects survives both a count-bounded pass and an age-bounded pass,
    an idle one is reclaimed, and Wait returns once the reclaimer stopped
    instead of assuming it did.

func ExampleExternalizer()
    ExampleExternalizer commits one oversized result: the original is kept
    first, the returned preview is bounded while the segment entry still
    reaches the whole result, and a replica write that failed comes back as
    ErrReplicaWriteFailed with the reference still effective.

