// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
//
// The tests of the semantic band summary: the gist one session's model answers
// for the oldest band, the bound it is cut to, the mechanical form a session
// without a model keeps, and the fact a restart reads the summary back from.
package assembly

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/features/rl"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/memory/replay"
)

// gistAnchorLine is the provenance anchor every mechanical summary line opens
// with: the moment and the recorder generation of the entry it stands for.
var gistAnchorLine = regexp.MustCompile(`^\[\d{2}-\d{2} \d{2}:\d{2}\|gen:(\?|[0-9A-Za-z_.-]+)\] `)

// bandSummaryRequestOpening is the stable opening of every band summary request
// instruction: the byte bound it states varies with the declared budget, so the
// scripted providers recognize a summary request by its opening.
const bandSummaryRequestOpening = "Compress the task-turn reads below"

// fakeSummaryGist answers the gist of one band from a scripted function and
// records the material and the budget it was asked about.
type fakeSummaryGist struct {
	answer   func(material string) (string, error)
	material []string
	budgets  []int
}

// SummaryGist answers the scripted gist of one band.
func (f *fakeSummaryGist) SummaryGist(_ context.Context, _ event.SessionID, material string, budget int) (string, error) {
	f.material = append(f.material, material)
	f.budgets = append(f.budgets, budget)
	return f.answer(material)
}

// mechanicalSummary builds the product a fold mints for the oldest band: one
// anchored line per covered entry, as historySummaryOf writes it.
func mechanicalSummary(t *testing.T) replay.FoldProduct {
	t.Helper()
	const line = "[11-06 12:00|gen:?] external_input: ask"
	return replay.FoldProduct{
		Entry: replay.ProjectionEntry{
			Key:     "-hist-mechanical",
			Type:    replay.ProjectionTypeHistorySummary,
			Summary: "history summary",
			Content: line + "; " + line,
		},
		Covered: []event.EventID{"ask-1", "plan-1"},
		Request: event.CompressRequestFor("assembly.test", "hist-mechanical", "the oldest band is over its volume threshold"),
	}
}

// TestBandSummaryGistReadsTheSessionModel pins the gist a band summary asks the model.
// - the request names the band's mechanical reads beside the instruction that keeps the anchors
// - the answered text is the gist, trimmed of the surrounding whitespace a model adds
func TestBandSummaryGistReadsTheSessionModel(t *testing.T) {
	ctx := context.Background()
	provider := &snapshotTestModel{answer: func(*model.Request) *model.Response {
		return &model.Response{Choices: []model.Choice{{Message: model.Message{
			Role: model.RoleAssistant, Content: "\n[11-06 12:00|gen:?] the band settled on one decision\n",
		}}}, Done: true}
	}}
	run := windowChainRun(t, ctx)
	run.mu.Lock()
	run.natives = map[event.AgentID]NativeAgent{"agent-a": {Model: provider}, "": {Model: provider}}
	run.mu.Unlock()

	face := bandSummaries{run: run}
	gist, err := face.SummaryGist(ctx, "session-gist", "[11-06 12:00|gen:?] external_input: ask", replay.DefaultAgeSummaryTextLimit)
	if err != nil {
		t.Fatalf("SummaryGist: %v", err)
	}
	if gist != "[11-06 12:00|gen:?] the band settled on one decision" {
		t.Fatalf("gist = %q, want the answered text trimmed of the whitespace around it", gist)
	}
	if len(provider.requests) != 1 {
		t.Fatalf("the model saw %d request(s), want the one band summary request", len(provider.requests))
	}
	asked := provider.requests[0].Messages[len(provider.requests[0].Messages)-1].Content
	if !strings.Contains(asked, bandSummaryInstruction(replay.DefaultAgeSummaryTextLimit)) || !strings.Contains(asked, "[11-06 12:00|gen:?] external_input: ask") {
		t.Fatalf("request = %q, want the instruction beside the mechanical reads of the band", asked)
	}
}

// TestBandSummaryGistReportsAMissingModel pins the summary a session beside no model answers.
// - a run with no model for the session answers ErrNoSummaryModel
// - a face beside no run answers the same, so the consumer keeps the mechanical form
func TestBandSummaryGistReportsAMissingModel(t *testing.T) {
	ctx := context.Background()
	if _, err := (bandSummaries{run: &Run{}}).SummaryGist(ctx, "session-bare", "material", replay.DefaultAgeSummaryTextLimit); !errors.Is(err, ErrNoSummaryModel) {
		t.Fatalf("err = %v, want ErrNoSummaryModel beside a run that names no model", err)
	}
	if _, err := (bandSummaries{}).SummaryGist(ctx, "session-bare", "material", replay.DefaultAgeSummaryTextLimit); !errors.Is(err, ErrNoSummaryModel) {
		t.Fatalf("err = %v, want ErrNoSummaryModel beside no run at all", err)
	}
}

