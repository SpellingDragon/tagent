package learning

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/features/rl"

	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// TestCaptureFidelityAndRedaction drives the request boundary capture and the attempt observation face and checks what they keep:
// - a request snapshot is fixed before the request starts, keeps the request body faithful and is never rebuilt from the options of a finished call
// - a snapshot refused from the options of a finished call returns the zero snapshot and never re-runs the provider option
// - no header, cookie or URL credential reaches a persisted snapshot: header names stay readable and a URL loses only its credential
// - the body text keeps its own clean URL verbatim, and the projection observes the caller's request without editing it
// - the safe projection is configurable per caller and still never rewrites request content
// - an unsupported logprobs state rests on a named declaration on both the write and the read side, and is valid beside it
// - logprobs are present only when returned, and an unknown state is never read as not_returned
// - a real decision records returned logprobs verbatim and shares the one terminal the model reduction derived
// - a failed decision shares the one terminal and keeps the error text with unknown logprobs
// - concurrent request boundaries fix their own snapshots without crossing
// - a snapshot reads back after a restart and rechecks against its reference, and tampered content or an empty reference is refused by name
// - the three logprobs states survive a marshal-parse round trip unchanged
// 契约: docs/wiki/rl/rl-architecture.md#trajectory-capture
func TestCaptureFidelityAndRedaction(t *testing.T) {
	t.Run("a snapshot is fixed before the request starts and a later request change never moves it", func(t *testing.T) {
		temp := 0.5
		maxTokens := 64
		cleanImageURL := "https://cdn.example.com/clean.png"
		body := "ship the report to https://cdn.example.com/plain.txt"
		view := rl.ModelViewRef{
			Model:         "model-a",
			ModelVersion:  "v1",
			PromptVersion: "prompt-v1",
			VisibleTools:  []rl.VersionedName{{Name: "echo-a", Version: "tool-v1"}},
			VisibleSkills: []rl.VersionedName{{Name: "read", Version: "skill-v1"}},
		}
		req := model.Request{
			Messages: []model.Message{
				{Role: model.RoleUser, Content: body},
				{Role: model.RoleAssistant, Content: "working", ToolCalls: []model.ToolCall{learnCall("call-1", "echo-a", `{"x":1}`)}},
				{Role: model.RoleUser, ContentParts: []model.ContentPart{{Type: "image", Image: &trpcmodel.Image{URL: cleanImageURL, Format: "png"}}}},
			},
		}
		req.Temperature = &temp
		req.MaxTokens = &maxTokens
		req.Tools = map[string]trpctool.Tool{"echo-a": &learnTool{name: "echo-a", output: "out-a"}}

		before := mustSnapshotJSON(t, req)
		snap, err := rl.SnapshotRequest(view, req, rl.SnapshotPolicy{})
		if err != nil {
			t.Fatalf("take the request snapshot: %v", err)
		}
		if after := mustSnapshotJSON(t, req); !bytes.Equal(before, after) {
			t.Fatalf("the request changed while its snapshot was taken, want an observation that never rewrites the request")
		}
		if got, want := mustSnapshotJSON(t, snap.Messages), mustSnapshotJSON(t, req.Messages); !bytes.Equal(got, want) {
			t.Fatalf("snapshot messages = %s, want the request messages kept verbatim (%s)", got, want)
		}
		if snap.Messages[0].Content != body || snap.Messages[1].Content != "working" {
			t.Fatalf("snapshot content = (%q, %q), want the business text kept as it stood", snap.Messages[0].Content, snap.Messages[1].Content)
		}
		if snap.Messages[2].ContentParts[0].Image.URL != cleanImageURL {
			t.Fatalf("snapshot clean URL = %q, want %q kept byte-for-byte", snap.Messages[2].ContentParts[0].Image.URL, cleanImageURL)
		}
		if snap.Model != "model-a" || snap.ModelVersion != "v1" || snap.PromptVersion != "prompt-v1" {
			t.Fatalf("snapshot view = (%q, %q, %q), want the view it was declared against", snap.Model, snap.ModelVersion, snap.PromptVersion)
		}
		if len(snap.VisibleTools) != 1 || snap.VisibleTools[0] != (rl.VersionedName{Name: "echo-a", Version: "tool-v1"}) {
			t.Fatalf("snapshot visible tools = %+v, want the versioned tool set pinned", snap.VisibleTools)
		}
		var params map[string]any
		if err := json.Unmarshal(snap.Parameters, &params); err != nil || params["temperature"] != 0.5 {
			t.Fatalf("snapshot parameters = %s (%v), want the actual generation parameters kept", snap.Parameters, err)
		}
		req.Messages[0].Content = "changed after the boundary"
		req.Temperature = new(float64)
		if snap.Messages[0].Content != body {
			t.Fatalf("snapshot content moved to %q, want the boundary copy fixed", snap.Messages[0].Content)
		}
		if err := json.Unmarshal(snap.Parameters, &params); err != nil || params["temperature"] != 0.5 {
			t.Fatalf("snapshot parameters moved to %s, want the boundary parameters fixed", snap.Parameters)
		}
		if _, err := rl.MarshalSnapshot(snap); err != nil {
			t.Fatalf("marshal the fixed snapshot: %v", err)
		}
	})

	t.Run("a request snapshot is refused rather than rebuilt from the options of a finished call", func(t *testing.T) {
		replayed := false
		opt := func(req *model.Request) { replayed = true }
		snap, err := rl.SnapshotFromOptions([]model.Message{{Role: model.RoleUser, Content: "hi"}}, opt)
		if !errors.Is(err, rl.ErrRebuildRefused) {
			t.Fatalf("rebuild from options error = %v, want ErrRebuildRefused", err)
		}
		if !reflect.DeepEqual(snap, rl.RequestSnapshot{}) {
			t.Fatalf("rebuild from options snapshot = %+v, want the zero snapshot beside the refusal", snap)
		}
		if replayed {
			t.Fatalf("the provider option ran while the rebuild was refused, want the options of a finished call never re-run")
		}
	})

	t.Run("no header, cookie or URL credential reaches a persisted snapshot while the request body stays faithful", func(t *testing.T) {
		const (
			secretHeaderAuth  = "sk-live-HEADER-8f3a"
			secretCookie      = "COOKIE-2b7c"
			secretAPIKeyHead  = "APIKEYHEAD-9d1e"
			secretUserinfo    = "USERINFO-4c5d"
			secretQuery       = "QUERY-7e8f"
			secretExtraBase   = "EXTRABASE-0a1b"
			secretExtraKey    = "EXTRAKEY-3c4d"
			secretNestedToken = "NESTED-5e6f"
		)
		body := "deploy notes at https://cdn.example.com/notes.txt"
		fileURL := "https://files.example.com/report.pdf"
		req := model.Request{
			Messages: []model.Message{
				{Role: model.RoleUser, Content: body},
				{Role: model.RoleUser, ContentParts: []model.ContentPart{
					{Type: "image", Image: &trpcmodel.Image{
						URL:    "https://alice:" + secretUserinfo + "@cdn.example.com/img.png?api_key=" + secretQuery + "&size=2",
						Format: "png",
					}},
					{Type: "file", File: &trpcmodel.File{URL: fileURL, MimeType: "application/pdf"}},
				}},
			},
			Headers: map[string]string{
				"Authorization": "Bearer " + secretHeaderAuth,
				"Cookie":        secretCookie,
				"X-Api-Key":     secretAPIKeyHead,
			},
			ExtraFields: map[string]any{
				"base_url": "https://bob:" + secretExtraBase + "@proxy.example.com/v1",
				"api_key":  secretExtraKey,
				"region":   "us-east-1",
				"nested":   map[string]any{"token": secretNestedToken},
			},
		}
		before := mustSnapshotJSON(t, req)
		snap, err := rl.SnapshotRequest(rl.ModelViewRef{Model: "model-a"}, req, rl.SnapshotPolicy{})
		if err != nil {
			t.Fatalf("take the request snapshot: %v", err)
		}
		content, err := rl.MarshalSnapshot(snap)
		if err != nil {
			t.Fatalf("marshal the snapshot: %v", err)
		}
		for name, secret := range map[string]string{
			"header authorization": secretHeaderAuth,
			"cookie":               secretCookie,
			"header api key":       secretAPIKeyHead,
			"url userinfo":         secretUserinfo,
			"url query key":        secretQuery,
			"extra base url":       secretExtraBase,
			"extra api key":        secretExtraKey,
			"nested token":         secretNestedToken,
		} {
			if bytes.Contains(content, []byte(secret)) {
				t.Fatalf("the %s credential %q reached the persisted snapshot: %s", name, secret, content)
			}
		}
		if snap.Headers["Authorization"] != rl.RedactedValue || snap.Headers["Cookie"] != rl.RedactedValue || snap.Headers["X-Api-Key"] != rl.RedactedValue {
			t.Fatalf("snapshot headers = %+v, want every value projected to %q with the names kept", snap.Headers, rl.RedactedValue)
		}
		wantImage := "https://cdn.example.com/img.png?api_key=" + rl.RedactedValue + "&size=2"
		if got := snap.Messages[1].ContentParts[0].Image.URL; got != wantImage {
			t.Fatalf("snapshot image URL = %q, want %q", got, wantImage)
		}
		if got := snap.Messages[1].ContentParts[1].File.URL; got != fileURL {
			t.Fatalf("snapshot clean file URL = %q, want %q kept byte-for-byte", got, fileURL)
		}
		if snap.Messages[0].Content != body {
			t.Fatalf("snapshot body = %q, want %q kept verbatim", snap.Messages[0].Content, body)
		}
		if snap.ExtraFields["api_key"] != rl.RedactedValue || snap.ExtraFields["base_url"] != "https://proxy.example.com/v1" || snap.ExtraFields["region"] != "us-east-1" {
			t.Fatalf("snapshot extra fields = %+v, want the credential projected and the rest kept", snap.ExtraFields)
		}
		if nested, ok := snap.ExtraFields["nested"].(map[string]any); !ok || nested["token"] != rl.RedactedValue {
			t.Fatalf("snapshot nested extra fields = %+v, want the nested token projected", snap.ExtraFields["nested"])
		}
		if after := mustSnapshotJSON(t, req); !bytes.Equal(before, after) {
			t.Fatalf("the request changed while its snapshot was taken, want an observation that never rewrites the request")
		}
		if req.Headers["Authorization"] != "Bearer "+secretHeaderAuth || req.ExtraFields["api_key"] != secretExtraKey {
			t.Fatalf("the request lost its own credentials to the projection, want the caller's request untouched")
		}
	})

	t.Run("the safe projection is configurable and never rewrites request content", func(t *testing.T) {
		body := "keep this text whole"
		req := model.Request{
			Messages: []model.Message{
				{Role: model.RoleUser, Content: body},
				{Role: model.RoleUser, ContentParts: []model.ContentPart{{Type: "image", Image: &trpcmodel.Image{URL: "https://cdn.example.com/a.png", Format: "png"}}}},
			},
			ExtraFields: map[string]any{"api_key": "EXTRAKEY-custom"},
		}
		policy := rl.SnapshotPolicy{
			ProjectExtraFields: func(map[string]any) map[string]any { return map[string]any{"kept": "projected"} },
			ProjectURL:         func(raw string) string { return "urn:projected:" + raw },
		}
		before := mustSnapshotJSON(t, req)
		snap, err := rl.SnapshotRequest(rl.ModelViewRef{Model: "model-a"}, req, policy)
		if err != nil {
			t.Fatalf("take the request snapshot under the configured projection: %v", err)
		}
		if got := snap.ExtraFields; len(got) != 1 || got["kept"] != "projected" {
			t.Fatalf("snapshot extra fields = %+v, want the configured projection applied", got)
		}
		if got := snap.Messages[1].ContentParts[0].Image.URL; got != "urn:projected:https://cdn.example.com/a.png" {
			t.Fatalf("snapshot URL = %q, want the configured URL projection applied", got)
		}
		if snap.Messages[0].Content != body {
			t.Fatalf("snapshot body = %q, want %q untouched by the projection", snap.Messages[0].Content, body)
		}
		if after := mustSnapshotJSON(t, req); !bytes.Equal(before, after) {
			t.Fatalf("the request changed under a configured projection, want the projection to run over the copy only")
		}
	})

	t.Run("an unsupported logprobs state is written only beside the declaration it rests on", func(t *testing.T) {
		unsupported := rl.AttemptRecord{
			Schema:   rl.SchemaCurrent,
			ID:       "attempt-u1",
			Logprobs: rl.LogprobsUnsupported,
		}
		if err := rl.ValidateAttempt(unsupported); !errors.Is(err, rl.ErrLogprobsSourceMissing) {
			t.Fatalf("validate a declaration-free unsupported state error = %v, want ErrLogprobsSourceMissing", err)
		}
		if _, err := rl.MarshalLine(rl.NewAttemptLine(unsupported)); !errors.Is(err, rl.ErrLogprobsSourceMissing) {
			t.Fatalf("marshal a declaration-free unsupported state error = %v, want the write side refusing it", err)
		}
		raw, err := json.Marshal(rl.Line{Schema: rl.SchemaCurrent, Kind: rl.RecordKindAttempt, Attempt: &unsupported})
		if err != nil {
			t.Fatalf("encode the refused wire shape: %v", err)
		}
		if _, err := rl.ParseLine(raw); !errors.Is(err, rl.ErrLogprobsSourceMissing) {
			t.Fatalf("parse a declaration-free unsupported state error = %v, want the read side refusing it", err)
		}
		declared := unsupported
		declared.LogprobsSource = "provider-declaration-1"
		if err := rl.ValidateAttempt(declared); err != nil {
			t.Fatalf("validate a declared unsupported state: %v", err)
		}
		data, err := rl.MarshalLine(rl.NewAttemptLine(declared))
		if err != nil {
			t.Fatalf("marshal a declared unsupported state: %v", err)
		}
		parsed, err := rl.ParseLine(data)
		if err != nil {
			t.Fatalf("parse a declared unsupported state: %v", err)
		}
		if parsed.Attempt.Logprobs != rl.LogprobsUnsupported || parsed.Attempt.LogprobsSource != "provider-declaration-1" {
			t.Fatalf("parsed logprobs = (%v, %q), want the declaration-backed state kept", parsed.Attempt.Logprobs, parsed.Attempt.LogprobsSource)
		}
	})

	t.Run("logprobs are present only when returned and unknown is never read as not_returned", func(t *testing.T) {
		absent := learnFinalTurn("no logprobs here")
		state, source := rl.LogprobsStateOf(absent)
		if state != rl.LogprobsUnknown || source != "" {
			t.Fatalf("state of a response that returned no logprobs = (%v, %q), want unknown with no source", state, source)
		}
		returned := learnFinalTurn("with logprobs")
		returned.Choices[0].Logprobs = &trpcmodel.Logprobs{Content: []trpcmodel.TokenLogprob{{Token: "with", Logprob: -0.25, Bytes: []int{119}}}}
		state, _ = rl.LogprobsStateOf(returned)
		if state != rl.LogprobsPresent {
			t.Fatalf("state of a response that returned logprobs = %v, want present", state)
		}
		if state == rl.LogprobsUnsupported {
			t.Fatalf("the response evidence derived unsupported, want a declaration-backed state only")
		}
		attempt := rl.AttemptRecord{Schema: rl.SchemaCurrent, ID: "attempt-w1", Logprobs: rl.LogprobsUnknown}
		data, err := rl.MarshalLine(rl.NewAttemptLine(attempt))
		if err != nil {
			t.Fatalf("marshal an unknown logprobs line: %v", err)
		}
		if !bytes.Contains(data, []byte(`"logprobs":"unknown"`)) || bytes.Contains(data, []byte("not_returned")) {
			t.Fatalf("wire of an unknown state = %s, want the explicit name unknown and never not_returned", data)
		}
		tampered := bytes.Replace(data, []byte(`"logprobs":"unknown"`), []byte(`"logprobs":"not_returned"`), 1)
		if _, err := rl.ParseLine(tampered); err == nil {
			t.Fatalf("a line naming the state not_returned parsed clean, want it refused as an unknown state name")
		}
	})

	t.Run("a real decision records returned logprobs verbatim and shares the one terminal of the reduction", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		provider := &learnModel{name: "model-a", answer: func(req *model.Request) *model.Response {
			resp := learnFinalTurn("settled")
			resp.Choices[0].Logprobs = &trpcmodel.Logprobs{Content: []trpcmodel.TokenLogprob{{Token: "settled", Logprob: -0.125, Bytes: []int{115}}}}
			return resp
		}}
		h := newHarness(t, provider, nil, "inv-capture-1")
		turn := h.drive(ctx, "ep-capture-1", "capture me")

		records := h.records.snapshot()
		if len(records.attempts) != 1 {
			t.Fatalf("attempt records = %d, want exactly one observable attempt", len(records.attempts))
		}
		attempt := records.attempts[0]
		if attempt.Logprobs != rl.LogprobsPresent {
			t.Fatalf("attempt logprobs = %v, want present beside the returned values", attempt.Logprobs)
		}
		content := attempt.Response.Choices[0].Logprobs
		if content == nil || len(content.Content) != 1 {
			t.Fatalf("attempt response logprobs = %+v, want the native values kept", content)
		}
		if token := content.Content[0]; token.Token != "settled" || token.Logprob != -0.125 {
			t.Fatalf("attempt logprob value = %+v, want the returned token kept verbatim", token)
		}
		if attempt.Terminal != model.KindCompleted || turn.Terminal != model.KindCompleted || turn.Decisions[0].Terminal != model.KindCompleted {
			t.Fatalf("terminals = (attempt %v, turn %v, decision %v), want the one shared reduction terminal", attempt.Terminal, turn.Terminal, turn.Decisions[0].Terminal)
		}
		if attempt.Terminal != turn.Decisions[0].Terminal {
			t.Fatalf("attempt terminal = %v, want the decision terminal it shares the reduction with", attempt.Terminal)
		}
	})

	t.Run("a failed decision shares the one terminal and keeps the error text with unknown logprobs", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		provider := &learnModel{name: "model-a", answer: func(req *model.Request) *model.Response {
			return learnFailTurn("provider exploded")
		}}
		h := newHarness(t, provider, nil, "inv-capture-2")
		turn := h.drive(ctx, "ep-capture-2", "fail me")

		records := h.records.snapshot()
		if len(records.attempts) != 1 {
			t.Fatalf("attempt records = %d, want exactly one observable attempt", len(records.attempts))
		}
		attempt := records.attempts[0]
		if attempt.Terminal != model.KindFailed || turn.Terminal != model.KindFailed || turn.Decisions[0].Terminal != model.KindFailed {
			t.Fatalf("terminals = (attempt %v, turn %v, decision %v), want the one shared failed terminal", attempt.Terminal, turn.Terminal, turn.Decisions[0].Terminal)
		}
		if attempt.Error != "provider exploded" {
			t.Fatalf("attempt error = %q, want the observed error text kept", attempt.Error)
		}
		if attempt.Logprobs != rl.LogprobsUnknown {
			t.Fatalf("attempt logprobs = %v, want unknown beside a response that returned none", attempt.Logprobs)
		}
	})

	t.Run("concurrent request boundaries fix their own snapshots without crossing", func(t *testing.T) {
		type boundary struct {
			body   string
			secret string
		}
		boundaries := []boundary{
			{body: "MARKER-A-7f2a", secret: "SECRETA-1a2b"},
			{body: "MARKER-B-9c4b", secret: "SECRETB-3c4d"},
		}
		var wg sync.WaitGroup
		for _, own := range boundaries {
			own := own
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < 50; i++ {
					req := model.Request{
						Messages: []model.Message{
							{Role: model.RoleUser, Content: own.body},
							{Role: model.RoleUser, ContentParts: []model.ContentPart{{Type: "image", Image: &trpcmodel.Image{
								URL:    "https://alice:" + own.secret + "@cdn.example.com/img.png",
								Format: "png",
							}}}},
						},
						Headers: map[string]string{"Authorization": "Bearer " + own.secret},
					}
					snap, err := rl.SnapshotRequest(rl.ModelViewRef{Model: "model-a"}, req, rl.SnapshotPolicy{})
					if err != nil {
						t.Errorf("take a concurrent snapshot: %v", err)
						return
					}
					content, err := rl.MarshalSnapshot(snap)
					if err != nil {
						t.Errorf("marshal a concurrent snapshot: %v", err)
						return
					}
					if !bytes.Contains(content, []byte(own.body)) || bytes.Contains(content, []byte(own.secret)) {
						t.Errorf("concurrent snapshot = %s, want its own body kept and its own credential projected", content)
						return
					}
					for _, other := range boundaries {
						if other.body == own.body {
							continue
						}
						if bytes.Contains(content, []byte(other.body)) || bytes.Contains(content, []byte(other.secret)) {
							t.Errorf("concurrent snapshot = %s, want no content of another request boundary", content)
							return
						}
					}
				}
			}()
		}
		wg.Wait()
	})

	t.Run("a snapshot reads back after a restart and rechecks against its reference", func(t *testing.T) {
		temp := 0.7
		req := model.Request{
			Messages: []model.Message{
				{Role: model.RoleUser, Content: "restart me"},
				{Role: model.RoleAssistant, Content: "working", ToolCalls: []model.ToolCall{learnCall("call-1", "echo-b", `{"y":2}`)}},
			},
			ExtraFields: map[string]any{"region": "us-east-1"},
		}
		req.Temperature = &temp
		req.Tools = map[string]trpctool.Tool{
			"echo-b": &learnTool{name: "echo-b", output: "out-b"},
			"echo-a": &learnTool{name: "echo-a", output: "out-a"},
		}
		view := rl.ModelViewRef{
			Model:         "model-a",
			ModelVersion:  "v1",
			PromptVersion: "prompt-v1",
			VisibleTools:  []rl.VersionedName{{Name: "echo-a", Version: "tool-v1"}, {Name: "echo-b", Version: "tool-v2"}},
		}
		snap, err := rl.SnapshotRequest(view, req, rl.SnapshotPolicy{})
		if err != nil {
			t.Fatalf("take the request snapshot: %v", err)
		}
		content, err := rl.MarshalSnapshot(snap)
		if err != nil {
			t.Fatalf("marshal the snapshot: %v", err)
		}
		ref, err := snap.Ref("snap-1", "entry-1")
		if err != nil {
			t.Fatalf("build the snapshot reference: %v", err)
		}
		if ref.SnapshotID != "snap-1" || ref.ReadEntry != "entry-1" || ref.Digest == "" {
			t.Fatalf("snapshot reference = %+v, want the named identity and read entry beside a real digest", ref)
		}
		if err := rl.VerifySnapshot(ref, content); err != nil {
			t.Fatalf("verify the read-back content against its reference: %v", err)
		}
		restarted, err := rl.ParseSnapshot(content)
		if err != nil {
			t.Fatalf("parse the persisted snapshot: %v", err)
		}
		if restarted.Model != snap.Model || restarted.ModelVersion != snap.ModelVersion || restarted.PromptVersion != snap.PromptVersion {
			t.Fatalf("restarted view = (%q, %q, %q), want the declared view read back unchanged", restarted.Model, restarted.ModelVersion, restarted.PromptVersion)
		}
		if !bytes.Equal(restarted.Parameters, snap.Parameters) {
			t.Fatalf("restarted parameters = %s, want %s read back unchanged", restarted.Parameters, snap.Parameters)
		}
		if len(restarted.VisibleTools) != 2 || restarted.VisibleTools[0] != (rl.VersionedName{Name: "echo-a", Version: "tool-v1"}) || restarted.VisibleTools[1] != (rl.VersionedName{Name: "echo-b", Version: "tool-v2"}) {
			t.Fatalf("restarted visible tools = %+v, want the versioned set read back unchanged", restarted.VisibleTools)
		}
		if len(restarted.ToolSchemas) != 2 || !bytes.Contains(restarted.ToolSchemas[0], []byte("echo-a")) || !bytes.Contains(restarted.ToolSchemas[1], []byte("echo-b")) {
			t.Fatalf("restarted tool schemas = %+v, want the declarations in tool name order", restarted.ToolSchemas)
		}
		if again, err := rl.MarshalSnapshot(restarted); err != nil || !bytes.Equal(again, content) {
			t.Fatalf("the restarted snapshot re-encoded to %s (err %v), want the exact persisted bytes read back", again, err)
		}
		if again, err := restarted.Ref("snap-1", "entry-1"); err != nil || again != ref {
			t.Fatalf("the restarted snapshot reference = %+v (err %v), want %v rechecked from what it read back", again, err, ref)
		}
		tampered := bytes.Replace(content, []byte(`"model-a"`), []byte(`"model-b"`), 1)
		if err := rl.VerifySnapshot(ref, tampered); !errors.Is(err, rl.ErrSnapshotMismatch) {
			t.Fatalf("verify tampered content error = %v, want ErrSnapshotMismatch", err)
		}
		if err := rl.VerifySnapshot(rl.SnapshotRef{}, content); !errors.Is(err, rl.ErrSnapshotUnreadable) {
			t.Fatalf("verify an empty reference error = %v, want ErrSnapshotUnreadable", err)
		}
	})

	t.Run("the three logprobs states survive a marshal-parse round trip unchanged", func(t *testing.T) {
		records := []rl.AttemptRecord{
			{Schema: rl.SchemaCurrent, ID: "attempt-r1", Logprobs: rl.LogprobsPresent, LogprobsSource: ""},
			{Schema: rl.SchemaCurrent, ID: "attempt-r2", Logprobs: rl.LogprobsUnknown, LogprobsSource: ""},
			{Schema: rl.SchemaCurrent, ID: "attempt-r3", Logprobs: rl.LogprobsUnsupported, LogprobsSource: "provider-declaration-1"},
		}
		for _, record := range records {
			if err := rl.ValidateAttempt(record); err != nil {
				t.Fatalf("validate attempt %s: %v", record.ID, err)
			}
			data, err := rl.MarshalLine(rl.NewAttemptLine(record))
			if err != nil {
				t.Fatalf("marshal attempt %s: %v", record.ID, err)
			}
			parsed, err := rl.ParseLine(data)
			if err != nil {
				t.Fatalf("parse attempt %s: %v", record.ID, err)
			}
			if parsed.Attempt.Logprobs != record.Logprobs || parsed.Attempt.LogprobsSource != record.LogprobsSource {
				t.Fatalf("attempt %s round trip = (%v, %q), want (%v, %q) kept", record.ID, parsed.Attempt.Logprobs, parsed.Attempt.LogprobsSource, record.Logprobs, record.LogprobsSource)
			}
		}
	})
}

// mustSnapshotJSON reports the JSON shape of one value as the snapshot face
// sees it.
func mustSnapshotJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encode %s: %v", fmt.Sprintf("%T", value), err)
	}
	return data
}

// stringsAnchor keeps the strings import honest beside the content checks.
var _ = strings.Contains
