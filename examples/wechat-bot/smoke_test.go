//go:build smoke

// smoke_test.go 是本地长程冒烟：真实微信宿主装配 + 本地 HTTP 注入面（POST /task）
// 驱动 300 轮机械泵，逐轮断言投递、资源与各过程，并把可观测面的判定打进报告。
//
// 运行: go test -tags smoke ./... -run TestLocalSmoke -count=1 -timeout 30m
//
// 契约: docs/wiki/examples/wechat-bot-runtime.md#module-layout
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	tagentmodel "github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/memory/replay"
	"github.com/SpellingDragon/tagent/runtime/task/approval"
)

// smokeKind 是一轮的响应形状。
type smokeKind int

const (
	smokeText smokeKind = iota
	smokeLong
	smokeDelegate
	smokeApproval
	smokeFile
)

// smokeSchedule 是 20 轮一个周期的配比：文本 12 / 长文 2 / 委派 3 / 审批 1 / 文件 2，
// 重复 15 次即 300 轮（60% / 10% / 15% / 5% / 10%）。
var smokeSchedule = [20]smokeKind{
	smokeText, smokeText, smokeDelegate, smokeText, smokeLong,
	smokeText, smokeFile, smokeText, smokeText, smokeDelegate,
	smokeText, smokeApproval, smokeText, smokeLong, smokeText,
	smokeDelegate, smokeText, smokeFile, smokeText, smokeText,
}

// smokeKindAt 报告第 round 轮的响应形状。
func smokeKindAt(round int) smokeKind { return smokeSchedule[round%len(smokeSchedule)] }

// smokeChatAt 报告第 round 轮的会话：每 10 轮换一个会话，三个会话轮流交错。
func smokeChatAt(round int) string { return smokeChats[(round/10)%len(smokeChats)] }

// smokeRoundMarker 报告第 round 轮的标记：与矩阵前置探测的标记（R1/R902/R903）
// 错开，避免等待函数命中旧一轮的外发。
func smokeRoundMarker(round int) string { return fmt.Sprintf("SMOKE-R1%03d", round) }

// smokeRoundText 渲染第 round 轮的用户文本；每会话首轮带早期标记，供折叠后仍可读的断言。
func smokeRoundText(round int, kind smokeKind) string {
	marker := smokeRoundMarker(round)
	text := marker + " plain question"
	switch kind {
	case smokeLong:
		text = marker + " smoke long output please"
	case smokeDelegate:
		text = marker + " smoke delegate artifact"
	case smokeApproval:
		text = marker + " smoke approval request"
	case smokeFile:
		text = marker + " smoke file please"
	}
	if round%10 == 0 {
		return fmt.Sprintf("%s-%s. %s", smokeEarlyPrefix, smokeChatAt(round), text)
	}
	return text
}

// smokeObservation 是一次冒烟的观测结论，汇总进报告。
type smokeObservation struct {
	Rounds          int
	Requests        int
	Switches        int
	Reload          smokeReloadAnswer
	ReloadModelSeen bool
	RestartAt       int
	RecoveredTasks  int
	HistoryRestored bool
	RestartRequests int
	// WorkerRequests / RestartWorkerRequests 记子 agent 的模型调用数，用来
	// 说明 trajectory 少掉的正是委派子 agent 的调用。
	WorkerRequests        int
	RestartWorkerRequests int
	CloseErr              error
	Trajectory            smokeTrajectory
	TrajectoryErr         error
	// Kinds 按响应形状记每类轮数，矩阵 5/6/9/10 的汇总证据从这里取。
	Kinds          [5]int
	FoldedSessions int
	FoldEntries    int
	SyntheticProof string
	ReflectionFact string
	ReflectionSeen bool
	FirstHeap      uint64
	LastHeap       uint64
	HeapSlope      int64
	MinGoroutines  int
	MaxGoroutines  int
	Samples        int
	SampleLog      []smokeSample
}