// TestCompressionSummaryRecordsTheGist pins the summary text a modeled session records.
// - the gist replaces the mechanical text of the summary the fold minted
// - a gist past the summary bound is cut to it, so the fact stays bounded
// - a product that is not the history summary keeps its text, so a segment fold is never reworded
func TestCompressionSummaryRecordsTheGist(t *testing.T) {
	ctx := context.Background()
	consumer := NewCompressionConsumer(nil).withSummaryGist(&fakeSummaryGist{
		answer: func(string) (string, error) {
			return strings.Repeat("[11-06 12:00|gen:?] one settled decision\n", 200), nil
		},
	})
	product := mechanicalSummary(t)
	got := consumer.summarizeProduct(ctx, "session-gist", product, replay.DefaultAgeSummaryTextLimit)
	if len(got.Entry.Content) > replay.DefaultAgeSummaryTextLimit {
		t.Fatalf("summary carries %d bytes, want at most %d: a gist answers the bound the mechanical form obeys", len(got.Entry.Content), replay.DefaultAgeSummaryTextLimit)
	}
	if len(got.Entry.Content) < replay.DefaultAgeSummaryTextLimit-100 {
		t.Fatalf("summary carries %d bytes, want the gist cut at the bound instead of dropped", len(got.Entry.Content))
	}
	if got.Entry.Key != product.Entry.Key || got.Entry.Type != product.Entry.Type {
		t.Fatalf("summary = %+v, want the identity of the product the fold minted", got.Entry)
	}

	segment := replay.FoldProduct{Entry: replay.ProjectionEntry{
		Key: "-seg-mechanical", Type: replay.ProjectionTypeSegmentFold, Content: "[11-06 12:00|gen:?] thinking_plan: plan",
	}}
	if kept := consumer.summarizeProduct(ctx, "session-gist", segment, replay.DefaultAgeSummaryTextLimit); kept.Entry.Content != segment.Entry.Content {
		t.Fatalf("segment fold = %q, want its own text kept: only the history summary is the model's", kept.Entry.Content)
	}
}

// TestCompressionSummaryKeepsTheMechanicalForm pins the summary an unanswered gist keeps.
// - a failed gist keeps the mechanical text the fold minted
// - an empty answer keeps it too, so a summary is never empty
func TestCompressionSummaryKeepsTheMechanicalForm(t *testing.T) {
	ctx := context.Background()
	for name, answer := range map[string]func(string) (string, error){
		"the model failed": func(string) (string, error) {
			return "", errors.New("the provider refused the request")
		},
		"the model answered nothing": func(string) (string, error) {
			return "   \n", nil
		},
	} {
		t.Run(name, func(t *testing.T) {
			consumer := NewCompressionConsumer(nil).withSummaryGist(&fakeSummaryGist{answer: answer})
			product := mechanicalSummary(t)
			got := consumer.summarizeProduct(ctx, "session-gist", product, replay.DefaultAgeSummaryTextLimit)
			if got.Entry.Content != product.Entry.Content {
				t.Fatalf("summary = %q, want the mechanical text %q kept", got.Entry.Content, product.Entry.Content)
			}
			for _, line := range strings.Split(got.Entry.Content, "; ") {
				if !gistAnchorLine.MatchString(line) {
					t.Fatalf("summary line = %q, want an anchored line: the mechanical form is what a degraded summary records", line)
				}
			}
		})
	}
	if consumer := NewCompressionConsumer(nil); consumer.summarizeProduct(context.Background(), "session-gist", mechanicalSummary(t), replay.DefaultAgeSummaryTextLimit).Entry.Content != mechanicalSummary(t).Entry.Content {
		t.Fatal("a consumer beside no gist face reworded the summary, want the mechanical form")
	}
}

