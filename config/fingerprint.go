package config

import "errors"

// 契约: docs/wiki/platform/org-hot-reload.md#fingerprint
//
// Fingerprint returns the canonical organization fingerprint of d: a stable
// string derived solely from the declaration surface, namely the references,
// the model selection, the prompt sources and the tool declarations.
//
// Live objects (model handles, tool instances, connections, locks and
// generation numbers) never contribute to the fingerprint, so reloading
// byte-identical content yields the same fingerprint. Fingerprint is not
// implemented in the phase 1 skeleton: it always fails.
func Fingerprint(d Declaration) (string, error) {
	return "", errors.New("phase 2: not implemented")
}
