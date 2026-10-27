//go:build smoke

// smoke_exec_test.go 是第二波冒烟的执行面：宿主文档声明 exec 后 Boot 自己装配
// tmux Executor 与执行资源管理器，exec_run 真跑命令并把命令输出带回下一轮决策；
// final_round 在轮数耗尽后把第 3 个决策转 closing round——收尾轮拒绝工具调用，
// 而收尾正文的投递仍经投递审核出口（hold→批准补投或放行）。台架复用
// smoke_harness_test.go 的真实装配，脚本日程与断言留在本文件。
//
// 契约: docs/wiki/agent/event-flow.md#watchdog-final-round
package main

import (
	"context"
	"fmt"
	"net/http"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tagentplugin "github.com/SpellingDragon/tagent/api/plugin"
	tagentexec "github.com/SpellingDragon/tagent/tool/exec"
)

const (
	// smokeExecTurn 是执行轮的轮次标记。
	smokeExecTurn = "SMOKE-R610"
	// smokeExecWatchdogTurn 是看门狗拒绝轮的轮次标记。
	smokeExecWatchdogTurn = "SMOKE-R620"
	// smokeExecClosingTurn 是看门狗交正文轮的轮次标记。
	smokeExecClosingTurn = "SMOKE-R621"

	// smokeExecBody 是 exec_run 打算跑出的命令正文：它只有真的执行了才会出现。
	smokeExecBody = "smoke-exec-ok"

	// smokeExecMaxRounds 与 smokeExecClosingSeconds 是 entry 声明的看门狗预算。
	smokeExecMaxRounds      = 2
	smokeExecClosingSeconds = 30

	// smokeExecClosingPrompt 是默认收尾指令的固定片段。
	smokeExecClosingPrompt = "this is your final round"
	// smokeExecRefusal 是收尾轮工具拒绝回执的固定片段。
	smokeExecRefusal = "this call was not admitted"
)

// smokeExecResource 是 exec_run 声明的执行资源 key：tmux 会话活在测试进程之外，
// each process takes a unique key, so a rerun never meets a stale session.
var smokeExecResource = fmt.Sprintf("smoke-exec-%d", time.Now().UnixNano())

// TestLocalSmokeWave2Exec 是第二波冒烟的执行面入口。
func TestLocalSmokeWave2Exec(t *testing.T) {
	if !tagentexec.IsTmuxAvailable() {
		t.Skip("tmux is not on PATH, so Boot refuses the declared exec face by name")
	}
	t.Logf("[wave2-exec] tmux available=%t; the host document declares the exec face Boot wires", true)
	t.Run("exec face is granted and its refusal is named", smokeExecRun)
	t.Run("watchdog closing round refuses the third tool call", smokeExecWatchdogRefusal)
	t.Run("watchdog closing round delivers its closing body", smokeExecWatchdogClosing)
}

// smokeExecRun 驱动一次 exec_run：模型先答工具调用，再按回执收尾。Boot 按声明
// 自动接上 tmux Executor，回执因此携带命令正文；断言正文进入下一轮决策，并随
// 收尾回答投递到用户。
func smokeExecRun(t *testing.T) {
	logs := installSmokeLog(t)
	provider, rig := smokeExecBoot(t, nil)
	chat := smokeChats[0]
	rig.drive(chat, smokeExecTurn+" smoke exec please")

	requests := smokeExecRequests(provider, smokeExecTurn)
	if len(requests) != 2 {
		t.Fatalf("the exec turn drove %d model request(s), want the exec call beside its receipt", len(requests))
	}
	args, ok := smokeExecToolArgs(requests[1].Messages, "exec_run")
	if !ok {
		t.Fatalf("the exec turn's second request carries no exec_run call: %s", requests[1].text())
	}
	if !strings.Contains(args, smokeExecBody) {
		t.Fatalf("exec_run arguments = %q, want the %q body", args, smokeExecBody)
	}
	receipt := strings.TrimSpace(smokeLastContent(requests[1].Messages))
	if !strings.Contains(receipt, "executed tmux/"+smokeExecResource) || !strings.Contains(receipt, smokeExecBody) {
		t.Fatalf("exec_run receipt = %q, want the executed resource tmux/%s carrying the %q command body",
			receipt, smokeExecResource, smokeExecBody)
	}
	t.Logf("[wave2-exec] exec face executed: %q", receipt)
	rig.waitText(chat, "exec-ok "+smokeExecTurn, 60*time.Second)
	if got := smokeExecTextCount(rig, chat, smokeExecBody); got == 0 {
		t.Fatalf("the exec result %q entered no delivered reply", smokeExecBody)
	}
	if got := smokeExecTextCount(rig, chat, tagentexec.ErrUnwired.Error()); got != 0 {
		t.Fatalf("the wired exec face delivered the %q refusal %d time(s), want none", tagentexec.ErrUnwired.Error(), got)
	}
	if got := logs.count("terminal=failed"); got != 0 {
		t.Fatalf("the exec turn left %d terminal=failed line(s), want none", got)
	}
}