// TestCompressionSummaryFactReplaysTheSameText pins the summary a restart reads.
// - the product fact carries the gist the consumer recorded
// - a projection restored from that fact carries the same text, so the recovery is the recorded summary and not a second generation
func TestCompressionSummaryFactReplaysTheSameText(t *testing.T) {
	ctx := context.Background()
	product := mechanicalSummary(t)
	recorded := "the band settled on one decision"
	product.Entry.Content = recorded
	minted := event.CompressProductFor(product.Request.ID, compressionProducer, product.Entry.Key, product.Covered, product.Entry.Content)
	if minted.FactKey() == "" {
		t.Fatal("the product carries no identity")
	}
	fact := compressProductEvent("session-gist", "agent-a", minted, time.Now().UTC())
	restored, ok := compressProductOf(fact)
	if !ok {
		t.Fatalf("the product fact was refused: %+v", fact)
	}
	projection := replay.NewReplayProjection()
	for _, key := range product.Covered {
		projection.Append(ctx, replay.ProjectionEntry{Key: key, Type: event.TypeExternalInput, Summary: "ask", Content: "ask"})
	}
	projection.RestoreProducts([]event.CompressProduct{restored})
	var summary *replay.ProjectionEntry
	for i, entry := range projection.Entries() {
		if entry.Type == replay.ProjectionTypeHistorySummary {
			summary = &projection.Entries()[i]
		}
	}
	if summary == nil {
		t.Fatalf("projection = %+v, want the summary the fact recorded", projection.Entries())
	}
	if summary.Content != recorded {
		t.Fatalf("restored summary = %q, want the recorded text %q: a replay reads the stored summary instead of generating it again", summary.Content, recorded)
	}
}

// summaryGistTurns is the material the end-to-end gist fixtures commit: enough
// turns for the fold to promote a band twice, which is the generation whose
// product is the history summary.
const summaryGistTurns = 24

// gistModelAnswer is the gist the scripted model answers one band summary with.
const gistModelAnswer = "[11-06 13:00|gen:?] the band settled on one decision"

// gistAnsweringModel is the scripted provider of a run that answers the band
// summary: the gist for the summary request and the plain turn answer beside it.
func gistAnsweringModel() *snapshotTestModel {
	return &snapshotTestModel{answer: func(req *model.Request) *model.Response {
		content := factWindowAnswer
		if last := req.Messages[len(req.Messages)-1].Content; strings.Contains(last, bandSummaryRequestOpening) {
			content = gistModelAnswer
		}
		return &model.Response{Choices: []model.Choice{{Message: model.Message{
			Role: model.RoleAssistant, Content: content,
		}}}, Done: true}
	}}
}

// askedBandSummaries reports the band summary requests one scripted provider saw.
func askedBandSummaries(provider *snapshotTestModel) []model.Request {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	asked := make([]model.Request, 0, len(provider.requests))
	for _, req := range provider.requests {
		if strings.Contains(req.Messages[len(req.Messages)-1].Content, bandSummaryRequestOpening) {
			asked = append(asked, req)
		}
	}
	return asked
}

// refusingGistModel fails the stream of every request it is asked, which is the
// provider failure a band summary degrades on.
type refusingGistModel struct{}

// GenerateContent streams one failed response.
func (refusingGistModel) GenerateContent(context.Context, *model.Request) (<-chan *model.Response, error) {
	stream := make(chan *model.Response, 1)
	stream <- &model.Response{Done: true, Error: &model.ResponseError{
		Message: "the provider refused the request", Type: model.ErrorTypeAPIError,
	}}
	close(stream)
	return stream, nil
}

// Info reports the refused provider's descriptor.
func (refusingGistModel) Info() model.Info { return model.Info{Name: "model-a"} }

// silentGistModel ends every stream beside a terminal, which is the request that
// never completed: the reduction stands no completed terminal for it.
type silentGistModel struct{}

// GenerateContent streams one closed channel that carries no response.
func (silentGistModel) GenerateContent(context.Context, *model.Request) (<-chan *model.Response, error) {
	stream := make(chan *model.Response)
	close(stream)
	return stream, nil
}

// Info reports the silent provider's descriptor.
func (silentGistModel) Info() model.Info { return model.Info{Name: "model-a"} }

// installRunModel swaps the model every agent of one assembled run holds, so a
// fixture states whether the session's agent has a model at all. A nil provider
// is the deployment that holds none.
func installRunModel(t *testing.T, run *Run, provider model.Model) {
	t.Helper()
	run.mu.Lock()
	defer run.mu.Unlock()
	for id, native := range run.natives {
		native.Model = provider
		run.natives[id] = native
	}
}

