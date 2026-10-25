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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/api/event"
	tagentmodel "github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/evolution/meditation"
	"github.com/SpellingDragon/tagent/memory/replay"
	"github.com/SpellingDragon/tagent/rl"
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
	// WorkerRequests / RestartWorkerRequests 记子 agent 的模型调用数：委派调用
	// 与入口调用一样入册，对账用它核对 worker 调用没有缺口。
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
// - every model call the provider served enters attempts.jsonl, delegated worker calls included
// - the seal accounts for every call of the last boot, entry and delegated alike
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
	if got := trajectory.Records; got != total {
		t.Fatalf("attempts.jsonl = %d lines against %d model calls (worker calls=%d): every call, entry or delegated, must enter the trajectory\nattempt heads:\n%s\nrequests:\n%s",
			got, total, obs.WorkerRequests, strings.Join(trajectory.Heads, "\n"), smokeRequestHeads(provider))
	}
	lastBoot := total - obs.RestartRequests
	lastBootWorker := obs.WorkerRequests - obs.RestartWorkerRequests
	if got := int(seal.Records); got != lastBoot {
		t.Fatalf("seal records = %d against the %d model calls of the last boot (%d total, %d worker calls): the seal must account for every call of its boot\nattempt heads:\n%s\nrequests:\n%s",
			got, lastBoot, total, lastBootWorker, strings.Join(trajectory.Heads, "\n"), smokeRequestHeads(provider))
	}
	t.Logf("[matrix 12] trajectory reconciled: attempts=%d model-calls=%d entry-calls=%d sub-agent-calls=%d seal-records=%d last-boot-calls=%d files=%d sealed=%t",
		trajectory.Records, total, total-obs.WorkerRequests, obs.WorkerRequests, seal.Records, lastBoot, len(seal.Files), seal.Sealed)
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

// smokeIntegrityApprovalTurn, smokeIntegrityReflectionReadTurn, smokeIntegrityReflectionTurn, smokeIntegrityApprovalChanged, smokeIntegrityFoldSwitchTurn, smokeIntegrityFoldEarlyTurn, smokeIntegrityFoldLateTurn, smokeIntegrityFoldTail, smokeIntegrityRebuildTurn, smokeIntegrityFoldFollowTurn, smokeIntegrityReplacementWarmTurn, smokeIntegrityReloadTurn, smokeIntegrityRouteTurn, smokeIntegrityRouteDenied, smokeIntegrityRouteOK, smokeIntegrityRouteAfter, smokeIntegrityRouteDenial, smokeIntegritySelfTurn, smokeIntegritySelfFoldTurn, smokeIntegritySelfAfter, smokeIntegrityInflightTurn, smokeIntegrityInflightMarker, smokeIntegrityInflightAnswer, smokeIntegrityAfterInflight, smokeIntegrityReplacementAfterRefused, smokeIntegrityCaptureRejected, smokeIntegrityCaptureTurn, smokeIntegrityCaptureSecond, smokeIntegrityCaptureFailed, smokeIntegrityConsumerRejected, smokeIntegrityConsumerOpening, smokeIntegrityConsumerRead and smokeIntegrityConsumerReload are the markers of the third-wave control matrix; each control uses its own segment so a waiting helper cannot hit another outbound.
const (
	// smokeIntegrityApprovalTurn 是扣留与批准后重试共用的一轮。
	smokeIntegrityApprovalTurn = "SMOKE-R7301"
	// smokeIntegrityReflectionReadTurn 是回流落地之后读到摘要的那一轮。
	smokeIntegrityReflectionReadTurn = "SMOKE-R7302"
	// smokeIntegrityReflectionTurn 是触发唯一一次反思的普通轮。
	smokeIntegrityReflectionTurn = "SMOKE-R7303"
	// smokeIntegrityApprovalChanged 是改参重试的一轮：新 digest 没有裁决。
	smokeIntegrityApprovalChanged = "SMOKE-R7304"
	// smokeIntegrityFoldSwitchTurn 是把注入面切到折叠会话的绑定轮。
	smokeIntegrityFoldSwitchTurn = "SMOKE-R7305"
	// smokeIntegrityFoldEarlyTurn 是被折进历史摘要的早期大正文轮。
	smokeIntegrityFoldEarlyTurn = "SMOKE-R7310"
	// smokeIntegrityFoldLateTurn 是触发折叠并把综述带进请求的那一轮。
	smokeIntegrityFoldLateTurn = "SMOKE-R7311"
	// smokeIntegrityFoldTail 是早期大正文的尾部标记：投影折成摘要后，原文仍以它
	// 逐字节可达。
	smokeIntegrityFoldTail = "SMOKE-EI-FOLD-TAIL"
	// smokeIntegrityRebuildTurn 是重启后驱动投影重建的控制轮。
	smokeIntegrityRebuildTurn = "SMOKE-R7312"
	// smokeIntegrityFoldFollowTurn 是折叠之后继续读取综述的正式回合。
	smokeIntegrityFoldFollowTurn = "SMOKE-R7313"
	// smokeIntegrityReplacementWarmTurn 是热换之前的预热轮。
	smokeIntegrityReplacementWarmTurn = "SMOKE-R7319"
	// smokeIntegrityReloadTurn 是热换之后走新版本的那一轮。
	smokeIntegrityReloadTurn = "SMOKE-R7320"
	// smokeIntegrityRouteTurn 是触发唯一一次反思的输入轮。
	smokeIntegrityRouteTurn = "SMOKE-R7401"
	// smokeIntegrityRouteDenied 是越权目标企图投递的正文：它不得落到任何会话链。
	smokeIntegrityRouteDenied = "SMOKE-EI-ROUTE-DENIED"
	// smokeIntegrityRouteOK 是合法目标投递的正文。
	smokeIntegrityRouteOK = "SMOKE-EI-ROUTE-OK"
	// smokeIntegrityRouteAfter 是读到合法摘要的下一轮。
	smokeIntegrityRouteAfter = "SMOKE-R7402"
	// smokeIntegrityRouteDenial 是模型可读的具名拒绝片段。
	smokeIntegrityRouteDenial = "the arguments name fields outside the declaration 'target'"
	// smokeIntegritySelfTurn 是自激场景的开场轮。
	smokeIntegritySelfTurn = "SMOKE-R7501"
	// smokeIntegritySelfFoldTurn 是自激场景里触发折叠的会话切换轮。
	smokeIntegritySelfFoldTurn = "SMOKE-R7510"
	// smokeIntegritySelfAfter 是自激场景的控制轮：它证明反思门仍然活着。
	smokeIntegritySelfAfter = "SMOKE-R7599"
	// smokeIntegrityInflightTurn 是挂起在模型请求里的那一轮。
	smokeIntegrityInflightTurn = "SMOKE-R7601"
	// smokeIntegrityInflightMarker 让钩子只拦住该轮的请求。
	smokeIntegrityInflightMarker = "SMOKE-EI-INFLIGHT"
	// smokeIntegrityInflightAnswer 是开始版本服务完该轮的回答正文。
	smokeIntegrityInflightAnswer = "inflight old version SMOKE-R7601"
	// smokeIntegrityAfterInflight 是替换完成之后的一轮。
	smokeIntegrityAfterInflight = "SMOKE-R7602"
	// smokeIntegrityReplacementAfterRefused 是替换被拒之后的下一轮。
	smokeIntegrityReplacementAfterRefused = "SMOKE-R7321"
	// smokeIntegrityCaptureRejected 是采集负锚里无会话被拒的注入。
	smokeIntegrityCaptureRejected = "SMOKE-R7700"
	// smokeIntegrityCaptureTurn 是采集正锚驱动的普通回合。
	smokeIntegrityCaptureTurn = "SMOKE-R7701"
	// smokeIntegrityCaptureSecond 是采集正锚里带委派子 agent 的回合。
	smokeIntegrityCaptureSecond = "SMOKE-R7702"
	// smokeIntegrityCaptureFailed 是采集负锚里返回 500 的回合。
	smokeIntegrityCaptureFailed = "SMOKE-R7703"
	// smokeIntegrityConsumerRejected 是尚无会话时注入面拒绝的那一条。
	smokeIntegrityConsumerRejected = "SMOKE-R7800"
	// smokeIntegrityConsumerOpening 是正式入口链上的第一次准入轮。
	smokeIntegrityConsumerOpening = "SMOKE-R7801"
	// smokeIntegrityConsumerRead 是 /task 驱动的普通回合。
	smokeIntegrityConsumerRead = "SMOKE-R7802"
	// smokeIntegrityConsumerReload 是热换之后由 /task 驱动的那一轮。
	smokeIntegrityConsumerReload = "SMOKE-R7803"
)

// TestEventIntegrityApprovalControl pins the approval gate to one execution:
// - positive checks the held -> approved -> executed once attribution
// - negative checks a refusal drives no execution, a changed retry keeps no old approval, and the approver session stays silent
// - consumer runs the approval retry over config -> Boot -> channel binding -> registerHandlers -> HTTP /task
// - the three anchors share one parent execution load instead of multiplying the matrix
func TestEventIntegrityApprovalControl(t *testing.T) {
	t.Run("positive", smokeIntegrityApprovalPositive)
	t.Run("negative", smokeIntegrityApprovalNegative)
	t.Run("consumer", smokeIntegrityApprovalConsumer)
}

// TestEventIntegrityReflectionDeliveryControl pins the reflection delivery:
// - positive checks the authorised target receives the summary once
// - negative checks an out-of-scope target is refused by name and the legal path still works
// - negative also checks the delivery does not reopen the novelty gate
// - consumer checks deliver_summary -> InjectReflection -> the target's next request over the formal chain
func TestEventIntegrityReflectionDeliveryControl(t *testing.T) {
	t.Run("positive", smokeIntegrityReflectionPositive)
	t.Run("negative", smokeIntegrityReflectionNegative)
	t.Run("consumer", smokeIntegrityReflectionConsumer)
}

// TestEventIntegrityFoldControl pins the compression projection:
// - positive checks the fold triggers, the original text stays reachable and the summary enters the request
// - negative checks a fold product does not self-trigger and a rebuild does not fold again
// - consumer checks later formal requests keep consuming the fold product
func TestEventIntegrityFoldControl(t *testing.T) {
	t.Run("positive", smokeIntegrityFoldPositive)
	t.Run("negative", smokeIntegrityFoldNegative)
	t.Run("consumer", smokeIntegrityFoldConsumer)
}

// TestEventIntegrityReplacementControl pins the model replacement:
// - positive checks /reload installs the new version and the next request takes it
// - negative checks the same version is not installed twice, a failed replacement installs nothing, and an in-flight stream does not drift
// - consumer runs only the /reload -> /task formal HTTP chain
func TestEventIntegrityReplacementControl(t *testing.T) {
	t.Run("positive", smokeIntegrityReplacementPositive)
	t.Run("negative", smokeIntegrityReplacementNegative)
	t.Run("consumer", smokeIntegrityReplacementConsumer)
}

// TestEventIntegrityCaptureControl pins the capture ledger:
// - positive checks attempts.jsonl records match the real requests and the seal is complete
// - negative checks no request writes no record, a failed attempt is not recorded as success, and an admission refusal never enters the loss account
// - consumer reads the records only through the attempts.jsonl formal path
func TestEventIntegrityCaptureControl(t *testing.T) {
	t.Run("positive", smokeIntegrityCapturePositive)
	t.Run("negative", smokeIntegrityCaptureNegative)
	t.Run("consumer", smokeIntegrityCaptureConsumer)
}

// smokeIntegrityHost 是一段控制事件场景的独立宿主：正式装配、脚本模型与日志面
// 同属一段，段末显式 close，避免上一段的后台 pass 与下一段的日志面共用。
type smokeIntegrityHost struct {
	rig      *smokeRig
	provider *smokeProvider
	cfg      *smokeConfig
	logs     *smokeLogRecorder
}

// bootSmokeIntegrityHost 装配一段场景的宿主。interval/minGap 决定冥想节奏：
// "1h" 让首次宿主可见提交触发唯一一次反思（计数可确定），"1ms" 放开节奏用于
// 判定自身产物是否重开新颖门。
func bootSmokeIntegrityHost(t *testing.T, interval, minGap string) *smokeIntegrityHost {
	t.Helper()
	logs := installSmokeLog(t)
	cfg := newSmokeConfig(t, "http://127.0.0.1:1/v1")
	cfg.MeditationInterval = interval
	if minGap != "" {
		cfg.MeditationMinGap = minGap
	}
	provider := newSmokeProvider(t, filepath.Join(cfg.WorkspaceDir, smokeFileMarker))
	cfg.Endpoint = provider.endpoint()
	cfg.write(t)
	rig := bootSmokeRig(t, cfg, provider)
	return &smokeIntegrityHost{rig: rig, provider: provider, cfg: cfg, logs: logs}
}

// close 显式关闭本段宿主；重复调用是安全的空操作。
func (h *smokeIntegrityHost) close(t *testing.T) {
	t.Helper()
	if err := h.rig.close(); err != nil {
		t.Fatalf("close the integrity host: %v", err)
	}
}

// smokeIntegrityApprovalPositive 判定审批门：critical 调用被扣留，批准只记录裁决，
// 重试才执行一次，正文只归审批会话。
func smokeIntegrityApprovalPositive(t *testing.T) {
	host := bootSmokeIntegrityHost(t, "1h", "")
	defer host.close(t)
	rig, provider := host.rig, host.provider
	chat := smokeChats[0]

	rig.drive(chat, smokeIntegrityApprovalTurn+" smoke approval request")
	rig.waitText(chat, "hold-ack "+smokeIntegrityApprovalTurn, 60*time.Second)
	pending := rig.pendingApproval(30 * time.Second)
	digest := shortDigest(pending.ArgsDigest)
	announce := rig.waitText(smokeApproverChat, digest, 30*time.Second)
	if !strings.Contains(announce.Body, "审批请求") {
		t.Fatalf("the approval announcement = %q, want the request text carrying %s", announce.Body, digest)
	}
	if got := smokeErrorsTextCount(rig, chat, smokeApprovalMarker); got != 0 {
		t.Fatalf("the held call delivered its body %d time(s) before the ruling, want none", got)
	}
	entryBefore := smokeRequestsFor(provider, smokeEntryModel)
	handled, reply, err := rig.gateway.Resolve(smokeApproverChat, "approve "+digest)
	if err != nil || !handled || !strings.Contains(reply, "approved") {
		t.Fatalf("the approval ruling = (%t, %q, %v), want the approved ruling", handled, reply, err)
	}
	if got := len(rig.gateway.Manager().Pending()); got != 0 {
		t.Fatalf("pending requests after the ruling = %d, want none", got)
	}
	decided := rig.gateway.Manager().Lookup(sendToolID, pending.ArgsDigest)
	if decided == nil || decided.Status != approval.ApprovalApproved || decided.DecidedBy != "wechat:"+smokeApproverChat {
		t.Fatalf("the decided request = %+v, want approved by wechat:%s", decided, smokeApproverChat)
	}
	if got := smokeRequestsFor(provider, smokeEntryModel); got != entryBefore {
		t.Fatalf("the ruling drove %d entry request(s), want none: a ruling records the decision, the retry performs the call", got-entryBefore)
	}
	rig.postTask(smokeIntegrityApprovalTurn + " smoke retry")
	rig.waitText(chat, "approved-done "+smokeIntegrityApprovalTurn, 120*time.Second)
	rig.waitText(chat, smokeApprovalMarker+" "+smokeIntegrityApprovalTurn, 120*time.Second)
	if got := smokeErrorsTextCount(rig, chat, smokeApprovalMarker+" "+smokeIntegrityApprovalTurn); got != 1 {
		t.Fatalf("the approved body was delivered %d time(s), want exactly one", got)
	}
	if got := smokeErrorsTextCount(rig, smokeApproverChat, smokeApprovalMarker); got != 0 {
		t.Fatalf("the approved body reached the approver chat %d time(s), want none", got)
	}
	bound := rig.app.host.router.BoundAddresses(event.AgentID(smokeSessionTag))
	if len(bound) != 1 || bound[0] != rig.addressOf(chat) {
		t.Fatalf("bound addresses after the approval = %+v, want only %+v", bound, rig.addressOf(chat))
	}
	t.Logf("[integrity-approval-positive] %s held -> approved -> executed once on %s", smokeIntegrityApprovalTurn, chat)
}