// TestLocalSmoke 是长程冒烟入口。
// - matrix 1 needs one admitted session before the injection face answers anything
// - matrix 2 replays one inbound message through the real registerHandlers inbound chain
// - matrix 4 injects an internal notice and expects the next request to read it
func TestLocalSmoke(t *testing.T) {
	if testing.Short() {
		t.Skip("the local smoke drives 300 rounds and is skipped under -short")
	}
	logs := installSmokeLog(t)
	cfg := newSmokeConfig(t, "http://127.0.0.1:1/v1")
	provider := newSmokeProvider(t, filepath.Join(cfg.WorkspaceDir, smokeFileMarker))
	cfg.Endpoint = provider.endpoint()
	cfg.write(t)
	rig := bootSmokeRig(t, cfg, provider)
	obs := &smokeObservation{Rounds: smokeRounds}

	if err := rig.healthz(); err != nil {
		t.Fatalf("healthz before any turn: %v", err)
	}
	rig.route(smokeChats[0], "SMOKE-R9000")
	rig.waitText(smokeChats[0], "answer SMOKE-R9000", 30*time.Second)
	rig.postTask("SMOKE-R1 plain question")
	rig.waitText(smokeChats[0], "answer SMOKE-R1", 30*time.Second)
	if requests := smokeUserTurnsMatching(provider, "SMOKE-R1 plain question"); requests != 1 {
		t.Fatalf("model requests driven by the first injection = %d, want exactly 1", requests)
	}
	t.Logf("[matrix 1] injection drove one model request and one delivery: user-turns=%d body=%q",
		smokeUserTurnsMatching(provider, "SMOKE-R1 plain question"), "answer SMOKE-R1")

	dedupText := "SMOKE-R902 smoke dedup probe"
	if err := rig.channel.emit(context.Background(), textMessage(smokeChats[0], dedupText)); err != nil {
		t.Fatalf("emit the first inbound message: %v", err)
	}
	rig.waitText(smokeChats[0], "answer SMOKE-R902", 30*time.Second)
	afterFirst := smokeUserTurnsMatching(provider, dedupText)
	if afterFirst != 1 {
		t.Fatalf("model requests carrying the probe = %d, want exactly 1", afterFirst)
	}
	if err := rig.channel.emit(context.Background(), textMessage(smokeChats[0], dedupText)); err != nil {
		t.Fatalf("emit the duplicate inbound message: %v", err)
	}
	time.Sleep(500 * time.Millisecond)
	if got := smokeUserTurnsMatching(provider, dedupText); got != afterFirst {
		t.Fatalf("model requests after the duplicate = %d, want the %d of the first delivery", got, afterFirst)
	}
	if dropped := logs.count("[Dedup] duplicate"); dropped == 0 {
		t.Fatal("the duplicate drop left no log line")
	}
	t.Logf("[matrix 2] duplicate dropped, requests stayed at %d, log lines=%d", afterFirst, logs.count("[Dedup]"))

	notice := smokeInboxMarker + " round-0"
	rig.app.host.inbox.InjectInternal(rig.addressOf(smokeChats[0]), "smoke", tagentmodel.Message{
		Role: tagentmodel.RoleUser, Content: notice,
	})
	rig.drive(smokeChats[0], "SMOKE-R903 plain question")
	rig.waitText(smokeChats[0], "answer SMOKE-R903", 30*time.Second)
	if !smokeUserRequestContains(provider, notice) {
		t.Fatalf("no model request carried the injected internal notice %q", notice)
	}
	t.Logf("[matrix 4] the internal notice reached the next request: %q", notice)

	runSmokePump(t, rig, provider, cfg, logs, obs)
	runSmokeFoldChecks(t, rig, provider, obs)
	runSmokeMeditationCheck(t, rig, provider, logs, obs)
	runSmokeTrajectoryCheck(t, rig, cfg, provider, obs)
	reportSmokeObservability(t, logs, obs)
}

// TestLocalSmokeGuards pins the injection-face refusals.
// - no admitted session: 503 with zero model requests
// - blank user text: 400 with zero model requests
// - unknown digest and non-approver rulings leave the request pending
// - the injection face carries no address: it drives the most recently admitted session only
// 契约: docs/wiki/examples/wechat-bot-runtime.md#module-layout
func TestLocalSmokeGuards(t *testing.T) {
	cfg := newSmokeConfig(t, "http://127.0.0.1:1/v1")
	provider := newSmokeProvider(t, filepath.Join(cfg.WorkspaceDir, smokeFileMarker))
	cfg.Endpoint = provider.endpoint()
	cfg.write(t)
	rig := bootSmokeRig(t, cfg, provider)
	defer func() {
		if err := rig.close(); err != nil {
			t.Errorf("close the guard rig: %v", err)
		}
	}()

	status, body := rig.postTaskStatus("SMOKE-G1 no session yet")
	if status != http.StatusServiceUnavailable || !strings.Contains(body, "no user session") {
		t.Fatalf("injection before any admitted session = %d %q, want 503", status, body)
	}
	if got := provider.count(); got != 0 {
		t.Fatalf("the rejected injection drove %d model requests, want 0", got)
	}
	t.Logf("[guards] injection without an admitted session: 503, model requests=0")

	status, body = rig.postTaskStatus("   ")
	if status != http.StatusBadRequest || !strings.Contains(body, "no user message") {
		t.Fatalf("blank injection = %d %q, want 400", status, body)
	}
	if got := provider.count(); got != 0 {
		t.Fatalf("the blank injection drove %d model requests, want 0", got)
	}
	t.Logf("[guards] blank injection: 400, model requests=0")

	rig.route(smokeChats[0], "SMOKE-R9700")
	rig.waitText(smokeChats[0], "answer SMOKE-R9700", 60*time.Second)
	if _, err := rig.gateway.Manager().Request(context.Background(), "guard-approval", sendToolID,
		[]byte(`{"text":"guard"}`), plugin.RiskCritical, "guard", "guard"); err != nil {
		t.Fatalf("register the guard approval: %v", err)
	}
	handled, reply, err := rig.gateway.Resolve(smokeApproverChat, "approve deadbeef")
	if !handled || !errors.Is(err, approval.ErrApprovalNotFound) {
		t.Fatalf("unknown digest ruling = (%t, %q, %v), want ErrApprovalNotFound", handled, reply, err)
	}
	pending := rig.gateway.Manager().Pending()
	if len(pending) != 1 || pending[0].Status != approval.ApprovalPending {
		t.Fatalf("pending after the refused ruling = %+v, want the one untouched pending request", pending)
	}
	handled, reply, err = rig.gateway.Resolve(smokeChats[0], "approve "+shortDigest(pending[0].ArgsDigest))
	if !handled || !errors.Is(err, ErrApprovalNotApprover) {
		t.Fatalf("non-approver ruling = (%t, %q, %v), want ErrApprovalNotApprover", handled, reply, err)
	}
	if got := len(rig.gateway.Manager().Pending()); got != 1 {
		t.Fatalf("pending after the non-approver ruling = %d, want the request still pending", got)
	}
	t.Logf("[guards] approval refused an unknown digest and a non-approver; the request stayed pending")
}

