package tasks // import "github.com/SpellingDragon/tagent/bench/tasks"

Package tasks owns the embedded live-benchmark task fixtures.

The JSON files are the single source the bench root decodes: prompts, required
capabilities, setup files, a machine-readable verifier declaration and one
positive/negative example pair per task. Keeping the assets in their own package
lets a runner or a training-sample consumer read the set without importing a
decode implementation.

FUNCTIONS

func FS() fs.FS
    FS returns the fixture set rooted at this package directory.