// smokeIntegrityApprovalNegative 判定裁决不驱动执行：批准只记录裁决；改参后的
// 重试不得沿用旧批准，必须重新扣留；审批人会话不收到任何执行正文。
func smokeIntegrityApprovalNegative(t *testing.T) {
	host := bootSmokeIntegrityHost(t, "1h", "")
	defer host.close(t)
	rig, provider := host.rig, host.provider
	chat := smokeChats[0]

	rig.drive(chat, smokeIntegrityApprovalTurn+" smoke approval request")
	rig.waitText(chat, "hold-ack "+smokeIntegrityApprovalTurn, 60*time.Second)
	pending := rig.pendingApproval(30 * time.Second)
	digest := shortDigest(pending.ArgsDigest)
	rig.waitText(smokeApproverChat, digest, 30*time.Second)

	entryBefore := smokeRequestsFor(provider, smokeEntryModel)
	handled, reply, err := rig.gateway.Resolve(smokeApproverChat, "approve "+digest)
	if err != nil || !handled || !strings.Contains(reply, "approved") {
		t.Fatalf("the approval ruling = (%t, %q, %v), want the approved ruling", handled, reply, err)
	}
	if got := smokeRequestsFor(provider, smokeEntryModel); got != entryBefore {
		t.Fatalf("the ruling drove %d entry request(s), want none", got-entryBefore)
	}
	if got := len(rig.gateway.Manager().Pending()); got != 0 {
		t.Fatalf("pending requests after the ruling = %d, want none", got)
	}

	rig.postTask(smokeIntegrityApprovalTurn + " smoke retry")
	rig.waitText(chat, "approved-done "+smokeIntegrityApprovalTurn, 120*time.Second)
	if got := smokeErrorsTextCount(rig, chat, smokeApprovalMarker+" "+smokeIntegrityApprovalTurn); got != 1 {
		t.Fatalf("the approved body was delivered %d time(s), want exactly one", got)
	}

	rig.postTask(smokeIntegrityApprovalChanged + " smoke retry")
	rig.waitText(chat, "hold-ack "+smokeIntegrityApprovalChanged, 120*time.Second)
	changed := rig.pendingApproval(30 * time.Second)
	if changed.ArgsDigest == pending.ArgsDigest {
		t.Fatalf("the changed retry reused the approved digest %s, want a new held identity", shortDigest(pending.ArgsDigest))
	}
	if got := smokeErrorsTextCount(rig, chat, smokeApprovalMarker+" "+smokeIntegrityApprovalChanged); got != 0 {
		t.Fatalf("the changed retry executed %d time(s) on the old approval, want none", got)
	}
	if pendingNow := rig.gateway.Manager().Pending(); len(pendingNow) != 1 || pendingNow[0].Status != approval.ApprovalPending {
		t.Fatalf("pending requests after the changed retry = %+v, want the one held request", pendingNow)
	}

	entryBefore = smokeRequestsFor(provider, smokeEntryModel)
	handled, _, err = rig.gateway.Resolve(smokeApproverChat, "approve deadbeef")
	if !handled || !errors.Is(err, approval.ErrApprovalNotFound) {
		t.Fatalf("the unknown-digest ruling = (%t, %v), want the named not-found refusal", handled, err)
	}
	if got := smokeRequestsFor(provider, smokeEntryModel); got != entryBefore {
		t.Fatalf("the unknown-digest ruling drove %d entry request(s), want none", got-entryBefore)
	}
	if got := len(rig.gateway.Manager().Pending()); got != 1 {
		t.Fatalf("pending requests after the unknown-digest ruling = %d, want the one held request", got)
	}

	handled, reply, err = rig.gateway.Resolve(smokeApproverChat, "reject "+shortDigest(changed.ArgsDigest))
	if err != nil || !handled || !strings.Contains(reply, "denied") {
		t.Fatalf("the denial ruling = (%t, %q, %v), want the denied receipt", handled, reply, err)
	}
	if got := smokeErrorsTextCount(rig, chat, smokeApprovalMarker+" "+smokeIntegrityApprovalChanged); got != 0 {
		t.Fatalf("the denied ruling executed the call %d time(s), want none", got)
	}
	if got := len(rig.gateway.Manager().Pending()); got != 0 {
		t.Fatalf("pending requests after the denial = %d, want none", got)
	}
	if denied := rig.gateway.Manager().Lookup(sendToolID, changed.ArgsDigest); denied == nil || denied.Status != approval.ApprovalDenied {
		t.Fatalf("the denied request = %+v, want denied under the changed digest", denied)
	}
	if got := smokeErrorsTextCount(rig, smokeApproverChat, smokeApprovalMarker); got != 0 {
		t.Fatalf("the approved body reached the approver chat %d time(s), want none", got)
	}
	t.Logf("[integrity-approval-negative] the ruling drove no request; the changed retry %s stayed held (digest %s); the approver chat received no body",
		smokeIntegrityApprovalChanged, shortDigest(changed.ArgsDigest))

	host.close(t)
	smokeIntegrityApprovalExpired(t)
}

// smokeIntegrityApprovalExpired 用最小有效期与可推进时钟：过期后的补批被具名
// 拒绝，重试同一调用仍被扣留——过期裁决不得沿用旧批准放行。
func smokeIntegrityApprovalExpired(t *testing.T) {
	logs := installSmokeLog(t)
	cfg := newSmokeConfig(t, "http://127.0.0.1:1/v1")
	cfg.MeditationInterval = "1h"
	cfg.Doc = smokeErrorsShortTTLDocument
	provider := newSmokeProvider(t, filepath.Join(cfg.WorkspaceDir, smokeFileMarker))
	cfg.Endpoint = provider.endpoint()
	cfg.write(t)

	appCfg, err := loadWechatConfig(cfg.ConfigPath)
	if err != nil {
		t.Fatalf("load the wechat section: %v", err)
	}
	if got := appCfg.Governance.ApprovalTTLMinutes; got != smokeErrorsTTLMinutes {
		t.Fatalf("approval_ttl_minutes = %d, want %d", got, smokeErrorsTTLMinutes)
	}
	clock := &smokeErrorsClock{now: time.Unix(1_700_000_000, 0)}
	gateway := newApprovalGateway(appCfg.Governance, appCfg.App.Wechat.Approvers)
	gateway.manager = approval.NewApprovalManager(cfg.ApprovalDir,
		time.Duration(smokeErrorsTTLMinutes)*time.Minute,
		approval.WithApprovalClock(clock.Now),
		approval.WithApprovalRescanInterval(0))
	gateway.manager.AddChannel(gateway)

	rig := bootSmokeRigWith(t, cfg, provider, gateway)
	defer func() {
		if err := rig.close(); err != nil {
			t.Errorf("close the expired-approval host: %v", err)
		}
	}()
	chat := smokeChats[2]

	rig.drive(chat, smokeIntegrityApprovalTurn+" smoke approval request")
	rig.waitText(chat, "hold-ack "+smokeIntegrityApprovalTurn, 60*time.Second)
	pending := rig.pendingApproval(30 * time.Second)
	digest := shortDigest(pending.ArgsDigest)
	rig.waitText(smokeApproverChat, digest, 30*time.Second)

	clock.Advance(time.Duration(smokeErrorsTTLMinutes)*time.Minute + time.Second)
	if got := len(rig.gateway.Manager().Pending()); got != 0 {
		t.Fatalf("pending requests after expiry = %d, want none", got)
	}
	if _, err := rig.gateway.Manager().Decide(pending.ID, approval.ApprovalApproved, "smoke-late"); !errors.Is(err, approval.ErrApprovalExpired) {
		t.Fatalf("late ruling = %v, want ErrApprovalExpired", err)
	}

	rig.postTask(smokeIntegrityApprovalTurn + " smoke retry")
	smokeErrorsWaitTextCount(t, rig, chat, "hold-ack "+smokeIntegrityApprovalTurn, 2, 60*time.Second)
	if got := smokeErrorsTextCount(rig, chat, smokeApprovalMarker); got != 0 {
		t.Fatalf("the expired ruling executed the call %d time(s), want none", got)
	}
	if got := smokeErrorsTextCount(rig, chat, "approved-done "+smokeIntegrityApprovalTurn); got != 0 {
		t.Fatalf("the expired ruling reported %d approved result(s), want none", got)
	}
	if got := len(rig.gateway.Manager().Pending()); got != 0 {
		t.Fatalf("pending requests after the expired retry = %d, want the expired request still naming the digest", got)
	}
	if latest := rig.gateway.Manager().Lookup(sendToolID, pending.ArgsDigest); latest == nil || latest.ID != pending.ID {
		t.Fatalf("lookup after the expired retry = %+v, want the expired request %s still naming the digest", latest, pending.ID)
	}
	if logs.count("ruling=expired") == 0 || logs.count(fmt.Sprintf("expires_ms=%d", pending.ExpiresMs)) == 0 {
		t.Fatalf("the expired ruling left no named INFO line for %s", pending.ID)
	}
	t.Logf("[integrity-approval-negative] approval %s expired at %d ms; the retry stayed held and no approved body was delivered",
		pending.ID, pending.ExpiresMs)
}

// smokeIntegrityApprovalConsumer 只走正式入口：配置 -> Boot -> 渠道绑定 ->
// registerHandlers -> HTTP /task。无会话时注入面拒绝且不驱动模型；首次渠道入站
// 被准入并驱动一轮；审批经渠道裁决、经 /task 重试执行一次；随后注入面读一轮。
func smokeIntegrityApprovalConsumer(t *testing.T) {
	host := bootSmokeIntegrityHost(t, "5s", "")
	defer host.close(t)
	rig, provider := host.rig, host.provider
	chat := smokeChats[0]

	if rig.app.cfg.Entry != smokeSessionTag {
		t.Fatalf("the loaded entry = %q, want %q", rig.app.cfg.Entry, smokeSessionTag)
	}
	if !rig.gateway.Enabled() {
		t.Fatal("the loaded governance section is disabled, want the approval gate attached")
	}
	if rig.face() == "" {
		t.Fatal("the booted host listens on no injection face")
	}
	if err := rig.healthz(); err != nil {
		t.Fatalf("healthz after boot: %v", err)
	}

	status, body := rig.postTaskStatus(smokeIntegrityConsumerRejected + " before any session")
	if status != http.StatusServiceUnavailable {
		t.Fatalf("task injection before any session = %d %q, want 503", status, body)
	}
	if !strings.Contains(body, "no user session") {
		t.Fatalf("the refusal body = %q, want the named no-session reason", body)
	}
	if got := provider.count(); got != 0 {
		t.Fatalf("the refused injection drove %d model request(s), want none", got)
	}

	if err := rig.channel.emit(context.Background(),
		textMessage(chat, smokeIntegrityConsumerOpening+" smoke approval request")); err != nil {
		t.Fatalf("emit the first inbound message: %v", err)
	}
	rig.waitText(chat, "hold-ack "+smokeIntegrityConsumerOpening, 120*time.Second)
	wantAddr := rig.addressOf(chat)
	if target, ok := rig.app.host.router.Target(wantAddr); !ok || target != chat {
		t.Fatalf("router target of %+v = (%q, %t), want %q", wantAddr, target, ok, chat)
	}
	if !smokeIntegrityHasFact(rig.sessionFacts(chat), event.TypeExternalInput, smokeIntegrityConsumerOpening) {
		t.Fatalf("the admitted inbound left no external_input fact on %s", chat)
	}

	pending := rig.pendingApproval(30 * time.Second)
	digest := shortDigest(pending.ArgsDigest)
	rig.waitText(smokeApproverChat, digest, 30*time.Second)
	handled, reply, err := rig.gateway.Resolve(smokeApproverChat, "approve "+digest)
	if err != nil || !handled || !strings.Contains(reply, "approved") {
		t.Fatalf("the channel ruling = (%t, %q, %v), want the approved ruling", handled, reply, err)
	}
	rig.postTask(smokeIntegrityConsumerOpening + " smoke retry")
	rig.waitText(chat, "approved-done "+smokeIntegrityConsumerOpening, 120*time.Second)
	if got := smokeErrorsTextCount(rig, chat, smokeApprovalMarker+" "+smokeIntegrityConsumerOpening); got != 1 {
		t.Fatalf("the approved body was delivered %d time(s) through the formal entry, want exactly one", got)
	}

	before := provider.count()
	rig.postTask(smokeIntegrityConsumerRead + " plain question")
	rig.waitText(chat, "answer "+smokeIntegrityConsumerRead, 120*time.Second)
	if got := provider.count(); got <= before {
		t.Fatalf("the injected turn drove no model request")
	}
	t.Logf("[integrity-approval-consumer] config->Boot->bind->registerHandlers->/task: rejected before admission, admitted %s, approved and executed once, read %s",
		chat, smokeIntegrityConsumerRead)
}