const (
	// smokeRounds 是机械泵的轮数。
	smokeRounds = 300
	// smokeReloadRound 是热换发生的那一轮（改文档模型名 → POST /reload）。
	smokeReloadRound = 150
	// smokeRestartRound 是宿主重建发生的那一轮。
	smokeRestartRound = 200
	// smokeFoldMinRounds 是折叠检查可用的最小轮次：会话要攒够 2400 字节才会折叠。
	smokeFoldMinRounds = 40
)

// smokePlannedRounds 报告本次要跑的轮数：SMOKE_ROUNDS 只用于本地缩短调试，
// 默认仍是 300 轮的长程。
func smokePlannedRounds() int {
	if raw := strings.TrimSpace(os.Getenv("SMOKE_ROUNDS")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return n
		}
	}
	return smokeRounds
}

// runSmokePump 跑满 300 轮：三个会话交错，五种响应形状按 60/10/15/5/10 配比，
// 中途热换一次、重建一次，每轮断言外发到达与模型请求增长，并周期采样资源。
// - restart routing counts only sessions driven after the restart; pre-restart bindings stay as the control
// - a rebuilt host has no admitted session, so a post-reboot round drives through production Drive before /task
func runSmokePump(t *testing.T, rig *smokeRig, provider *smokeProvider, cfg *smokeConfig, logs *smokeLogRecorder, obs *smokeObservation) {
	t.Helper()
	baseline := make(map[string]event.Address, len(smokeChats))
	for _, chat := range smokeChats {
		baseline[chat] = rig.addressOf(chat)
	}
	obs.MinGoroutines = int(^uint(0) >> 1)
	planned := smokePlannedRounds()
	preRestartBindings := -1
	sinceRestart := make(map[string]struct{}, len(smokeChats))
	obs.Rounds = planned
	t.Logf("[pump] %d rounds over %d sessions (reload@%d restart@%d)",
		planned, len(smokeChats), smokeReloadRound, smokeRestartRound)

	for round := 0; round < planned; round++ {
		chat := smokeChatAt(round)
		if rig.lastChat != chat {
			switchMarker := fmt.Sprintf("SMOKE-R8%03d", round)
			rig.route(chat, switchMarker)
			rig.waitText(chat, "answer "+switchMarker, 120*time.Second)
			obs.Switches++
		}
		if round == smokeReloadRound {
			obs.Reload = rig.reload(smokeReloadModel)
			if !obs.Reload.Reloaded || len(obs.Reload.ModelChanges) != 1 {
				t.Fatalf("round %d reload = %+v, want one judged model change", round, obs.Reload)
			}
			change := obs.Reload.ModelChanges[0]
			if change.From != "local/"+smokeEntryModel || change.To != "local/"+smokeReloadModel {
				t.Fatalf("round %d model change = %q -> %q, want local/%s -> local/%s",
					round, change.From, change.To, smokeEntryModel, smokeReloadModel)
			}
			t.Logf("[matrix 11a] hot reload judged %s -> %s at round %d", change.From, change.To, round)
		}
		if round == smokeRestartRound {
			obs.RestartAt = round
			preRestartBindings = len(rig.app.host.router.BoundAddresses(event.AgentID(smokeSessionTag)))
			obs.RestartRequests = provider.count()
			obs.RestartWorkerRequests = smokeRequestsFor(provider, smokeWorkerModel)
			if err := rig.close(); err != nil {
				obs.CloseErr = err
				t.Fatalf("round %d close before restart: %v", round, err)
			}
			rig.reboot()
			obs.RecoveredTasks = len(rig.host.Run().Recovery().Tasks)
			rig.drive(chat, "SMOKE-R8200 switch to "+chat)
			rig.waitText(chat, "answer SMOKE-R8200", 120*time.Second)
			clear(sinceRestart)
			t.Logf("[matrix 11b] host rebuilt at round %d: recovered tasks=%d requests before restart=%d",
				round, obs.RecoveredTasks, obs.RestartRequests)
		}
		driveSmokeRound(t, rig, provider, cfg, round)
		obs.Kinds[smokeKindAt(round)]++
		sinceRestart[chat] = struct{}{}
		if round == smokeReloadRound {
			seen := false
			for _, req := range provider.recorded() {
				if req.Model == smokeReloadModel {
					seen = true
					break
				}
			}
			obs.ReloadModelSeen = seen
			if !seen {
				t.Fatalf("round %d drove no request under the reloaded model %q", round, smokeReloadModel)
			}
			t.Logf("[matrix 11a] the model name after reload reached the provider: %s", smokeReloadModel)
		}
		if round == smokeRestartRound {
			obs.HistoryRestored = smokeRequestsSinceContain(provider, obs.RestartRequests,
				smokeEarlyPrefix+"-"+smokeChatAt(round))
			t.Logf("[matrix 11b] the first request after restart carries the pre-restart history: %t", obs.HistoryRestored)
		}
		if round%20 == 0 {
			sample := rig.sampleResources(round)
			obs.Samples++
			obs.SampleLog = append(obs.SampleLog, sample)
			if sample.Goroutines < obs.MinGoroutines {
				obs.MinGoroutines = sample.Goroutines
			}
			if sample.Goroutines > obs.MaxGoroutines {
				obs.MaxGoroutines = sample.Goroutines
			}
			if obs.FirstHeap == 0 {
				obs.FirstHeap = sample.HeapInuse
			}
			obs.LastHeap = sample.HeapInuse
		}
		if round%50 == 0 {
			if err := rig.healthz(); err != nil {
				t.Fatalf("round %d healthz: %v", round, err)
			}
		}
	}

	obs.Requests = provider.count()
	obs.WorkerRequests = smokeRequestsFor(provider, smokeWorkerModel)
	for _, chat := range smokeChats {
		if got := rig.addressOf(chat); got != baseline[chat] {
			t.Fatalf("session %s rebound its address across the run: %+v -> %+v", chat, baseline[chat], got)
		}
	}
	wantBound := len(sinceRestart)
	if preRestartBindings >= 0 {
		if wantBound == 0 {
			wantBound = len(smokeChats)
		}
	} else {
		wantBound = plannedSessions(planned)
	}
	if bound := rig.app.host.router.BoundAddresses(event.AgentID(smokeSessionTag)); len(bound) != wantBound {
		t.Fatalf("bound addresses = %d, want the %d sessions driven since the last restart (pre-restart bindings=%d)",
			len(bound), wantBound, preRestartBindings)
	}
	obs.HeapSlope = smokeHeapSlope(obs.SampleLog)
	t.Logf("[matrix 3] routing held: %d switches, %d stable bindings (pre-restart bindings=%d, bindings are not persisted)",
		obs.Switches, wantBound, preRestartBindings)
	t.Logf("[pump] done: rounds=%d requests=%d switches=%d samples=%d goroutines=[%d,%d] heap=%d->%d slope=%d B/round",
		obs.Rounds, obs.Requests, obs.Switches, obs.Samples,
		obs.MinGoroutines, obs.MaxGoroutines, obs.FirstHeap, obs.LastHeap, obs.HeapSlope)
	t.Logf("[matrix 5] delegations=%d: every round got the accept ack and a settle follow-up carrying the worker body", obs.Kinds[smokeDelegate])
	t.Logf("[matrix 6] approvals=%d: every round went Hold -> approve <digest> -> retry executed -> approved body delivered", obs.Kinds[smokeApproval])
	t.Logf("[matrix 9] file replies=%d: every round sent the path answer and the fake bot recorded a file send", obs.Kinds[smokeFile])
	t.Logf("[matrix 10] long outputs=%d: every >2000B reply went through the sender truncation fallback", obs.Kinds[smokeLong])
	if spread := obs.MaxGoroutines - obs.MinGoroutines; spread > 64 {
		t.Fatalf("goroutine range over the run = %d, want a bounded spread (<=64)", spread)
	}
	if growth := int64(obs.LastHeap) - int64(obs.FirstHeap); growth > 48<<20 {
		t.Fatalf("heap growth over the run = %d bytes, want bounded growth (<=48MiB)", growth)
	}
	if obs.HeapSlope > 128<<10 {
		t.Fatalf("heap slope = %d bytes/round, want no leak slope (<=128KiB/round)", obs.HeapSlope)
	}
	if errors := logs.count("ERROR"); errors > 0 {
		t.Logf("[pump] the debug log carries %d ERROR line(s); first samples: %v", errors, logs.sample("ERROR", 3))
	}
}

