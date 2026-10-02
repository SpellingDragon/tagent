package govx // import "github.com/SpellingDragon/tagent/tool/govx"

Package govx provides the governance face tools: goal declaration/query
and audit query tools that make the bounded-autonomy gate usable from the
conversation. Entry-only (wired in tagent.go alongside refine); all tools are
advisory/record-keeping — the gate itself stays in agent/governance.

FUNCTIONS

func NewGoalTools(gate *governance.GovernanceGate) []tool.Tool
    NewGoalTools builds the five governance face tools bound to the shared gate
    (Goals/Ledger/Approval accessors). Entry-only wiring lives in tagent.go.
