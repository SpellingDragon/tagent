// Package benchexample holds the canonical example of the live-benchmark
// acceptance port.
//
// 契约: docs/wiki/rl/rl-architecture.md#live-benchmark
package benchexample

import (
	"fmt"
	"strings"

	"github.com/SpellingDragon/tagent/bench"
)

// containsVerifier is the smallest honest verifier: it decides from the response
// text it was given, not from a claim the model makes about itself.
type containsVerifier struct {
	want string
}

// Verify accepts output only when it carries the required literal.
func (v containsVerifier) Verify(_ string, output string) error {
	if !strings.Contains(output, v.want) {
		return fmt.Errorf("output does not contain %q", v.want)
	}
	return nil
}

var _ bench.Verifier = containsVerifier{}

// ExampleVerifier drives one acceptance port through a passing and a failing
// artifact, showing that success is decided by the declared evidence.
func ExampleVerifier() {
	var verifier bench.Verifier = containsVerifier{want: "ORBIT"}
	if err := verifier.Verify("", "the codeword is ORBIT"); err != nil {
		panic(err)
	}
	if err := verifier.Verify("", "the codeword is NEBULA"); err == nil {
		panic("negative artifact was accepted")
	}
}