// driveSmokeRound 驱动一轮并按形状断言它应有的产出：每一轮都要有外发到达，
// 每一轮都要让模型请求数增长。
// - an approval wait keys on this round's digest: the approver chat still holds announcements from earlier rounds
func driveSmokeRound(t *testing.T, rig *smokeRig, provider *smokeProvider, cfg *smokeConfig, round int) {
	t.Helper()
	chat := smokeChatAt(round)
	kind := smokeKindAt(round)
	marker := smokeRoundMarker(round)
	text := smokeRoundText(round, kind)
	before := provider.count()
	switch kind {
	case smokeText:
		rig.postTask(text)
		evt := rig.waitText(chat, "answer "+marker, 120*time.Second)
		if evt.Body != "answer "+marker {
			t.Fatalf("round %d text delivery = %q, want %q", round, evt.Body, "answer "+marker)
		}
	case smokeLong:
		rig.postTask(text)
		evt := rig.waitText(chat, "LONG "+marker, 120*time.Second)
		if !strings.Contains(evt.Body, "[Message truncated]") {
			t.Fatalf("round %d long delivery = %d bytes with no truncation fallback", round, len(evt.Body))
		}
		if len(evt.Body) > 2100 {
			t.Fatalf("round %d long delivery = %d bytes, want the 2000-byte fallback", round, len(evt.Body))
		}
	case smokeDelegate:
		rig.postTask(text)
		rig.waitText(chat, "delegate ack "+marker, 120*time.Second)
		rig.waitText(chat, "settle-ack "+marker, 120*time.Second)
		if !smokeSettleRequestSeen(provider, marker) {
			t.Fatalf("round %d settle follow-up carries no worker body %q", round, "worker result "+marker)
		}
	case smokeApproval:
		rig.postTask(text)
		rig.waitText(chat, "hold-ack "+marker, 120*time.Second)
		pending := rig.pendingApproval(30 * time.Second)
		digest := pending.ArgsDigest
		if len(digest) > 8 {
			digest = digest[:8]
		}
		announce := rig.waitText(smokeApproverChat, digest, 30*time.Second)
		if !strings.Contains(announce.Body, "审批请求") {
			t.Fatalf("round %d approval announcement = %q, want the digest %s", round, announce.Body, digest)
		}
		handled, reply, err := rig.gateway.Resolve(smokeApproverChat, "approve "+digest)
		if err != nil || !handled || !strings.Contains(reply, "approved") {
			t.Fatalf("round %d approval ruling = (%t, %q, %v), want the approved ruling", round, handled, reply, err)
		}
		rig.postTask(marker + " smoke retry")
		rig.waitText(chat, "approved-done "+marker, 120*time.Second)
		rig.waitText(chat, smokeApprovalMarker+" "+marker, 120*time.Second)
	case smokeFile:
		path := filepath.Join(cfg.WorkspaceDir, smokeFileMarker)
		files := rig.fileCount(chat, path)
		rig.postTask(text)
		rig.waitText(chat, "here is the report for "+marker, 120*time.Second)
		rig.waitFile(chat, path, files, 120*time.Second)
	}
	if got := provider.count(); got <= before {
		t.Fatalf("round %d (%s) drove no model request", round, text)
	}
}

