//go:build smoke

// smoke_errors_test.go 是第二波冒烟的失败路径：模型回合失败后的原始错误告知、热换
// 被租约拒绝后 lease 持有的版本继续服务、审批过期后重试的调用仍被扣留。台架复用
// smoke_harness_test.go 的真实装配，故障注入与断言留在本文件。
//
// 契约: docs/wiki/examples/wechat-bot-runtime.md#startup-and-routing
package main

import (
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/features/rl"
	"github.com/SpellingDragon/tagent/runtime/task/approval"
)

const (
	// smokeErrorsFailureText 是框架在回合失败时直投的原始错误里的一段：
	// 它取自 provider 的 500 响应，不是任何应用层的兜底句子。
	smokeErrorsFailureText = "500 Internal Server Error"
	// smokeErrorsFallbackText 是旧的无原因兜底文案，任何一层都不许再外发它。
	smokeErrorsFallbackText = "执行出错：本轮模型回合失败，未产出交付内容。"
	// smokeErrorsMissingProvider 是坏文档引用却未声明的 provider 键。
	smokeErrorsMissingProvider = "smoke-errors-missing"
	// smokeErrorsTTLMinutes 是审批有效期的最小配置值。
	smokeErrorsTTLMinutes = 1

	// smokeErrorsFailedTurn 是注入 500 的那一轮标记。
	smokeErrorsFailedTurn = "SMOKE-R510"
	// smokeErrorsRecoveredTurn 是失败之后恢复的那一轮标记。
	smokeErrorsRecoveredTurn = "SMOKE-R511"
	// smokeErrorsReloadTurn 是热换之前正常服务的那一轮标记。
	smokeErrorsReloadTurn = "SMOKE-R520"
	// smokeErrorsReloadAfterTurn 是热换被拒之后继续服务的那一轮标记。
	smokeErrorsReloadAfterTurn = "SMOKE-R521"
	// smokeErrorsApprovalTurn 是扣留与过期重试共用的那一轮标记。
	smokeErrorsApprovalTurn = "SMOKE-R530"
	// smokeErrorsAfterApprovalTurn 是过期重试之后恢复正常的那一轮标记。
	smokeErrorsAfterApprovalTurn = "SMOKE-R531"
)

// TestLocalSmokeWave2Errors 是第二波冒烟的失败路径入口：三条路径各自核对降级、拒绝与扣留，失败之后宿主仍服务下一轮。
func TestLocalSmokeWave2Errors(t *testing.T) {
	t.Run("model turn failure degrades and the next turn recovers", smokeErrorsModelTurn)
	t.Run("refused replacement keeps the serving model version", smokeErrorsReloadRefused)
	t.Run("expired approval keeps the retried call held", smokeErrorsApprovalExpired)
}

