// 契约: docs/wiki/platform/org-hot-reload.md#generations
//
// The model dimension vocabulary of a configuration change: the metadata keys a
// config_change fact of the model dimension carries — the version the move
// starts from, the version it asks for and the result the producer states — and
// the result stamp a producer writes when it asks a lease to apply the move.
//
// The independent model replacement family is retired: a model move is one
// configuration change of the model dimension, never a second fact family. No
// type, constructor or event shape stands beside the five-field change; a lease
// observer reads the same fact under these keys, exactly as it reads the
// dimension and the publication generation beside them.
package event

// MetaKeyModelFromVersion, MetaKeyModelToVersion and MetaKeyModelReplacementResult
// name the metadata keys a configuration change of the model dimension carries
// beside the frozen trigger source stamp: the version the move starts from, the
// version it asks for and the result the producer states. The model move adds no
// exported field to the change: these keys are its payload.
const (
	// MetaKeyModelFromVersion names the version the move starts from.
	MetaKeyModelFromVersion = "model_from_version"
	// MetaKeyModelToVersion names the version the move asks for.
	MetaKeyModelToVersion = "model_to_version"
	// MetaKeyModelReplacementResult names the result the producer states.
	MetaKeyModelReplacementResult = "model_replacement_result"
)

// ModelReplacementResultRequested is the result a producer states when it files
// a move for the lease to apply: the fact records the judgment, and the lease
// records whether the replacement actually took effect in its own history.
const ModelReplacementResultRequested = "requested"
