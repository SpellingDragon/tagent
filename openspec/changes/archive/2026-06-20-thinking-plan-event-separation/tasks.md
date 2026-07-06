## 1. Core Event Type Change

- [x] 1.1 Modify `ExtractEventType` in `tagent/event/types.go`: change `RoleAssistant + ToolCalls` return from `TypeActionCommand` to `TypeThinkingPlan`
- [x] 1.2 Modify `IsSpecialEventType` in `tagent/event/types.go`: add `TypeThinkingPlan` case returning `true`
- [x] 1.3 Run `go test ./event/...` to verify no breakage

## 2. Verify Downstream Module Compatibility

- [x] 2.1 Verify MemoryPlugin auto-adapts via `inferEventInfo → ExtractEventType` — `[Memory]` log should now show `type=thinking_plan` for tool_call events
- [x] 2.2 Verify SummaryPlugin auto-adapts via `ExtractEventType` — tag output should include `thinking_plan:...` prefix
- [x] 2.3 Verify ContextIntervention Phase 1 view transformation — `[evt_xxx|thinking_plan]` prefix appears correctly
- [x] 2.4 Verify SmartCompressor — `thinking_plan` events get preserved as-is (special event) in compression
- [x] 2.5 Run `go test ./plugin/... ./agent/...` to confirm no regression

## 3. Test Updates

- [x] 3.1 Add test case in `event/types_test.go` (or equivalent): `ExtractEventType` returns `thinking_plan` for assistant with ToolCalls
- [x] 3.2 Add test case for `IsSpecialEventType("thinking_plan")` returning `true`
- [x] 3.3 Update any test that asserts assistant+ToolCalls → `action_command` to expect `thinking_plan`
- [x] 3.4 Run full test suite: `go test ./...`

## 4. Wiki Documentation Sync

- [x] 4.1 Update `docs/wiki/event/event-architecture.md`: event type inference table, section 5.3 → `RoleAssistant+ToolCalls → thinking_plan`
- [x] 4.2 Update `docs/wiki/agent/agent-architecture.md`: section 11 event type inference table → align with event wiki and code
- [x] 4.3 Update `docs/wiki/plugin/plugin-architecture.md`: section 7.2 event type inference table
- [ ] 4.4 Update `docs/wiki/memory/memory-architecture.md`: if event_key format is mentioned, ensure Snowflake int64 format is reflected

## 5. Final Verification

- [x] 5.1 Verify compilation: `go build ./...` success
- [x] 5.2 Full test suite: `go test ./...` all pass
