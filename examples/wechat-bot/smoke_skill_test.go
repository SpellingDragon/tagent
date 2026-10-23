//go:build smoke

// smoke_skill_test.go 是第二波冒烟的技能面：skill_search 按名字与描述从技能仓
// 回答短名单，skill_load 按短名单给出的名字载入技能，未知名字按名拒绝而回合照常
// 收尾。台架复用 smoke_harness_test.go 的真实装配，临时技能仓、脚本日程与断言
// 留在本文件。
//
// 运行: go test -tags smoke ./... -run TestLocalSmokeWave2Skill -count=1 -timeout 10m -v
//
// 契约: docs/wiki/tool/tool-architecture.md#recall-subtools
package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tagentmodel "github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/tool/knowledge"

	"trpc.group/trpc-go/trpc-agent-go/skill"
)

const (
	// smokeSkillAlpha 是检索词命中的技能名。
	smokeSkillAlpha = "smoke-skill-alpha"
	// smokeSkillBeta 是检索词不命中的技能名。
	smokeSkillBeta = "smoke-skill-beta"
	// smokeSkillAlphaBody 是 alpha 的正文标记。
	smokeSkillAlphaBody = "SMOKE-SKILL-ALPHA-BODY"
	// smokeSkillBetaBody 是 beta 的正文标记。
	smokeSkillBetaBody = "SMOKE-SKILL-BETA-BODY"
	// smokeSkillQuery 只命中 alpha：beta 的名字与描述都不含这个词。
	smokeSkillQuery = "alpha"
	// smokeSkillMissing 是技能仓里不存在的名字。
	smokeSkillMissing = "smoke-skill-missing"
	// smokeSkillWait 是技能回合的交付等待上限。
	smokeSkillWait = 60 * time.Second
)

// TestLocalSmokeWave2Skill 是第二波冒烟的技能面入口。
// - the entry declaration carries skill_search and skill_load over the deployment's skill repository
// - the search answer reaches the next decision with the name and description the repository holds
// - the load answer reaches the next decision and names the skill the search selected
// - the next request after the load carries the skill body the repository holds, and no request before it does
// - an unknown skill name is refused by name and the turn still delivers its answer
func TestLocalSmokeWave2Skill(t *testing.T) {
	t.Run("search answers the shortlist and the load names the skill back", smokeSkillSearchThenLoad)
	t.Run("an unknown skill name is refused and the turn still settles", smokeSkillUnknownName)
}

// smokeSkillSearchThenLoad 让脚本模型按短名单载入 alpha：检索结果进入下一次决策，
// 载入结果进入收尾决策，用户收到本轮的终局回答。
func smokeSkillSearchThenLoad(t *testing.T) {
	marker := "SMOKE-R610"
	cfg, provider := smokeSkillConfig(t)
	provider.hook = func(w http.ResponseWriter, _ int, modelName string, messages []smokeModelMessage) bool {
		if modelName != smokeEntryModel || smokeMarkerIn(messages) != marker {
			return false
		}
		last := smokeLastContent(messages)
		switch {
		case strings.Contains(last, "loaded: "+smokeSkillAlpha):
			fmt.Fprint(w, smokeAnswerBody("answer "+marker))
			return true
		case strings.Contains(last, `"tool":"`+knowledge.SkillSearchToolName+`"`):
			fmt.Fprint(w, smokeToolCallBody("call-skill-load", knowledge.SkillLoadToolName,
				`{"skill":"`+smokeSkillAlpha+`"}`))
			return true
		case smokeSkillFirstDecision(messages, marker):
			fmt.Fprint(w, smokeToolCallBody("call-skill-search", knowledge.SkillSearchToolName,
				`{"query":"`+smokeSkillQuery+`"}`))
			return true
		}
		return false
	}

	rig := bootSmokeRig(t, cfg, provider)
	chat := smokeChats[0]
	rig.drive(chat, marker+" smoke skill shortlist")
	rig.waitText(chat, "answer "+marker, smokeSkillWait)

	requests := smokeSkillRequests(provider, marker)
	if len(requests) != 3 {
		t.Fatalf("entry requests = %d, want the search decision, the load decision and the answer:\n%s",
			len(requests), smokeSkillDigest(requests))
	}
	search := smokeSkillResult(t, requests[1], "call-skill-search")
	for _, want := range []string{knowledge.SkillSearchToolName, smokeSkillAlpha, "the smoke alpha runbook"} {
		if !strings.Contains(search.Content, want) {
			t.Fatalf("the search answer %q does not carry %q", search.Content, want)
		}
	}
	if strings.Contains(search.Content, smokeSkillBeta) {
		t.Fatalf("the search answer %q carries the skill the query misses", search.Content)
	}
	load := smokeSkillResult(t, requests[2], "call-skill-load")
	if !strings.Contains(load.Content, "loaded: "+smokeSkillAlpha) {
		t.Fatalf("the load answer %q does not name the selected skill", load.Content)
	}
	if !strings.Contains(requests[2].text(), smokeSkillAlphaBody) {
		t.Fatalf("the decision after the load carries no %q; the loaded body never reached the next request:\n%s",
			smokeSkillAlphaBody, smokeSkillDigest(requests))
	}
	for i := 0; i < 2; i++ {
		if strings.Contains(requests[i].text(), smokeSkillAlphaBody) {
			t.Fatalf("decision %d carries %q before any load was accepted", i+1, smokeSkillAlphaBody)
		}
	}

	t.Logf("[wave2-skill] search answer = %s", search.Content)
	t.Logf("[wave2-skill] load answer = %q", load.Content)
	t.Logf("[wave2-skill] loaded body reached the next request: %t", strings.Contains(requests[2].text(), smokeSkillAlphaBody))
}

