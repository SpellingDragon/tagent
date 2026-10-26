package recallexample // import "github.com/SpellingDragon/tagent/examples/contract/tool_recall"

Package recallexample holds the canonical example of the observation-table
face the recall tools read one event through: the declarative query page,
the reference of one landed event, and the original reference that event was
landed with.

The file is the minimal implementation beside the Example function that drives
it.

FUNCTIONS

func ExampleTable()
    ExampleTable answers one page, one landed event and one original reference
    from the same table: a key the table holds no event for reads as not held
    instead of as an empty page, and the page never carries an event outside the
    partitions the query names.

