// The compression pipeline anchors: judgment, execution and restore are three
// facts of one chain. A producer judges material over its bound and files a
// compress request; a consumer executes the judgment and files the compress
// product; the projection restores the product from the chain instead of
// folding the material again. The first anchor is the producer alone: a host
// that mounts no consumer reads every judgment and gets no product, and no
// error either.
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
package host

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/memory/replay"
)

// productFactRequest, productFactKey and productFactText are the metadata keys a
// compress product fact states its fields under, as the durable chain spells them:
// the restore path reads a product back from exactly these keys, so a green anchor
// here proves the fact the restart reads.
const (
	productFactRequest = "compress_request"
	productFactKey     = "compress_key"
	productFactText    = "compress_text"
)

// compressProductFact is one compress product fact as the chain states it.
type compressProductFact struct {
	// Request is the judgment the product answers.
	Request event.EventID
	// Key is the product key the projection carries the entry under.
	Key event.EventID
	// Text is the product text the projection presents.
	Text string
}

// compressTurnLegs reports the four legs one task turn commits: the input that
// opens it, the process detail a fold consumes and the output that closes it.
func compressTurnLegs(turn int) []struct {
	position string
	typ      string
	summary  string
} {
	return []struct {
		position string
		typ      string
		summary  string
	}{
		{"0-in", event.TypeExternalInput, fmt.Sprintf("ask %02d", turn)},
		{"1-plan", event.TypeThinkingPlan, fmt.Sprintf("plan %02d", turn)},
		{"2-call", event.TypeActionCommand, fmt.Sprintf("call %02d", turn)},
		{"3-out", event.TypeAgentOutput, fmt.Sprintf("answer %02d", turn)},
	}
}

// compressBody reports the original text one leg carries: long enough that a few
// turns fill a small window, and distinct per leg so a body the fold consumed is
// recognisable on the wire.
func compressBody(turn int, position string) string {
	return strings.Repeat(fmt.Sprintf("compress 正文 %s %02d。", position, turn), 12)
}

// commitCompressTurn commits one task turn under the run, in commit order.
func commitCompressTurn(t *testing.T, ctx context.Context, run *assembly.Run, addr event.Address, turn int) {
	t.Helper()
	base := time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)
	for position, leg := range compressTurnLegs(turn) {
		if _, err := run.CommitToSession(ctx, addr, event.Event{
			Key:       event.EventID(fmt.Sprintf("%02d-%s", turn, leg.position)),
			Type:      leg.typ,
			Timestamp: base.Add(time.Duration(turn*10+position) * time.Minute),
			Meta:      map[string]string{event.MetaKeyEventSummary: leg.summary},
			Message:   model.Message{Role: model.RoleUser, Content: compressBody(turn, leg.position)},
		}); err != nil {
			t.Fatalf("commit turn %d leg %s: %v", turn, leg.position, err)
		}
	}
}

// compressFactsOf reports the facts of one frozen type one session chain holds,
// in commit order.
func compressFactsOf(run *assembly.Run, session event.SessionID, factType string) []event.Event {
	var out []event.Event
	for _, evt := range run.SessionFacts(event.Address{SessionID: session}) {
		if evt.Type == factType {
			out = append(out, evt)
		}
	}
	return out
}

// compressProductsOf reports the product facts one session chain holds, in
// commit order, beside the request identities they answer.
func compressProductsOf(t *testing.T, run *assembly.Run, session event.SessionID) ([]compressProductFact, map[event.EventID]int) {
	t.Helper()
	facts := compressFactsOf(run, session, event.CompressProductType)
	products := make([]compressProductFact, 0, len(facts))
	answers := map[event.EventID]int{}
	for _, evt := range facts {
		request := event.EventID(evt.Meta[productFactRequest])
		key := event.EventID(evt.Meta[productFactKey])
		if request == "" || key == "" {
			t.Fatalf("the product fact %s states no judgment or no product key: %v", evt.Key, evt.Meta)
		}
		products = append(products, compressProductFact{Request: request, Key: key, Text: evt.Meta[productFactText]})
		answers[request]++
	}
	return products, answers
}