// smokeSkillUnknownName 让脚本模型载入技能仓里不存在的名字：工具结果按名拒绝，
// 拒绝不终止回合，用户仍收到终局回答。
func smokeSkillUnknownName(t *testing.T) {
	marker := "SMOKE-R611"
	cfg, provider := smokeSkillConfig(t)
	provider.hook = func(w http.ResponseWriter, _ int, modelName string, messages []smokeModelMessage) bool {
		if modelName != smokeEntryModel || smokeMarkerIn(messages) != marker {
			return false
		}
		last := smokeLastContent(messages)
		switch {
		case strings.Contains(last, "unknown skill"):
			fmt.Fprint(w, smokeAnswerBody("answer "+marker))
			return true
		case smokeSkillFirstDecision(messages, marker):
			fmt.Fprint(w, smokeToolCallBody("call-skill-missing", knowledge.SkillLoadToolName,
				`{"skill":"`+smokeSkillMissing+`"}`))
			return true
		}
		return false
	}

	rig := bootSmokeRig(t, cfg, provider)
	chat := smokeChats[1]
	rig.drive(chat, marker+" smoke skill missing")
	rig.waitText(chat, "answer "+marker, smokeSkillWait)

	requests := smokeSkillRequests(provider, marker)
	if len(requests) != 2 {
		t.Fatalf("entry requests = %d, want the refused load decision and the answer:\n%s",
			len(requests), smokeSkillDigest(requests))
	}

	refused := smokeSkillResult(t, requests[1], "call-skill-missing")
	if !strings.Contains(refused.Content, "unknown skill: "+smokeSkillMissing) {
		t.Fatalf("the refused load answer = %q, want the name the repository does not hold", refused.Content)
	}
	t.Logf("[wave2-skill] negative anchor: load of %q answered %q and the turn still delivered",
		smokeSkillMissing, refused.Content)
}

// smokeSkillConfig 造两个假技能并渲染只声明技能面的宿主文档：技能仓根是临时目录，
// 入口 agent 声明两个技能工具，模型与存储走台架的本地替身。
func smokeSkillConfig(t *testing.T) (*smokeConfig, *smokeProvider) {
	t.Helper()
	root := t.TempDir()
	smokeSkillWrite(t, root, smokeSkillAlpha, "the smoke alpha runbook", smokeSkillAlphaBody+"\n")
	smokeSkillWrite(t, root, smokeSkillBeta, "the smoke beta notes", smokeSkillBetaBody+"\n")
	cfg := newSmokeConfig(t, "http://127.0.0.1:1/v1")
	cfg.Doc = func(c *smokeConfig) string { return smokeSkillDocument(c, root) }
	provider := newSmokeProvider(t, filepath.Join(cfg.WorkspaceDir, smokeFileMarker))
	cfg.Endpoint = provider.endpoint()
	cfg.write(t)
	return cfg, provider
}

// smokeSkillWrite 在技能仓根下写一个技能：frontmatter 的 name 与 description
// 加正文。
func smokeSkillWrite(t *testing.T, root, name, description, body string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create the skill directory %s: %v", dir, err)
	}
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\n\n" + body
	if err := os.WriteFile(filepath.Join(dir, skill.SkillFile), []byte(content), 0o644); err != nil {
		t.Fatalf("write the skill file %s: %v", filepath.Join(dir, skill.SkillFile), err)
	}
}

