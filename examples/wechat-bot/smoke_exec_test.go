//go:build smoke

// smoke_exec_test.go 是第二波冒烟的执行面：exec_run 在宿主未接执行资源管理器时以
// 具名失败进回复（负锚，接线后同分支转正向），final_round 在轮数耗尽后把第 3 个
// 决策转 closing round——send 被拒回执进 transcript、收尾正文终止回合。台架复用
// smoke_harness_test.go 的真实装配，脚本日程与断言留在本文件。
//
// 契约: docs/wiki/agent/event-flow.md#watchdog-final-round
package main

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tagentmodel "github.com/SpellingDragon/tagent/api/model"
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
	// smokeExecResource 是 exec_run 声明的执行资源 key。
	smokeExecResource = "smoke-exec"

	// smokeExecMaxRounds 与 smokeExecClosingSeconds 是 entry 声明的看门狗预算。
	smokeExecMaxRounds      = 2
	smokeExecClosingSeconds = 30

	// smokeExecClosingPrompt 是默认收尾指令的固定片段。
	smokeExecClosingPrompt = "this is your final round"
	// smokeExecRefusal 是收尾轮工具拒绝回执的固定片段。
	smokeExecRefusal = "this call was not admitted"
)

// TestLocalSmokeWave2Exec 是第二波冒烟的执行面入口。
func TestLocalSmokeWave2Exec(t *testing.T) {
	t.Logf("[wave2-exec] tmux available=%t; the wechat host wires no execution resource manager",
		tagentexec.IsTmuxAvailable())
	t.Run("exec face is granted and its refusal is named", smokeExecRun)
	t.Run("watchdog closing round refuses the third send call", smokeExecWatchdogRefusal)
	t.Run("watchdog closing round delivers its closing body", smokeExecWatchdogClosing)
}

// smokeExecRun 驱动一次 exec_run：模型先答工具调用，再按回执收尾。宿主接线后
// 回执携带命令正文，本分支断言结果正文进回复；当前宿主未接线，回执是具名失败，
// 断言拒绝正文进回复且命令正文不出现（GAP 如实记录）。
func smokeExecRun(t *testing.T) {
	logs := installSmokeLog(t)
	provider, rig := smokeExecBoot(t)
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
	switch {
	case strings.Contains(receipt, smokeExecBody):
		t.Logf("[wave2-exec] exec face executed: %q", receipt)
		rig.waitText(chat, "exec-ok "+smokeExecTurn, 60*time.Second)
		if got := smokeExecTextCount(rig, chat, smokeExecBody); got == 0 {
			t.Fatalf("the exec result %q entered no delivered reply", smokeExecBody)
		}
	case strings.Contains(receipt, tagentexec.ErrUnwired.Error()):
		t.Logf("[wave2-exec] GAP(exec): the wechat host wires no execution resource manager, so exec_run was refused by name: %q", receipt)
		rig.waitText(chat, "exec-gap "+smokeExecTurn, 60*time.Second)
		if got := smokeExecTextCount(rig, chat, tagentexec.ErrUnwired.Error()); got == 0 {
			t.Fatalf("the exec refusal entered no delivered reply")
		}
		if got := smokeExecTextCount(rig, chat, smokeExecBody); got != 0 {
			t.Fatalf("the unwired exec face delivered the command body %q %d time(s), want none", smokeExecBody, got)
		}
	default:
		t.Fatalf("exec_run receipt = %q, want the command body or the named %q refusal", receipt, tagentexec.ErrUnwired.Error())
	}
	if got := logs.count("terminal=failed"); got != 0 {
		t.Fatalf("the exec turn left %d terminal=failed line(s), want none", got)
	}
}