// smokeSettleRequestSeen 报告是否有一次结转回合读到了该轮的结算通知（正文含
// 子 agent 结果）。
func smokeSettleRequestSeen(provider *smokeProvider, marker string) bool {
	for _, req := range provider.recorded() {
		last := smokeLastUserText(req.Messages)
		if strings.Contains(last, "tagent: task") && strings.Contains(last, "settled") &&
			strings.Contains(last, "worker result "+marker) {
			return true
		}
	}
	return false
}

// smokeRequestsSinceContain 报告 from 之后到达的请求里是否有一次读到给定子串。
func smokeRequestsSinceContain(provider *smokeProvider, from int, want string) bool {
	requests := provider.recorded()
	if from > len(requests) {
		from = len(requests)
	}
	for _, req := range requests[from:] {
		if strings.Contains(req.text(), want) {
			return true
		}
	}
	return false
}

// smokeHeapSlope 用最小二乘拟合采样点，报告每轮的堆增长字节数。
func smokeHeapSlope(samples []smokeSample) int64 {
	if len(samples) < 2 {
		return 0
	}
	var sumX, sumY, sumXY, sumXX float64
	for _, sample := range samples {
		x := float64(sample.Round)
		y := float64(sample.HeapInuse)
		sumX += x
		sumY += y
		sumXY += x * y
		sumXX += x * x
	}
	n := float64(len(samples))
	denominator := n*sumXX - sumX*sumX
	if denominator == 0 {
		return 0
	}
	return int64((n*sumXY - sumX*sumY) / denominator)
}

