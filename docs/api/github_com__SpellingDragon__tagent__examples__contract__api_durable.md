package durableexample // import "github.com/SpellingDragon/tagent/examples/contract/api_durable"

Package durableexample holds the canonical example of the durable fact
surface every fact ledger is written through and replayed from: the minimal
implementation beside the Example function that drives it.

FUNCTIONS

func ExampleFactLog()
    ExampleFactLog stages two keys, scans them all in full and completes the
    barrier before anything is reported durable.