// smokeIntegrityReflectionPositive 判定回流落授权目标一次：meditation 触发一次
// 反思，模型显式 deliver_summary，内容只进目标会话，且下一次正式请求读到它。
func smokeIntegrityReflectionPositive(t *testing.T) {
	host := bootSmokeIntegrityHost(t, "1h", "")
	defer host.close(t)
	rig, provider := host.rig, host.provider
	chat := smokeChats[0]

	rig.drive(chat, smokeIntegrityReflectionTurn+" plain question")
	rig.waitText(chat, "answer "+smokeIntegrityReflectionTurn, 120*time.Second)
	reflection := smokeIntegrityWaitMeditationFact(t, rig, chat, 120*time.Second)
	smokeIntegrityWaitReflectCount(t, provider, 2, 60*time.Second)
	smokeIntegrityAssertReflectQuiet(t, provider, 2, 500*time.Millisecond)
	want := rig.addressOf(chat)
	if reflection.Owner != want || reflection.Source != want || reflection.Target != want {
		t.Fatalf("the reflection delivery addresses owner=%+v source=%+v target=%+v, want %+v on every face",
			reflection.Owner, reflection.Source, reflection.Target, want)
	}
	if active := rig.host.Run().ActiveSessions(event.AgentID(smokeSessionTag)); len(active) != 1 || active[0] != want {
		t.Fatalf("active sessions = %+v, want only the delivered target %+v", active, want)
	}
	if got := len(rig.sessionFacts(smokeApproverChat)); got != 0 {
		t.Fatalf("the approver chat carries %d session fact(s), want none: it is not a delivery target", got)
	}
	rig.postTask(smokeIntegrityReflectionReadTurn + " plain question")
	rig.waitText(chat, "answer "+smokeIntegrityReflectionReadTurn, 120*time.Second)
	if !smokeUserRequestContains(provider, smokeReflectionMarker) {
		t.Fatalf("the turn after the meditation delivery read no reflection summary\nreflect requests:\n%s",
			smokeRequestDetail(provider, smokeReflectModel))
	}
	t.Logf("[integrity-reflection-positive] %s delivered once to %+v and the next request read %s",
		chat, want, smokeReflectionMarker)
}

// smokeIntegrityReflectionNegative 判定两条负向语义：越权目标被具名拒绝且拒绝
// 文本可读、拒绝后合法目标仍可投递；回流产物不重开新颖门，真实输入仍能触发反思。
func smokeIntegrityReflectionNegative(t *testing.T) {
	smokeIntegrityWrongRoute(t)
	smokeIntegrityReflectionSelfManaged(t)
}

// smokeIntegrityWrongRoute 只触发一次反思：模型先试图把摘要投给声明之外的
// 审批人会话，被拒后读到具名拒绝并改投声明目标。越权正文因此不得落到任何链。
func smokeIntegrityWrongRoute(t *testing.T) {
	installSmokeLog(t)
	cfg := newSmokeConfig(t, "http://127.0.0.1:1/v1")
	cfg.MeditationInterval = "1h"
	provider := newSmokeProvider(t, filepath.Join(cfg.WorkspaceDir, smokeFileMarker))
	cfg.Endpoint = provider.endpoint()
	cfg.write(t)

	var mu sync.Mutex
	denied, delivered := false, false
	provider.hook = func(w http.ResponseWriter, _ int, modelName string, messages []smokeModelMessage) bool {
		if modelName != smokeReflectModel {
			return false
		}
		mu.Lock()
		defer mu.Unlock()
		last := smokeLastContent(messages)
		switch {
		case strings.Contains(last, "[deliver_summary_denied]"):
			denied = true
			fmt.Fprint(w, smokeToolCallBody("call-reflect-route-ok", "deliver_summary",
				`{"content":"`+smokeIntegrityRouteOK+`"}`))
			return true
		case strings.Contains(last, "[deliver_summary_ok]"):
			delivered = true
			fmt.Fprint(w, smokeAnswerBody("reflection recorded"))
			return true
		}
		fmt.Fprint(w, smokeToolCallBody("call-reflect-route-denied", "deliver_summary",
			`{"content":"`+smokeIntegrityRouteDenied+`","target":"`+smokeApproverChat+`"}`))
		return true
	}

	rig := bootSmokeRig(t, cfg, provider)
	chat := smokeChats[0]
	rig.drive(chat, smokeIntegrityRouteTurn+" plain question")
	rig.waitText(chat, "answer "+smokeIntegrityRouteTurn, 120*time.Second)

	deadline := time.Now().Add(120 * time.Second)
	for {
		mu.Lock()
		seenDenied, seenDelivered := denied, delivered
		mu.Unlock()
		if seenDenied && seenDelivered {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the reflection never returned from the refused target (denied=%t delivered=%t)\nreflect requests:\n%s",
				seenDenied, seenDelivered, smokeRequestDetail(provider, smokeReflectModel))
		}
		time.Sleep(20 * time.Millisecond)
	}
	smokeIntegrityWaitReflectCount(t, provider, 3, 60*time.Second)
	smokeIntegrityAssertReflectQuiet(t, provider, 3, 500*time.Millisecond)

	readable := false
	for _, req := range provider.recorded() {
		if req.Model != smokeReflectModel {
			continue
		}
		if strings.Contains(req.text(), smokeIntegrityRouteDenial) && strings.Contains(req.text(), "'target'") {
			readable = true
			break
		}
	}
	if !readable {
		t.Fatalf("no reflection request read the named refusal %q\nreflect requests:\n%s",
			smokeIntegrityRouteDenial, smokeRequestDetail(provider, smokeReflectModel))
	}

	for _, evt := range rig.host.Run().SessionFacts(event.Address{SessionID: rig.addressOf(chat).SessionID}) {
		if strings.Contains(evt.Message.Content, smokeIntegrityRouteDenied) {
			t.Fatalf("the refused delivery landed a fact %s: %q", evt.Key, evt.Message.Content)
		}
	}
	if got := len(rig.sessionFacts(smokeApproverChat)); got != 0 {
		t.Fatalf("the approver chat carries %d session fact(s), want none", got)
	}
	if got := smokeErrorsTextCount(rig, smokeApproverChat, smokeIntegrityRouteDenied); got != 0 {
		t.Fatalf("the refused delivery reached the approver chat %d time(s), want none", got)
	}

	delivery := smokeIntegrityWaitDelivered(t, rig, chat, smokeIntegrityRouteOK, 60*time.Second)
	if delivery.Target != rig.addressOf(chat) {
		t.Fatalf("the accepted delivery target = %+v, want %+v", delivery.Target, rig.addressOf(chat))
	}
	accepted := 0
	for _, evt := range rig.sessionFacts(chat) {
		if evt.Meta[event.MetaKeyTriggerSource] == meditation.LineageReflection &&
			strings.Contains(evt.Message.Content, smokeIntegrityRouteOK) {
			accepted++
		}
	}
	if accepted != 1 {
		t.Fatalf("the accepted summary landed %d time(s), want exactly one", accepted)
	}
	rig.postTask(smokeIntegrityRouteAfter + " plain question")
	rig.waitText(chat, "answer "+smokeIntegrityRouteAfter, 120*time.Second)
	if !smokeUserRequestContains(provider, smokeIntegrityRouteOK) {
		t.Fatalf("the turn after the accepted delivery read no summary")
	}
	if err := rig.close(); err != nil {
		t.Fatalf("close the wrong-route host: %v", err)
	}
	t.Logf("[integrity-negative] the refused target %q stayed out of every chain; the named refusal was readable; the accepted summary landed once on %+v",
		smokeApproverChat, delivery.Target)
}

// smokeIntegrityReflectionSelfManaged 把反思节奏放到 1ms：回流产物的 trigger 是
// 冥想自己的 lineage，不是面向宿主的交互。产物落地后反思必须静默（不重开新颖
// 门、不重 arm），而真实输入仍能触发下一轮反思——证明静默不是驱动已死。
func smokeIntegrityReflectionSelfManaged(t *testing.T) {
	host := bootSmokeIntegrityHost(t, "1ms", "0s")
	defer host.close(t)
	rig, provider := host.rig, host.provider
	chat := smokeChats[0]

	rig.drive(chat, smokeIntegritySelfTurn+" plain question")
	rig.waitText(chat, "answer "+smokeIntegritySelfTurn, 120*time.Second)
	reflection := smokeIntegrityWaitMeditationFact(t, rig, chat, 120*time.Second)
	if got := reflection.Meta[event.MetaKeyTriggerSource]; !meditation.SelfManagedLineage(got) {
		t.Fatalf("the reflection product carries trigger %q, want a self-managed lineage", got)
	}
	if (meditation.NoveltyPolicy{}).Novel(reflection) {
		t.Fatalf("the reflection product opened the freshness gate (trigger=%q)", reflection.Meta[event.MetaKeyTriggerSource])
	}
	settled := smokeIntegrityWaitReflectSettled(t, provider, 500*time.Millisecond, 60*time.Second)
	noNovelty := host.logs.count(": no-novelty (")
	smokeIntegrityAssertReflectQuiet(t, provider, settled, time.Second)
	if got := host.logs.count(": no-novelty ("); got != noNovelty {
		t.Fatalf("the reflection product opened %d no-novelty pass(es), want none", got-noNovelty)
	}

	before := smokeRequestsFor(provider, smokeReflectModel)
	rig.postTask(smokeIntegritySelfAfter + " plain question")
	rig.waitText(chat, "answer "+smokeIntegritySelfAfter, 120*time.Second)
	smokeIntegrityWaitReflectCount(t, provider, before+2, 60*time.Second)
	t.Logf("[integrity-reflection-negative] the reflection product stayed self-managed; the live input grew reflect requests from %d to %d",
		before, smokeRequestsFor(provider, smokeReflectModel))
}

// smokeIntegrityReflectionConsumer 只走正式装配链：渠道入站（registerHandlers）
// 驱动一轮，冥想 agent 的 deliver_summary 经 InjectReflection 落到目标会话链，
// 随后的 HTTP /task 请求从正式装配里读到它；审批人链保持零投递。
func smokeIntegrityReflectionConsumer(t *testing.T) {
	host := bootSmokeIntegrityHost(t, "1h", "")
	defer host.close(t)
	rig, provider := host.rig, host.provider
	chat := smokeChats[0]

	if err := rig.channel.emit(context.Background(),
		textMessage(chat, smokeIntegrityReflectionTurn+" plain question")); err != nil {
		t.Fatalf("emit the reflection turn: %v", err)
	}
	rig.waitText(chat, "answer "+smokeIntegrityReflectionTurn, 120*time.Second)
	delivery := smokeIntegrityWaitMeditationFact(t, rig, chat, 120*time.Second)
	smokeIntegrityWaitReflectCount(t, provider, 2, 60*time.Second)
	smokeIntegrityAssertReflectQuiet(t, provider, 2, 500*time.Millisecond)

	deliveredByHost := false
	for _, req := range provider.recorded() {
		if req.Model != smokeReflectModel {
			continue
		}
		for _, msg := range req.Messages {
			for _, call := range msg.ToolCalls {
				if call.Function.Name == "deliver_summary" {
					deliveredByHost = true
				}
			}
		}
	}
	if !deliveredByHost {
		t.Fatalf("the hosted meditation agent issued no deliver_summary call\nreflect requests:\n%s",
			smokeRequestDetail(provider, smokeReflectModel))
	}
	if delivery.Target != rig.addressOf(chat) {
		t.Fatalf("the delivery target = %+v, want %+v", delivery.Target, rig.addressOf(chat))
	}
	if !smokeIntegrityHasFact(rig.sessionFacts(chat), event.TypeExternalInput, smokeReflectionMarker) {
		t.Fatalf("the reflection summary is not a fact of the target chain %s", chat)
	}
	if got := len(rig.sessionFacts(smokeApproverChat)); got != 0 {
		t.Fatalf("the approver chat carries %d session fact(s), want none", got)
	}
	if got := smokeErrorsTextCount(rig, smokeApproverChat, smokeReflectionMarker); got != 0 {
		t.Fatalf("the reflection summary reached the approver chat %d time(s), want none", got)
	}

	rig.postTask(smokeIntegrityReflectionReadTurn + " plain question")
	rig.waitText(chat, "answer "+smokeIntegrityReflectionReadTurn, 120*time.Second)
	if !smokeUserRequestContains(provider, smokeReflectionMarker) {
		t.Fatalf("the turn after the meditation delivery read no reflection summary\nreflect requests:\n%s",
			smokeRequestDetail(provider, smokeReflectModel))
	}
	if got := smokeIntegrityReflectionDeliveries(rig, chat); got != 1 {
		t.Fatalf("the reflection landed %d time(s) on %s, want exactly one", got, chat)
	}
	t.Logf("[integrity-reflection-consumer] deliver_summary by the hosted meditation agent -> InjectReflection -> %s read %s; the approver chain stayed empty",
		chat, smokeReflectionMarker)
}

// smokeIntegrityReflectionDeliveries 报告某会话链上来自冥想的回流事实数。
func smokeIntegrityReflectionDeliveries(rig *smokeRig, chat string) int {
	count := 0
	for _, evt := range rig.sessionFacts(chat) {
		if evt.Type != event.TypeExternalInput {
			continue
		}
		if evt.Meta[event.MetaKeyTriggerSource] != meditation.LineageReflection {
			continue
		}
		if strings.Contains(evt.Message.Content, smokeReflectionMarker) {
			count++
		}
	}
	return count
}

// smokeIntegrityFoldPositive 判定小窗折叠真的触发：投影长出带早期输入的历史
// 摘要、原文票据逐字节可达、综述进入触发轮之后的正式请求，且折叠铸出的合成
// 条目与压缩请求都是自管产物。
func smokeIntegrityFoldPositive(t *testing.T) {
	host := bootSmokeIntegrityHost(t, "1h", "")
	defer host.close(t)
	rig, provider := host.rig, host.provider
	chat := smokeChats[1]

	smokeIntegrityAssertFold(t, rig, provider, chat)
	compress := smokeIntegrityCompressFacts(t, rig, chat)
	if len(compress) == 0 {
		t.Fatalf("the fold filed no compress request on %s", chat)
	}
	for _, fact := range compress {
		if got := fact.Meta[event.MetaKeyTriggerSource]; !meditation.SelfManagedLineage(got) {
			t.Fatalf("the compress product carries trigger %q, want a self-managed lineage", got)
		}
		if (meditation.NoveltyPolicy{}).Novel(fact) {
			t.Fatalf("the compress product opened the freshness gate (key=%s)", fact.Key)
		}
	}
	entries, carried := rig.host.Run().ReplayEntries(rig.addressOf(chat).SessionID)
	if !carried {
		t.Fatalf("session %s registered no projection after the fold", chat)
	}
	synthetics := syntheticEntries(entries)
	if len(synthetics) == 0 {
		t.Fatalf("the fold minted no synthetic entry on %s", chat)
	}
	for _, entry := range synthetics {
		if !replay.SyntheticKey(entry.Key) {
			t.Fatalf("the fold product %s is not a synthetic key", entry.Key)
		}
	}
	t.Logf("[integrity-fold-positive] %s folded into %d synthetic entr(ies) and %d compress request(s); the original ticket stayed reachable",
		chat, len(synthetics), len(compress))
}

