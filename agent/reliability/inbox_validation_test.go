package reliability

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// §3.1 strict receive validation + exact-integer identity.
//
// These cases lock the three gaps the delta spec "可靠输入全序持久化" /
// "严格校验 v2 状态/固定槽/必要源字段/JSON/非 nil Message" and the
// "大整数相邻身份不被合并" scenario require. Each was a real fail-before on
// the pre-§3.1 code (documented in evidence.md); the fixes live in
// readEnvelope/Enqueue/jsonEqual.

// writeRawEnvelope drops a hand-built bytes blob straight into the inbox dir so
// a test can exercise the on-disk validation path (readEnvelope) independent of
// Enqueue's own guards.
func writeRawEnvelope(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
	return p
}

// A. readEnvelope must reject a durable state outside the three legal ones — a
// corrupt/tampered "state" must never be consumed as if it were pending.
func TestInbox_ReadEnvelopeRejectsIllegalState(t *testing.T) {
	dir := t.TempDir()
	// A structurally valid v2 envelope whose State is not pending/claimed/receipted.
	body := `{"version":2,"request_id":"r","source":"user","state":"bogus",` +
		`"messages":[{"slot":0,"source_event":{"id":"e","content":"x"}}]}`
	p := writeRawEnvelope(t, dir, "00000000000000000001.json", body)
	_, err := readEnvelope(p)
	require.Error(t, err, "an illegal durable state must be rejected, not consumed as pending")
	require.Contains(t, err.Error(), "state")
}

// readEnvelope still accepts each of the three legal states (so the whitelist
// guard above is not over-broad).
func TestInbox_ReadEnvelopeAcceptsLegalStates(t *testing.T) {
	for _, st := range []string{InboxStatePending, InboxStateClaimed, InboxStateReceipted} {
		dir := t.TempDir()
		body := `{"version":2,"request_id":"r","source":"user","state":"` + st + `",` +
			`"messages":[{"slot":0,"source_event":{"id":"e","content":"x"}}]}`
		p := writeRawEnvelope(t, dir, "00000000000000000002.json", body)
		_, err := readEnvelope(p)
		require.NoErrorf(t, err, "legal state %q must be accepted", st)
	}
}

// C. Enqueue must refuse a nil (JSON null) or malformed source_event — those are
// not "valid empty input", they are an illegal/absent Message at receive time.
func TestInbox_EnqueueRejectsNilOrMalformedSourceEvent(t *testing.T) {
	in, err := NewInbox(t.TempDir(), 10)
	require.NoError(t, err)

	// JSON null == a nil Message (len 4, so the old empty-bytes guard missed it).
	_, err = in.Enqueue(&Envelope{RequestID: "n", Source: "user",
		Messages: []MessageSlot{{Slot: 0, SourceEvent: json.RawMessage("null")}}})
	require.Error(t, err, "a null source_event (nil Message) must be refused at receive")

	// Malformed JSON is not a lossless snapshot.
	_, err = in.Enqueue(&Envelope{RequestID: "m", Source: "user",
		Messages: []MessageSlot{{Slot: 0, SourceEvent: json.RawMessage("{not json")}}})
	require.Error(t, err, "an unparseable source_event must be refused at receive")

	require.Equal(t, int64(0), in.Pending(), "refused inputs leave no durable item")
}

// C' §3.1: an external_input source event MUST carry a non-nil Message. A well-formed
// JSON object with "message":null or no message field is NOT a lossless input — the old
// guard only rejected wholly-null/empty/unparseable payloads, so these slipped through
// and later dereferenced a nil Message in buildBusFact. Must be refused at receive.
func TestInbox_EnqueueRejectsExternalInputWithNilMessage(t *testing.T) {
	in, err := NewInbox(t.TempDir(), 10)
	require.NoError(t, err)

	_, err = in.Enqueue(&Envelope{RequestID: "t1", Source: "user",
		Messages: []MessageSlot{{Slot: 0, SourceEvent: json.RawMessage(`{"id":"e","type":"external_input","message":null}`)}}})
	require.Error(t, err, `external_input with "message":null must be refused at receive`)
	require.Contains(t, err.Error(), "nil Message")

	_, err = in.Enqueue(&Envelope{RequestID: "t2", Source: "user",
		Messages: []MessageSlot{{Slot: 0, SourceEvent: json.RawMessage(`{"id":"e","type":"external_input"}`)}}})
	require.Error(t, err, "external_input with an absent Message must be refused at receive")

	// Positive control: the same type WITH a message (even empty-text) is legal input.
	_, err = in.Enqueue(&Envelope{RequestID: "t3", Source: "user",
		Messages: []MessageSlot{{Slot: 0, SourceEvent: json.RawMessage(`{"id":"e","type":"external_input","message":{"role":"user","content":""}}`)}}})
	require.NoError(t, err, "external_input with a valid empty-text Message is legal")

	require.Equal(t, int64(1), in.Pending(), "only the valid input becomes durable")
}

