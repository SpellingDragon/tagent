// 契约: docs/FACT-VOCABULARY.md#families
//
// The filing seam of the compress request family: where the judgment that
// material is due for compression is filed, and the answer the filing gives.
//
// The fact itself — who judged, why, what was judged, and the identity the
// judgment is quoted by — is event vocabulary (event.CompressRequest).
//
// 契约: docs/wiki/agent/compression-and-telemetry.md#output-overflow
package memory

import (
	"context"

	"github.com/SpellingDragon/tagent/api/event"
)

// CompressRequestFiling files one compress request into the log its consumer
// reads and answers the request as it stands on record, so a repeated judgment
// keeps the identity and the filing moment of the first one. A filing that fails
// is reported by name and a judgment is never dropped silently.
type CompressRequestFiling func(ctx context.Context, request event.CompressRequest) (event.CompressRequest, error)