// compressSyntheticKeysOf reports the synthetic product keys one session's
// projection carries, sorted.
func compressSyntheticKeysOf(t *testing.T, run *assembly.Run, session event.SessionID) []event.EventID {
	t.Helper()
	entries, held := run.ReplayEntries(session)
	if !held {
		t.Fatalf("session %q registered no projection", session)
	}
	keys := make([]event.EventID, 0, len(entries))
	for _, entry := range entries {
		if replay.SyntheticKey(entry.Key) {
			keys = append(keys, entry.Key)
		}
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}

// compressRequestIDsOf reports the judgment identities one session chain holds.
func compressRequestIDsOf(run *assembly.Run, session event.SessionID) map[event.EventID]bool {
	ids := map[event.EventID]bool{}
	for _, evt := range compressFactsOf(run, session, event.CompressRequestType) {
		ids[evt.Key] = true
	}
	return ids
}

// compressDigestOf reports a stable digest of one projection's entries, so an
// anchor reads whether a judgment rewrote the material at all.
func compressDigestOf(entries []replay.ProjectionEntry) string {
	var out strings.Builder
	for _, entry := range entries {
		out.WriteString(string(entry.Key))
		out.WriteByte('=')
		out.WriteString(entry.Content)
		out.WriteByte('\n')
	}
	return out.String()
}

// foldFace is the session face one test hands the age fold plugin directly: the
// projection and window of one session beside the judgments the plugin files.
type foldFace struct {
	projection map[event.SessionID]*replay.ReplayProjection
	window     assembly.FoldWindow
	sessions   map[event.EventID]event.SessionID
	filed      []event.CompressRequest
}

// SessionOf reports the session chain one committed fact belongs to.
func (f *foldFace) SessionOf(_ context.Context, id event.EventID) (event.SessionID, bool) {
	session, held := f.sessions[id]
	return session, held
}

// ProjectionOf reports the projection and window one session registered.
func (f *foldFace) ProjectionOf(_ context.Context, session event.SessionID) (*replay.ReplayProjection, assembly.FoldWindow, bool) {
	projection, held := f.projection[session]
	return projection, f.window, held
}

// FileCompressRequest files one judgment the fold answered.
func (f *foldFace) FileCompressRequest(_ context.Context, _ event.SessionID, request event.CompressRequest) error {
	f.filed = append(f.filed, request)
	return nil
}

// TestCompressRequestIsFiledWithoutExecution is the negative anchor of the three-stage split: a host mounting no consumer files no product.
//   - the producer files one compress request per product the fold would mint
//   - the producer folds nothing itself: the projection stands exactly as it stood
//   - no consumer is mounted, so no product is filed and the observation answers no error
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestCompressRequestIsFiledWithoutExecution(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	projection := replay.NewReplayProjection()
	for turn := 0; turn < 4; turn++ {
		for _, leg := range compressTurnLegs(turn) {
			projection.Append(ctx, replay.ProjectionEntry{
				Key:     event.EventID(fmt.Sprintf("%02d-%s", turn, leg.position)),
				Type:    leg.typ,
				Summary: leg.summary,
				Content: compressBody(turn, leg.position),
				Time:    time.Date(2026, 10, 20, 8, turn, 0, 0, time.UTC),
			})
		}
	}
	before := compressDigestOf(projection.Entries())
	face := &foldFace{
		projection: map[event.SessionID]*replay.ReplayProjection{"session-compat": projection},
		window: assembly.FoldWindow{
			MaxTokens:         100,
			CompressThreshold: config.DefaultCompressThreshold,
			KeepRecentTasks:   config.DefaultKeepRecentTasks,
		},
		sessions: map[event.EventID]event.SessionID{"evt-compat": "session-compat"},
	}
	plugin := assembly.NewAgeFoldPlugin(face)
	if err := plugin.ObserveCommit(ctx, "evt-compat"); err != nil {
		t.Fatalf("the producer refused the commit although no consumer is mounted: %v", err)
	}
	if len(face.filed) == 0 {
		t.Fatalf("the producer filed no judgment although the projection carries %d entries over the window", len(projection.Entries()))
	}
	if after := compressDigestOf(projection.Entries()); after != before {
		t.Fatalf("the producer folded the material itself: the projection changed under a host that mounts no consumer")
	}
	t.Logf("[compress 1] negative: a producer beside no consumer filed %d judgment(s) and left the %d-entry projection untouched", len(face.filed), len(projection.Entries()))
}

// TestCompressPipelineJudgesThenExecutes anchors the three stages of compression over the public commit entry.
//   - the age fold files a compress request for every product the material is due
//   - the consumer executes each judgment and files the compress product beside it
//   - every synthetic entry of the projection is answered by a product fact, so no stage mints a product of its own
//   - one request answers exactly one product, and a later commit files no second one
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestCompressPipelineJudgesThenExecutes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	provider := &entryModel{name: "model-a", answer: func(*model.Request) *model.Response {
		return entryFinalTurn("the compress answer")
	}}
	decl := &config.Declaration{
		Entry: "agent-a",
		Agents: []config.AgentSpec{{
			ID:        "agent-a",
			Model:     "model-a",
			MaxTokens: 100,
		}},
	}
	run, err := assembly.Assemble(ctx, decl, map[event.AgentID]assembly.NativeAgent{
		"agent-a": {Model: provider},
	}, assembly.WithGovernance(passPolicy{}))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}
	addr := event.Address{AgentID: "agent-a", SessionID: "session-compress-pipeline"}
	const turns = 6
	for turn := 0; turn < turns; turn++ {
		commitCompressTurn(t, ctx, run, addr, turn)
	}
	products, answers := compressProductsOf(t, run, addr.SessionID)
	if len(products) == 0 {
		t.Fatalf("the executed judgments filed no compress product after %d turn(s), so consumption never ran", turns)
	}
	if len(products) < 2 {
		t.Fatalf("the chain carries %d product(s) after %d turn(s), want the segment fold and the history summary both executed", len(products), turns)
	}
	if !compressCoversBothKinds(products) {
		t.Fatalf("the products %v state no segment fold and no history summary: one product kind never reached the chain", keysOfProducts(products))
	}
	requests := compressRequestIDsOf(run, addr.SessionID)
	if len(requests) == 0 {
		t.Fatal("the fold filed no compress request, so no product could have answered one")
	}
	for _, product := range products {
		if !requests[product.Request] {
			t.Fatalf("the product %s answers the judgment %s the chain never filed", product.Key, product.Request)
		}
	}
	for request, answered := range answers {
		if answered != 1 {
			t.Fatalf("the judgment %s answers %d product(s), want at most one", request, answered)
		}
	}
	synthetic := compressSyntheticKeysOf(t, run, addr.SessionID)
	if len(synthetic) == 0 {
		t.Fatal("the executed judgments left the projection unfolded: it carries no synthetic entry")
	}
	answered := map[event.EventID]bool{}
	for _, product := range products {
		answered[product.Key] = true
	}
	for _, key := range synthetic {
		if !answered[key] {
			t.Fatalf("the projection carries the synthetic entry %s that no product fact answers: a stage minted a product of its own: %v", key, synthetic)
		}
	}
	filedProducts, filedRequests := len(products), len(requests)
	for turn := turns; turn < turns+2; turn++ {
		commitCompressTurn(t, ctx, run, addr, turn)
	}
	afterProducts, afterAnswers := compressProductsOf(t, run, addr.SessionID)
	if len(afterProducts) < filedProducts {
		t.Fatalf("the later commits dropped the products from %d to %d, want the chain to keep them", filedProducts, len(afterProducts))
	}
	for request, answered := range afterAnswers {
		if answered != 1 {
			t.Fatalf("the judgment %s answers %d product(s) after further commits, want at most one", request, answered)
		}
	}
	for _, product := range products {
		if afterAnswers[product.Request] != 1 {
			t.Fatalf("the re-observed judgment %s changed its answer count, want the one product it already had", product.Request)
		}
	}
	t.Logf("[compress 2] chain: %d request(s) -> %d product(s) -> %d synthetic entr(ies); later commits filed no second product for any existing judgment",
		filedRequests, filedProducts, len(synthetic))
}