// smokeExecWatchdogRefusal 让模型在 closing round 里再答一次 exec_run：该调用不被
// 受理，回执写进会话 transcript；前两轮的两次 exec 调用走同一资源键，第二轮因此
// 拿不到第二个会话，而命令正文从不外发。
func smokeExecWatchdogRefusal(t *testing.T) {
	installSmokeLog(t)
	provider, rig := smokeExecBoot(t, nil)
	chat := smokeChats[1]
	before := len(rig.gateway.Manager().Pending())
	rig.drive(chat, smokeExecWatchdogTurn+" smoke watchdog send")

	requests := smokeExecRequests(provider, smokeExecWatchdogTurn)
	if len(requests) != 3 {
		t.Fatalf("the watchdog turn drove %d model request(s), want two tool rounds beside the closing round", len(requests))
	}
	closing := requests[2]
	if !smokeExecClosingRequest(closing.Messages) {
		t.Fatalf("the third request carries no closing instruction: %q", smokeLastUserText(closing.Messages))
	}
	if got := strings.TrimSpace(smokeLastContent(closing.Messages)); !strings.Contains(got, smokeExecClosingPrompt) {
		t.Fatalf("the closing request's last message = %q, want the closing instruction", got)
	}

	entries, _ := rig.host.Run().ReplayEntries(rig.addressOf(chat).SessionID)
	admitted := 0
	for _, entry := range entries {
		if strings.Contains(entry.Content, "smoke-exec-ok") {
			admitted++
		}
		if strings.Contains(entry.Content, smokeExecRefusal) {
			t.Fatalf("the refused closing-round call left a fact on the mapped sequence (one face: a refused call's receipt is round-scoped): %q", entry.Content)
		}
	}
	if admitted != 2 {
		t.Fatalf("the mapped sequence carries %d admitted exec receipt(s), want the two admitted rounds (%d entries): recent requests:\n%s",
			admitted, len(entries), rig.recentRequests(6))
	}

	if after := len(rig.gateway.Manager().Pending()); after != before {
		t.Fatalf("pending approvals %d -> %d, want no approval registration beside the exec face", before, after)
	}
	if got := len(smokeExecRequests(provider, smokeExecWatchdogTurn)); got != 3 {
		t.Fatalf("the closing round drove a further decision: model requests = %d, want the drive to end there", got)
	}
	if got := smokeExecTextCount(rig, chat, smokeExecBody); got != 0 {
		t.Fatalf("the exec output reached the user %d time(s), want the refused closing round to deliver nothing", got)
	}
	t.Logf("[wave2-exec] watchdog closing round: request 3 carries the closing instruction, the third tool call was refused (%q), pending approvals stayed %d, the exec output never reached the user",
		smokeExecRefusal, before)
}

