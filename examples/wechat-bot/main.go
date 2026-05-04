package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/wechat-robot-go/wechat"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/skill"
)

func main() {
	// 1. Load single config file (tagent.yaml)
	configPath := "tagent.yaml"
	if envPath := os.Getenv("TAGENT_CONFIG"); envPath != "" {
		configPath = envPath
	}

	tagentCfg, err := tagent.LoadConfig(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Load config failed: %v\n", err)
		os.Exit(1)
	}

	// Extract app-specific wechat config from tagent.yaml's app.wechat section
	wechatCfg := loadWechatConfig(tagentCfg.App)
	if err := wechatCfg.EnsureDirs(); err != nil {
		fmt.Fprintf(os.Stderr, "Create dirs failed: %v\n", err)
		os.Exit(1)
	}

	apiKey := tagentCfg.APIKey()
	if apiKey == "" {
		fmt.Fprintf(os.Stderr, "API key not set. Set %s environment variable.\n", tagentCfg.APIKeyEnv)
		os.Exit(1)
	}

	// Set framework log level
	log.SetLevel(tagentCfg.LogLevel)

	// Resolve effective model name
	effectiveModel := tagentCfg.Model
	entryCfg := tagentCfg.Agents[tagentCfg.Entry]
	if entryCfg.Model == "" {
		entryCfg.Model = effectiveModel
	}
	tagentCfg.Agents[tagentCfg.Entry] = entryCfg

	fmt.Println("===========================================")
	fmt.Println("  tagent WeChat Bot")
	fmt.Println("===========================================")
	fmt.Printf("  Agent Name:  %s\n", tagentCfg.Entry)
	fmt.Printf("  Model:       %s\n", effectiveModel)
	fmt.Printf("  Max Tokens:  %d\n", entryCfg.MaxTokens)
	fmt.Printf("  Log Level:   %s\n", tagentCfg.LogLevel)
	fmt.Printf("  Config:      %s\n", configPath)
	fmt.Println("===========================================")

	// 2. Create LLM model
	apiEndpoint := tagentCfg.APIEndpoint
	if apiEndpoint == "" {
		apiEndpoint = "https://open.bigmodel.cn/api/paas/v4"
	}
	llmModel := openai.New(
		effectiveModel,
		openai.WithAPIKey(apiKey),
		openai.WithBaseURL(apiEndpoint),
	)

	// 3. Load skills repository (optional)
	var skillRepo *skill.FSRepository
	if skillRepo, err = skill.NewFSRepository("./skills"); err != nil {
		log.Warnf("Failed to load skills from ./skills: %v (knowledge will run without skill tools)", err)
	} else {
		for _, s := range skillRepo.Summaries() {
			dir, _ := skillRepo.Path(s.Name)
			fmt.Printf("  Skill indexed: %s (%s/SKILL.md) - %s\n", s.Name, dir, s.Description)
		}
	}

	// Use the same model for summary generation (Stage 2 compression)
	// Without this, SmartCompress drops old segments with only a notice string,
	// causing context breakage. With SummaryModel, it generates LLM summaries.
	ta, err := tagent.New(*tagentCfg,
		tagent.WithModel(llmModel),
		tagent.WithSummaryModel(llmModel),
		tagent.WithSkillRepo(skillRepo),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Create tagent agent failed: %v\n", err)
		os.Exit(1)
	}
	defer ta.Close()

	// 3. Setup signal handling
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// 4. Create WeChat bot
	slogLogger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	bot := wechat.NewBot(wechat.WithLogger(slogLogger))

	// 5. Login
	fmt.Println("Logging in to WeChat...")
	err = bot.Login(ctx, func(qrCode string) {
		fmt.Println("\nPlease scan the QR code with WeChat:")
		fmt.Println("----------------------------------------")
		if strings.HasPrefix(qrCode, "http") {
			fmt.Println(qrCode)
		} else {
			fmt.Println("[QR code image content]")
		}
		fmt.Println("----------------------------------------")
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Login failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Login successful!")

	// 6. Register message handler
	requestTimeout := time.Duration(tagentCfg.RequestTimeoutSeconds) * time.Second

	bot.OnMessage(func(ctx context.Context, msg *wechat.Message) error {
		text := msg.Text()
		if text == "" {
			return nil
		}

		// Show typing indicator
		_ = bot.SendTyping(ctx, msg.FromUserID)

		// Create request context with timeout
		reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
		defer cancel()

		// Keep sending typing indicator while processing
		done := make(chan struct{})
		go func() {
			ticker := time.NewTicker(10 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-done:
					return
				case <-ticker.C:
					_ = bot.SendTyping(reqCtx, msg.FromUserID)
				}
			}
		}()

		// Run tagent agent
		response, err := generateResponse(reqCtx, ta, text)
		close(done)

		if err != nil {
			if reqCtx.Err() == context.DeadlineExceeded {
				response = fmt.Sprintf("Processing timed out (exceeded %v). Please try again.", requestTimeout)
			} else {
				slog.Error("Agent response failed", "error", err)
				response = "Sorry, I encountered an error. Please try again."
			}
		}

		// Stop typing
		_ = bot.StopTyping(ctx, msg.FromUserID)

		// Send reply (handle long text)
		if len(response) > 2000 {
			token, _ := bot.GetContextToken(msg.FromUserID)
			if token != "" {
				_, err = wechat.SendLongText(ctx, bot.Client(), bot.Media(), msg.FromUserID, response, token)
				if err != nil {
					return bot.Reply(ctx, msg, response[:2000]+"\n\n[Message truncated]")
				}
				return nil
			}
		}

		return bot.Reply(ctx, msg, response)
	})

	// 7. Run
	fmt.Println("Bot is running. Press Ctrl+C to stop.")
	if err := bot.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Bot stopped with error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Bot stopped gracefully.")
}

// generateResponse runs the tagent agent and returns the final output.
// It logs audit information for each event received from the agent,
// with heartbeat monitoring to detect stuck/deadlocked event loops.
func generateResponse(ctx context.Context, ta *agent.TagentAgent, userMessage string) (string, error) {
	startTime := time.Now()
	log.Infof("[AUDIT] >>> user message: %s", truncate(userMessage, 200))

	msg := model.NewUserMessage(userMessage)

	eventCh, err := ta.RunSimple(ctx, "wechat-user", "wechat-session", msg)
	if err != nil {
		return "", fmt.Errorf("agent run failed: %w", err)
	}

	// Collect the final output with audit trail and heartbeat monitoring
	var (
		finalOutput   string
		eventCount    int
		lastEventTime = time.Now()
		stats         sessionStats

		// Tool call tracking (per tool name → call count)
		toolCallCounts = make(map[string]int)
	)

	// Heartbeat: log a warning if no event received for 30s
	heartbeatTicker := time.NewTicker(30 * time.Second)
	defer heartbeatTicker.Stop()

	// Use select-based event consumption instead of range,
	// so we can detect context cancellation and heartbeat gaps.
loop:
	for {
		select {
		case evt, ok := <-eventCh:
			if !ok {
				break loop // channel closed
			}
			eventCount++
			lastEventTime = time.Now()
			logAuditEvent(eventCount, evt, &stats)
			trackToolCalls(evt, toolCallCounts)

			if evt.Response != nil && len(evt.Response.Choices) > 0 {
				choice := evt.Response.Choices[len(evt.Response.Choices)-1]
				if choice.Message.Content != "" && len(choice.Message.ToolCalls) == 0 {
					finalOutput = choice.Message.Content
				}
			}

		case <-heartbeatTicker.C:
			gap := time.Since(lastEventTime)
			if gap > 60*time.Second {
				log.Errorf("[AUDIT] ⚠️ NO EVENT for %v — agent may be stuck (total_events=%d, elapsed=%v)",
					gap, eventCount, time.Since(startTime).Round(time.Second))
			} else if gap > 30*time.Second {
				log.Warnf("[AUDIT] no event for %v (total_events=%d, elapsed=%v)",
					gap, eventCount, time.Since(startTime).Round(time.Second))
			}

		case <-ctx.Done():
			log.Warnf("[AUDIT] context cancelled after %v (events=%d): %v",
				time.Since(startTime).Round(time.Second), eventCount, ctx.Err())
			break loop
		}
	}

	// Session summary (aligned with trpcclaw's observability)
	logSessionSummary(startTime, eventCount, &stats, toolCallCounts)

	if finalOutput == "" {
		finalOutput = "No response generated"
	}

	return finalOutput, nil
}

// sessionStats tracks observability data aligned with trpcclaw's model.
type sessionStats struct {
	llmCalls       int
	toolCalls      int
	errors         int
	compressions   int
	totalTokensIn  int
	totalTokensOut int
	maxTokenUsage  int // peak prompt_tokens seen
}

// logAuditEvent logs audit information for a single agent event.
func logAuditEvent(idx int, evt *event.Event, stats *sessionStats) {
	if evt == nil {
		log.Debugf("[AUDIT] event #%d: nil event", idx)
		return
	}

	if evt.Response == nil {
		log.Debugf("[AUDIT] event #%d: nil response tag=%s author=%s", idx, evt.Tag, evt.Author)
		return
	}

	rsp := evt.Response

	// Error event
	if rsp.Error != nil {
		stats.errors++
		log.Errorf("[AUDIT] event #%d: ERROR type=%s message=%s",
			idx, rsp.Error.Type, rsp.Error.Message)
		return
	}

	// Runner completion
	if evt.IsRunnerCompletion() {
		log.Infof("[AUDIT] event #%d: RUNNER_COMPLETE", idx)
		return
	}

	// Tool response
	if rsp.Object == model.ObjectTypeToolResponse {
		stats.toolCalls++
		// Log tool response with result snippet for observability
		snippet := ""
		if len(rsp.Choices) > 0 {
			content := rsp.Choices[len(rsp.Choices)-1].Message.Content
			snippet = truncate(content, 200)
		}
		log.Infof("[AUDIT] event #%d: TOOL_RESPONSE tag=%s author=%s result=%s",
			idx, evt.Tag, evt.Author, snippet)
		return
	}

	// LLM response with tool calls
	if len(rsp.Choices) > 0 {
		choice := rsp.Choices[len(rsp.Choices)-1]
		if len(choice.Message.ToolCalls) > 0 {
			stats.llmCalls++
			var toolNames []string
			for _, tc := range choice.Message.ToolCalls {
				toolNames = append(toolNames, tc.Function.Name)
			}
			log.Infof("[AUDIT] event #%d: LLM_TOOL_CALL tools=%v tag=%s author=%s",
				idx, toolNames, evt.Tag, evt.Author)
			for _, tc := range choice.Message.ToolCalls {
				log.Debugf("[AUDIT] event #%d:   tool=%s args=%s",
					idx, tc.Function.Name, truncate(string(tc.Function.Arguments), 500))
			}
		} else if choice.Message.Content != "" {
			// LLM text response
			stats.llmCalls++
			log.Infof("[AUDIT] event #%d: LLM_RESPONSE content_len=%d tag=%s author=%s",
				idx, len(choice.Message.Content), evt.Tag, evt.Author)
			log.Debugf("[AUDIT] event #%d: LLM response content: %s",
				idx, truncate(choice.Message.Content, 500))
		}
	}

	// Usage info (always log at Info for token budget visibility)
	if rsp.Usage != nil {
		stats.totalTokensIn += int(rsp.Usage.PromptTokens)
		stats.totalTokensOut += int(rsp.Usage.CompletionTokens)
		if int(rsp.Usage.PromptTokens) > stats.maxTokenUsage {
			stats.maxTokenUsage = int(rsp.Usage.PromptTokens)
		}
		log.Infof("[AUDIT] event #%d: TOKEN_BUDGET prompt=%d completion=%d total=%d",
			idx, rsp.Usage.PromptTokens, rsp.Usage.CompletionTokens, rsp.Usage.TotalTokens)
	}
}

// trackToolCalls extracts tool call names from an event for session statistics.
func trackToolCalls(evt *event.Event, counts map[string]int) {
	if evt == nil || evt.Response == nil {
		return
	}
	for _, choice := range evt.Response.Choices {
		for _, tc := range choice.Message.ToolCalls {
			counts[tc.Function.Name]++
		}
	}
}

// logSessionSummary logs a session summary aligned with trpcclaw's observability model:
// token budget, compressions, tool call distribution, errors.
func logSessionSummary(startTime time.Time, eventCount int, stats *sessionStats, toolCallCounts map[string]int) {
	elapsed := time.Since(startTime).Round(time.Millisecond)

	log.Infof("[SESSION] ========== Summary ==========")
	log.Infof("[SESSION] elapsed=%v events=%d llm_calls=%d tool_responses=%d errors=%d",
		elapsed, eventCount, stats.llmCalls, stats.toolCalls, stats.errors)
	log.Infof("[SESSION] tokens: in=%d out=%d peak_prompt=%d",
		stats.totalTokensIn, stats.totalTokensOut, stats.maxTokenUsage)

	if len(toolCallCounts) > 0 {
		log.Infof("[SESSION] tool_calls: %v", toolCallCounts)
	}

	// Health warnings (aligned with trpcclaw's CheckExpectations)
	if stats.errors > 0 {
		log.Warnf("[SESSION] ⚠️ %d errors occurred during session", stats.errors)
	}
	if eventCount > 100 {
		log.Warnf("[SESSION] ⚠️ high event count (%d), possible runaway loop", eventCount)
	}
	if stats.maxTokenUsage > 7000 {
		log.Warnf("[SESSION] ⚠️ peak prompt tokens %d near context limit", stats.maxTokenUsage)
	}

	log.Infof("[SESSION] ==============================")
}

// truncate truncates a string to maxLen characters with ellipsis.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// marshalJSON is a helper to safely marshal a value for debug logging.
func marshalJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("<marshal error: %v>", err)
	}
	return string(b)
}