// TestCompressProductRestoresAfterRestart is the restore anchor: a restarted run presents the products the chain holds and folds nothing again.
//   - the commits under a budgeted window fold the projection and file the products they executed
//   - a second assembly over the same storage restores the same product keys, key for key
//   - the restart files no judgment and no product: the fold form is read, never re-derived
//   - the next real request carries the restored product text and none of the raw detail the product consumed
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestCompressProductRestoresAfterRestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	storage := ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()}
	decl := &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a", MaxTokens: 100}},
	}
	newAgents := func(provider *entryModel) map[event.AgentID]assembly.NativeAgent {
		return map[event.AgentID]assembly.NativeAgent{"agent-a": {Model: provider}}
	}
	first := &entryModel{name: "model-a", answer: func(*model.Request) *model.Response {
		return entryFinalTurn("the first-boot answer")
	}}
	run, err := assembly.Assemble(ctx, decl, newAgents(first), assembly.WithGovernance(passPolicy{}), assembly.WithStorage(storage))
	if err != nil {
		t.Fatalf("assemble the durable runtime: %v", err)
	}
	addr := event.Address{AgentID: "agent-a", SessionID: "session-compress-restore"}
	const turns = 6
	for turn := 0; turn < turns; turn++ {
		commitCompressTurn(t, ctx, run, addr, turn)
	}
	online := compressSyntheticKeysOf(t, run, addr.SessionID)
	if len(online) == 0 {
		t.Fatalf("the commits folded nothing under the 100-token window, so there is no product to restore")
	}
	products, _ := compressProductsOf(t, run, addr.SessionID)
	if len(products) == 0 {
		t.Fatal("the fold filed its judgment but no consumer product reached the chain, so a restart has nothing to restore")
	}
	backed := map[event.EventID]bool{}
	for _, product := range products {
		backed[product.Key] = true
	}
	for _, key := range online {
		if !backed[key] {
			t.Fatalf("the projection carries the synthetic entry %s that no product fact answers: the fold form was minted without a product", key)
		}
	}

	probe := &entryModel{name: "model-a", answer: func(*model.Request) *model.Response {
		return entryFinalTurn("the restarted answer")
	}}
	restarted, err := assembly.Assemble(ctx, decl, newAgents(probe), assembly.WithGovernance(passPolicy{}), assembly.WithStorage(storage))
	if err != nil {
		t.Fatalf("restart over the same storage: %v", err)
	}
	restored := compressSyntheticKeysOf(t, restarted, addr.SessionID)
	if strings.Join(keysOfEventIDs(restored), "|") != strings.Join(keysOfEventIDs(online), "|") {
		t.Fatalf("the restart restored the products %v, want the %v the chain holds: the fold form is read from the facts, never re-folded", restored, online)
	}
	if filed := len(compressFactsOf(restarted, addr.SessionID, event.CompressRequestType)); filed != 0 {
		t.Fatalf("the restart filed %d compress request(s), want none: a restore re-derives no judgment", filed)
	}
	if filed := len(compressFactsOf(restarted, addr.SessionID, event.CompressProductType)); filed != len(products) {
		t.Fatalf("the restart holds %d product fact(s), want the %d the chain recorded: a restore mints no second product", filed, len(products))
	}

	if _, err := restarted.Invoke(ctx, assembly.InvokeRequest{
		InvocationID: "inv-compress-probe",
		Address:      addr,
		Input:        []model.Message{{Role: model.RoleUser, Content: "the compress probe input"}},
	}); err != nil {
		t.Fatalf("drive the probe turn after the restart: %v", err)
	}
	recorded := probe.recorded()
	if len(recorded) == 0 {
		t.Fatal("the restarted run served no model request, so the restored projection never reached the wire")
	}
	wire := compressWireOf(recorded[0])
	carried := 0
	for _, product := range products {
		if product.Text != "" && strings.Contains(wire, product.Text) {
			carried++
		}
	}
	if carried == 0 {
		t.Fatalf("the request after the restart carries none of the %d restored product text(s), so the restore never reached the wire: %v", len(products), compressWireOf(recorded[0]))
	}
	if !strings.Contains(wire, "external_input: ask 00") {
		t.Fatalf("the request after the restart carries no product that consumed the first turn, so the restored form is incomplete: %v", keysOfEventIDs(restored))
	}
	if strings.Contains(wire, compressBody(0, "1-plan")) {
		t.Fatalf("the request after the restart carries the raw process detail %q the fold consumed, so the projection was re-folded or never restored", compressBody(0, "1-plan"))
	}
	t.Logf("[compress 3] consumer: %d product(s) restored key for key (%v); the probe request carries %d of their texts and no raw detail", len(restored), restored, carried)
}

// compressWireOf renders every message content of one served request in message
// order, so a presence check reads exactly what the wire carried.
func compressWireOf(request model.Request) string {
	var out strings.Builder
	for _, message := range request.Messages {
		out.WriteString(message.Content)
		out.WriteByte('\n')
	}
	return out.String()
}

// compressCoversBothKinds reports whether the products state both product kinds:
// the segment fold of a new-band turn and the history summary of the older bands.
func compressCoversBothKinds(products []compressProductFact) bool {
	segment, summary := false, false
	for _, product := range products {
		switch {
		case strings.HasPrefix(string(product.Key), "-seg-"):
			segment = true
		case strings.HasPrefix(string(product.Key), "-hist-"):
			summary = true
		}
	}
	return segment && summary
}

// keysOfProducts renders one product key sequence for a comparison message.
func keysOfProducts(products []compressProductFact) []string {
	out := make([]string, 0, len(products))
	for _, product := range products {
		out = append(out, string(product.Key))
	}
	return out
}

// keysOfEventIDs renders one key sequence for a comparison message.
func keysOfEventIDs(keys []event.EventID) []string {
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, string(key))
	}
	return out
}
