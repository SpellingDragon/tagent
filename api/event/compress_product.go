// 契约: docs/FACT-VOCABULARY.md#families
//
// The compress product family: the product a consumer minted for one compress
// request, stated as the fact it is — which request it answers, which producer
// executed it, the product key the projection carries, the entries the product
// covers and the product text. A product is filed once per request: the identity
// is content-addressed over the request and the key, so a retried execution
// answers the same identity instead of filing a second product.
package event

import (
	"crypto/sha256"
	"encoding/hex"
)

// CompressProductType is the frozen type of a compress product fact.
const CompressProductType = "compress.product"

// CompressProduct is one compress product fact: the material a consumer folded
// for one judgment. It is immutable once filed, and it is the only source the
// projection restores a synthetic product entry from. What the product is — a
// folded segment or a history summary — is read off the key's prefix, and the
// filing moment is the commit stamp of the fact, so neither is stated twice.
type CompressProduct struct {
	// ID is the identity of the product: content-addressed over the answers and
	// the product key, so one request answers one product identity.
	ID EventID
	// Answers names the judgment this product answers and the mechanism that
	// executed the compression.
	Answers ProductAnswers
	// Key is the product key the projection carries the entry under; its prefix
	// states the kind.
	Key EventID
	// Covered lists the entry keys the product covers, in their original order.
	Covered []EventID
	// Text is the product text the projection presents.
	Text string
}

// ProductAnswers names the judgment one product answers beside the mechanism
// that executed it.
type ProductAnswers struct {
	// Request is the judgment this product answers.
	Request EventID
	// Producer names the mechanism that executed the compression.
	Producer string
}

// CompressProductFor returns the product fact one execution files: the identity
// is content-addressed over the request it answers and the key it carries, so a
// repeated execution of the same judgment files nothing new. The filing moment
// stays zero for the log to stamp on first filing.
func CompressProductFor(request EventID, producer string, key EventID, covered []EventID, text string) CompressProduct {
	sum := sha256.Sum256([]byte(string(request) + "\x00" + producer + "\x00" + string(key)))
	return CompressProduct{
		ID:      EventID("cmpout-" + hex.EncodeToString(sum[:8])),
		Answers: ProductAnswers{Request: request, Producer: producer},
		Key:     key,
		Covered: append([]EventID(nil), covered...),
		Text:    text,
	}
}

// FactKey returns the product's identity as a fact key: the identity one
// execution is quoted by, unchanged by every retry of that execution.
//
// 契约: docs/FACT-VOCABULARY.md#families
func (p CompressProduct) FactKey() string { return string(p.ID) }

// FactType returns the frozen type a compress product is read as.
func (p CompressProduct) FactType() string { return CompressProductType }

// The compress product family satisfies the fact category contract.
var _ Fact = CompressProduct{}