// smokeIntegrityFoldNegative 判定折叠产物的负向语义。产物不自激：把宿主写下
// 的链重建为投影，同一窗口的折叠只铸同样的分段产物，第二个折叠不会消费任何
// 材料、也不会铸任何产物。重启不新折：同一存储上重启宿主后，新的正式回合可以
// 经恢复的综述读到重启前的旧材料——呈现既折叠态是合法的——但不得新铸折叠产物、
// 不得新写压缩请求：重启后的合成键集落在重启前的合成键集内，事实链上的压缩
// 请求数不增，原文票据跨重启仍逐字节可读；重启之后的在线回合自行再折归规则
// 自身，禁止的只是旧材料在综述之外重现。
func smokeIntegrityFoldNegative(t *testing.T) {
	host := bootSmokeIntegrityHost(t, "1ms", "0s")
	defer host.close(t)
	rig, provider := host.rig, host.provider
	chat := smokeChats[1]

	smokeIntegrityAssertFold(t, rig, provider, chat)
	compress := smokeIntegrityCompressFacts(t, rig, chat)
	if len(compress) == 0 {
		t.Fatalf("the fold filed no compress request on %s", chat)
	}
	for _, fact := range compress {
		if got := fact.Meta[event.MetaKeyTriggerSource]; !meditation.SelfManagedLineage(got) {
			t.Fatalf("the compress product carries trigger %q, want a self-managed lineage", got)
		}
		if (meditation.NoveltyPolicy{}).Novel(fact) {
			t.Fatalf("the compress product opened the freshness gate (key=%s)", fact.Key)
		}
	}
	entries, carried := rig.host.Run().ReplayEntries(rig.addressOf(chat).SessionID)
	if !carried {
		t.Fatalf("session %s registered no projection after the fold", chat)
	}
	synthetics := syntheticEntries(entries)
	if len(synthetics) == 0 {
		t.Fatalf("the fold minted no synthetic entry on %s", chat)
	}
	for _, entry := range synthetics {
		if !replay.SyntheticKey(entry.Key) {
			t.Fatalf("the fold product %s is not a synthetic key", entry.Key)
		}
	}
	settled := smokeIntegrityWaitReflectSettled(t, provider, 500*time.Millisecond, 60*time.Second)
	noNovelty := host.logs.count(": no-novelty (")
	smokeIntegrityAssertReflectQuiet(t, provider, settled, time.Second)
	if got := host.logs.count(": no-novelty ("); got != noNovelty {
		t.Fatalf("the fold product opened %d no-novelty pass(es), want none", got-noNovelty)
	}

	threshold := tagent.FoldWindow{MaxTokens: 2000, CompressThreshold: 0.3}.VolumeThresholdBytes()
	plan := replay.AgeFoldPlan{KeepRecent: 2, VolumeThreshold: threshold}
	rebuilt := smokeIntegrityRebuildProjection(t, rig, chat)
	first, err := rebuilt.FoldByAge(context.Background(), plan)
	if err != nil {
		t.Fatalf("fold the projection rebuilt from the chain: %v", err)
	}
	if !first.Triggered || len(first.Products) == 0 {
		t.Fatalf("the rebuilt projection folded %+v, want the fold the material is due", first)
	}
	recovered := smokeEntryDigest(rebuilt.Entries())
	second, err := rebuilt.FoldByAge(context.Background(), plan)
	if err != nil {
		t.Fatalf("fold the rebuilt projection again: %v", err)
	}
	if second.NewFolded != 0 || len(second.Products) != 0 {
		t.Fatalf("the second fold consumed %d entr(ies) and minted %d product(s), want none: a fold product must not fold itself again",
			second.NewFolded, len(second.Products))
	}
	if got := smokeEntryDigest(rebuilt.Entries()); got != recovered {
		t.Fatalf("the second fold rewrote the projection:\nbefore:\n%s\nafter:\n%s", recovered, got)
	}

	original := event.Event{}
	for _, evt := range rig.sessionFacts(chat) {
		if evt.Type == event.TypeExternalInput &&
			strings.Contains(evt.Message.Content, smokeIntegrityFoldEarlyTurn) {
			original = evt
			break
		}
	}
	if original.Key == "" {
		t.Fatalf("no external_input fact carries %s before the restart", smokeIntegrityFoldEarlyTurn)
	}
	committed, err := rig.host.Run().CommittedEvent(context.Background(), original.Key)
	if err != nil {
		t.Fatalf("read the committed original %s beside the fold: %v", original.Key, err)
	}
	if !strings.Contains(committed.Message.Content, smokeIntegrityFoldTail) {
		t.Fatalf("the committed original lost its tail beside the fold (fact %s)", original.Key)
	}
	synthetics = smokeIntegrityFoldBoundary(t, rig, chat)
	boundaryKeys := make([]string, 0, len(synthetics))
	for _, entry := range synthetics {
		boundaryKeys = append(boundaryKeys, string(entry.Key))
	}
	t.Logf("[integrity-fold-boundary] %s held %d fold product(s) at the restore boundary: %v",
		chat, len(synthetics), boundaryKeys)
	host.close(t)
	rig.reboot()

	entriesAfter, carried := rig.host.Run().ReplayEntries(rig.addressOf(chat).SessionID)
	if !carried {
		t.Fatalf("session %s registered no projection after the restart", chat)
	}
	before := make(map[event.EventID]struct{}, len(synthetics))
	beforeKeys := make([]string, 0, len(synthetics))
	for _, entry := range synthetics {
		before[entry.Key] = struct{}{}
		beforeKeys = append(beforeKeys, string(entry.Key))
	}
	restored := syntheticEntries(entriesAfter)
	restoredKeys := make(map[event.EventID]struct{}, len(restored))
	afterKeys := make([]string, 0, len(restored))
	for _, entry := range restored {
		restoredKeys[entry.Key] = struct{}{}
		afterKeys = append(afterKeys, string(entry.Key))
	}
	if len(restored) == 0 {
		t.Fatalf("the restart restored no fold product, want the %d key(s) the chain held %v", len(beforeKeys), beforeKeys)
	}
	for _, entry := range restored {
		if _, kept := before[entry.Key]; !kept {
			t.Fatalf("the restart minted the synthetic key %s, want the restored key set %v to equal the pre-restart %v",
				entry.Key, afterKeys, beforeKeys)
		}
	}
	for _, key := range beforeKeys {
		if _, kept := restoredKeys[event.EventID(key)]; !kept {
			t.Fatalf("the restart dropped the fold product %s the chain held %v, want the restored key set %v",
				key, beforeKeys, afterKeys)
		}
	}
	if filed := smokeIntegrityCompressFacts(t, rig, chat); len(filed) != 0 {
		t.Fatalf("the restore filed %d compress request(s), want none: the fold form is projection-derived and writes no judgment of its own", len(filed))
	}
	restoredOriginal, err := rig.host.Run().CommittedEvent(context.Background(), original.Key)
	if err != nil {
		t.Fatalf("read the committed original %s after the restart: %v", original.Key, err)
	}
	if !strings.Contains(restoredOriginal.Message.Content, smokeIntegrityFoldTail) {
		t.Fatalf("the committed original lost its tail across the restart (fact %s)", original.Key)
	}

	rig.route(chat, smokeIntegrityRebuildTurn)
	rig.waitText(chat, "answer "+smokeIntegrityRebuildTurn, 120*time.Second)
	restart := smokeIntegrityRequestsForLastUser(provider, smokeEntryModel, smokeIntegrityRebuildTurn)
	if len(restart) != 1 {
		t.Fatalf("the restarted turn drove %d entry request(s), want one", len(restart))
	}
	for _, msg := range restart[0].Messages {
		if strings.Contains(msg.Content, smokeIntegrityFoldEarlyTurn) && !strings.Contains(msg.Content, "history summary:") {
			t.Fatalf("the restarted turn presented the pre-restart material %q outside the summary: %q",
				smokeIntegrityFoldEarlyTurn, msg.Content)
		}
	}
	settled = smokeIntegrityWaitReflectSettled(t, provider, 500*time.Millisecond, 60*time.Second)
	smokeIntegrityAssertReflectQuiet(t, provider, settled, time.Second)
	t.Logf("[integrity-fold-negative] the rebuilt projection minted %d product(s) and a second fold consumed nothing; the restart restored the %d fold product(s) the chain held %v with no judgment of its own and the original %s stayed readable across it",
		len(first.Products), len(restored), afterKeys, original.Key)
}

// smokeIntegrityFoldBoundary 报告活进程在重启边界持有的折叠产物，作为重启恢复键集
// 的比较基准。在线折叠在每次提交后重新审视投影，反思落定之前的读取可能落后于进程
// 随后铸造的产物；重启必须恢复进程在关闭前持有的键集，边界读取因此在关闭前取。
func smokeIntegrityFoldBoundary(t *testing.T, rig *smokeRig, chat string) []replay.ProjectionEntry {
	t.Helper()
	entries, carried := rig.host.Run().ReplayEntries(rig.addressOf(chat).SessionID)
	if !carried {
		t.Fatalf("session %s registered no projection at the restore boundary", chat)
	}
	synthetics := syntheticEntries(entries)
	if len(synthetics) == 0 {
		t.Fatalf("the fold minted no synthetic entry on %s at the restore boundary", chat)
	}
	return synthetics
}

// smokeIntegrityRebuildProjection 从宿主真实写下的会话事实链重建投影：压缩请求
// 是判断而非材料，重建跳过它，与产品冷启动的映射一致。
func smokeIntegrityRebuildProjection(t *testing.T, rig *smokeRig, chat string) *replay.ReplayProjection {
	t.Helper()
	session := rig.addressOf(chat).SessionID
	chainKey := replay.ChainKey{PartitionID: 1, SessionID: session}
	chain := replay.NewMemoryEventChain()
	for _, evt := range rig.host.Run().SessionFacts(event.Address{SessionID: session}) {
		if evt.Type == event.CompressRequestType || evt.Message.Content == "" {
			continue
		}
		if err := chain.Commit(context.Background(), replay.ChainFact{
			Key:     evt.Key,
			Chain:   chainKey,
			Type:    evt.Type,
			Summary: smokeIntegrityChainSummary(evt.Meta[event.MetaKeyEventSummary], evt.Message.Content),
			Content: evt.Message.Content,
			Time:    evt.Timestamp,
		}); err != nil {
			t.Fatalf("commit the chain fact %s: %v", evt.Key, err)
		}
	}
	rebuilt := replay.NewReplayProjection()
	if _, err := replay.RebuildProjectionFromWAL(context.Background(), rebuilt, chain, replay.NewMemoryRelations(), chainKey, replay.RebuildOptions{}); err != nil {
		t.Fatalf("rebuild the projection from the session chain: %v", err)
	}
	return rebuilt
}

// smokeIntegrityChainSummary 复现宿主提交时给投影条目算的一行摘要（无显式摘要
// 时取正文首行，超过 120 个字符截断），重建的投影因此与活投影同形。
func smokeIntegrityChainSummary(summary, content string) string {
	if summary != "" {
		return summary
	}
	line := content
	if at := strings.IndexByte(line, '\n'); at >= 0 {
		line = line[:at]
	}
	runes := []rune(line)
	if len(runes) <= 120 {
		return line
	}
	return string(runes[:120]) + "…"
}

// smokeIntegrityFoldConsumer 判定正式请求装配持续消费折叠产物：折叠之后的下一
// 个 /task 回合仍从投影读到综述，且被折掉的原文标记随综述进入请求。
func smokeIntegrityFoldConsumer(t *testing.T) {
	host := bootSmokeIntegrityHost(t, "1h", "")
	defer host.close(t)
	rig, provider := host.rig, host.provider
	chat := smokeChats[0]

	smokeIntegrityAssertFold(t, rig, provider, chat)
	rig.postTask(smokeIntegrityFoldFollowTurn + " plain question")
	rig.waitText(chat, "answer "+smokeIntegrityFoldFollowTurn, 120*time.Second)
	follow := smokeIntegrityRequestsForLastUser(provider, smokeEntryModel, smokeIntegrityFoldFollowTurn)
	if len(follow) != 1 {
		t.Fatalf("the follow-up turn drove %d entry request(s), want one", len(follow))
	}
	if !strings.Contains(follow[0].text(), "history summary:") {
		t.Fatalf("the follow-up request read no history summary\nrequests carrying a history summary:\n%s",
			smokeRequestsContaining(provider, "history summary"))
	}
	if !strings.Contains(follow[0].text(), "external_input: "+smokeIntegrityFoldEarlyTurn) {
		t.Fatalf("the follow-up request read no summary carrying the folded %s", smokeIntegrityFoldEarlyTurn)
	}
	entries, carried := rig.host.Run().ReplayEntries(rig.addressOf(chat).SessionID)
	if !carried {
		t.Fatalf("session %s registered no projection", chat)
	}
	threshold := tagent.FoldWindow{MaxTokens: 2000, CompressThreshold: 0.3}.VolumeThresholdBytes()
	if volume := len(replay.RenderProjection(entries)); volume > threshold {
		t.Fatalf("the projection grew to %d bytes after the follow-up, want <= the %d-byte window", volume, threshold)
	}
	t.Logf("[integrity-fold-consumer] the formal follow-up request on %s consumed the history summary carrying %s",
		chat, smokeIntegrityFoldEarlyTurn)
}

