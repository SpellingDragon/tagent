package learning

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/cli"
	"github.com/SpellingDragon/tagent/memory/query"
	"github.com/SpellingDragon/tagent/rl"
)

// feedbackCredential is the request credential the feedback service of these
// checks authenticates with.
const feedbackCredential = "feedback-test-credential"

// feedbackReply is the outcome of one raw feedback request: the status code
// beside the response body.
type feedbackReply struct {
	status int
	body   string
}

// sendFeedback issues one raw POST over the RL feedback face with the
// authorization header the caller arms it with, and reports the outcome as it
// stands.
func sendFeedback(base, authorization, body string) (feedbackReply, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest(http.MethodPost, base+"/feedback", strings.NewReader(body))
	if err != nil {
		return feedbackReply{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	resp, err := client.Do(req)
	if err != nil {
		return feedbackReply{}, err
	}
	defer resp.Body.Close()
	reply, err := io.ReadAll(resp.Body)
	if err != nil {
		return feedbackReply{}, err
	}
	return feedbackReply{status: resp.StatusCode, body: string(reply)}, nil
}

// postFeedback issues one feedback request and fails the check when the request
// itself could not be made.
func postFeedback(t *testing.T, base, authorization, body string) feedbackReply {
	t.Helper()
	reply, err := sendFeedback(base, authorization, body)
	if err != nil {
		t.Fatalf("post feedback: %v", err)
	}
	return reply
}

// feedbackBody encodes one feedback submission over the declared wire contract.
func feedbackBody(t *testing.T, eventKey, conclusion string) string {
	t.Helper()
	encoded, err := json.Marshal(rl.FeedbackPayload{EventKey: eventKey, Conclusion: conclusion})
	if err != nil {
		t.Fatalf("encode the feedback payload: %v", err)
	}
	return string(encoded)
}

// decodeFeedbackSubmission decodes one accepted submission reply.
func decodeFeedbackSubmission(t *testing.T, reply feedbackReply) rl.FeedbackSubmission {
	t.Helper()
	var submission rl.FeedbackSubmission
	if err := json.Unmarshal([]byte(reply.body), &submission); err != nil {
		t.Fatalf("decode the feedback submission %q: %v", reply.body, err)
	}
	return submission
}

// attemptBinding is one stored attempt beside the decision it observed.
type attemptBinding struct {
	attempt  event.AttemptID
	decision event.DecisionID
}

// feedbackService is one assembled runtime beside its sealed capture, the two
// feedback accounts bound to that capture's records and the host HTTP face
// serving the feedback endpoint over a real listener.
type feedbackService struct {
	base      string
	src       *rl.CaptureExportSource
	feedback  *rl.FeedbackLedger
	rewards   *rl.RewardLedger
	endpoint  *rl.FeedbackEndpoint
	decisions []event.DecisionID
	attempts  []attemptBinding
	run       *tagent.Run
}

// newFeedbackService assembles one formal run, seals the capture its records are
// written to, and serves the feedback endpoint over that capture behind the host
// authentication gate under the credential and request bounds the caller names.
func newFeedbackService(t *testing.T, ctx context.Context, token string, limits rl.RequestLimits) *feedbackService {
	t.Helper()
	dir := t.TempDir()
	writer, err := rl.NewFileCaptureWriter(dir, 0)
	if err != nil {
		t.Fatalf("build the capture writer: %v", err)
	}
	sink, err := rl.NewCaptureSink("cap-feedback", rl.SinkConfig{}, writer)
	if err != nil {
		t.Fatalf("build the capture sink: %v", err)
	}
	run := formalRun(t, ctx, sink, exportAnswer)
	turn := formalInvoke(t, ctx, run, "inv-feedback", "session-feedback", "ep-feedback")
	if turn.Terminal != model.KindCompleted || len(turn.Decisions) != 2 {
		t.Fatalf("turn = (%v, %d decisions), want the completed turn of two decisions driving the record writes", turn.Terminal, len(turn.Decisions))
	}
	if _, err := sink.Seal(ctx); err != nil {
		t.Fatalf("seal the formal capture: %v", err)
	}
	src, err := rl.NewCaptureExportSource(dir)
	if err != nil {
		t.Fatalf("build the export source over the sealed capture: %v", err)
	}
	service := &feedbackService{src: src, feedback: rl.NewFeedbackLedger(), rewards: rl.NewRewardLedger(), run: run}
	for _, stored := range storedRecordsOf(t, dir) {
		switch stored.line.Kind {
		case rl.RecordKindDecision:
			service.decisions = append(service.decisions, stored.line.Decision.ID)
		case rl.RecordKindAttempt:
			service.attempts = append(service.attempts, attemptBinding{attempt: stored.line.Attempt.ID, decision: stored.line.Attempt.DecisionID})
		}
	}
	if len(service.decisions) != 2 || len(service.attempts) != 2 {
		t.Fatalf("stored records = (%d decisions, %d attempts), want the formal chain's two of each", len(service.decisions), len(service.attempts))
	}
	endpoint, err := rl.NewFeedbackEndpoint(
		rl.NewCaptureFeedbackResolver(src),
		service.feedback,
		service.rewards,
		exportTestRule(),
		rl.WithFeedbackToken(token),
		rl.WithFeedbackLimits(limits),
	)
	if err != nil {
		t.Fatalf("build the feedback endpoint: %v", err)
	}
	service.endpoint = endpoint
	shell := cli.NewCLI(run, query.AuthorizationScope{
		Agents:   []event.AgentID{"agent-a"},
		Sessions: []event.Address{{AgentID: "agent-a", SessionID: "session-feedback"}},
	}, cli.WithRequestCredential(token), cli.WithFeedbackEndpoint(endpoint))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	serveCtx, stopServe := context.WithCancel(ctx)
	serving := make(chan error, 1)
	go func() { serving <- shell.Serve(serveCtx, listener) }()
	stopped := false
	t.Cleanup(func() {
		if stopped {
			return
		}
		stopped = true
		stopServe()
		if err := <-serving; err != nil {
			t.Errorf("serve: %v", err)
		}
	})
	service.base = "http://" + listener.Addr().String()
	return service
}

// TestFeedbackHTTPContract drives the RL feedback HTTP face over the formal chain and checks its contract.
//   - a request without a matched credential is refused by name and has no side effect
//   - an authenticated submission binds to the decision or attempt the sealed capture records under its event key
//   - a late submission derives a new revision and leaves every earlier revision exactly as it was
//   - a repeated delivery of the same conclusion derives nothing new
//   - a submission naming no key is unbound, an unknown key is refused by name and conflicting evidence is ambiguous
//   - concurrent submissions keep their own attribution and attribution reads back unchanged after a restart
//   - every redirect hop is rechecked against the same allowlist and a body over the bound is refused
//
// 契约: docs/wiki/rl/rl-architecture.md#http-api
func TestFeedbackHTTPContract(t *testing.T) {
	t.Run("无认证具名拒且零副作用: an unauthenticated submission is refused by name and writes nothing", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		service := newFeedbackService(t, ctx, feedbackCredential, rl.RequestLimits{})
		body := feedbackBody(t, string(service.decisions[0]), "good")
		for _, authorization := range []string{"", "Bearer not-the-credential", "Basic " + feedbackCredential} {
			reply := postFeedback(t, service.base, authorization, body)
			if reply.status != http.StatusUnauthorized {
				t.Fatalf("authorization %q status = %d (%s), want the 401 named refusal", authorization, reply.status, reply.body)
			}
			if !strings.Contains(reply.body, "the request subject is not authenticated") {
				t.Fatalf("authorization %q refusal = %q, want the named denial", authorization, reply.body)
			}
		}
		if records := service.feedback.All(); len(records) != 0 {
			t.Fatalf("refused submissions wrote %d feedback records", len(records))
		}
		if attribution := service.endpoint.Attribution(); attribution != (rl.FeedbackAttribution{}) {
			t.Fatalf("refused submissions were attributed: %+v", attribution)
		}
		if records := service.rewards.Records(service.decisions[0], ""); len(records) != 0 {
			t.Fatalf("refused submissions derived %d rewards", len(records))
		}
	})

	t.Run("带认证可达并绑定到真实 decision 与 attempt: an authenticated submission binds to the recorded identity", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		service := newFeedbackService(t, ctx, feedbackCredential, rl.RequestLimits{})
		decision := service.decisions[0]
		reply := postFeedback(t, service.base, "Bearer "+feedbackCredential, feedbackBody(t, string(decision), "good"))
		if reply.status != http.StatusOK {
			t.Fatalf("bound submission status = %d (%s), want the accepted binding", reply.status, reply.body)
		}
		submission := decodeFeedbackSubmission(t, reply)
		if submission.State != rl.FeedbackBound || submission.DecisionID != decision || submission.AttemptID != "" {
			t.Fatalf("submission = %+v, want a decision boundary binding to %q", submission, decision)
		}
		if submission.FeedbackID == "" || submission.RewardInstanceID == "" || submission.RewardRevision != 1 || submission.RewardVersion != "reward.rule.v1" {
			t.Fatalf("submission = %+v, want the feedback identity beside the first reward revision", submission)
		}
		records := service.feedback.BoundTo(string(decision))
		if len(records) != 1 {
			t.Fatalf("bound feedback records = %d, want exactly one under the event key", len(records))
		}
		record := records[0]
		if record.Schema != rl.SchemaCurrent || record.Source != rl.FeedbackSourceAPI ||
			record.Version != rl.FeedbackVersionCurrent || record.Conclusion != "good" ||
			record.DecisionID != decision || record.RecordedAt.IsZero() {
			t.Fatalf("feedback record = %+v, want the api sourced versioned record of the recorded decision", record)
		}
		reward, held := service.rewards.Latest(decision, "")
		if !held || reward.State != rl.RewardRated || reward.Value != 1 || reward.RuleVersion != "reward.rule.v1" {
			t.Fatalf("decision reward = (%+v, %v), want the rated value derived under the pinned rule", reward, held)
		}
		if len(reward.DerivedFrom) != 1 || reward.DerivedFrom[0].FeedbackID != record.FeedbackID {
			t.Fatalf("reward sources = %+v, want the feedback it was derived from", reward.DerivedFrom)
		}

		attempt := service.attempts[1]
		reply = postFeedback(t, service.base, "Bearer "+feedbackCredential, feedbackBody(t, string(attempt.attempt), "bad"))
		if reply.status != http.StatusOK {
			t.Fatalf("attempt submission status = %d (%s), want the accepted binding", reply.status, reply.body)
		}
		submission = decodeFeedbackSubmission(t, reply)
		if submission.State != rl.FeedbackBound || submission.DecisionID != attempt.decision || submission.AttemptID != attempt.attempt {
			t.Fatalf("attempt submission = %+v, want the attempt binding of (%q, %q)", submission, attempt.decision, attempt.attempt)
		}
		attemptReward, held := service.rewards.Latest(attempt.decision, attempt.attempt)
		if !held || attemptReward.State != rl.RewardRated || attemptReward.Value != -1 {
			t.Fatalf("attempt reward = (%+v, %v), want the rated value of the attempt evidence", attemptReward, held)
		}

		named := rl.FeedbackPayload{EventKey: string(decision), DecisionID: service.decisions[1], Conclusion: "good"}
		encoded, err := json.Marshal(named)
		if err != nil {
			t.Fatalf("encode the conflicting payload: %v", err)
		}
		reply = postFeedback(t, service.base, "Bearer "+feedbackCredential, string(encoded))
		if reply.status != http.StatusConflict || !strings.Contains(reply.body, "conflicting decisions or attempts") {
			t.Fatalf("conflicting submission = (%d, %s), want the 409 named conflict", reply.status, reply.body)
		}
		if records := service.feedback.BoundTo(string(decision)); len(records) != 1 {
			t.Fatalf("the conflicting submission wrote a record: %+v", records)
		}
		if attribution := service.endpoint.Attribution(); attribution.Bound != 2 || attribution.Ambiguous != 1 {
			t.Fatalf("attribution = %+v, want two bound submissions beside one ambiguous", attribution)
		}
	})

	t.Run("延迟反馈派生新版本且不改旧版本: a late submission derives a new revision and leaves the recorded one as it was", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		service := newFeedbackService(t, ctx, feedbackCredential, rl.RequestLimits{})
		exporter := rl.NewTrainingExporter(service.src, service.feedback, service.rewards, exportTestRule())

		first, err := exporter.Export(ctx, rl.ExportRequest{PartitionIDs: []string{service.src.CaptureID()}})
		if err != nil || first.Written != 4 {
			t.Fatalf("first export = (%+v, %v), want the four samples of the formal capture", first, err)
		}
		firstLines, held := exporter.Snapshot(first.Manifest.ExportID)
		if !held || len(firstLines) != 4 {
			t.Fatalf("first snapshot holds %d lines (held=%v), want four", len(firstLines), held)
		}
		sampled := firstLines[0]
		prior := sampled.Reward
		if prior.State != rl.RewardUnrated || prior.Revision != 1 || prior.InstanceID == "" {
			t.Fatalf("a sample beside no feedback must carry a pinned unrated instance: %+v", prior)
		}
		decision := sampled.Provenance.DecisionID

		reply := postFeedback(t, service.base, "Bearer "+feedbackCredential, feedbackBody(t, string(decision), "good"))
		if reply.status != http.StatusOK {
			t.Fatalf("late submission status = %d (%s), want the accepted binding", reply.status, reply.body)
		}
		late := decodeFeedbackSubmission(t, reply)
		if late.State != rl.FeedbackBound || late.RewardRevision != 2 {
			t.Fatalf("late submission = %+v, want the revision derived beside the recorded unrated one", late)
		}

		second, err := exporter.Export(ctx, rl.ExportRequest{PartitionIDs: []string{service.src.CaptureID()}})
		if err != nil || second.Written != 4 {
			t.Fatalf("second export = (%+v, %v), want the four samples over the late evidence", second, err)
		}
		secondLines, _ := exporter.Snapshot(second.Manifest.ExportID)
		citations := map[string]rl.RewardRecord{}
		for _, line := range secondLines {
			citations[string(line.Provenance.DecisionID)+"|"+string(line.Provenance.AttemptID)] = line.Reward
		}
		later, held := citations[string(sampled.Provenance.DecisionID)+"|"+string(sampled.Provenance.AttemptID)]
		if !held {
			t.Fatalf("the second export holds no sample for the sampled boundary %+v", sampled.Provenance)
		}
		if later.State != rl.RewardRated || later.Revision != 2 {
			t.Fatalf("late feedback must derive a new revision: %+v", later)
		}
		if later.InstanceID == prior.InstanceID {
			t.Fatalf("late feedback reused the instance %q", later.InstanceID)
		}
		if len(later.DerivedFrom) != 1 || later.DerivedFrom[0].FeedbackID != late.FeedbackID {
			t.Fatalf("later reward sources = %+v, want the late feedback it was derived from", later.DerivedFrom)
		}
		history := service.rewards.Records(decision, sampled.Provenance.AttemptID)
		if len(history) != 2 || history[0].Revision != 1 || history[0].State != rl.RewardUnrated || history[0].Value != 0 {
			t.Fatalf("the recorded derivation was rewritten: %+v", history)
		}
		firstAgain, _ := exporter.Snapshot(first.Manifest.ExportID)
		if firstAgain[0].Reward.InstanceID != prior.InstanceID || firstAgain[0].Reward.Revision != 1 {
			t.Fatalf("the first snapshot's citation moved: %+v", firstAgain[0].Reward)
		}

		repeat := postFeedback(t, service.base, "Bearer "+feedbackCredential, feedbackBody(t, string(decision), "good"))
		if repeat.status != http.StatusOK {
			t.Fatalf("repeated submission status = %d (%s), want the idempotent acceptance", repeat.status, repeat.body)
		}
		if again := decodeFeedbackSubmission(t, repeat); again.FeedbackID != late.FeedbackID || again.RewardRevision != late.RewardRevision {
			t.Fatalf("repeated submission = %+v, want the same feedback and derivation as %+v", again, late)
		}
		third, err := exporter.Export(ctx, rl.ExportRequest{PartitionIDs: []string{service.src.CaptureID()}})
		if err != nil {
			t.Fatalf("third export: %v", err)
		}
		if history := service.rewards.Records(decision, sampled.Provenance.AttemptID); len(history) != 2 {
			t.Fatalf("a repeated delivery derived a new revision: %+v", history)
		}
		thirdLines, _ := exporter.Snapshot(third.Manifest.ExportID)
		for _, line := range thirdLines {
			key := string(line.Provenance.DecisionID) + "|" + string(line.Provenance.AttemptID)
			if cited, held := citations[key]; held && line.Reward.InstanceID != cited.InstanceID {
				t.Fatalf("unchanged evidence moved the cited instance: %+v vs %+v", line.Reward, cited)
			}
		}
	})

	t.Run("无证据标 unbound 且结论缺失具名拒: a submission without a reference is unbound and never invents a reward", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		service := newFeedbackService(t, ctx, feedbackCredential, rl.RequestLimits{})
		reply := postFeedback(t, service.base, "Bearer "+feedbackCredential, feedbackBody(t, "", "good"))
		if reply.status != http.StatusOK {
			t.Fatalf("unbound submission status = %d (%s), want the explicit unbound answer", reply.status, reply.body)
		}
		submission := decodeFeedbackSubmission(t, reply)
		if submission.State != rl.FeedbackUnbound || submission.FeedbackID != "" || submission.RewardInstanceID != "" {
			t.Fatalf("submission = %+v, want an unbound answer carrying no feedback or reward identity", submission)
		}
		if records := service.feedback.All(); len(records) != 0 {
			t.Fatalf("the unbound submission wrote %d feedback records", len(records))
		}
		for _, decision := range service.decisions {
			if records := service.rewards.Records(decision, ""); len(records) != 0 {
				t.Fatalf("the unbound submission derived a reward for %q: %+v", decision, records)
			}
		}
		if attribution := service.endpoint.Attribution(); attribution.Unbound != 1 || attribution.Bound != 0 {
			t.Fatalf("attribution = %+v, want one unbound submission", attribution)
		}

		reply = postFeedback(t, service.base, "Bearer "+feedbackCredential, feedbackBody(t, string(service.decisions[0]), "  "))
		if reply.status != http.StatusBadRequest || !strings.Contains(reply.body, "carries no conclusion") {
			t.Fatalf("conclusionless submission = (%d, %s), want the 400 named refusal", reply.status, reply.body)
		}
		if records := service.feedback.All(); len(records) != 0 {
			t.Fatalf("the conclusionless submission wrote %d feedback records", len(records))
		}
	})

	t.Run("event_key 不存在具名拒绝且零写入: an unknown event key is refused rather than guessed", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		service := newFeedbackService(t, ctx, feedbackCredential, rl.RequestLimits{})
		reply := postFeedback(t, service.base, "Bearer "+feedbackCredential, feedbackBody(t, "dec-not-recorded", "good"))
		if reply.status != http.StatusNotFound || !strings.Contains(reply.body, "no recorded event carries the feedback event key") {
			t.Fatalf("unknown key submission = (%d, %s), want the 404 named refusal", reply.status, reply.body)
		}
		if records := service.feedback.All(); len(records) != 0 {
			t.Fatalf("the unknown key submission wrote %d feedback records", len(records))
		}
		if attribution := service.endpoint.Attribution(); attribution.Bound != 0 || attribution.Unbound != 0 {
			t.Fatalf("the unknown key submission was attributed: %+v", attribution)
		}
	})

	t.Run("并发反馈各自归属: concurrent submissions keep their own attribution", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		service := newFeedbackService(t, ctx, feedbackCredential, rl.RequestLimits{})
		type outcome struct {
			decision event.DecisionID
			reply    feedbackReply
			err      error
		}
		outcomes := make([]outcome, 4)
		var wg sync.WaitGroup
		for i := range outcomes {
			decision := service.decisions[i%len(service.decisions)]
			conclusion := fmt.Sprintf("case-%d", i)
			wg.Add(1)
			go func(i int, decision event.DecisionID, conclusion string) {
				defer wg.Done()
				encoded, err := json.Marshal(rl.FeedbackPayload{EventKey: string(decision), Conclusion: conclusion})
				if err != nil {
					outcomes[i] = outcome{decision: decision, err: err}
					return
				}
				reply, err := sendFeedback(service.base, "Bearer "+feedbackCredential, string(encoded))
				outcomes[i] = outcome{decision: decision, reply: reply, err: err}
			}(i, decision, conclusion)
		}
		wg.Wait()
		for i, got := range outcomes {
			if got.err != nil {
				t.Fatalf("concurrent submission %d: %v", i, got.err)
			}
			if got.reply.status != http.StatusOK {
				t.Fatalf("concurrent submission %d status = %d (%s)", i, got.reply.status, got.reply.body)
			}
			if submission := decodeFeedbackSubmission(t, got.reply); submission.State != rl.FeedbackBound || submission.DecisionID != got.decision {
				t.Fatalf("concurrent submission %d = %+v, want a binding to %q", i, submission, got.decision)
			}
		}
		cited := map[string]bool{}
		for _, decision := range service.decisions {
			records := service.feedback.BoundTo(string(decision))
			if len(records) != 2 {
				t.Fatalf("decision %q holds %d feedback records, want the two submissions it received", decision, len(records))
			}
			for _, record := range records {
				if record.DecisionID != decision {
					t.Fatalf("a record bound to %q carries decision %q", decision, record.DecisionID)
				}
				cited[record.FeedbackID] = true
			}
			reward, held := service.rewards.Latest(decision, "")
			if !held || reward.State != rl.RewardRated {
				t.Fatalf("decision %q reward = (%+v, %v), want a rated derivation", decision, reward, held)
			}
			derived := map[string]bool{}
			for _, record := range service.rewards.Records(decision, "") {
				for _, source := range record.DerivedFrom {
					derived[source.FeedbackID] = true
				}
			}
			for _, record := range records {
				if !derived[record.FeedbackID] {
					t.Fatalf("decision %q never derived from its feedback %q", decision, record.FeedbackID)
				}
			}
		}
		if len(cited) != 4 {
			t.Fatalf("attributed feedback identities = %v, want the four distinct submissions", cited)
		}
	})

	t.Run("重启后归属与派生版本保持: attribution and derivation versions read back unchanged", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		service := newFeedbackService(t, ctx, feedbackCredential, rl.RequestLimits{})
		decision := service.decisions[0]
		if reply := postFeedback(t, service.base, "Bearer "+feedbackCredential, feedbackBody(t, string(decision), "good")); reply.status != http.StatusOK {
			t.Fatalf("submission status = %d (%s)", reply.status, reply.body)
		}
		bound := service.feedback.BoundTo(string(decision))
		latest, held := service.rewards.Latest(decision, "")
		if !held || len(bound) != 1 || latest.InstanceID == "" {
			t.Fatalf("recorded state = (%d records, reward %+v held=%v), want one bound record beside a pinned reward", len(bound), latest, held)
		}

		rebornFeedback := rl.NewFeedbackLedger()
		for _, record := range service.feedback.All() {
			if err := rebornFeedback.Restore(record); err != nil {
				t.Fatalf("restore feedback %+v: %v", record, err)
			}
		}
		rebornRewards := rl.NewRewardLedger()
		for _, record := range service.rewards.Records(decision, "") {
			if err := rebornRewards.Restore(record); err != nil {
				t.Fatalf("restore reward %+v: %v", record, err)
			}
		}
		if restored := rebornFeedback.BoundTo(string(decision)); !reflect.DeepEqual(restored, bound) {
			t.Fatalf("restored attribution = %+v, want the recorded %+v", restored, bound)
		}
		restoredReward, held := rebornRewards.Latest(decision, "")
		if !held || restoredReward.InstanceID != latest.InstanceID || restoredReward.Revision != latest.Revision ||
			restoredReward.RuleVersion != latest.RuleVersion || !reflect.DeepEqual(restoredReward.DerivedFrom, latest.DerivedFrom) {
			t.Fatalf("restored reward = (%+v, %v), want the recorded instance %+v", restoredReward, held, latest)
		}
		tampered := bound[0]
		tampered.Conclusion = "different"
		if err := rebornFeedback.Restore(tampered); !errors.Is(err, rl.ErrFeedbackConflict) {
			t.Fatalf("tampered restore = %v, want the named conflict", err)
		}
	})

	t.Run("逐跳重定向按同一 allowlist 复核: every redirect hop is rechecked before it is followed", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var outsideHits int64
		outside := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			atomic.AddInt64(&outsideHits, 1)
			io.WriteString(w, "outside")
		}))
		defer outside.Close()
		outsideURL := strings.Replace(outside.URL, "127.0.0.1", "localhost", 1)
		redirecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, outsideURL+"/secret", http.StatusFound)
		}))
		defer redirecting.Close()
		client, err := rl.NewEndpointClient(rl.EndpointAllowlist{
			Hosts: []string{"127.0.0.1"}, Schemes: []string{"http"}, MaxRedirects: 3,
		}, rl.RequestLimits{})
		if err != nil {
			t.Fatalf("build the endpoint client: %v", err)
		}
		if _, err := client.Get(ctx, redirecting.URL); !errors.Is(err, rl.ErrEndpointRedirect) {
			t.Fatalf("redirect outside the allowlist = %v, want the named hop refusal", err)
		}
		if hits := atomic.LoadInt64(&outsideHits); hits != 0 {
			t.Fatalf("the refused hop reached its destination %d times", hits)
		}

		var landing int64
		var landingURL string
		allowlisted := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/start" {
				http.Redirect(w, r, landingURL+"/landing", http.StatusFound)
				return
			}
			atomic.AddInt64(&landing, 1)
			io.WriteString(w, "landed")
		}))
		defer allowlisted.Close()
		landingURL = allowlisted.URL
		body, err := client.Get(ctx, allowlisted.URL+"/start")
		if err != nil || string(body) != "landed" || atomic.LoadInt64(&landing) != 1 {
			t.Fatalf("allowlisted hop = (%q, %v, %d landings), want the answer of the rechecked hop", body, err, landing)
		}

		noHops, err := rl.NewEndpointClient(rl.EndpointAllowlist{
			Hosts: []string{"127.0.0.1"}, Schemes: []string{"http"}, MaxRedirects: 0,
		}, rl.RequestLimits{})
		if err != nil {
			t.Fatalf("build the hop-less client: %v", err)
		}
		if _, err := noHops.Get(ctx, allowlisted.URL+"/start"); !errors.Is(err, rl.ErrEndpointRedirectLimit) {
			t.Fatalf("hop beside a zero hop count = %v, want the named chain refusal", err)
		}
		if atomic.LoadInt64(&landing) != 1 {
			t.Fatalf("the refused hop was followed: %d landings", landing)
		}
	})

	t.Run("响应体大小限制: a response body over the bound is refused with its reason", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		big := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			io.WriteString(w, strings.Repeat("x", 4096))
		}))
		defer big.Close()
		allowlist := rl.EndpointAllowlist{Hosts: []string{"127.0.0.1"}, Schemes: []string{"http"}}
		client, err := rl.NewEndpointClient(allowlist, rl.RequestLimits{MaxResponseBytes: 512})
		if err != nil {
			t.Fatalf("build the bounded client: %v", err)
		}
		if _, err := client.Get(ctx, big.URL); !errors.Is(err, rl.ErrBodyTooLarge) || !strings.Contains(err.Error(), "512") {
			t.Fatalf("oversized response = %v, want the named refusal carrying the 512 byte bound", err)
		}
		small := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			io.WriteString(w, "ok")
		}))
		defer small.Close()
		if body, err := client.Get(ctx, small.URL); err != nil || string(body) != "ok" {
			t.Fatalf("bounded response = (%q, %v), want the body inside the bound", body, err)
		}
	})

	t.Run("请求体超限 413 且零副作用: a request body over the bound is refused and writes nothing", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		service := newFeedbackService(t, ctx, feedbackCredential, rl.RequestLimits{MaxBodyBytes: 64})
		reply := postFeedback(t, service.base, "Bearer "+feedbackCredential,
			feedbackBody(t, string(service.decisions[0]), strings.Repeat("long conclusion ", 16)))
		if reply.status != http.StatusRequestEntityTooLarge || !strings.Contains(reply.body, "64 byte bound") {
			t.Fatalf("oversized submission = (%d, %s), want the 413 named refusal carrying the bound", reply.status, reply.body)
		}
		if records := service.feedback.All(); len(records) != 0 {
			t.Fatalf("the oversized submission wrote %d feedback records", len(records))
		}
	})

	t.Run("目的地与监听地址校验: a destination outside the allowlist and an open listen address are refused by name", func(t *testing.T) {
		allowlist := rl.EndpointAllowlist{Hosts: []string{"api.example"}, Schemes: []string{"https"}, MaxRedirects: 2}
		if parsed, err := allowlist.Check("https://api.example:8443/v1/models"); err != nil || parsed.Hostname() != "api.example" {
			t.Fatalf("allowlisted destination = (%v, %v), want the parsed URL beside a free port", parsed, err)
		}
		for _, refused := range []struct {
			raw  string
			want error
		}{
			{"http://api.example/v1", rl.ErrEndpointScheme},
			{"ftp://api.example/v1", rl.ErrEndpointScheme},
			{"https://api.example/v1#fragment", rl.ErrEndpointFragment},
			{"https://user:secret@api.example/v1", rl.ErrEndpointUserInfo},
			{"https://other.example/v1", rl.ErrEndpointHost},
			{"https:///v1", rl.ErrEndpointHost},
		} {
			if _, err := allowlist.Check(refused.raw); !errors.Is(err, refused.want) {
				t.Fatalf("destination %q = %v, want %v", refused.raw, err, refused.want)
			}
		}
		if _, err := rl.NewEndpointClient(rl.EndpointAllowlist{Schemes: []string{"https"}}, rl.RequestLimits{}); !errors.Is(err, rl.ErrEndpointHost) {
			t.Fatalf("empty allowlist = %v, want the named refusal", err)
		}
		if _, err := rl.NewEndpointClient(allowlist, rl.RequestLimits{MaxResponseBytes: -1}); !errors.Is(err, rl.ErrRequestLimitsInvalid) {
			t.Fatalf("negative bound = %v, want the named refusal", err)
		}

		for _, admitted := range []struct {
			addr   string
			token  string
			unsafe bool
		}{
			{"127.0.0.1:8089", "", false},
			{"[::1]:8089", "", false},
			{"localhost:8089", "", false},
			{"0.0.0.0:8089", "token", false},
			{"0.0.0.0:8089", "", true},
		} {
			if err := rl.ValidateListenAddr(admitted.addr, admitted.token, admitted.unsafe); err != nil {
				t.Fatalf("listen address %q = %v, want the admitted address", admitted.addr, err)
			}
		}
		for _, refused := range []string{"0.0.0.0:8089", ":8089"} {
			err := rl.ValidateListenAddr(refused, "", false)
			if !errors.Is(err, rl.ErrInsecureListenAddr) {
				t.Fatalf("listen address %q = %v, want the named insecure refusal", refused, err)
			}
			if !strings.Contains(err.Error(), rl.RLTokenEnv) {
				t.Fatalf("refusal = %v, want the token route named", err)
			}
		}
		t.Setenv(rl.RLTokenEnv, " env-token ")
		if token := rl.RLTokenFromEnv(); token != "env-token" {
			t.Fatalf("environment token = %q, want the trimmed token", token)
		}
	})

	t.Run("端点自有关闭与冲突标 ambiguous: the face's own gate closes and conflicting evidence is ambiguous", func(t *testing.T) {
		closed := newFeedbackEndpointOver(t, "", stubFeedbackResolver{})
		direct := httptest.NewServer(closed)
		defer direct.Close()
		reply := feedbackOverFace(t, direct.URL, "Bearer anything", feedbackBody(t, "dec-1", "good"))
		if reply.status != http.StatusUnauthorized || !strings.Contains(reply.body, "the feedback endpoint is closed") {
			t.Fatalf("closed face = (%d, %s), want the named closed refusal", reply.status, reply.body)
		}

		gated := newFeedbackEndpointOver(t, "s3cret", stubFeedbackResolver{})
		direct = httptest.NewServer(gated)
		defer direct.Close()
		for _, authorization := range []string{"", "Bearer wrong", "Basic s3cret"} {
			reply := feedbackOverFace(t, direct.URL, authorization, feedbackBody(t, "dec-1", "good"))
			if reply.status != http.StatusUnauthorized || !strings.Contains(reply.body, "the feedback request is not authenticated") {
				t.Fatalf("authorization %q = (%d, %s), want the named authentication refusal", authorization, reply.status, reply.body)
			}
		}
		if records := gated.ledger.All(); len(records) != 0 {
			t.Fatalf("refused requests wrote %d feedback records", len(records))
		}

		ambiguous := newFeedbackEndpointOver(t, "s3cret", ambiguousFeedbackResolver{})
		direct = httptest.NewServer(ambiguous)
		defer direct.Close()
		reply = feedbackOverFace(t, direct.URL, "Bearer s3cret", feedbackBody(t, "dec-1", "good"))
		if reply.status != http.StatusConflict || !strings.Contains(reply.body, "conflicting decisions or attempts") {
			t.Fatalf("ambiguous evidence = (%d, %s), want the 409 named conflict", reply.status, reply.body)
		}
		if records := ambiguous.ledger.All(); len(records) != 0 {
			t.Fatalf("the ambiguous submission wrote %d feedback records", len(records))
		}
		if attribution := ambiguous.endpoint.Attribution(); attribution.Ambiguous != 1 || attribution.Bound != 0 {
			t.Fatalf("attribution = %+v, want one ambiguous submission", attribution)
		}
	})
}