// runSmokeFoldChecks 断言折叠：每个会话的投影都长出合成产物，且早期的用户输入
// 真的被历史摘要带进了之后某一次请求的上下文。
// - a session driven only before the restart has no projection; the check skips it
// - the summary may fold more than once, so only the early input is asserted
func runSmokeFoldChecks(t *testing.T, rig *smokeRig, provider *smokeProvider, obs *smokeObservation) {
	t.Helper()
	if planned := smokePlannedRounds(); planned < smokeFoldMinRounds {
		t.Logf("[matrix 8] fold check skipped: SMOKE_ROUNDS=%d is a fast-iteration mode (needs >=%d rounds to cross the 2400-byte fold threshold)",
			planned, smokeFoldMinRounds)
		return
	}
	for _, chat := range smokeChats {
		entries, carried := rig.host.Run().ReplayEntries(rig.addressOf(chat).SessionID)
		if !carried {
			t.Logf("[matrix 8] session %s carries no projection on this boot (not driven since the restart); skipped", chat)
			continue
		}
		synthetics := syntheticEntries(entries)
		if len(synthetics) == 0 {
			t.Fatalf("session %s carries no synthetic fold product (%d entries)", chat, len(entries))
		}
		early := smokeEarlyPrefix + "-" + chat
		proof := ""
		for _, entry := range synthetics {
			if entry.Type == replay.ProjectionTypeHistorySummary && strings.Contains(entry.Content, early) {
				proof = entry.Content
				break
			}
		}
		if proof == "" {
			t.Fatalf("session %s folded no history summary carrying %q (products=%d)", chat, early, len(synthetics))
		}
		if !smokeUserRequestContains(provider, "external_input: "+early) {
			t.Fatalf("no model request rendered the history summary carrying %q\nprojection:\n%s\nhistory-summary requests:\n%s",
				early, smokeEntryDigest(entries), smokeRequestsContaining(provider, "history summary"))
		}
		obs.FoldedSessions++
		obs.FoldEntries += len(synthetics)
		if obs.SyntheticProof == "" {
			obs.SyntheticProof = proof
		}
		t.Logf("[matrix 8] session %s: entries=%d synthetic=%d, the history summary keeps the early input",
			chat, len(entries), len(synthetics))
	}
	if obs.FoldedSessions == 0 {
		t.Fatal("no session carried a fold projection")
	}
}

// runSmokeMeditationCheck 断言冥想：事件驱动触发 → 反思 → deliver_summary →
// 用户会话链可见，并由下一轮请求读到。
// - the reflection summary lands on the driven session first: production Drive moves the injection address, then /task
func runSmokeMeditationCheck(t *testing.T, rig *smokeRig, provider *smokeProvider, logs *smokeLogRecorder, obs *smokeObservation) {
	t.Helper()
	deadline := time.Now().Add(120 * time.Second)
	reflectionChat := ""
	for {
		for _, chat := range smokeChats {
			for _, evt := range rig.sessionFacts(chat) {
				if evt.Type != event.TypeExternalInput {
					continue
				}
				if evt.Meta[event.MetaKeyTriggerSource] != "meditation" {
					continue
				}
				if !strings.Contains(evt.Message.Content, smokeReflectionMarker) {
					continue
				}
				obs.ReflectionFact = string(evt.Key)
				obs.ReflectionSeen = true
				reflectionChat = chat
				t.Logf("[matrix 7] the reflection summary reached the session chain of %s: key=%s", chat, evt.Key)
			}
		}
		if obs.ReflectionSeen {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no meditation summary landed in a user session chain within %s (reflect requests=%d)\nfacts:\n%s\nreflect requests:\n%s\nlogs:\n%s",
				120*time.Second, smokeRequestsFor(provider, smokeReflectModel), rig.factDigest(), smokeRequestDetail(provider, smokeReflectModel), logs.digest("reflection", "deliver_summary", "meditation", "tool", "budget"))
		}
		time.Sleep(100 * time.Millisecond)
	}
	if smokeRequestsFor(provider, smokeReflectModel) == 0 {
		t.Fatal("the reflection loop answered no model request")
	}
	rig.drive(reflectionChat, "SMOKE-R7555 switch to "+reflectionChat)
	rig.waitText(reflectionChat, "answer SMOKE-R7555", 60*time.Second)
	rig.postTask("SMOKE-R7777 plain question")
	rig.waitText(reflectionChat, "answer SMOKE-R7777", 60*time.Second)
	if !smokeUserRequestContains(provider, smokeReflectionMarker) {
		t.Fatal("the turn after the meditation delivery read no reflection summary")
	}
	t.Logf("[matrix 7] the next user turn read the reflection summary: reflect requests=%d",
		smokeRequestsFor(provider, smokeReflectModel))
}

// smokeRequestsFor 报告某个模型名已经回答的请求数。
func smokeRequestsFor(provider *smokeProvider, model string) int {
	count := 0
	for _, req := range provider.recorded() {
		if req.Model == model {
			count++
		}
	}
	return count
}