// smokeSkillDocument 渲染技能面的最小宿主文档：一个入口 agent、两个技能工具、
// 本地 provider 与 localfile 存储。
func smokeSkillDocument(c *smokeConfig, skillRoot string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "entry: %s\n", smokeSessionTag)
	b.WriteString("provider: local\n")
	fmt.Fprintf(&b, "model: %s\n", c.Model)
	fmt.Fprintf(&b, "prompt_dir: %s\n", c.PromptDir)
	b.WriteString("request_timeout_seconds: 60\n")

	fmt.Fprintf(&b, "providers:\n  local: { provider: openai, api_endpoint: %q, api_key_env: SMOKE_API_KEY }\n", c.Endpoint)
	fmt.Fprintf(&b, "agents:\n  %s:\n    model: %s\n", smokeSessionTag, c.Model)
	fmt.Fprintf(&b, "    skill_dirs: [%q]\n", skillRoot)
	b.WriteString("    tools:\n")
	fmt.Fprintf(&b, "      - { kind: tool, id: %s }\n", knowledge.SkillSearchToolName)
	fmt.Fprintf(&b, "      - { kind: tool, id: %s }\n", knowledge.SkillLoadToolName)

	b.WriteString("    max_tokens: 2000\n")
	b.WriteString("    compress_threshold: 0.3\n")
	b.WriteString("    keep_recent_tasks: 1\n")
	b.WriteString("    max_tool_iterations: 3\n")
	fmt.Fprintf(&b, "storage: { type: localfile, root: %s }\n", c.StorageRoot)
	b.WriteString("app:\n  wechat:\n")

	fmt.Fprintf(&b, "    config_dir: %s\n", c.ConfigDir)
	fmt.Fprintf(&b, "    token_file: %s\n", filepath.Join(c.ConfigDir, "token.json"))
	fmt.Fprintf(&b, "    context_token_dir: %s\n", c.ContextDir)
	fmt.Fprintf(&b, "    workspace_dir: %s\n", c.WorkspaceDir)
	return b.String()
}

// smokeSkillFirstDecision 报告这次决策是不是本轮的第一条用户输入：技能回合的
// 工具结果同样以用户角色的观察进入下一次决策，标记仍取它之前的输入。
func smokeSkillFirstDecision(messages []smokeModelMessage, marker string) bool {
	return strings.Contains(smokeLastContent(messages), marker+" smoke skill")
}

// smokeSkillRequests 报告标记落在本轮输入里的入口请求。
func smokeSkillRequests(provider *smokeProvider, marker string) []smokeModelRequest {
	var out []smokeModelRequest
	for _, req := range provider.recorded() {
		if req.Model != smokeEntryModel {
			continue
		}
		if smokeMarkerIn(req.Messages) == marker {
			out = append(out, req)
		}
	}
	return out
}

// smokeSkillResult 报告一次决策里属于该调用 id 的工具结果：调用回执以工具角色
// 带调用 id，结果正文随后以用户角色的观察进入下一次决策。
func smokeSkillResult(t *testing.T, req smokeModelRequest, callID string) smokeModelMessage {
	t.Helper()
	for i, msg := range req.Messages {
		if msg.Role != string(tagentmodel.RoleTool) || msg.ToolCallID != callID {
			continue
		}
		for _, next := range req.Messages[i+1:] {
			if next.Role == string(tagentmodel.RoleUser) {
				return next
			}
		}
	}
	t.Fatalf("no tool result for %s in the decision:\n%s", callID, smokeSkillDigest([]smokeModelRequest{req}))
	return smokeModelMessage{}
}

// smokeSkillDigest 渲染若干请求的消息角色、调用 id 与正文首行，供失败诊断。
func smokeSkillDigest(requests []smokeModelRequest) string {
	lines := make([]string, 0, len(requests))
	for i, req := range requests {
		lines = append(lines, fmt.Sprintf("request %d (%s):", i+1, req.Model))
		for _, msg := range req.Messages {
			head := strings.SplitN(strings.TrimSpace(msg.Content), "\n", 2)[0]
			if len(head) > 120 {
				head = head[:120] + "…"
			}
			lines = append(lines, fmt.Sprintf("  %s call=%s %s", msg.Role, msg.ToolCallID, head))
		}
	}
	return strings.Join(lines, "\n")
}