// feedbackFace is one feedback endpoint served directly, beside the account it
// writes to and the endpoint itself.
type feedbackFace struct {
	ledger   *rl.FeedbackLedger
	endpoint *rl.FeedbackEndpoint
}

// ServeHTTP serves one submission over the endpoint the face holds.
func (f feedbackFace) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.endpoint.ServeHTTP(w, r)
}

// newFeedbackEndpointOver builds one feedback face over a stub resolver and the
// credential the caller names, beside a fresh feedback account.
func newFeedbackEndpointOver(t *testing.T, token string, resolver rl.FeedbackTargetResolver) feedbackFace {
	t.Helper()
	ledger := rl.NewFeedbackLedger()
	endpoint, err := rl.NewFeedbackEndpoint(
		resolver,
		ledger,
		rl.NewRewardLedger(),
		exportTestRule(),
		rl.WithFeedbackToken(token),
	)
	if err != nil {
		t.Fatalf("build the feedback endpoint: %v", err)
	}
	return feedbackFace{ledger: ledger, endpoint: endpoint}
}

// feedbackOverFace issues one raw POST against a directly served feedback face.
func feedbackOverFace(t *testing.T, base, authorization, body string) feedbackReply {
	t.Helper()
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest(http.MethodPost, base, strings.NewReader(body))
	if err != nil {
		t.Fatalf("build the feedback request: %v", err)
	}
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("post feedback: %v", err)
	}
	defer resp.Body.Close()
	reply, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read the feedback reply: %v", err)
	}
	return feedbackReply{status: resp.StatusCode, body: string(reply)}
}

// stubFeedbackResolver resolves every key as unknown evidence.
type stubFeedbackResolver struct{}

// ResolveFeedbackTarget refuses every key as evidence the resolver does not hold.
func (stubFeedbackResolver) ResolveFeedbackTarget(context.Context, string) (rl.FeedbackTarget, error) {
	return rl.FeedbackTarget{}, errors.New("stub: no evidence")
}

// ambiguousFeedbackResolver resolves every key to conflicting evidence.
type ambiguousFeedbackResolver struct{}

// ResolveFeedbackTarget reports the evidence as conflicting for every key.
func (ambiguousFeedbackResolver) ResolveFeedbackTarget(context.Context, string) (rl.FeedbackTarget, error) {
	return rl.FeedbackTarget{}, rl.ErrFeedbackTargetAmbiguous
}