// historySummaryEntryOf answers the oldest-band summary the projection of one
// session reads.
func historySummaryEntryOf(t *testing.T, ctx context.Context, run *Run, session event.SessionID) replay.ProjectionEntry {
	t.Helper()
	projection, _, carried := run.compress.sessions.ProjectionOf(ctx, session)
	if !carried {
		t.Fatalf("session %s registered no projection", session)
	}
	entries := projection.Entries()
	for _, entry := range entries {
		if entry.Type == replay.ProjectionTypeHistorySummary {
			return entry
		}
	}
	t.Fatalf("projection = %+v, want the history summary the fold minted", entries)
	return replay.ProjectionEntry{}
}

// historySummaryFactTextOf answers the text the chain records for one summary
// product, read back through the fact the execution filed.
func historySummaryFactTextOf(t *testing.T, ctx context.Context, run *Run, key event.EventID) string {
	t.Helper()
	text, found := "", false
	if err := run.tier.Segments.EachIndexNewestFirst(ctx, func(factKey event.EventID, _ int, _ int64, typ string, _ event.SessionID) error {
		if found || typ != event.CompressProductType {
			return nil
		}
		stored, held, err := run.tier.Segments.GetEvent(ctx, factKey)
		if err != nil || !held {
			return err
		}
		body, err := memory.DecodeEventBody(stored.Body)
		if err != nil {
			return err
		}
		evt := body.Event()
		evt.Key = stored.Key
		product, stated := compressProductOf(evt)
		if stated && product.Key == key {
			text, found = product.Text, true
		}
		return nil
	}); err != nil {
		t.Fatalf("read the chain for the summary %s: %v", key, err)
	}
	if !found {
		t.Fatalf("the chain files no product for the summary %s", key)
	}
	return text
}

// assertMechanicalSummary pins the bounded mechanical form a degraded summary
// records: anchored lines under the summary bound, never the text of one band.
func assertMechanicalSummary(t *testing.T, text string) {
	t.Helper()
	if text == "" {
		t.Fatal("the summary carries no text")
	}
	if len(text) > replay.DefaultAgeSummaryTextLimit {
		t.Fatalf("summary carries %d bytes, want at most %d", len(text), replay.DefaultAgeSummaryTextLimit)
	}
	for _, line := range strings.Split(text, "; ") {
		if strings.HasPrefix(line, "+") && strings.Contains(line, "covered by key") {
			continue
		}
		if !gistAnchorLine.MatchString(line) {
			t.Fatalf("summary line = %q, want an anchored mechanical line", line)
		}
	}
}

// TestCompressionConsumerFilesTheModelSummary pins the summary a folded modeled session files.
// - the fold asks the session's model for the gist of the band it consumes
// - the projection the model reads carries that gist, not the mechanical form
// - the fact the execution filed carries it too, so the summary is a stored fact
func TestCompressionConsumerFilesTheModelSummary(t *testing.T) {
	ctx := context.Background()
	run := windowChainRun(t, ctx)
	provider := gistAnsweringModel()
	installRunModel(t, run, provider)
	addr := event.Address{AgentID: "agent-a", SessionID: "session-gist-fact"}
	commitWindowTurns(t, ctx, run, addr, summaryGistTurns)

	summary := historySummaryEntryOf(t, ctx, run, addr.SessionID)
	if summary.Content != gistModelAnswer {
		t.Fatalf("summary = %q, want the gist %q", summary.Content, gistModelAnswer)
	}
	if filed := historySummaryFactTextOf(t, ctx, run, summary.Key); filed != gistModelAnswer {
		t.Fatalf("filed summary = %q, want the gist %q the chain records", filed, gistModelAnswer)
	}
	asked := askedBandSummaries(provider)
	if len(asked) == 0 {
		t.Fatal("the model was never asked for the band summary")
	}
	for _, req := range asked {
		if !strings.Contains(req.Messages[len(req.Messages)-1].Content, "[") {
			t.Fatalf("band summary request = %q, want the anchored reads of the band", req.Messages[len(req.Messages)-1].Content)
		}
	}
}

// TestCompressionConsumerKeepsTheMechanicalSummary pins the summary a session beside no model keeps.
// - the projection reads the bounded mechanical form the fold minted
// - the filed fact carries that same form, never an invented summary
func TestCompressionConsumerKeepsTheMechanicalSummary(t *testing.T) {
	ctx := context.Background()
	run := windowChainRun(t, ctx)
	installRunModel(t, run, nil)
	addr := event.Address{AgentID: "agent-a", SessionID: "session-gist-mechanical"}
	commitWindowTurns(t, ctx, run, addr, summaryGistTurns)

	summary := historySummaryEntryOf(t, ctx, run, addr.SessionID)
	assertMechanicalSummary(t, summary.Content)
	if filed := historySummaryFactTextOf(t, ctx, run, summary.Key); filed != summary.Content {
		t.Fatalf("filed summary = %q, want the mechanical text %q the projection reads", filed, summary.Content)
	}
}