// smokeErrorsModelTurn 让脚本模型对指定轮返回 500：回合以失败终端收尾，用户收到
// 一次框架直投的原始错误，下一轮在新的请求上恢复。
func smokeErrorsModelTurn(t *testing.T) {
	logs := installSmokeLog(t)
	cfg := newSmokeConfig(t, "http://127.0.0.1:1/v1")
	provider := newSmokeProvider(t, filepath.Join(cfg.WorkspaceDir, smokeFileMarker))
	cfg.Endpoint = provider.endpoint()
	cfg.write(t)
	provider.hook = func(w http.ResponseWriter, _ int, modelName string, messages []smokeModelMessage) bool {
		if modelName != smokeEntryModel {
			return false
		}
		if !strings.Contains(smokeLastUserText(messages), smokeErrorsFailedTurn) {
			return false
		}
		http.Error(w, "smoke: the scripted model answers 500", http.StatusInternalServerError)
		return true
	}

	rig := bootSmokeRig(t, cfg, provider)
	chat := smokeChats[0]
	rig.drive(chat, smokeErrorsFailedTurn+" smoke five hundred")
	rig.waitText(chat, smokeErrorsFailureText, 60*time.Second)

	failed := smokeErrorsRequestsForLastUser(provider, smokeEntryModel, smokeErrorsFailedTurn)
	if len(failed) == 0 {
		t.Fatalf("the failing turn reached no model request")
	}
	if got := smokeErrorsTextCount(rig, chat, smokeErrorsFailureText); got != 1 {
		t.Fatalf("failure notice delivered %d times, want exactly one", got)
	}
	if got := smokeErrorsTextCount(rig, chat, smokeErrorsFallbackText); got != 0 {
		t.Fatalf("the fixed fallback sentence was delivered %d times, want none", got)
	}
	if got := smokeErrorsTextCount(rig, chat, "answer "+smokeErrorsFailedTurn); got != 0 {
		t.Fatalf("the failed turn delivered %d answer(s), want none", got)
	}
	if logs.count("terminal=failed") == 0 {
		t.Fatalf("the failed turn left no terminal=failed line")
	}

	rig.drive(chat, smokeErrorsRecoveredTurn+" plain question")
	rig.waitText(chat, "answer "+smokeErrorsRecoveredTurn, 60*time.Second)
	if got := smokeErrorsRequestsForLastUser(provider, smokeEntryModel, smokeErrorsRecoveredTurn); len(got) == 0 {
		t.Fatalf("the recovered turn drove no model request")
	}
	if got := smokeErrorsRequestsForLastUser(provider, smokeEntryModel, smokeErrorsFailedTurn); len(got) != len(failed) {
		t.Fatalf("the failed input was replayed: %d request(s), want %d", len(got), len(failed))
	}
	if got := smokeErrorsTextCount(rig, chat, smokeErrorsFailureText); got != 1 {
		t.Fatalf("failure notice delivered %d times after recovery, want exactly one", got)
	}
	if got := smokeErrorsTextCount(rig, chat, smokeErrorsFallbackText); got != 0 {
		t.Fatalf("the fixed fallback sentence was delivered %d times after recovery, want none", got)
	}
	if err := rig.healthz(); err != nil {
		t.Fatalf("health after the failed turn: %v", err)
	}
	t.Logf("[wave2-errors] 500 on %s: %d request(s) failed, one notice delivered, %s answered next",
		smokeErrorsFailedTurn, len(failed), smokeErrorsRecoveredTurn)
}