// runSmokeTrajectoryCheck 收尾关闭宿主并核对轨迹：attempts.jsonl 的行数对模型
// 调用数，seal.json 的清单对最后一次装配的请求数。
// - delegated worker calls do not enter the trajectory, so the reconciliation expects one missing row per worker call
func runSmokeTrajectoryCheck(t *testing.T, rig *smokeRig, cfg *smokeConfig, provider *smokeProvider, obs *smokeObservation) {
	t.Helper()
	obs.CloseErr = rig.close()
	if obs.CloseErr != nil {
		t.Fatalf("close the host before reading the trajectory: %v", obs.CloseErr)
	}
	trajectory, err := readTrajectory(cfg.StorageRoot)
	obs.Trajectory, obs.TrajectoryErr = trajectory, err
	if err != nil {
		t.Fatalf("read the trajectory under %s: %v", cfg.StorageRoot, err)
	}
	if trajectory.Seal == nil {
		t.Fatal("the trajectory carries no seal manifest")
	}
	seal := trajectory.Seal
	if !seal.Sealed {
		t.Fatalf("the trajectory seal is not sealed: %+v", seal)
	}
	if seal.Partial {
		t.Fatalf("the trajectory seal is partial: reasons=%v in_flight=%d", seal.Reasons, seal.InFlight)
	}
	if seal.Dropped != 0 {
		t.Fatalf("the trajectory capture dropped %d record(s)", seal.Dropped)
	}
	total := provider.count()
	expected := total - obs.WorkerRequests
	if delta := trajectory.Records - expected; delta < -1 || delta > 1 {
		t.Fatalf("attempts.jsonl = %d lines against %d captured-eligible model calls (delta=%d, worker calls=%d)\nattempt heads:\n%s\nrequests:\n%s",
			trajectory.Records, expected, delta, obs.WorkerRequests, strings.Join(trajectory.Heads, "\n"), smokeRequestHeads(provider))
	}
	lastBoot := total - obs.RestartRequests
	lastBootWorker := obs.WorkerRequests - obs.RestartWorkerRequests
	if got := int(seal.Records); got != lastBoot-lastBootWorker {
		t.Fatalf("seal records = %d against the %d captured-eligible model calls of the last boot (%d total, %d worker)\nattempt heads:\n%s\nrequests:\n%s",
			got, lastBoot-lastBootWorker, lastBoot, lastBootWorker, strings.Join(trajectory.Heads, "\n"), smokeRequestHeads(provider))
	}
	t.Logf("[matrix 12] trajectory reconciled: attempts=%d entry-calls=%d sub-agent-calls=%d seal-records=%d last-boot-entry-calls=%d files=%d sealed=%t",
		trajectory.Records, expected, obs.WorkerRequests, seal.Records, lastBoot-lastBootWorker, len(seal.Files), seal.Sealed)
}

// smokeUserTurnsMatching 报告最后一条用户消息恰为该文本的模型请求数。
func smokeUserTurnsMatching(provider *smokeProvider, text string) int {
	count := 0
	for _, req := range provider.recorded() {
		if strings.TrimSpace(smokeLastUserText(req.Messages)) == text {
			count++
		}
	}
	return count
}

// smokeUserRequestContains 报告是否有一次用户回合的请求读到给定子串：冥想 agent
// 自己的反思回合跳过，避免把它观察到的摘要算成用户会话的上下文。
func smokeUserRequestContains(provider *smokeProvider, want string) bool {
	for _, req := range provider.recorded() {
		if req.Model == smokeReflectModel {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(smokeLastUserText(req.Messages)), "[reflection]") {
			continue
		}
		if strings.Contains(req.text(), want) {
			return true
		}
	}
	return false
}

// smokeAnyRequestContains 报告是否有一次模型请求读到给定子串。
func smokeAnyRequestContains(provider *smokeProvider, want string) bool {
	for _, req := range provider.recorded() {
		if strings.Contains(req.text(), want) {
			return true
		}
	}
	return false
}

// reportSmokeObservability 汇总可观测面：逐过程的日志可定位性、healthz 次数与
// 轨迹产物，缺口按"缺失/部分"两类列出，供战报的四象限结论引用。
// - a probe counts only log lines that exist in the production source; processes without any log line are listed in gaps
func reportSmokeObservability(t *testing.T, logs *smokeLogRecorder, obs *smokeObservation) {
	t.Helper()
	probes := []struct {
		process string
		needle  string
	}{
		{"投递 sender", "[Agent]["},
		{"内部通知 inbox", "[inbox]"},
		{"去重 Dedup", "[Dedup]"},
		{"审批传输 gateway", "[approval]"},
		{"审批判定 policy", "tagent: approval ruled"},
		{"回合边界 observer", "tagent: turn start"},
		{"冥想驱动 meditation", "tagent: meditation driver of"},
		{"委派结算 delegation settle", "tagent: delegation settled"},
		{"恢复 restore", "tagent: restore folded chains"},
	}
	covered, missing := make([]string, 0, len(probes)), make([]string, 0, len(probes))
	for _, probe := range probes {
		if lines := logs.count(probe.needle); lines > 0 {
			covered = append(covered, fmt.Sprintf("%s=%d", probe.process, lines))
			continue
		}
		missing = append(missing, probe.process)
	}
	gaps := []string{
		"注入面 /task：成功注入无日志行（host.go inject 只在注入的任务失败时 assemblyWarn）",
		"折叠 fold：memory/replay 全程无日志，投影产物只能从 ReplayEntries 读出",
		"委派 admission：只有拒绝有日志（delegation refused result=admission），受理成功无行",
		"轨迹 capture：rl 包不写日志，只有返回错误",
	}
	t.Logf("[observability] log lines=%d distinct probes=%d", logs.count(""), len(probes))
	t.Logf("[observability] 已覆盖: %s", strings.Join(covered, ", "))
	t.Logf("[observability] 探针未命中: %s", strings.Join(missing, ", "))
	t.Logf("[observability] 源码审计缺口（无任何日志行）: %s", strings.Join(gaps, " ｜ "))
	t.Logf("[observability] healthz: 注入面前后各探活一次，均返回 ok（见 pump 的每 50 轮探活）")
	t.Logf("[observability] trajectory: attempts=%d seal=%+v", obs.Trajectory.Records, obs.Trajectory.Seal)
	t.Logf("[observability] 12 项矩阵: 完成会话=%d 折叠会话=%d 合成产物=%d 冥想可见=%t 重启恢复任务=%d 历史恢复=%t",
		obs.Rounds, obs.FoldedSessions, obs.FoldEntries, obs.ReflectionSeen, obs.RecoveredTasks, obs.HistoryRestored)
}