// smokeIntegrityInflightVersion 把一轮入口请求挂在模型端点上，在它借出开始版本时
// 热换：替换不得等待在途流，放行后的回答仍由开始版本服务；下一轮才用新版本。
func smokeIntegrityInflightVersion(t *testing.T) {
	cfg := newSmokeConfig(t, "http://127.0.0.1:1/v1")
	cfg.MeditationInterval = "1h"
	provider := newSmokeProvider(t, filepath.Join(cfg.WorkspaceDir, smokeFileMarker))
	cfg.Endpoint = provider.endpoint()
	cfg.write(t)

	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	provider.hook = func(w http.ResponseWriter, _ int, modelName string, messages []smokeModelMessage) bool {
		if modelName != smokeEntryModel {
			return false
		}
		if !strings.Contains(smokeLastUserText(messages), smokeIntegrityInflightMarker) {
			return false
		}
		once.Do(func() { close(entered) })
		<-release
		fmt.Fprint(w, smokeAnswerBody(smokeIntegrityInflightAnswer))
		return true
	}

	rig := bootSmokeRig(t, cfg, provider)
	chat := smokeChats[0]
	if err := rig.channel.emit(context.Background(),
		textMessage(chat, smokeIntegrityInflightTurn+" "+smokeIntegrityInflightMarker)); err != nil {
		t.Fatalf("emit the in-flight message: %v", err)
	}
	select {
	case <-entered:
	case <-time.After(60 * time.Second):
		t.Fatalf("the in-flight turn reached no model request")
	}
	inFlightBefore := smokeRequestsFor(provider, smokeEntryModel)

	done := make(chan smokeIntegrityReloadResult, 1)
	go func() { done <- rig.smokeIntegrityReload(smokeReloadModel) }()
	var reloaded smokeReloadAnswer
	select {
	case result := <-done:
		if result.err != nil {
			t.Fatalf("the reload beside the in-flight request: %v", result.err)
		}
		reloaded = result.answer
	case <-time.After(30 * time.Second):
		t.Fatalf("the reload waited for the in-flight request: a replacement must not block a borrowed version")
	}
	if !reloaded.Reloaded || len(reloaded.ModelChanges) != 1 {
		t.Fatalf("reload beside the in-flight request = %+v, want one judged model change", reloaded)
	}
	publications := rig.host.Run().LeasePublications()
	if len(publications) != 1 || publications[0].Model != smokeReloadModel {
		t.Fatalf("lease publications beside the in-flight request = %+v, want the one installed version", publications)
	}
	if losses := rig.host.Run().LeaseLosses(); len(losses) != 0 {
		t.Fatalf("the reload lost %d replacement judgment(s): %v", len(losses), losses)
	}

	close(release)
	rig.waitText(chat, smokeIntegrityInflightAnswer, 60*time.Second)
	served := smokeIntegrityRequestsForLastUser(provider, smokeEntryModel, smokeIntegrityInflightMarker)
	if len(served) != 1 || served[0].Model != smokeEntryModel {
		t.Fatalf("the in-flight request was served %d time(s) under %v, want once under %s",
			len(served), smokeIntegrityModels(served), smokeEntryModel)
	}
	if got := smokeRequestsFor(provider, smokeEntryModel); got != inFlightBefore {
		t.Fatalf("the in-flight turn drove %d further old-version request(s) after the reload, want none", got-inFlightBefore)
	}

	rig.postTask(smokeIntegrityAfterInflight + " plain question")
	rig.waitText(chat, "answer "+smokeIntegrityAfterInflight, 120*time.Second)
	if got := smokeIntegrityRequestsForLastUser(provider, smokeReloadModel, smokeIntegrityAfterInflight); len(got) != 1 {
		t.Fatalf("the turn after the in-flight reload drove %d request(s) under %s, want one", len(got), smokeReloadModel)
	}
	if err := rig.close(); err != nil {
		t.Fatalf("close the in-flight host: %v", err)
	}
	t.Logf("[integrity-replacement-positive] the reload returned beside the in-flight stream; the stream stayed on %s and the next turn used %s",
		smokeEntryModel, smokeReloadModel)
}

// smokeIntegrityReplacementPositive 判定 /reload 安装新版本、下一 /task 请求
// 取新版本、在途流保持开始版本，且替换事实是自管产物（不重开新颖门）。
func smokeIntegrityReplacementPositive(t *testing.T) {
	host := bootSmokeIntegrityHost(t, "1h", "")
	defer host.close(t)
	rig, provider := host.rig, host.provider
	chat := smokeChats[0]

	rig.drive(chat, smokeIntegrityReplacementWarmTurn+" plain question")
	rig.waitText(chat, "answer "+smokeIntegrityReplacementWarmTurn, 120*time.Second)

	answer := rig.reload(smokeReloadModel)
	if !answer.Reloaded || len(answer.ModelChanges) != 1 {
		t.Fatalf("reload = %+v, want one judged model change", answer)
	}
	publications := rig.host.Run().LeasePublications()
	if len(publications) != 1 || publications[0].Model != smokeReloadModel {
		t.Fatalf("lease publications = %+v, want the one installed %s version", publications, smokeReloadModel)
	}
	replacement, err := rig.host.Run().CommittedEvent(context.Background(), event.EventID(answer.ModelChanges[0].Fact))
	if err != nil {
		t.Fatalf("read the committed replacement fact: %v", err)
	}
	if got := replacement.Meta[event.MetaKeyTriggerSource]; !meditation.SelfManagedLineage(got) {
		t.Fatalf("the replacement product carries trigger %q, want a self-managed lineage", got)
	}
	if (meditation.NoveltyPolicy{}).Novel(replacement) {
		t.Fatalf("the replacement product opened the freshness gate (key=%s)", replacement.Key)
	}

	rig.postTask(smokeIntegrityReloadTurn + " plain question")
	rig.waitText(chat, "answer "+smokeIntegrityReloadTurn, 120*time.Second)
	if got := smokeIntegrityRequestsForLastUser(provider, smokeReloadModel, smokeIntegrityReloadTurn); len(got) != 1 {
		t.Fatalf("the turn after the reload drove %d request(s) under %s, want one", len(got), smokeReloadModel)
	}

	host.close(t)
	smokeIntegrityInflightVersion(t)
	t.Logf("[integrity-replacement-positive] %s installed %s and the next request %s used it",
		answer.ModelChanges[0].From, smokeReloadModel, smokeIntegrityReloadTurn)
}

// smokeIntegrityReplacementNegative 判定两条负向语义：同一文档重复热换不安装
// 第二个版本、替换失败不安装且下一轮仍由租约持有的版本服务。
func smokeIntegrityReplacementNegative(t *testing.T) {
	smokeIntegrityReplacementRepeat(t)
	smokeIntegrityReplacementRefused(t)
}

// smokeIntegrityReplacementRepeat 对同一文档再热换：不判第二次移动、不安装
// 第二个版本，也不为替换产物重开一轮反思。
func smokeIntegrityReplacementRepeat(t *testing.T) {
	host := bootSmokeIntegrityHost(t, "1h", "")
	defer host.close(t)
	rig, provider := host.rig, host.provider
	chat := smokeChats[0]

	rig.drive(chat, smokeIntegrityReplacementWarmTurn+" plain question")
	rig.waitText(chat, "answer "+smokeIntegrityReplacementWarmTurn, 120*time.Second)
	settled := smokeIntegrityWaitReflectSettled(t, provider, 500*time.Millisecond, 60*time.Second)
	noNovelty := host.logs.count(": no-novelty (")

	first := rig.reload(smokeReloadModel)
	if !first.Reloaded || len(first.ModelChanges) != 1 {
		t.Fatalf("the first reload = %+v, want one judged model change", first)
	}
	installed := len(rig.host.Run().LeasePublications())
	if installed != 1 {
		t.Fatalf("the first reload published %d version(s), want one", installed)
	}
	again := rig.reload(smokeReloadModel)
	if !again.Reloaded || len(again.ModelChanges) != 0 {
		t.Fatalf("the identical reload = %+v, want no judged model change", again)
	}
	if got := len(rig.host.Run().LeasePublications()); got != installed {
		t.Fatalf("the identical reload published %d version(s), want the installed %d left alone", got, installed)
	}
	if got := host.logs.count(": no-novelty ("); got != noNovelty {
		t.Fatalf("the identical reload opened %d no-novelty pass(es), want none", got-noNovelty)
	}
	smokeIntegrityAssertReflectQuiet(t, provider, settled, 500*time.Millisecond)
	t.Logf("[integrity-replacement-negative] the identical reload left the installed %d version(s) and opened no reflection pass", installed)
}

// smokeIntegrityReplacementRefused 把入口模型热换到未声明的 provider：替换事实
// 照常提交，租约重建被拒（Unwrap 到 rl.ErrLeaseRebuildFailed），不发布新版本，
// 下一轮仍由租约持有的版本服务。
func smokeIntegrityReplacementRefused(t *testing.T) {
	host := bootSmokeIntegrityHost(t, "1h", "")
	defer host.close(t)
	rig, provider := host.rig, host.provider
	chat := smokeChats[1]

	rig.drive(chat, smokeIntegrityReplacementWarmTurn+" plain question")
	rig.waitText(chat, "answer "+smokeIntegrityReplacementWarmTurn, 120*time.Second)

	host.cfg.Doc = smokeErrorsBadProviderDocument
	result := rig.smokeIntegrityReload(smokeReloadModel)
	if result.err != nil {
		t.Fatalf("the refused replacement: %v", result.err)
	}
	answer := result.answer
	if !answer.Reloaded || len(answer.ModelChanges) != 1 {
		t.Fatalf("reload = %+v, want one judged model change", answer)
	}
	change := answer.ModelChanges[0]
	if want := smokeErrorsMissingProvider + "/" + smokeReloadModel; change.To != want {
		t.Fatalf("model change = %+v, want %s", change, want)
	}
	run := rig.host.Run()
	if publications := run.LeasePublications(); len(publications) != 0 {
		t.Fatalf("the refused replacement published %d version(s): %+v", len(publications), publications)
	}
	if losses := run.LeaseLosses(); len(losses) != 0 {
		t.Fatalf("the refused replacement lost %d change(s): %v", len(losses), losses)
	}
	gapFound := false
	for _, presentation := range run.Presentations() {
		if string(presentation.Fact) != change.Fact {
			continue
		}
		for _, gap := range presentation.Gaps {
			if gap.Plugin != "model-lease" {
				continue
			}
			gapFound = true
			if !errors.Is(gap.Err, rl.ErrLeaseRebuildFailed) {
				t.Fatalf("gap error = %v, want ErrLeaseRebuildFailed", gap.Err)
			}
			if !strings.Contains(gap.Err.Error(), smokeErrorsMissingProvider) {
				t.Fatalf("refusal %q does not name the missing provider", gap.Err)
			}
		}
	}
	if !gapFound {
		t.Fatalf("no model-lease observation gap on the replacement fact %s", change.Fact)
	}
	if host.logs.count("plugin=model-lease") == 0 || host.logs.count("result=refused") == 0 {
		t.Fatalf("the refused replacement left no model-lease refusal line")
	}

	rig.postTask(smokeIntegrityReplacementAfterRefused + " plain question")
	rig.waitText(chat, "answer "+smokeIntegrityReplacementAfterRefused, 120*time.Second)
	if got := smokeIntegrityRequestsForLastUser(provider, smokeEntryModel, smokeIntegrityReplacementAfterRefused); len(got) != 1 {
		t.Fatalf("the turn after the refused replacement drove %d request(s) under %s, want one", len(got), smokeEntryModel)
	}
	for _, req := range provider.recorded() {
		if req.Model == smokeReloadModel {
			t.Fatalf("the refused version %q served a turn", smokeReloadModel)
		}
	}
	t.Logf("[integrity-replacement-negative] the reload judged %s -> %s and refused it; %s kept serving %s",
		change.From, change.To, smokeIntegrityReplacementAfterRefused, smokeEntryModel)
}

// smokeIntegrityReplacementConsumer 只走正式 HTTP 链：渠道入站准入会话，
// POST /reload 安装新版本，随后的 POST /task 请求由新版本服务。
func smokeIntegrityReplacementConsumer(t *testing.T) {
	host := bootSmokeIntegrityHost(t, "5s", "")
	defer host.close(t)
	rig, provider := host.rig, host.provider
	chat := smokeChats[0]

	if err := rig.channel.emit(context.Background(),
		textMessage(chat, smokeIntegrityReplacementWarmTurn+" plain question")); err != nil {
		t.Fatalf("emit the warm turn: %v", err)
	}
	rig.waitText(chat, "answer "+smokeIntegrityReplacementWarmTurn, 120*time.Second)

	result := rig.smokeIntegrityReload(smokeReloadModel)
	if result.err != nil || !result.answer.Reloaded || len(result.answer.ModelChanges) != 1 {
		t.Fatalf("reload = (%+v, %v), want one judged model change", result.answer, result.err)
	}
	rig.postTask(smokeIntegrityConsumerReload + " plain question")
	rig.waitText(chat, "answer "+smokeIntegrityConsumerReload, 120*time.Second)
	if got := smokeIntegrityRequestsForLastUser(provider, smokeReloadModel, smokeIntegrityConsumerReload); len(got) != 1 {
		t.Fatalf("the injected turn after the reload drove %d request(s) under %s, want one", len(got), smokeReloadModel)
	}
	if got := smokeIntegrityRequestsForLastUser(provider, smokeEntryModel, smokeIntegrityConsumerReload); len(got) != 0 {
		t.Fatalf("the injected turn after the reload drove %d request(s) under the old %s, want none", len(got), smokeEntryModel)
	}
	t.Logf("[integrity-replacement-consumer] /reload -> /task: %s served by %s", smokeIntegrityConsumerReload, smokeReloadModel)
}

// smokeIntegrityCapturePositive 判定采集账目：attempts.jsonl 的记录数逐项等于
// 实际模型请求数（含委派子 agent 调用），seal 已封、dropped=0、in-flight=0。
func smokeIntegrityCapturePositive(t *testing.T) {
	host := bootSmokeIntegrityHost(t, "1h", "")
	defer host.close(t)
	rig, provider := host.rig, host.provider
	chat := smokeChats[0]

	rig.drive(chat, smokeIntegrityCaptureTurn+" plain question")
	rig.waitText(chat, "answer "+smokeIntegrityCaptureTurn, 120*time.Second)
	rig.postTask(smokeIntegrityCaptureSecond + " smoke delegate")
	rig.waitText(chat, "delegate ack "+smokeIntegrityCaptureSecond, 120*time.Second)
	smokeIntegrityWaitReflectSettled(t, provider, 300*time.Millisecond, 60*time.Second)

	host.close(t)
	trajectory, err := readTrajectory(host.cfg.StorageRoot)
	if err != nil {
		t.Fatalf("read the trajectory under %s: %v", host.cfg.StorageRoot, err)
	}
	if trajectory.Seal == nil {
		t.Fatal("the trajectory carries no seal manifest")
	}
	seal := *trajectory.Seal
	if !seal.Sealed {
		t.Fatalf("the trajectory seal is not sealed: %+v", seal)
	}
	if seal.Partial {
		t.Fatalf("the trajectory seal is partial: reasons=%v in_flight=%d", seal.Reasons, seal.InFlight)
	}
	if seal.Dropped != 0 {
		t.Fatalf("the trajectory capture dropped %d record(s)", seal.Dropped)
	}
	if seal.InFlight != 0 {
		t.Fatalf("the sealed trajectory still counts %d in-flight record(s)", seal.InFlight)
	}
	total := provider.count()
	if total == 0 {
		t.Fatal("the capture host drove no model request")
	}
	if trajectory.Records != total {
		t.Fatalf("attempts.jsonl = %d lines against %d model calls\nattempt heads:\n%s\nrequests:\n%s",
			trajectory.Records, total, strings.Join(trajectory.Heads, "\n"), smokeRequestHeads(provider))
	}
	if int(seal.Records) != total {
		t.Fatalf("seal records = %d against %d model calls", seal.Records, total)
	}
	if got := len(trajectory.Heads); got != trajectory.Records {
		t.Fatalf("the trajectory reported %d records but %d heads", trajectory.Records, got)
	}
	recordsEntry := false
	for _, file := range seal.Files {
		if file.Name != rl.SinkRecordsFile {
			continue
		}
		recordsEntry = true
		if file.Records != int64(total) {
			t.Fatalf("seal entry %s = %d record(s), want %d", file.Name, file.Records, total)
		}
	}
	if !recordsEntry {
		t.Fatalf("the seal names no %s entry: files=%+v", rl.SinkRecordsFile, seal.Files)
	}
	t.Logf("[integrity-capture-positive] attempts=%d model-calls=%d seal-records=%d dropped=%d in-flight=%d",
		trajectory.Records, total, seal.Records, seal.Dropped, seal.InFlight)
}