// smokeErrorsReloadRefused 把入口模型热换到未声明的 provider：替换事实照常提交，
// 租约的重建被拒，下一次请求仍按 lease 持有的版本发出。
func smokeErrorsReloadRefused(t *testing.T) {
	logs := installSmokeLog(t)
	cfg := newSmokeConfig(t, "http://127.0.0.1:1/v1")
	provider := newSmokeProvider(t, filepath.Join(cfg.WorkspaceDir, smokeFileMarker))
	cfg.Endpoint = provider.endpoint()
	cfg.write(t)

	rig := bootSmokeRig(t, cfg, provider)
	chat := smokeChats[1]
	rig.drive(chat, smokeErrorsReloadTurn+" plain question")
	rig.waitText(chat, "answer "+smokeErrorsReloadTurn, 60*time.Second)

	cfg.Doc = smokeErrorsBadProviderDocument
	answer := rig.reload(smokeReloadModel)
	if !answer.Reloaded || len(answer.ModelChanges) != 1 {
		t.Fatalf("reload = %+v, want one judged model change", answer)
	}
	change := answer.ModelChanges[0]
	wantTo := smokeErrorsMissingProvider + "/" + smokeReloadModel
	if change.Agent != smokeSessionTag || change.From != "local/"+smokeEntryModel || change.To != wantTo {
		t.Fatalf("model change = %+v, want %s local/%s -> %s",
			change, smokeSessionTag, smokeEntryModel, wantTo)
	}
	if change.Fact == "" {
		t.Fatalf("the judged change carries no replacement fact")
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
	if logs.count("plugin=model-lease") == 0 || logs.count("result=refused") == 0 {
		t.Fatalf("the refused replacement left no model-lease refusal line")
	}

	rig.drive(chat, smokeErrorsReloadAfterTurn+" plain question")
	rig.waitText(chat, "answer "+smokeErrorsReloadAfterTurn, 60*time.Second)
	if got := smokeErrorsRequestsForLastUser(provider, smokeEntryModel, smokeErrorsReloadAfterTurn); len(got) == 0 {
		t.Fatalf("the turn after the refused replacement served no request from the held version")
	}
	for _, req := range provider.recorded() {
		if req.Model == smokeReloadModel {
			t.Fatalf("the refused version %q served a turn", smokeReloadModel)
		}
	}
	if err := rig.healthz(); err != nil {
		t.Fatalf("health after the refused replacement: %v", err)
	}
	t.Logf("[wave2-errors] reload judged %s -> %s and refused it; %s kept serving %s",
		change.From, change.To, smokeErrorsReloadAfterTurn, smokeEntryModel)
}

// smokeErrorsApprovalExpired 用配置面的最小有效期加可推进时钟：扣留的请求过期后
// 不能被补批；模型重试同一调用时仍被扣留——过期请求仍是该 tool+digest 的最新
// identity, so the gate registers nothing new and the book holds no pending.
func smokeErrorsApprovalExpired(t *testing.T) {
	logs := installSmokeLog(t)
	cfg := newSmokeConfig(t, "http://127.0.0.1:1/v1")
	provider := newSmokeProvider(t, filepath.Join(cfg.WorkspaceDir, smokeFileMarker))
	cfg.Endpoint = provider.endpoint()
	cfg.Doc = smokeErrorsShortTTLDocument
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
	chat := smokeChats[2]
	rig.drive(chat, smokeErrorsApprovalTurn+" smoke approval request")
	smokeErrorsWaitTextCount(t, rig, chat, "hold-ack "+smokeErrorsApprovalTurn, 1, 60*time.Second)

	pending := rig.pendingApproval(30 * time.Second)
	wantTTL := int64((time.Duration(smokeErrorsTTLMinutes) * time.Minute) / time.Millisecond)
	if ttl := pending.ExpiresMs - pending.CreatedMs; ttl != wantTTL {
		t.Fatalf("request validity = %d ms, want %d ms", ttl, wantTTL)
	}
	if pending.Status != approval.ApprovalPending {
		t.Fatalf("request status = %q, want pending", pending.Status)
	}
	announce := rig.waitText(smokeApproverChat, shortDigest(pending.ArgsDigest), 30*time.Second)
	if !strings.Contains(announce.Body, "approve "+shortDigest(pending.ArgsDigest)) {
		t.Fatalf("approval announcement = %q, want the approve command", announce.Body)
	}

	clock.Advance(time.Duration(smokeErrorsTTLMinutes)*time.Minute + time.Second)
	if got := len(rig.gateway.Manager().Pending()); got != 0 {
		t.Fatalf("pending requests after expiry = %d, want none", got)
	}
	if _, err := rig.gateway.Manager().Decide(pending.ID, approval.ApprovalApproved, "smoke-errors-late"); !errors.Is(err, approval.ErrApprovalExpired) {
		t.Fatalf("late ruling = %v, want ErrApprovalExpired", err)
	}

	rig.drive(chat, smokeErrorsApprovalTurn+" smoke retry")
	smokeErrorsWaitTextCount(t, rig, chat, "hold-ack "+smokeErrorsApprovalTurn, 2, 60*time.Second)
	if got := smokeErrorsTextCount(rig, chat, "hold-ack "+smokeErrorsApprovalTurn); got != 2 {
		t.Fatalf("held acknowledgements = %d, want exactly two", got)
	}
	renewed := rig.gateway.Manager().Pending()
	if len(renewed) != 0 {
		t.Fatalf("pending requests after the retry = %d, want none: the expired request still names the digest", len(renewed))
	}
	if latest := rig.gateway.Manager().Lookup(sendToolID, pending.ArgsDigest); latest == nil || latest.ID != pending.ID {
		t.Fatalf("lookup after the retry = %+v, want the expired request %s still naming the digest", latest, pending.ID)
	}
	if clock.Now().UnixMilli() <= pending.ExpiresMs {
		t.Fatalf("the retry happened at %d ms, want after the expiry %d", clock.Now().UnixMilli(), pending.ExpiresMs)
	}
	if got := smokeErrorsTextCount(rig, chat, smokeApprovalMarker); got != 0 {
		t.Fatalf("the held call delivered its body %d time(s), want none", got)
	}
	if got := smokeErrorsTextCount(rig, chat, "approved-done "+smokeErrorsApprovalTurn); got != 0 {
		t.Fatalf("the held call reported %d approved result(s), want none", got)
	}
	if logs.count("ruling=expired") == 0 || logs.count(fmt.Sprintf("expires_ms=%d", pending.ExpiresMs)) == 0 {
		t.Fatalf("the expired ruling left no INFO line for %s", pending.ID)
	}

	rig.drive(chat, smokeErrorsAfterApprovalTurn+" plain question")
	rig.waitText(chat, "answer "+smokeErrorsAfterApprovalTurn, 60*time.Second)
	if got := len(rig.gateway.Manager().Pending()); got != 0 {
		t.Fatalf("pending requests after the recovery turn = %d, want the retried call still held beside no pending", got)
	}
	if err := rig.healthz(); err != nil {
		t.Fatalf("health after the expired approval: %v", err)
	}
	t.Logf("[wave2-errors] approval %s expired at %d ms; the retry stayed held, the expired request still names the digest and no pending was registered",
		pending.ID, pending.ExpiresMs)
}

// smokeErrorsShortTTLDocument 把审批夹具（重声明 send 的审批通道）的有效期改到
// 最小分钟值。过期裁决/重试仍被扣留只能由 tool+digest 的审批登记表达，投递挂起
// 没有 TTL 与摘要，故这个场景保留在审批通道上，不换对象。
func smokeErrorsShortTTLDocument(c *smokeConfig) string {
	base := *c
	base.Doc = smokeApprovalDocument
	document := base.document()
	patched := strings.Replace(document, "approval_ttl_minutes: 30",
		fmt.Sprintf("approval_ttl_minutes: %d", smokeErrorsTTLMinutes), 1)
	if patched == document {
		panic("smoke: the rendered document carries no approval validity to shorten")
	}
	return patched
}

// smokeErrorsBadProviderDocument 让入口 agent 引用未声明的 provider，并改到热换
// 后的模型名：热换期的重建因此被拒，agent 集合与工具面保持不变。文档以起机
// 渲染（smokeApprovalDocument，即起机文档）为底注入 provider 行，变更面只有
// 这一行（一档），不会先撞二档拒绝。
func smokeErrorsBadProviderDocument(c *smokeConfig) string {
	base := *c
	base.Doc = smokeApprovalDocument
	entry := fmt.Sprintf("  %s:\n    model: %s\n", smokeSessionTag, c.Model)
	document := base.document()
	patched := strings.Replace(document, entry,
		entry+fmt.Sprintf("    provider: %s\n", smokeErrorsMissingProvider), 1)
	if patched == document {
		panic("smoke: the rendered document carries no entry agent block to move")
	}
	return patched
}

// smokeErrorsClock 是可推进的审批时钟。
type smokeErrorsClock struct {
	mu  sync.Mutex
	now time.Time
}

// Now reports the current fake time.
func (c *smokeErrorsClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance moves the fake time forward.
func (c *smokeErrorsClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// smokeErrorsTextCount 报告替身通道向该会话投递过多少次含给定子串的文本。
func smokeErrorsTextCount(rig *smokeRig, chat, want string) int {
	count := 0
	for _, evt := range rig.recorded() {
		if evt.Kind == "text" && evt.Target == chat && strings.Contains(evt.Body, want) {
			count++
		}
	}
	return count
}

// smokeErrorsWaitTextCount 等到该会话收到至少 count 条含给定子串的文本。
func smokeErrorsWaitTextCount(t *testing.T, rig *smokeRig, chat, want string, count int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if got := smokeErrorsTextCount(rig, chat, want); got >= count {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("no %d text(s) to %s carrying %q after %s (recorded=%d)\nrecent outbound:\n%s",
				count, chat, want, timeout, len(rig.recorded()), rig.recentOutbound(8))
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// smokeErrorsRequestsForLastUser 报告该模型在最后一条用户消息携带该标记时收到的请求。
func smokeErrorsRequestsForLastUser(provider *smokeProvider, model, marker string) []smokeModelRequest {
	var out []smokeModelRequest
	for _, req := range provider.recorded() {
		if req.Model != model {
			continue
		}
		if strings.Contains(smokeLastUserText(req.Messages), marker) {
			out = append(out, req)
		}
	}
	return out
}