// smokeRequestHeads 渲染每次模型请求的模型名与最后一条用户消息首行，供轨迹对账失败时对照。
func smokeRequestHeads(provider *smokeProvider) string {
	requests := provider.recorded()
	lines := make([]string, 0, len(requests))
	for i, req := range requests {
		head := strings.SplitN(smokeLastUserText(req.Messages), "\n", 2)[0]
		if len(head) > 48 {
			head = head[:48] + "…"
		}
		lines = append(lines, fmt.Sprintf("%d %s %s", i+1, req.Model, head))
	}
	return strings.Join(lines, "\n")
}

// factDigest 渲染各会话链上已落地的事实，供冥想投递缺失时对照。
func (r *smokeRig) factDigest() string {
	lines := make([]string, 0, 24)
	for _, chat := range smokeChats {
		for _, evt := range r.sessionFacts(chat) {
			head := strings.SplitN(evt.Message.Content, "\n", 2)[0]
			if len(head) > 56 {
				head = head[:56] + "…"
			}
			lines = append(lines, fmt.Sprintf("- %s %s src=%q %s", chat, evt.Type, evt.Meta[event.MetaKeyTriggerSource], head))
		}
	}
	if len(lines) == 0 {
		return "(no facts)"
	}
	return strings.Join(lines, "\n")
}

// smokeRequestDetail 渲染某模型收到的请求全文预览（逐条消息），供冥想轮诊断。
func smokeRequestDetail(provider *smokeProvider, modelName string) string {
	lines := make([]string, 0, 24)
	for _, req := range provider.recorded() {
		if req.Model != modelName {
			continue
		}
		lines = append(lines, "- request:")
		for _, msg := range req.Messages {
			head := strings.SplitN(strings.TrimSpace(msg.Content), "\n", 2)[0]
			if len(head) > 72 {
				head = head[:72] + "…"
			}
			lines = append(lines, fmt.Sprintf("    %s %s", msg.Role, head))
		}
	}
	if len(lines) == 0 {
		return "(no reflect requests)"
	}
	return strings.Join(lines, "\n")
}

// digest 渲染含任一关键词的日志行（最多 120 行），供失败诊断。
func (l *smokeLogRecorder) digest(needles ...string) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	lines := make([]string, 0, 40)
	for _, line := range l.lines {
		for _, needle := range needles {
			if strings.Contains(line, needle) {
				if len(line) > 200 {
					line = line[:200] + "…"
				}
				lines = append(lines, "- "+line)
				break
			}
		}
	}
	if len(lines) > 120 {
		lines = lines[len(lines)-120:]
	}
	if len(lines) == 0 {
		return "(no matching log lines)"
	}
	return strings.Join(lines, "\n")
}

// smokeEntryDigest 渲染投影条目的键、类型与正文首行，供折叠断言失败时对照。
func smokeEntryDigest(entries []replay.ProjectionEntry) string {
	lines := make([]string, 0, len(entries))
	for _, entry := range entries {
		head := strings.SplitN(strings.TrimSpace(entry.Content), "\n", 2)[0]
		if len(head) > 88 {
			head = head[:88] + "…"
		}
		lines = append(lines, fmt.Sprintf("- key=%s type=%s %s", entry.Key, entry.Type, head))
	}
	return strings.Join(lines, "\n")
}

// smokeRequestsContaining 渲染所有含该子串的模型请求片段，供"投影有、组装没有"的对照。
func smokeRequestsContaining(provider *smokeProvider, needle string) string {
	lines := make([]string, 0, 8)
	for i, req := range provider.recorded() {
		if !strings.Contains(req.text(), needle) {
			continue
		}
		body := req.text()
		at := strings.Index(body, needle)
		snippet := body[at:]
		if len(snippet) > 220 {
			snippet = snippet[:220] + "…"
		}
		lines = append(lines, fmt.Sprintf("- request %d (%s): %s", i+1, req.Model, snippet))
	}
	if len(lines) == 0 {
		return "(no request carries it)"
	}
	return strings.Join(lines, "\n")
}

// plannedSessions 报告若干轮会驱动到几个不同会话。
func plannedSessions(planned int) int {
	seen := make(map[string]struct{}, len(smokeChats))
	for round := 0; round < planned; round++ {
		seen[smokeChatAt(round)] = struct{}{}
	}
	return len(seen)
}