// smokeIntegrityCaptureNegative 判定两条负向语义：未发生请求不产生记录也不入
// 损失账目；失败尝试记为失败终端并带错误，绝不记成成功。
func smokeIntegrityCaptureNegative(t *testing.T) {
	smokeIntegrityCaptureNoRequest(t)
	smokeIntegrityCaptureFailedAttempt(t)
}

// smokeIntegrityCaptureNoRequest 在没有会话时拒结注入面：模型请求为零，采集
// 因此没有任何记录文件；seal 仍然完整且 dropped=0、in-flight=0——准入拒绝不
// 进入损失账目，也不是一条被丢弃的记录。
func smokeIntegrityCaptureNoRequest(t *testing.T) {
	host := bootSmokeIntegrityHost(t, "5s", "")
	defer host.close(t)
	rig, provider := host.rig, host.provider

	status, body := rig.postTaskStatus(smokeIntegrityCaptureRejected + " before any session")
	if status != http.StatusServiceUnavailable {
		t.Fatalf("task injection before any session = %d %q, want 503", status, body)
	}
	if !strings.Contains(body, "no user session") {
		t.Fatalf("the refusal body = %q, want the named no-session reason", body)
	}
	if got := provider.count(); got != 0 {
		t.Fatalf("the refused injection drove %d model request(s), want none", got)
	}

	host.close(t)
	seal, err := smokeIntegritySeal(host.cfg.StorageRoot)
	if err != nil {
		t.Fatalf("read the capture seal: %v", err)
	}
	if !seal.Sealed {
		t.Fatalf("the empty capture seal is not sealed: %+v", seal)
	}
	if seal.Partial {
		t.Fatalf("the empty capture seal is partial: %+v", seal)
	}
	if seal.Records != 0 || seal.Dropped != 0 || seal.InFlight != 0 {
		t.Fatalf("the empty capture seal = records=%d dropped=%d in-flight=%d, want all zero",
			seal.Records, seal.Dropped, seal.InFlight)
	}
	if len(seal.Files) != 0 {
		t.Fatalf("the empty capture wrote %d file(s), want none: no request means no record", len(seal.Files))
	}
	lines, err := smokeIntegrityAttemptLines(host.cfg.StorageRoot)
	if err != nil {
		t.Fatalf("read the attempts file: %v", err)
	}
	if len(lines) != 0 {
		t.Fatalf("the empty capture wrote %d attempt line(s), want none", len(lines))
	}
	t.Logf("[integrity-capture-negative] the refused injection drove no request and the sealed capture stays empty (dropped=0, in-flight=0)")
}

// smokeIntegrityCaptureFailedAttempt 让一轮入口请求返回 500：失败尝试以失败终端
// 入册并带错误文本，绝不记成成功；记录数仍等于实际请求数。
func smokeIntegrityCaptureFailedAttempt(t *testing.T) {
	host := bootSmokeIntegrityHost(t, "1h", "")
	defer host.close(t)
	rig, provider := host.rig, host.provider
	chat := smokeChats[1]

	host.provider.hook = func(w http.ResponseWriter, _ int, modelName string, messages []smokeModelMessage) bool {
		if modelName != smokeEntryModel {
			return false
		}
		if !strings.Contains(smokeLastUserText(messages), smokeIntegrityCaptureFailed) {
			return false
		}
		http.Error(w, "smoke: the scripted model answers 500", http.StatusInternalServerError)
		return true
	}
	rig.drive(chat, smokeIntegrityCaptureFailed+" smoke five hundred")
	rig.waitText(chat, smokeErrorsFailureText, 60*time.Second)
	smokeIntegrityWaitReflectSettled(t, provider, 300*time.Millisecond, 60*time.Second)
	failedRequests := smokeIntegrityRequestsForLastUser(provider, smokeEntryModel, smokeIntegrityCaptureFailed)
	if len(failedRequests) == 0 {
		t.Fatal("the failing turn reached no model request")
	}

	host.close(t)
	lines, err := smokeIntegrityAttemptLines(host.cfg.StorageRoot)
	if err != nil {
		t.Fatalf("read the attempts file: %v", err)
	}
	if len(lines) == 0 {
		t.Fatal("the failing turn left no attempt record")
	}
	if len(lines) > provider.count() {
		t.Fatalf("attempts.jsonl = %d line(s) against %d model call(s): the capture cannot record more attempts than calls",
			len(lines), provider.count())
	}
	failedAttempts, failedForChat := 0, 0
	for _, line := range lines {
		if line.Attempt == nil {
			continue
		}
		attempt := line.Attempt
		if attempt.Terminal == tagentmodel.KindFailed {
			failedAttempts++
			if strings.Contains(string(attempt.DecisionID), chat) {
				failedForChat++
			}
			if attempt.Error == "" {
				t.Fatalf("the failed attempt %s carries no error text", attempt.ID)
			}
			if !strings.Contains(attempt.Error, "500") {
				t.Fatalf("the failed attempt error = %q, want it to name the 500 response", attempt.Error)
			}
			continue
		}
		if attempt.Error != "" {
			t.Fatalf("attempt %s carries error %q with terminal %v, want a failed terminal",
				attempt.ID, attempt.Error, attempt.Terminal)
		}
	}
	if failedAttempts == 0 {
		t.Fatal("attempts.jsonl records no failed attempt: the 500 was recorded as success")
	}
	if failedForChat == 0 {
		t.Fatalf("attempts.jsonl records no failed attempt on %s, want the failing turn recorded under its decision", chat)
	}
	t.Logf("[integrity-capture-negative] %d failed attempt(s), %d of them on %s, recorded with terminal=%v and the 500 error; no failure was recorded as success",
		failedAttempts, failedForChat, chat, tagentmodel.KindFailed)
}

// smokeIntegrityCaptureConsumer 只从 attempts.jsonl 的正式读取路径核对记录：
// 记录数与模型请求数相等，seal 的文件摘要与磁盘字节一致，测试只读不回写。
func smokeIntegrityCaptureConsumer(t *testing.T) {
	host := bootSmokeIntegrityHost(t, "1h", "")
	defer host.close(t)
	rig, provider := host.rig, host.provider
	chat := smokeChats[0]

	rig.drive(chat, smokeIntegrityCaptureTurn+" plain question")
	rig.waitText(chat, "answer "+smokeIntegrityCaptureTurn, 120*time.Second)
	smokeIntegrityWaitReflectSettled(t, provider, 300*time.Millisecond, 60*time.Second)

	host.close(t)
	trajectory, err := readTrajectory(host.cfg.StorageRoot)
	if err != nil {
		t.Fatalf("read the trajectory under %s: %v", host.cfg.StorageRoot, err)
	}
	if trajectory.Seal == nil {
		t.Fatal("the trajectory carries no seal manifest")
	}
	if got := trajectory.Records; got != provider.count() {
		t.Fatalf("attempts.jsonl = %d line(s) against %d model call(s)", got, provider.count())
	}
	var records *rl.CaptureFileDigest
	for i := range trajectory.Seal.Files {
		if trajectory.Seal.Files[i].Name == rl.SinkRecordsFile {
			records = &trajectory.Seal.Files[i]
		}
	}
	if records == nil {
		t.Fatalf("the seal names no %s entry: files=%+v", rl.SinkRecordsFile, trajectory.Seal.Files)
	}
	path := filepath.Join(host.cfg.StorageRoot, "trajectories", rl.SinkRecordsFile)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	sum := sha256.Sum256(raw)
	if got := hex.EncodeToString(sum[:]); got != records.SHA256 {
		t.Fatalf("the seal digest = %s, want the digest of the bytes on disk %s", records.SHA256, got)
	}
	if int64(len(raw)) != records.Bytes {
		t.Fatalf("the seal bytes = %d, want %d", records.Bytes, len(raw))
	}
	if records.Records != int64(trajectory.Records) {
		t.Fatalf("the seal entry counts %d record(s), want %d", records.Records, trajectory.Records)
	}
	again, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("re-read %s: %v", path, err)
	}
	if sha256.Sum256(again) != sum {
		t.Fatalf("a second read of %s differs: the consumer must not rewrite the capture", path)
	}
	lines, err := smokeIntegrityAttemptLines(host.cfg.StorageRoot)
	if err != nil {
		t.Fatalf("read the versioned lines: %v", err)
	}
	for _, line := range lines {
		if line.Kind != rl.RecordKindAttempt || line.Attempt == nil {
			t.Fatalf("attempts.jsonl carries a non-attempt line %q", line.Kind)
		}
	}
	if heads := strings.Join(trajectory.Heads, "\n"); !strings.Contains(heads, "answer "+smokeIntegrityCaptureTurn) {
		t.Fatalf("the read-back heads carry no answer for %s:\n%s", smokeIntegrityCaptureTurn, heads)
	}
	t.Logf("[integrity-capture-consumer] read back %d attempt line(s); seal digest %s matches the %d byte(s) on disk",
		trajectory.Records, records.SHA256, len(raw))
}

// smokeIntegritySeal 读回一次采集的 seal.json。
func smokeIntegritySeal(root string) (rl.CaptureManifest, error) {
	raw, err := os.ReadFile(filepath.Join(root, "trajectories", rl.SealFileName))
	if err != nil {
		return rl.CaptureManifest{}, err
	}
	var seal rl.CaptureManifest
	if err := json.Unmarshal(raw, &seal); err != nil {
		return rl.CaptureManifest{}, err
	}
	return seal, nil
}