// TestCompressionConsumerDegradesAFailedSummary pins the summary a failed gist degrades to.
// - a failed stream and a stream that ended beside a terminal both keep the mechanical form
// - the fold still files its product beside the summary, so a failed gist blocks nothing
func TestCompressionConsumerDegradesAFailedSummary(t *testing.T) {
	ctx := context.Background()
	for name, provider := range map[string]model.Model{
		"the provider failed the stream":     refusingGistModel{},
		"the stream ended beside a terminal": silentGistModel{},
	} {
		t.Run(name, func(t *testing.T) {
			run := windowChainRun(t, ctx)
			installRunModel(t, run, provider)
			addr := event.Address{AgentID: "agent-a", SessionID: "session-gist-degraded"}
			commitWindowTurns(t, ctx, run, addr, summaryGistTurns)

			summary := historySummaryEntryOf(t, ctx, run, addr.SessionID)
			assertMechanicalSummary(t, summary.Content)
			if filed := historySummaryFactTextOf(t, ctx, run, summary.Key); filed != summary.Content {
				t.Fatalf("filed summary = %q, want the mechanical text %q the projection reads", filed, summary.Content)
			}
			if count := countFoldProducts(t, ctx, run); count < 2 {
				t.Fatalf("the fixture filed %d product(s), want the fold products beside the degraded summary", count)
			}
		})
	}
}

// TestCompressionSummaryReplaysTheRecordedGist pins the summary a restart reads.
// - the anchored rebuild of the chain carries the gist the live projection reads
// - the restarting run's model is never asked for a gist, so the recovery reads the recorded summary
func TestCompressionSummaryReplaysTheRecordedGist(t *testing.T) {
	ctx := context.Background()
	run := windowChainRun(t, ctx)
	installRunModel(t, run, gistAnsweringModel())
	addr := event.Address{AgentID: "agent-a", SessionID: "session-gist-replay"}
	commitWindowTurns(t, ctx, run, addr, summaryGistTurns)
	live := historySummaryEntryOf(t, ctx, run, addr.SessionID)

	rebooted := windowChainRun(t, ctx)
	rebootProvider := gistAnsweringModel()
	installRunModel(t, rebooted, rebootProvider)
	seeds, report, err := rebooted.seedAnchoredReplay(ctx, run.tier)
	if err != nil {
		t.Fatalf("rebuild the chain from its anchor: %v", err)
	}
	if report.Anchors != 1 {
		t.Fatalf("the rebuild used %d anchor(s), want the one the fold filed", report.Anchors)
	}
	seed, held := seeds[event.Address{AgentID: addr.AgentID, SessionID: addr.SessionID}]
	if !held {
		t.Fatalf("the rebuild answered no seed for %+v", addr)
	}
	var restored *replay.ProjectionEntry
	for _, entry := range seed.projection.Entries() {
		if entry.Type == replay.ProjectionTypeHistorySummary {
			copied := entry
			restored = &copied
		}
	}
	if restored == nil {
		t.Fatalf("rebuilt projection = %+v, want the recorded summary", seed.projection.Entries())
	}
	if restored.Content != gistModelAnswer {
		t.Fatalf("rebuilt summary = %q, want the recorded gist %q", restored.Content, gistModelAnswer)
	}
	if restored.Content != live.Content {
		t.Fatalf("rebuilt summary = %q, want the text the live projection read %q", restored.Content, live.Content)
	}
	if asked := askedBandSummaries(rebootProvider); len(asked) != 0 {
		t.Fatalf("the rebuild asked the model %d time(s) for a gist, want the recorded summary read instead", len(asked))
	}
}

// TestBandSummaryPromptStatesTheDeclaredBudget pins the instruction a band summary is asked under.
//   - the byte bound the instruction states is the declared budget
//   - no fixed literal bound stands beside a declaration that names another one
//
// 契约: docs/wiki/agent/event-flow.md#unified-compression
func TestBandSummaryPromptStatesTheDeclaredBudget(t *testing.T) {
	instruction := bandSummaryInstruction(512)
	if !strings.Contains(instruction, "within 512 bytes") {
		t.Fatalf("instruction = %q, want it to state the declared 512-byte bound", instruction)
	}
	if strings.Contains(instruction, "2048") {
		t.Fatalf("instruction = %q, want no fixed literal bound beside the declared one", instruction)
	}
}