// smokeExecWatchdogRefusal 让模型在 closing round 里再答一次 send：该调用不被
// 受理，回执写进会话 transcript，审批面只登记前两轮的两次扣留。
func smokeExecWatchdogRefusal(t *testing.T) {
	installSmokeLog(t)
	provider, rig := smokeExecBoot(t)
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

	history := rig.host.Run().SessionHistory(rig.addressOf(chat))
	if !smokeExecHistoryCarries(history, sendToolID, smokeExecRefusal) {
		t.Fatalf("the closing round's send call left no refusal receipt in the session history (%d message(s)): %+v\nrecent requests:\n%s",
			len(history), history, rig.recentRequests(6))
	}

	if after := len(rig.gateway.Manager().Pending()); after != before+2 {
		t.Fatalf("pending approvals %d -> %d, want exactly the two ordinary rounds held and the closing call admitted nowhere", before, after)
	}
	if got := len(smokeExecRequests(provider, smokeExecWatchdogTurn)); got != 3 {
		t.Fatalf("the closing round drove a further decision: model requests = %d, want the drive to end there", got)
	}
	t.Logf("[wave2-exec] watchdog closing round: request 3 carries the closing instruction, the third send call was refused (%q), pending approvals %d -> %d",
		smokeExecRefusal, before, before+2)
}

// smokeExecWatchdogClosing 让模型在 closing round 直接交正文：回合以收尾正文
// 终止，收尾轮没有受理任何工具调用。
func smokeExecWatchdogClosing(t *testing.T) {
	installSmokeLog(t)
	provider, rig := smokeExecBoot(t)
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
	rig.waitText(chat, "closing summary "+smokeExecClosingTurn, 60*time.Second)
	if got := len(smokeExecRequests(provider, smokeExecClosingTurn)); got != 3 {
		t.Fatalf("the closing turn drove %d model request(s) after its closing answer, want the drive to end there", got)
	}
	if after := len(rig.gateway.Manager().Pending()); after != before+2 {
		t.Fatalf("pending approvals %d -> %d, want exactly the two ordinary rounds held and the closing answer admitting nothing", before, after)
	}
	if got := smokeExecTextCount(rig, chat, smokeExecRefusal); got != 0 {
		t.Fatalf("the closing answer carried %d refusal receipt(s), want none", got)
	}
	t.Logf("[wave2-exec] watchdog closing round delivered %q; pending approvals %d -> %d",
		"closing summary "+smokeExecClosingTurn, before, before+2)
}

// smokeExecBoot 装配一台执行面宿主：entry 声明 exec 与 final_round，治理开启，
// 模型与出站都指向本地替身。
func smokeExecBoot(t *testing.T) (*smokeProvider, *smokeRig) {
	t.Helper()
	cfg := newSmokeConfig(t, "http://127.0.0.1:1/v1")
	provider := newSmokeProvider(t, filepath.Join(cfg.WorkspaceDir, smokeFileMarker))
	cfg.Endpoint = provider.endpoint()
	cfg.Doc = smokeExecDocument
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
      - { kind: tool, id: send }
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
// 看门狗轮在前两轮连打 send，收尾轮按子场景分别答 send 或直接交正文。
func smokeExecScript(w http.ResponseWriter, _ int, modelName string, messages []smokeModelMessage) bool {
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
		case strings.Contains(end, tagentexec.ErrUnwired.Error()):
			fmt.Fprint(w, smokeAnswerBody("exec-gap "+marker+": "+end))
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
			fmt.Fprint(w, smokeToolCallBody("call-watch-closing-"+marker, sendToolID, smokeSendArgs(marker)))
			return true
		}
		fmt.Fprint(w, smokeToolCallBody(
			fmt.Sprintf("call-watch-%s-%d", marker, len(messages)), sendToolID, smokeSendArgs(marker)))
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

// smokeExecHistoryCarries 报告会话历史里是否有一条指定工具、携带给定正文的回执。
func smokeExecHistoryCarries(messages []tagentmodel.Message, tool, want string) bool {
	for _, msg := range messages {
		if msg.ToolName == tool && strings.Contains(msg.Content, want) {
			return true
		}
	}
	return false
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
