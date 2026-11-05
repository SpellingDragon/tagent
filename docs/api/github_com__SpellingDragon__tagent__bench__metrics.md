package metrics // import "github.com/SpellingDragon/tagent/bench/metrics"

CONSTANTS

const KeyDeliveryRate = "delivery_rate"
    KeyDeliveryRate is the sender-confirmed delivery rate key.

const KeyInterventions = "interventions"
    KeyInterventions is the human intervention key.

const KeyLatency = "latency"
    KeyLatency is the cold and hot latency key.

const KeySuccessRate = "success_rate"
    KeySuccessRate is the verifier pass-rate key.

const KeyTokenCost = "token_cost"
    KeyTokenCost is the prompt and completion token key.

const SchemaVersion = 1
    SchemaVersion names the document shape this package validates.

const StatusBlocked = "blocked"
    StatusBlocked marks a run stopped by a missing dependency.

const StatusCompleted = "completed"
    StatusCompleted marks a run that produced all three runner results.

const StatusSkipped = "skipped"
    StatusSkipped marks a run that refused to spend quota without a key.

FUNCTIONS

func Aggregate(runner, status, caliber string, attempts [][]byte) ([]byte, error)
    Aggregate folds one runner's attempts into its five-metric block.

func NewAttempt(raw map[string]any) ([]byte, error)
    NewAttempt validates and canonicalizes one attempt record.

func ValidateRun(raw []byte) error
    ValidateRun checks one run metrics document for the five-metric contract.

func ValidateRunner(raw []byte) error
    ValidateRunner checks one runner block for the five-metric contract.