// TestBandSummaryGistReadsTheLease pins the model face a band summary asks.
//   - an agent holding a model lease is served by the lease's version in effect
//   - the model the host contributed beside that lease is asked nothing
//
// 契约: docs/wiki/rl/rl-architecture.md#swappable-model
func TestBandSummaryGistReadsTheLease(t *testing.T) {
	ctx := context.Background()
	leased := &snapshotTestModel{answer: func(*model.Request) *model.Response {
		return &model.Response{Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "the leased answer"}}}, Done: true}
	}}
	contributed := &snapshotTestModel{answer: func(*model.Request) *model.Response {
		return &model.Response{Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "the contributed answer"}}}, Done: true}
	}}
	lease, err := rl.NewModelLease(leased, "model-a@v1")
	if err != nil {
		t.Fatalf("build the model lease: %v", err)
	}
	run := windowChainRun(t, ctx, WithModelLease("agent-a", lease))
	installRunModel(t, run, contributed)
	session := event.SessionID("session-gist-lease")
	if err := run.attachScopedReplay(event.Address{AgentID: "agent-a", SessionID: session}, FoldWindow{MaxTokens: 100}, replay.NewReplayProjection()); err != nil {
		t.Fatalf("attach the session projection: %v", err)
	}

	gist, err := (bandSummaries{run: run}).SummaryGist(ctx, session, "[11-06 12:00|gen:?] external_input: ask", replay.DefaultAgeSummaryTextLimit)
	if err != nil {
		t.Fatalf("SummaryGist: %v", err)
	}
	if gist != "the leased answer" {
		t.Fatalf("gist = %q, want the leased version's answer: the summary asks the model face the agent's own requests ask", gist)
	}
	if quoted := contributed.requests; len(quoted) != 0 {
		t.Fatalf("the contributed model was asked %d time(s), want the lease to serve the summary", len(quoted))
	}
}

// TestBandSummaryGistCarriesTheDeclaredSampling pins the sampling a band summary request carries.
//   - the temperature and the output limit the agent declares reach the provider although the request itself names none
//
// 契约: docs/wiki/rl/rl-architecture.md#swappable-model
func TestBandSummaryGistCarriesTheDeclaredSampling(t *testing.T) {
	ctx := context.Background()
	temperature := 0.25
	provider := &snapshotTestModel{answer: func(*model.Request) *model.Response {
		return &model.Response{Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "the declared answer"}}}, Done: true}
	}}
	run, err := Assemble(ctx, &config.Declaration{
		Entry: "agent-a",
		Agents: []config.AgentSpec{{
			ID:           "agent-a",
			Model:        "model-a",
			ReplayWindow: 100,
			Temperature:  &temperature,
			MaxTokens:    777,
		}},
	}, map[event.AgentID]NativeAgent{"agent-a": {Model: provider}},
		WithGovernance(snapshotTestGovernance{}),
		WithStorage(ledger.StorageConfig{Type: ledger.StorageMemory}))
	if err != nil {
		t.Fatalf("assemble the run: %v", err)
	}
	session := event.SessionID("session-gist-sampling")
	if err := run.attachScopedReplay(event.Address{AgentID: "agent-a", SessionID: session}, FoldWindow{MaxTokens: 100}, replay.NewReplayProjection()); err != nil {
		t.Fatalf("attach the session projection: %v", err)
	}
	if _, err := (bandSummaries{run: run}).SummaryGist(ctx, session, "[11-06 12:00|gen:?] external_input: ask", replay.DefaultAgeSummaryTextLimit); err != nil {
		t.Fatalf("SummaryGist: %v", err)
	}
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if len(provider.requests) != 1 {
		t.Fatalf("the provider saw %d request(s), want the one band summary request", len(provider.requests))
	}
	asked := provider.requests[0]
	if asked.Temperature == nil || *asked.Temperature != temperature {
		t.Fatalf("summary request temperature = %v, want the declared %v", asked.Temperature, temperature)
	}
	if asked.MaxTokens == nil || *asked.MaxTokens != 777 {
		t.Fatalf("summary request max tokens = %v, want the declared 777", asked.MaxTokens)
	}
}