// WechatAppConfig holds WeChat-specific configuration extracted from tagent.yaml's app.wechat section.
type WechatAppConfig struct {
	ConfigDir       string `yaml:"config_dir"`
	TokenFile       string `yaml:"token_file"`
	ContextTokenDir string `yaml:"context_token_dir"`
}

// DefaultWechatAppConfig returns default WeChat configuration.
func DefaultWechatAppConfig() WechatAppConfig {
	return WechatAppConfig{
		ConfigDir:       ".wechat-config",
		TokenFile:       "token.json",
		ContextTokenDir: ".wechat-context-tokens",
	}
}

// loadWechatConfig extracts WeChat config from tagent.yaml's app.wechat section.
func loadWechatConfig(app map[string]any) WechatAppConfig {
	cfg := DefaultWechatAppConfig()
	if app == nil {
		return cfg
	}
	raw, ok := app["wechat"]
	if !ok {
		return cfg
	}
	// Re-marshal/unmarshal to get typed struct from map[string]any
	data, err := json.Marshal(raw)
	if err != nil {
		return cfg
	}
	_ = json.Unmarshal(data, &cfg)
	return cfg
}

// EnsureDirs creates necessary WeChat directories.
func (c *WechatAppConfig) EnsureDirs() error {
	dirs := []string{
		c.ConfigDir,
		c.ContextTokenDir,
	}
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create dir %s: %w", dir, err)
		}
	}
	return nil
}

// GetWechatTokenFile returns the full path to the WeChat token file.
func (c *WechatAppConfig) GetWechatTokenFile() string {
	return c.ConfigDir + "/" + c.TokenFile
}