// smokeExecWatchdogClosing 让模型在 closing round 直接交正文：回合以收尾正文
// 终止，收尾轮没有受理任何工具调用。收尾正文的投递经投递审核出口：审核挂起时
// 渠道零条，批准后恰一条。
func smokeExecWatchdogClosing(t *testing.T) {
	installSmokeLog(t)
	ruler := &smokeReviewRuler{holdPrefix: []string{"closing summary "}}
	provider, rig := smokeExecBoot(t, ruler)
	chat := smokeChats[2]
	before := len(rig.gateway.Manager().Pending())
	rig.drive(chat, smokeExecClosingTurn+" smoke watchdog final")

	requests := smokeExecRequests(provider, smokeExecClosingTurn)
	if len(requests) != 3 {
		t.Fatalf("the closing turn drove %d model request(s), want two tool rounds beside the closing round", len(requests))
	}
	if !smokeExecClosingRequest(requests[2].Messages) {
		t.Fatalf("the third request carries no closing instruction: %q", smokeLastUserText(requests[2].Messages))
	}
	want := "closing summary " + smokeExecClosingTurn
	if got := smokeExecTextCount(rig, chat, want); got != 0 {
		t.Fatalf("the closing body reached the channel %d time(s) before the review approved it, want none", got)
	}
	if !ruler.sawContent(want) {
		t.Fatalf("the closing body never reached the delivery review; reviewed = %+v", ruler.seenDeliveries())
	}
	held := rig.waitHeldDelivery(chat, want, 30*time.Second)
	if held.Delivery.Content != want || held.Delivery.Lineage != "user" {
		t.Fatalf("held closing delivery = %+v, want the closing body under the user lineage", held.Delivery)
	}
	rig.approveDelivery(context.Background(), held)
	rig.waitText(chat, want, 60*time.Second)
	if got := smokeExecTextCount(rig, chat, want); got != 1 {
		t.Fatalf("the approved closing body was delivered %d time(s), want exactly one", got)
	}
	if got := len(smokeExecRequests(provider, smokeExecClosingTurn)); got != 3 {
		t.Fatalf("the closing turn drove %d model request(s) after its closing answer, want the drive to end there", got)
	}
	if after := len(rig.gateway.Manager().Pending()); after != before {
		t.Fatalf("pending approvals %d -> %d, want no approval registration beside the delivery review", before, after)
	}
	if got := smokeExecTextCount(rig, chat, smokeExecRefusal); got != 0 {
		t.Fatalf("the closing answer carried %d refusal receipt(s), want none", got)
	}
	t.Logf("[wave2-exec] watchdog closing round delivered %q through the delivery review: held, approved, exactly one",
		want)
}

// smokeExecBoot 装配一台执行面宿主：entry 声明 exec 与 final_round，治理开启，
// 模型与出站都指向本地替身。ruler 非 nil 时把投递审核注入宿主。
func smokeExecBoot(t *testing.T, ruler tagentplugin.DeliveryRuler) (*smokeProvider, *smokeRig) {
	t.Helper()
	t.Cleanup(func() {
		_ = osexec.Command("tmux", "kill-session", "-t", smokeExecResource).Run()
	})
	cfg := newSmokeConfig(t, "http://127.0.0.1:1/v1")
	provider := newSmokeProvider(t, filepath.Join(cfg.WorkspaceDir, smokeFileMarker))
	cfg.Endpoint = provider.endpoint()
	cfg.Doc = smokeExecDocument
	cfg.DeliveryRuler = ruler
	cfg.write(t)
	provider.hook = smokeExecScript
	return provider, bootSmokeRig(t, cfg, provider)
}

// smokeExecDocument 渲染执行面的最小宿主文档。
func smokeExecDocument(c *smokeConfig) string {
	return fmt.Sprintf(`entry: %s
provider: local
model: %s
prompt_dir: %s
request_timeout_seconds: 60
providers:
  local: { provider: openai, api_endpoint: "%s", api_key_env: SMOKE_API_KEY }
agents:
  %s:
    model: %s
    tools:
      - { kind: tool, id: exec }
    final_round: { max_rounds: %d, timeout_seconds: %d }
storage: { type: localfile, root: %s }
governance:
  enabled: true
  enforcement: normal
  dir: %s
  approval_ttl_minutes: 30
app:
  wechat:
    config_dir: %s
    token_file: %s
    context_token_dir: %s
    workspace_dir: %s
    approvers: [%s]
`,
		smokeSessionTag, c.Model, c.PromptDir, c.Endpoint,
		smokeSessionTag, c.Model, smokeExecMaxRounds, smokeExecClosingSeconds,
		c.StorageRoot, c.ApprovalDir,
		c.ConfigDir, filepath.Join(c.ConfigDir, "token.json"), c.ContextDir,
		c.WorkspaceDir, smokeApproverChat,
	)
}