// smokeIntegrityAttemptLines 读回 attempts.jsonl 的版本化记录行；没有请求的
// 采集不写该文件，读回零行而不是读失败。
func smokeIntegrityAttemptLines(root string) ([]rl.Line, error) {
	raw, err := os.ReadFile(filepath.Join(root, "trajectories", rl.SinkRecordsFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	lines := make([]rl.Line, 0, 16)
	for _, text := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(text) == "" {
			continue
		}
		var line rl.Line
		if err := json.Unmarshal([]byte(text), &line); err != nil {
			return nil, err
		}
		lines = append(lines, line)
	}
	return lines, nil
}

// smokeIntegrityBulk 渲染一条折叠用的单行大正文：标记在首位（摘要取前 120 字符），
// 尾部标记证明原文在事实链上逐字节可达。填充词不带脚本关键词，避免命中响应脚本。
func smokeIntegrityBulk(marker string) string {
	return marker + " " + strings.Repeat("fold-filler-segment ", 60) + smokeIntegrityFoldTail
}

// smokeIntegrityRequestsForLastUser 报告最后一条用户消息含 marker 的模型请求；
// model 非空时只算该模型名。
func smokeIntegrityRequestsForLastUser(provider *smokeProvider, model, marker string) []smokeModelRequest {
	out := make([]smokeModelRequest, 0, 2)
	for _, req := range provider.recorded() {
		if model != "" && req.Model != model {
			continue
		}
		if strings.Contains(smokeLastUserText(req.Messages), marker) {
			out = append(out, req)
		}
	}
	return out
}

// smokeIntegrityModels 渲染一组请求的模型名，供版本漂移失败时对照。
func smokeIntegrityModels(requests []smokeModelRequest) []string {
	out := make([]string, 0, len(requests))
	for _, req := range requests {
		out = append(out, req.Model)
	}
	return out
}

// smokeIntegrityHasFact 报告链上是否有给定类型且正文含子串的事实。
func smokeIntegrityHasFact(facts []event.Event, kind, want string) bool {
	for _, evt := range facts {
		if evt.Type == kind && strings.Contains(evt.Message.Content, want) {
			return true
		}
	}
	return false
}

// smokeIntegrityWaitMeditationFact 等到目标会话链上出现一条冥想投递的外部输入事实。
func smokeIntegrityWaitMeditationFact(t *testing.T, rig *smokeRig, chat string, timeout time.Duration) event.Event {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		for _, evt := range rig.sessionFacts(chat) {
			if evt.Type != event.TypeExternalInput {
				continue
			}
			if evt.Meta[event.MetaKeyTriggerSource] != meditation.LineageReflection {
				continue
			}
			if !strings.Contains(evt.Message.Content, smokeReflectionMarker) {
				continue
			}
			return evt
		}
		if time.Now().After(deadline) {
			t.Fatalf("no meditation summary reached the chain of %s within %s\nfacts:\n%s\nreflect requests:\n%s",
				chat, timeout, rig.factDigest(), smokeRequestDetail(rig.provider, smokeReflectModel))
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// smokeIntegrityWaitDelivered 等到目标会话链上出现一条带指定正文的冥想投递事实。
func smokeIntegrityWaitDelivered(t *testing.T, rig *smokeRig, chat, content string, timeout time.Duration) event.Event {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		for _, evt := range rig.sessionFacts(chat) {
			if evt.Type != event.TypeExternalInput {
				continue
			}
			if evt.Meta[event.MetaKeyTriggerSource] != meditation.LineageReflection {
				continue
			}
			if strings.Contains(evt.Message.Content, content) {
				return evt
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no meditation delivery carrying %q reached %s within %s\nfacts:\n%s",
				content, chat, timeout, rig.factDigest())
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// smokeIntegrityCompressFacts 报告某会话链上的压缩请求事实。压缩请求的 Owner 只有
// 会话，所以用空 AgentID 的地址读整条链。
func smokeIntegrityCompressFacts(t *testing.T, rig *smokeRig, chat string) []event.Event {
	t.Helper()
	session := rig.addressOf(chat).SessionID
	out := make([]event.Event, 0, 2)
	for _, evt := range rig.host.Run().SessionFacts(event.Address{SessionID: session}) {
		if evt.Type == event.CompressRequestType {
			out = append(out, evt)
		}
	}
	return out
}

// smokeIntegrityAssertFold 驱动小窗口折叠并断言三件事：投影长出带早期输入的历史
// 摘要、触发轮之后的请求真的读到了综述、原文在事实链上逐字节仍在而投影已用
// 综述取代它。体积断言取文档声明的窗口（max_tokens 2000 × 4 × 0.3）。
func smokeIntegrityAssertFold(t *testing.T, rig *smokeRig, provider *smokeProvider, chat string) {
	t.Helper()
	rig.route(chat, smokeIntegrityFoldSwitchTurn)
	rig.waitText(chat, "answer "+smokeIntegrityFoldSwitchTurn, 120*time.Second)
	rig.postTask(smokeIntegrityBulk(smokeIntegrityFoldEarlyTurn))
	rig.waitText(chat, "answer "+smokeIntegrityFoldEarlyTurn, 120*time.Second)
	rig.postTask(smokeIntegrityBulk(smokeIntegrityFoldLateTurn))
	rig.waitText(chat, "answer "+smokeIntegrityFoldLateTurn, 120*time.Second)

	entries, carried := rig.host.Run().ReplayEntries(rig.addressOf(chat).SessionID)
	if !carried {
		t.Fatalf("session %s registered no projection", chat)
	}
	synthetic := syntheticEntries(entries)
	proof := ""
	for _, entry := range synthetic {
		if entry.Type == replay.ProjectionTypeHistorySummary &&
			strings.Contains(entry.Content, "external_input: "+smokeIntegrityFoldEarlyTurn) {
			proof = entry.Content
			break
		}
	}
	if proof == "" {
		t.Fatalf("the fold minted no history summary carrying %s (entries=%d synthetic=%d, rendered=%d bytes)\nprojection:\n%s",
			smokeIntegrityFoldEarlyTurn, len(entries), len(synthetic),
			len(replay.RenderProjection(entries)), smokeEntryDigest(entries))
	}

	late := smokeIntegrityRequestsForLastUser(provider, smokeEntryModel, smokeIntegrityFoldLateTurn)
	if len(late) != 1 {
		t.Fatalf("the late fold turn drove %d entry request(s), want one", len(late))
	}
	wantRendered := "external_input: " + smokeIntegrityFoldEarlyTurn
	if !strings.Contains(late[0].text(), "history summary:") || !strings.Contains(late[0].text(), wantRendered) {
		t.Fatalf("the request after the fold read no history summary carrying %q\nprojection:\n%s\nrequests carrying a history summary:\n%s",
			wantRendered, smokeEntryDigest(entries), smokeRequestsContaining(provider, "history summary"))
	}
	threshold := tagent.FoldWindow{MaxTokens: 2000, CompressThreshold: 0.3}.VolumeThresholdBytes()
	if volume := len(replay.RenderProjection(entries)); volume > threshold {
		t.Fatalf("the folded projection is %d bytes, want <= the %d-byte window\nprojection:\n%s",
			volume, threshold, smokeEntryDigest(entries))
	}

	original := event.Event{}
	for _, evt := range rig.sessionFacts(chat) {
		if evt.Type == event.TypeExternalInput &&
			strings.Contains(evt.Message.Content, smokeIntegrityFoldEarlyTurn) {
			original = evt
			break
		}
	}
	if original.Key == "" {
		t.Fatalf("no external_input fact carries %s", smokeIntegrityFoldEarlyTurn)
	}
	committed, err := rig.host.Run().CommittedEvent(context.Background(), original.Key)
	if err != nil {
		t.Fatalf("read the committed original %s after the fold: %v", original.Key, err)
	}
	if !strings.Contains(committed.Message.Content, smokeIntegrityFoldTail) {
		t.Fatalf("the committed original lost its tail after the fold (fact %s)", original.Key)
	}
	for _, entry := range entries {
		if entry.Key == original.Key {
			t.Fatalf("the folded original %s is still an entry of the projection", original.Key)
		}
	}
	t.Logf("[integrity-fold] %s: entries=%d synthetic=%d rendered=%d bytes (window=%d); the next request read a history summary carrying %q and the original fact %s stays reachable",
		chat, len(entries), len(synthetic), len(replay.RenderProjection(entries)), threshold,
		wantRendered, original.Key)
}

// smokeIntegrityWaitReflectCount 等到冥想的模型请求数达到 want。
func smokeIntegrityWaitReflectCount(t *testing.T, provider *smokeProvider, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for smokeRequestsFor(provider, smokeReflectModel) < want {
		if time.Now().After(deadline) {
			t.Fatalf("the reflection drove %d request(s) within %s, want %d\nreflect requests:\n%s",
				smokeRequestsFor(provider, smokeReflectModel), timeout, want, smokeRequestDetail(provider, smokeReflectModel))
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// smokeIntegrityWaitReflectSettled 等到反思请求数在 stable 窗口内稳定下来，报告
// 稳定后的计数。
func smokeIntegrityWaitReflectSettled(t *testing.T, provider *smokeProvider, stable, timeout time.Duration) int {
	t.Helper()
	deadline := time.Now().Add(timeout)
	count := smokeRequestsFor(provider, smokeReflectModel)
	since := time.Now()
	for {
		time.Sleep(25 * time.Millisecond)
		current := smokeRequestsFor(provider, smokeReflectModel)
		if current != count {
			count, since = current, time.Now()
		}
		if time.Since(since) >= stable {
			return count
		}
		if time.Now().After(deadline) {
			t.Fatalf("the reflection requests did not settle within %s (count=%d)", timeout, count)
		}
	}
}

// smokeIntegrityAssertReflectQuiet 断言反思请求数在 window 内保持稳定。
func smokeIntegrityAssertReflectQuiet(t *testing.T, provider *smokeProvider, want int, window time.Duration) {
	t.Helper()
	time.Sleep(window)
	if got := smokeRequestsFor(provider, smokeReflectModel); got != want {
		t.Fatalf("the reflection requests moved from %d to %d within %s, want no further pass",
			want, got, window)
	}
}

// smokeIntegrityReloadResult 是异步热换的结果：reload 在在途请求进行中不得阻塞，
// 所以调用方用超时守住它。
type smokeIntegrityReloadResult struct {
	answer smokeReloadAnswer
	err    error
}

// smokeIntegrityReload 改写文档并 POST /reload，把失败交回调用方而不结束测试。
func (r *smokeRig) smokeIntegrityReload(model string) smokeIntegrityReloadResult {
	r.cfg.Model = model
	if err := os.WriteFile(r.cfg.ConfigPath, []byte(r.cfg.document()), 0o644); err != nil {
		return smokeIntegrityReloadResult{err: fmt.Errorf("write the reloaded document: %w", err)}
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Post("http://"+r.face()+"/reload", "application/json", nil)
	if err != nil {
		return smokeIntegrityReloadResult{err: fmt.Errorf("post the reload: %w", err)}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return smokeIntegrityReloadResult{err: fmt.Errorf("reload status = %d: %s", resp.StatusCode, body)}
	}
	var answer smokeReloadAnswer
	if err := json.Unmarshal(body, &answer); err != nil {
		return smokeIntegrityReloadResult{err: fmt.Errorf("decode the reload answer %q: %w", body, err)}
	}
	return smokeIntegrityReloadResult{answer: answer}
}

// smokeCalibrationWindowFlag and smokeCalibrationSeedFlag are the two calibration entry flags, passed by the outer layer via -args (default 256/1).
var (
	smokeCalibrationWindowFlag = flag.Int("window", 256, "回放窗口档：256 或 32768（03.10 长程校准）")
	smokeCalibrationSeedFlag   = flag.Int("seed", 1, "缩样时间线的 seed（03.10 长程校准）")
)

// TestEventIntegrityLongRunCalibration drives the 03.10 calibration over the 03.11 kernel:
// - four sessions share one host, with 20 inputs and 2 restarts per session and seed/window parameterized
// - it measures six costs and writes the extrapolated budget into the report
// - it is a one-off off_contract read-only calibration, not a per-MR green test (design D8.1)
// - the report is the only input for the 05.1 timeouts
// - the same test binary self-executes as the real host process: TAGENT_HTTP_PORT=0, a SIGKILL between generations
// - the model endpoint lives in the parent, so every input marker is reconciled across restarts
func TestEventIntegrityLongRunCalibration(t *testing.T) {
	if testing.Short() {
		t.Skip("the long-run calibration spawns host processes and is skipped under -short")
	}
	switch os.Getenv(smokeCalibrationModeEnv) {
	case smokeCalibrationModeHost:
		smokeCalibrationHostChild(t)
		return
	case smokeCalibrationModeScenario:
		if os.Getenv(smokeCalibrationBaselineEnv) != "1" {
			t.Fatalf("the scenario mode is only entered by the non-race baseline (set %s=1)", smokeCalibrationBaselineEnv)
		}
		smokeCalibrationBaselineScenario(t)
		return
	}
	if !smokeCalibrationRaceEnabled() {
		t.Fatalf("the calibration measures the -race overhead and must run under -race; rerun with -race (the 03.11 matrix is -race)")
	}

	report, reportPath := smokeCalibrationPrimary(t)
	t.Logf("[calibration] window=%d seed=%d run_dir=%s report=%s", report.Window, report.Seed, report.Raw.RunDir, reportPath)

	t.Run("positive", func(t *testing.T) {
		if err := smokeCalibrationValidate(report); err != nil {
			t.Fatalf("the calibration report is incomplete: %v", err)
		}
		if report.Window != *smokeCalibrationWindowFlag {
			t.Fatalf("report window = %d, want the requested %d", report.Window, *smokeCalibrationWindowFlag)
		}
		if report.Measurements.ColdBuildSource == "" {
			t.Fatal("the cold build cost carries no source")
		}
		recon := report.Reconciliation
		if len(recon.MissingInputs) > 0 || len(recon.DuplicateInputs) > 0 {
			t.Fatalf("input requests do not reconcile: missing=%v duplicate=%v", recon.MissingInputs, recon.DuplicateInputs)
		}
		if recon.DrivenInputs != report.Scale.InputsTotal {
			t.Fatalf("driven inputs = %d, want the %d of the timeline", recon.DrivenInputs, report.Scale.InputsTotal)
		}
		if recon.RebindRequests != recon.ExpectedRebinds || len(recon.MissingRebinds) > 0 {
			t.Fatalf("rebind requests = %d/%d, missing=%v", recon.RebindRequests, recon.ExpectedRebinds, recon.MissingRebinds)
		}
		measurements := report.Measurements
		t.Logf("[calibration] 六项成本：冷构建=%.0fms(%s) 热运行=%.0fms(%.2fs) 每输入均值=%.2fms 中位=%.2fms p95=%.2fms 输入样本=%d 每重启=%.2fms 重启样本=%d 重绑均值=%.2fms(%d) race开销=%.2fx 存储=%dB→%dB(+%dB/%d文件) 峰值RSS=%.1fMiB CPU=%dms FDs=%d",
			measurements.ColdBuildMS, measurements.ColdBuildSource,
			measurements.HotRunWallMS, measurements.HotRunWallMS/1000,
			measurements.PerInputMS, measurements.PerInputMedianMS, measurements.PerInputP95MS, measurements.InputSamples,
			measurements.PerRestartMS, len(measurements.RestartDetail), measurements.RebindMeanMS, measurements.RebindSamples,
			measurements.Race.OverheadFactor, measurements.Storage.BeforeBytes, measurements.Storage.AfterBytes,
			measurements.Storage.GrowthBytes, measurements.Storage.FilesAdded,
			float64(measurements.Peak.MaxChildRSSBytes)/(1<<20), measurements.Peak.MaxChildCPUMillis, measurements.Peak.MaxChildOpenFDs)
		t.Logf("[calibration] 外推（%s）：wall=%.1fs（含历史增长修正 %.1fs）存储=%dB RSS线性=%.1fMiB 有界=%.1fMiB CPU=%dms",
			report.Extrapolation.Note, report.Extrapolation.FullWallMS/1000, report.Extrapolation.ContextAdjustedFullWallMS/1000,
			report.Extrapolation.FullStorageBytes, float64(report.Extrapolation.FullRSSBytesLinear)/(1<<20),
			float64(report.Extrapolation.FullRSSBytesBounded)/(1<<20), report.Extrapolation.FullCPUMillis)
		t.Logf("[calibration] 预算：建议 go test 超时=%ds/分片，作业超时=%dm；候选 short=%dm long=%dm mutations=%dm；超预算=%t",
			report.Budget.Recommended.GoTestTimeoutSeconds, report.Budget.Recommended.LongJobTimeoutMinutes,
			report.Budget.Candidates.ShortMinutes, report.Budget.Candidates.LongMinutes, report.Budget.Candidates.MutationsMinutes,
			report.Budget.OverBudget)
		for _, item := range report.Budget.Feasibility {
			t.Logf("[calibration] 可行性 %s：候选=%dm 判定=%s 需要=%.2fmin(单组合 %.2fmin) 建议=%dm (%s)",
				item.Surface, item.CandidateMinutes, item.Verdict, item.RequiredMinutes,
				item.RequiredPerShardMinutes, item.RecommendedMinutes, item.Reason)
		}
		t.Logf("[calibration] 对账：provider=%d 入口输入=%d/%d 重绑=%d/%d 反思=%d 重启首轮带上下文=%d/%d",
			recon.ProviderRequests, recon.DrivenInputs, recon.ExpectedInputs, recon.RebindRequests, recon.ExpectedRebinds,
			recon.ReflectRequests, recon.RestoredContextInputs, recon.RestoredContextChecks)
		if report.Scale.RestartsTotal != smokeCalibrationRestartsPerSession*smokeCalibrationSessions {
			t.Fatalf("restart samples = %d, want %d", report.Scale.RestartsTotal, smokeCalibrationRestartsPerSession*smokeCalibrationSessions)
		}
		if report.Scale.SharedHost != true || report.Scale.MeditationEnabled != true || report.Scale.FsyncDropped {
			t.Fatalf("scale invariants violated: %+v", report.Scale)
		}
	})

	t.Run("negative", func(t *testing.T) {
		if err := smokeCalibrationValidate(report); err != nil {
			t.Fatalf("the measured report must validate before mutations: %v", err)
		}
		cases := []struct {
			name   string
			mutate func(*smokeCalibrationReport)
			want   error
		}{
			{"missing-cold-build", func(r *smokeCalibrationReport) { r.Measurements.ColdBuildMS = 0 }, errCalibrationColdBuild},
			{"missing-hot-run", func(r *smokeCalibrationReport) { r.Measurements.PerInputMS = 0 }, errCalibrationHotRun},
			{"missing-restart-cost", func(r *smokeCalibrationReport) {
				r.Measurements.PerRestartMS = 0
				r.Measurements.RestartDetail = nil
			}, errCalibrationRestarts},
			{"missing-race-overhead", func(r *smokeCalibrationReport) { r.Measurements.Race.BaselineWallMS = 0 }, errCalibrationRace},
			{"missing-storage-growth", func(r *smokeCalibrationReport) { r.Measurements.Storage.GrowthBytes = 0 }, errCalibrationStorage},
			{"missing-peak-resource", func(r *smokeCalibrationReport) { r.Measurements.Peak.MaxChildRSSBytes = 0 }, errCalibrationPeak},
			{"fixed-timeout-as-cost", func(r *smokeCalibrationReport) { r.Budget.Source = "candidate" }, errCalibrationFixedTimeout},
			{"p95-from-three-samples", func(r *smokeCalibrationReport) {
				r.Budget.P95.Claimed = true
				r.Budget.P95.Samples = 3
			}, errCalibrationP95},
			{"silent-over-budget", func(r *smokeCalibrationReport) {
				r.Extrapolation.FullWallMS = 46 * 60 * 1000
				r.Extrapolation.ContextAdjustedFullWallMS = 0
				r.Budget.OverBudget = false
				r.Budget.OverBudgetReported = false
			}, errCalibrationOverBudget},
			{"missing-review-note", func(r *smokeCalibrationReport) { r.Extrapolation.Note = "" }, errCalibrationReviewNote},
			{"window-outside-levels", func(r *smokeCalibrationReport) { r.Window = 1024 }, errCalibrationWindow},
			{"input-samples-short", func(r *smokeCalibrationReport) {
				r.Measurements.InputSamples = r.Scale.InputsTotal - 1
			}, errCalibrationInputs},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				clone := smokeCalibrationClone(t, report)
				tc.mutate(clone)
				if err := smokeCalibrationValidate(clone); !errors.Is(err, tc.want) {
					t.Fatalf("mutated report error = %v, want %v", err, tc.want)
				}
			})
		}
	})

	t.Run("consumer", func(t *testing.T) {
		budget, err := smokeCalibrationReadBudget(reportPath)
		if err != nil {
			t.Fatalf("read the budget from the written report: %v", err)
		}
		if budget.Source != "measured" {
			t.Fatalf("budget source = %q, want measured", budget.Source)
		}
		if budget.Recommended.GoTestTimeoutSeconds <= 0 || budget.Recommended.LongJobTimeoutMinutes <= 0 {
			t.Fatalf("budget recommendation = %+v, want positive go test and job timeouts", budget.Recommended)
		}
		if budget.Recommended.LongJobTimeoutMinutes*60 <= budget.Recommended.GoTestTimeoutSeconds {
			t.Fatalf("job timeout %dm does not exceed the go test timeout %ds: a CI kill would hide the test failure",
				budget.Recommended.LongJobTimeoutMinutes, budget.Recommended.GoTestTimeoutSeconds)
		}
		if budget.Candidates.ShortMinutes != smokeCalibrationCandidateShort ||
			budget.Candidates.LongMinutes != smokeCalibrationCandidateLong ||
			budget.Candidates.MutationsMinutes != smokeCalibrationCandidateMutations {
			t.Fatalf("budget candidates = %+v, want the 20/45/30 proposal recorded beside the recommendation", budget.Candidates)
		}
		if budget.Recommended != report.Budget.Recommended {
			t.Fatalf("the file budget %+v differs from the measured %+v: one source only", budget.Recommended, report.Budget.Recommended)
		}
		t.Logf("[consumer] 05.1 reads budget.recommended.go_test_timeout_seconds_per_long_shard=%ds and budget.recommended.long_job_timeout_minutes=%dm from %s",
			budget.Recommended.GoTestTimeoutSeconds, budget.Recommended.LongJobTimeoutMinutes, reportPath)
	})
}

// TestEventIntegrityLongRun drives the frozen 03.11 long-run matrix:
// - four sessions share one host, with 300 inputs and 30 rotating restart points per session (120 real restarts)
// - the six combinations total 7200 inputs and 720 restarts
// - the three anchors share one matrix run: positive reconciles every identity
// - negative checks the three fault points drop nothing silently, and consumer walks the formal host and /task chain
// - the same test binary self-executes as the real host process: one SIGKILL per restart point, 121 generations
// - the model endpoint lives in the parent, so every input marker is reconciled across restarts
func TestEventIntegrityLongRun(t *testing.T) {
	if testing.Short() {
		t.Skip("the long-run matrix spawns host processes and is skipped under -short")
	}
	if os.Getenv(smokeCalibrationModeEnv) == smokeCalibrationModeHost {
		smokeLongHostChild(t)
		return
	}
	if !smokeCalibrationRaceEnabled() {
		t.Fatalf("the 03.11 long-run matrix must run under -race; rerun with -race")
	}

	report, reportPath := smokeLongPrimary(t)
	t.Logf("[long-run] seed=%d window=%d run_dir=%s report=%s wall=%.2fs inputs=%d restarts=%d",
		report.Seed, report.Window, report.RunDir, reportPath, report.WallMS/1000,
		report.Scale.InputsTotal, report.Scale.RestartsTotal)

	t.Run("positive", func(t *testing.T) {
		scale, recon := report.Scale, report.Recon
		if want := smokeLongSessions * smokeLongInputsPerSession; scale.InputsTotal != want {
			t.Fatalf("inputs total = %d, want %d", scale.InputsTotal, want)
		}
		if want := smokeLongSessions * smokeLongRestartsPerSession; scale.RestartsTotal != want {
			t.Fatalf("restarts total = %d, want %d", scale.RestartsTotal, want)
		}
		if scale.Sessions != smokeLongSessions || !scale.SharedHost || !scale.MeditationEnabled || scale.FsyncDropped {
			t.Fatalf("scale = %+v, want four sessions on one host with meditation on and fsync intact", scale)
		}
		if recon.InputSamples != scale.InputsTotal {
			t.Fatalf("driven input samples = %d, want %d", recon.InputSamples, scale.InputsTotal)
		}
		if len(recon.MissingInputRequests) > 0 || len(recon.DuplicateInputRequests) > 0 {
			t.Fatalf("input identity does not reconcile: missing=%v duplicate=%v", recon.MissingInputRequests, recon.DuplicateInputRequests)
		}
		if clean := scale.InputsTotal - scale.Faults; recon.InputRequests != clean+recon.FaultInputAttempts+recon.FaultInputRetries {
			t.Fatalf("input requests = %d, want %d (%d clean inputs + %d fault first attempts + %d fault retries)",
				recon.InputRequests, clean+recon.FaultInputAttempts+recon.FaultInputRetries,
				clean, recon.FaultInputAttempts, recon.FaultInputRetries)
		}
		if want := (scale.RestartsTotal + 1) * scale.Sessions; recon.ExpectedRebinds != want || recon.RebindRequests != want {
			t.Fatalf("rebind requests = %d/%d (expected %d)", recon.RebindRequests, recon.ExpectedRebinds, want)
		}
		if len(recon.MissingRebinds) > 0 || len(recon.DuplicateRebinds) > 0 {
			t.Fatalf("rebind identity does not reconcile: missing=%v duplicate=%v", recon.MissingRebinds, recon.DuplicateRebinds)
		}
		if want := scale.DelegateInputs - 1; recon.ExpectedNotifications != want || recon.NotificationRequests != want {
			t.Fatalf("settle notifications = %d/%d, want %d (the storage-denied delegate is the exception)",
				recon.NotificationRequests, recon.ExpectedNotifications, want)
		}
		if len(recon.MissingNotifications) > 0 || len(recon.DuplicateNotifications) > 0 {
			t.Fatalf("notification identity does not reconcile: missing=%v duplicate=%v", recon.MissingNotifications, recon.DuplicateNotifications)
		}
		if len(recon.MissingActions) > 0 || len(recon.DuplicateActions) > 0 || recon.ActionEvents != recon.ExpectedActions {
			t.Fatalf("action identity does not reconcile: events=%d/%d missing=%v duplicate=%v",
				recon.ActionEvents, recon.ExpectedActions, recon.MissingActions, recon.DuplicateActions)
		}
		if len(report.Restarts) != scale.RestartsTotal {
			t.Fatalf("restart samples = %d, want %d", len(report.Restarts), scale.RestartsTotal)
		}
		for _, sample := range report.Restarts {
			if sample.Rebound != scale.Sessions {
				t.Fatalf("generation %d rebound %d sessions, want %d", sample.Generation, sample.Rebound, scale.Sessions)
			}
			if sample.KillToReadyMS <= 0 || sample.Trigger == "" {
				t.Fatalf("restart sample %d carries no kill→ready measurement or trigger: %+v", sample.Generation, sample)
			}
		}
		if report.Drain == nil {
			t.Fatal("the final generation reported no drain result")
		}
		if report.Drain.Error != "" {
			t.Fatalf("the final drain ended by error: %s", report.Drain.Error)
		}
		if !report.Drain.Drained && len(report.Drain.Unsettled) == 0 {
			t.Fatal("the final drain is neither drained nor a named unsettled list")
		}
		if report.Storage.GrowthBytes <= 0 || report.Storage.FilesAdded <= 0 {
			t.Fatalf("storage growth = %+v, want a positive durable footprint", report.Storage)
		}
		if len(report.Storage.Problems) > 0 {
			t.Fatalf("storage walk problems: %v", report.Storage.Problems)
		}
		if report.Peak.MaxChildRSSBytes <= 0 {
			t.Fatalf("peak child RSS = %d, want a measured value", report.Peak.MaxChildRSSBytes)
		}
		if want := (scale.RestartsTotal + 1) * scale.Sessions; len(report.Folds) != want {
			t.Fatalf("fold samples = %d, want %d (four sessions per generation)", len(report.Folds), want)
		}
		if report.Window == 256 && recon.FoldSynthetics == 0 {
			t.Fatal("the 256 window produced no folded synthetic entries: compaction never interleaved with replacement")
		}
		t.Logf("[positive] inputs=%d requests=%d fault_retries=%d rebinds=%d notifications=%d actions=%d restarts=%d drained=%t unsettled=%d storage=+%dB/%d files peak_rss=%.1fMiB folds=%d",
			recon.InputSamples, recon.InputRequests, recon.FaultInputRetries,
			recon.RebindRequests, recon.NotificationRequests, recon.ActionEvents,
			len(report.Restarts), report.Drain.Drained, len(report.Drain.Unsettled),
			report.Storage.GrowthBytes, report.Storage.FilesAdded,
			float64(report.Peak.MaxChildRSSBytes)/(1<<20), recon.FoldSynthetics)
	})

	t.Run("negative", func(t *testing.T) {
		recon := report.Recon
		if len(report.Faults) != 3 {
			t.Fatalf("fault samples = %d, want the three injected faults", len(report.Faults))
		}
		if recon.FaultsAnswered+recon.FaultsNamed != recon.Faults || len(recon.FaultsDropped) > 0 {
			t.Fatalf("fault outcomes = answered %d + named %d of %d, dropped=%v",
				recon.FaultsAnswered, recon.FaultsNamed, recon.Faults, recon.FaultsDropped)
		}
		for _, fault := range report.Faults {
			switch fault.Kind {
			case smokeLongFaultStorage:
				if fault.StorageProbe == "" {
					t.Fatalf("the storage deny fault @%s/round%d left no denied write probe", fault.Session, fault.Round)
				}
				if fault.FactsAfter < fault.FactsBefore {
					t.Fatalf("facts went backwards under the storage deny: %d→%d", fault.FactsBefore, fault.FactsAfter)
				}
			case smokeLongFaultModel:
				if fault.AnswerBody == "" && fault.DriveError == "" && fault.NamedLogs == 0 {
					t.Fatalf("the model fault @%s/round%d left no user-visible or named failure", fault.Session, fault.Round)
				}
			case smokeLongFaultChannel:
				if fault.RefusedSends == 0 {
					t.Fatalf("the channel fault @%s/round%d never reached the refusing send", fault.Session, fault.Round)
				}
				if fault.NamedLogs == 0 {
					t.Fatalf("the channel fault @%s/round%d left no SendTextToUser failed log", fault.Session, fault.Round)
				}
			default:
				t.Fatalf("unknown fault kind %q", fault.Kind)
			}
		}
		if recon.Reloads != smokeLongReloadGens || recon.ReloadsInstalled != smokeLongReloadGens {
			t.Fatalf("reloads installed = %d/%d, want %d", recon.ReloadsInstalled, recon.Reloads, smokeLongReloadGens)
		}
		for _, reload := range report.Reloads {
			if !reload.Reloaded || reload.Error != "" {
				t.Fatalf("reload generation %d did not install: %+v", reload.Generation, reload)
			}
		}
		if recon.ConcurrentBatches != 2 || recon.ConcurrentAnswered != 2*smokeLongSessions {
			t.Fatalf("concurrent completion = %d batches / %d answered, want 2 batches with four each",
				recon.ConcurrentBatches, recon.ConcurrentAnswered)
		}
		for _, batch := range report.Concurrent {
			if batch.Sessions != smokeLongSessions || batch.Answered != smokeLongSessions || len(batch.Errors) > 0 {
				t.Fatalf("concurrent batch @round %d = %+v, want four answered sessions and no errors", batch.Round, batch)
			}
		}
		if report.Window == 256 && recon.FoldSynthetics == 0 {
			t.Fatal("replacement ran beside no compaction: the 256 window folded nothing")
		}
		t.Logf("[negative] faults=%d answered=%d named=%d dropped=%d reloads=%d concurrent=%d/%d folds=%d",
			recon.Faults, recon.FaultsAnswered, recon.FaultsNamed, len(recon.FaultsDropped),
			recon.ReloadsInstalled, recon.ConcurrentBatches, recon.ConcurrentAnswered, recon.FoldSynthetics)
	})

	t.Run("consumer", func(t *testing.T) {
		recon := report.Recon
		want := recon.ExpectedConsumerTurns
		if want <= 0 {
			t.Fatal("the plan names no consumer turn: the /task chain was never exercised")
		}
		if recon.ConsumerTurns != want {
			t.Fatalf("consumer turns = %d, want %d (the consumer session's first non-concurrent input of each generation)", recon.ConsumerTurns, want)
		}
		if recon.ConsumerAnswered != recon.ConsumerTurns {
			t.Fatalf("consumer turns answered = %d/%d", recon.ConsumerAnswered, recon.ConsumerTurns)
		}
		for _, turn := range report.Consumer {
			if turn.Status != http.StatusAccepted || !turn.Answered || turn.Error != "" {
				t.Fatalf("consumer turn %s = %+v, want 202 and an answer", turn.Marker, turn)
			}
		}
		if len(recon.MissingInputRequests) > 0 || len(recon.DuplicateInputRequests) > 0 {
			t.Fatalf("the consumer markers do not reconcile among the inputs: missing=%v duplicate=%v",
				recon.MissingInputRequests, recon.DuplicateInputRequests)
		}
		t.Logf("[consumer] /task turns=%d answered=%d via %s", recon.ConsumerTurns, recon.ConsumerAnswered, reportPath)
	})
}