// C (positive control). A valid empty-text Message and a non-text (image-only)
// Message are both legal inputs and MUST be accepted — the distinction is
// "valid empty" vs "illegal (null/unparseable)", NOT "empty text = no input".
func TestInbox_EnqueueAcceptsValidEmptyAndNonTextMessages(t *testing.T) {
	in, err := NewInbox(t.TempDir(), 10)
	require.NoError(t, err)

	emptyText, _ := json.Marshal(map[string]any{"id": "e1", "content": ""})
	_, err = in.Enqueue(&Envelope{RequestID: "et", Source: "user",
		Messages: []MessageSlot{{Slot: 0, SourceEvent: emptyText}}})
	require.NoError(t, err, "a valid empty-text message is legal input")

	imageOnly, _ := json.Marshal(map[string]any{"id": "e2", "content": "", "parts": []any{
		map[string]any{"type": "image_url", "url": "http://x/y.png"}}})
	_, err = in.Enqueue(&Envelope{RequestID: "img", Source: "user",
		Messages: []MessageSlot{{Slot: 0, SourceEvent: imageOnly}}})
	require.NoError(t, err, "a non-text (image) payload is legal input, not an empty one")
}

// B. jsonEqual must compare big-integer identities EXACTLY. Two adjacent int64
// event keys above 2^53 are DIFFERENT identities; the pre-§3.1 float64 JSON
// round-trip collapsed them, corrupting prepare/completion idempotency.
func TestJsonEqual_BigIntAdjacentKeysDistinct(t *testing.T) {
	a := json.RawMessage(`{"event_key":9007199254740992}`) // 2^53
	b := json.RawMessage(`{"event_key":9007199254740993}`) // 2^53 + 1
	require.False(t, jsonEqual(a, b),
		"adjacent big-integer keys differ by 1 and MUST NOT compare equal")

	// Nested + array positions must stay distinct too.
	na := json.RawMessage(`{"slots":[{"event_key":9007199254740992}]}`)
	nb := json.RawMessage(`{"slots":[{"event_key":9007199254740993}]}`)
	require.False(t, jsonEqual(na, nb), "nested adjacent big-int keys must differ")

	// Positive controls: genuine identity/equality is preserved (key order and
	// whitespace insensitive) and equal big ints stay equal.
	require.True(t, jsonEqual(
		json.RawMessage(`{"b":2,"a":1}`), json.RawMessage(`{"a":1,"b":2}`)),
		"same object, different key order, must be equal")
	require.True(t, jsonEqual(
		json.RawMessage(`{"event_key":9007199254740993}`),
		json.RawMessage(`{"event_key":9007199254740993}`)),
		"identical big-int keys must be equal")
}

// B (end-to-end): a prepare whose ONLY difference from an already-frozen fact
// is an adjacent big-int event key must be a CONFLICT, not silently accepted as
// an idempotent re-prepare.
func TestInbox_PrepareFacts_BigIntAdjacentFactIsConflict(t *testing.T) {
	in, err := NewInbox(t.TempDir(), 10)
	require.NoError(t, err)
	mustEnqueue(t, in, env("bigp", "user"))
	_, path, err := in.ClaimNext()
	require.NoError(t, err)

	require.NoError(t, in.PrepareFacts(path, "rk",
		[]json.RawMessage{json.RawMessage(`{"event_key":9007199254740992}`)}))
	require.Error(t,
		in.PrepareFacts(path, "rk", []json.RawMessage{json.RawMessage(`{"event_key":9007199254740993}`)}),
		"an adjacent big-int fact must be a conflict, not an idempotent no-op")
}
