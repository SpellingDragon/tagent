## 1. Core Fix: collectCompressedKeys

- [x] 1.1 Add `parseEventKeyFromPrefix(content string) int64` helper in `agent/smart_compress.go`
- [x] 1.2 Replace `collectCompressedKeys` body: iterate old segments' messages, call `parseEventKeyFromPrefix`, deduplicate, return keys
- [x] 1.3 Remove unused imports added by old implementation (Session.Events access no longer needed for key collection)

## 2. Unit Tests

- [x] 2.1 Add `TestParseEventKeyFromPrefix` covering: valid prefix, no prefix, malformed prefix, large key value
- [x] 2.2 Add `TestCollectCompressedKeys` covering: single key, multiple keys, duplicate keys, messages without prefix, mixed valid/invalid
- [x] 2.3 Run existing `smart_compress_test.go` to verify no regression in `Compress`/`splitByTaskBoundary`/`generateSummary`

## 3. Verification

- [x] 3.1 Run `go test ./agent/...` to confirm all tests pass
- [x] 3.2 Run `go build ./...` to confirm no compilation errors
- [x] 3.3 Manual verify: review `buildCompressEvent` output format still includes compressed keys list correctly
