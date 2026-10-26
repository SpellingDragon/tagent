package config

// FactKey returns the declaration's identity as a fact key: the canonical
// organization fingerprint in its hex spelling (see Fingerprint). That identity
// is synthesised from the declaration surface alone, so reloading equivalent
// content answers the same key and any change to the surface answers a different
// one. The raw source document and every live binding stay outside it, and a
// declaration the canonical encoding cannot represent answers no key: the empty
// string, which names no fact.
//
// 契约: docs/FACT-VOCABULARY.md#families
func (d Declaration) FactKey() string {
	fingerprint, err := Fingerprint(d)
	if err != nil {
		return ""
	}
	return fingerprint
}

// FactType returns the frozen type a declaration is read as.
func (d Declaration) FactType() string { return "config.declaration" }