// smokeExecScript 是执行面的响应脚本：exec 轮先答 exec_run 再按回执收尾；
// 看门狗轮在前两轮都答 exec_run（同一资源键），收尾轮按子场景分别答 exec_run
// （被 final_round 拒）或直接交正文。
func smokeExecScript(w http.ResponseWriter, index int, modelName string, messages []smokeModelMessage) bool {
	if modelName != smokeEntryModel {
		return false
	}
	marker := smokeMarkerIn(messages)
	switch marker {
	case smokeExecTurn:
		if smokeExecClosingRequest(messages) {
			fmt.Fprint(w, smokeAnswerBody("exec-final "+marker))
			return true
		}
		end := strings.TrimSpace(smokeLastContent(messages))
		switch {
		case strings.Contains(end, smokeExecBody):
			fmt.Fprint(w, smokeAnswerBody("exec-ok "+marker+": "+end))
			return true
		}
		if smokeEndedWithTool(messages) {
			fmt.Fprint(w, smokeAnswerBody("exec-unexpected "+marker+": "+end))
			return true
		}
		fmt.Fprint(w, smokeToolCallBody("call-exec-"+marker, "exec_run", smokeExecArgs()))
		return true
	case smokeExecWatchdogTurn, smokeExecClosingTurn:
		if smokeExecClosingRequest(messages) {
			if marker == smokeExecClosingTurn {
				fmt.Fprint(w, smokeAnswerBody("closing summary "+marker))
				return true
			}
			fmt.Fprint(w, smokeToolCallBody("call-watch-closing-"+marker, "exec_run", smokeExecArgs()))
			return true
		}
		fmt.Fprint(w, smokeToolCallBody(
			fmt.Sprintf("call-watch-%s-%d", marker, index), "exec_run", smokeExecArgs()))
		return true
	}
	return false
}

// smokeExecArgs 渲染 exec_run 的参数：一条只打印固定正文的快命令。
func smokeExecArgs() string {
	return `{"command":"echo","args":["` + smokeExecBody + `"],"kind":"tmux","resource":"` + smokeExecResource + `"}`
}

// smokeExecRequests 报告该轮次标记驱动的模型请求，按到达顺序。
func smokeExecRequests(provider *smokeProvider, marker string) []smokeModelRequest {
	var out []smokeModelRequest
	for _, req := range provider.recorded() {
		if req.Model != smokeEntryModel {
			continue
		}
		if smokeMarkerIn(req.Messages) != marker {
			continue
		}
		out = append(out, req)
	}
	return out
}

// smokeExecToolArgs 报告请求里指定工具的调用参数。
func smokeExecToolArgs(messages []smokeModelMessage, name string) (string, bool) {
	for i := range messages {
		for _, call := range messages[i].ToolCalls {
			if call.Function.Name == name {
				return call.Function.Arguments, true
			}
		}
	}
	return "", false
}

// smokeExecClosingRequest 报告这次请求是否已是收尾轮：最后一条用户消息是收尾指令。
func smokeExecClosingRequest(messages []smokeModelMessage) bool {
	return strings.Contains(smokeLastUserText(messages), smokeExecClosingPrompt)
}

// smokeExecTextCount 报告替身通道向该会话投递过多少条携带给定子串的文本。
func smokeExecTextCount(rig *smokeRig, chat, want string) int {
	count := 0
	for _, evt := range rig.recorded() {
		if evt.Kind == "text" && evt.Target == chat && strings.Contains(evt.Body, want) {
			count++
		}
	}
	return count
}
