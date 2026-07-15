package tagent_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/model/openai"

	tagentagent "github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/testutil"
)

// TestPlanAgentBug_AgentToolWrapper_SubAgentRun 用 AgentToolWrapper.Call()
// 触发 sub-agent 路径（这才是 wechat-bot 中 plan agent 的真实调用路径）。
//
// wechat-bot 日志中失败的场景是主 agent 调 tool "plan"，
// 而 AgentToolWrapper.Call() 内部用 agent.Run() 触发 plan agent。
//
// 假设根因：
//
//	AgentToolWrapper.Run() 创建临时 invBus + 新 ContextManager + 新 session，
//	user message 作为 external_input 发到 invBus，被 runEventLoop 消费后
//	调 runFlow；框架 ContentRequestProcessor 把 session 事件加入 request，
//	但新 session 中可能没有 user message event，或 extractCurrentTurnMessages
//	把 unprefixed user message 过滤掉了。
//
// 运行方式：
//
//	TRPC_CLAW_MODEL_NAME=glm-5.2 go test -v \
//	    -run TestPlanAgentBug_AgentToolWrapper_SubAgentRun \
//	    ./tests/ -timeout 180s
func TestPlanAgentBug_AgentToolWrapper_SubAgentRun(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	cfg, err := testutil.LoadConfig()
	if err != nil {
		t.Skipf("无法加载配置: %v", err)
	}
	t.Logf("配置: model=%s endpoint=%s", cfg.ModelName, cfg.Endpoint)

	zhipuModel := openai.New(
		cfg.ModelName,
		openai.WithAPIKey(cfg.APIKey),
		openai.WithBaseURL(cfg.Endpoint),
	)

	thinking := true
	subAg, err := tagentagent.NewTagentAgent(&tagentagent.TagentConfig{
		Model:             zhipuModel,
		MaxToolIterations: 2,
		MaxTokens:         8000,
		Temperature:       0.3,
		SystemPrompt:      "You are a plan agent. Create concise work plans. Keep responses short.",
		Name:              "plan",
		Description:       "Plan agent",
		ThinkingEnabled:   &thinking,
	})
	if err != nil {
		t.Fatalf("创建 agent 失败: %v", err)
	}

	// 包装为 AgentToolWrapper（模拟主 agent 把 plan 当 tool 调用）
	wrapper := tagentagent.NewAgentToolWrapper(
		subAg,
		"Create work plans", // tool description
		nil,                 // eventParams
		nil,                 // parentStore
	)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	userInput := "Create a 3-step plan for learning Go."
	t.Logf("用户输入 (模拟主 agent 调用): %s", userInput)

	args, _ := json.Marshal(map[string]string{"request": userInput})
	result, err := wrapper.Call(ctx, args)
	if err != nil {
		t.Fatalf("❌ BUG: AgentToolWrapper.Call 报错: %v", err)
	}

	resultStr, ok := result.(string)
	if !ok {
		t.Fatalf("❌ BUG: 返回值不是 string, got %T", result)
	}

	t.Logf("plan agent 返回 (len=%d): %.300s", len(resultStr), resultStr)

	// 关键断言
	if strings.Contains(resultStr, "An error occurred during execution") {
		t.Errorf("❌ BUG REPRODUCED: plan agent 返回了框架通用错误消息")
		t.FailNow()
	}
	if resultStr == "" {
		t.Errorf("❌ BUG REPRODUCED: plan agent 返回了空字符串")
		t.FailNow()
	}

	t.Logf("✅ plan agent 正常返回: %.200s...", resultStr)
}
