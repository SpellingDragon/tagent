package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
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
	telemetrytrace "trpc.group/trpc-go/trpc-agent-go/telemetry/trace"
)

// msgMu ensures sequential message processing — only one InjectMessage +
// outputCh consumption cycle runs at a time.
var msgMu sync.Mutex

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

	// Resolve entry agent's effective model name
	entryCfg := tagentCfg.Agents[tagentCfg.Entry]
	effectiveModel := entryCfg.Model
	if effectiveModel == "" {
		effectiveModel = tagentCfg.Model
	}

	fmt.Println("===========================================")
	fmt.Println("  tagent WeChat Bot")
	fmt.Println("===========================================")
	fmt.Printf("  Agent Name:  %s\n", tagentCfg.Entry)
	fmt.Printf("  Model:       %s\n", effectiveModel)
	fmt.Printf("  Provider:    %s\n", tagentCfg.Provider)
	fmt.Printf("  Max Tokens:  %d\n", entryCfg.MaxTokens)
	fmt.Printf("  Log Level:   %s\n", tagentCfg.LogLevel)
	fmt.Printf("  Config:      %s\n", configPath)
	fmt.Println("===========================================")

	// 2. Create LLM model for the entry agent
	// Resolve the provider's connection info from config (providers map).
	providerName := tagentCfg.Provider
	apiEndpoint := tagentCfg.APIEndpoint
	if pcfg, ok := tagentCfg.Providers[providerName]; ok {
		if pcfg.APIEndpoint != "" {
			apiEndpoint = pcfg.APIEndpoint
		}
	}
	// TAGENT_API_ENDPOINT overrides config (e.g. AReaL proxy for RL training)
	if envEndpoint := os.Getenv("TAGENT_API_ENDPOINT"); envEndpoint != "" {
		apiEndpoint = envEndpoint
	}
	llmModel := openai.New(
		effectiveModel,
		openai.WithAPIKey(apiKey),
		openai.WithBaseURL(apiEndpoint),
	)

	// Wrap in SwappableModel for runtime LLM endpoint updates.
	// When AReaL adapter sends POST /task with llm_base_url, the HTTPAPI
	// callback swaps the inner model to use AReaL's proxy URL (dynamically
	// allocated port). This ensures all LLM requests are captured by AReaL's
	// proxy for RL training (logprobs + completion_ids).
	swappableModel := agent.NewSwappableModel(llmModel)

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

	// 4. Configure tagent options.
	// - WithModel: global fallback for agents without their own model declaration.
	// - WithModelOverrides: entry agent uses SwappableModel (for AReaL proxy support).
	//   Other agents with model/provider fields are resolved internally by tagent.New()
	//   via the provider.Model() factory (supports multi-vendor: openai/anthropic/gemini/etc).
	opts := []tagent.Option{
		tagent.WithModel(swappableModel),
		tagent.WithSummaryModel(swappableModel),
		tagent.WithModelOverrides(map[string]model.Model{
			tagentCfg.Entry: swappableModel,
		}),
		tagent.WithSkillRepo(skillRepo),
	}

	ta, err := tagent.New(*tagentCfg, opts...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Create tagent agent failed: %v\n", err)
		os.Exit(1)
	}
	defer ta.Close()

	// 5. Start persistent event loop (the only execution mode for top-level tagent)
	//    User/Session ID 可通过环境变量覆盖（AReaL adapter 通过 HTTPAPI /task 提交任务时使用）
	loopUser := os.Getenv("TAGENT_USER_ID")
	if loopUser == "" {
		loopUser = "wechat-user"
	}
	loopSession := os.Getenv("TAGENT_SESSION_ID")
	if loopSession == "" {
		loopSession = "wechat-session"
	}
	outputCh, err := ta.StartLoop(loopUser, loopSession)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Start loop failed: %v\n", err)
		os.Exit(1)
	}

	// 5b. Start HTTPAPI for local observability and RL task submission.
	//     Endpoints: GET /healthz, POST /task
	httpPort := os.Getenv("TAGENT_HTTP_PORT")
	if httpPort == "" {
		httpPort = "8089"
	}
	httpAPI := agent.NewHTTPAPI(ta)
	// Set model update callback: when AReaL adapter sends llm_base_url,
	// create a new openai model with that URL and swap it in.
	httpAPI.SetModelUpdateFn(func(baseURL string) {
		newModel := openai.New(
			effectiveModel,
			openai.WithAPIKey(apiKey),
			openai.WithBaseURL(baseURL),
		)
		swappableModel.Swap(newModel)
		// Update TrajectoryRecorder's endpoint to reflect the swap
		if tr := ta.TrajectoryRecorder(); tr != nil {
			tr.SetModelEndpoint(baseURL)
		}
		log.Infof("[HTTPAPI] LLM base URL updated to %s", baseURL)
	})
	go func() {
		fmt.Printf("  HTTPAPI:     http://localhost:%s\n", httpPort)
		if err := http.ListenAndServe(":"+httpPort, httpAPI); err != nil {
			log.Warnf("HTTPAPI stopped: %v", err)
		}
	}()

	// 6. Setup signal handling
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// 7. OTLP telemetry — distributed tracing export (optional).
	//    Set OTEL_EXPORTER_OTLP_ENDPOINT to enable (e.g., "localhost:4317" for Jaeger/Tempo).
	//    Without this, the tracer is noop (zero overhead).
	if otlpEndpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"); otlpEndpoint != "" {
		otelCleanup, err := telemetrytrace.Start(ctx,
			telemetrytrace.WithEndpoint(otlpEndpoint),
			telemetrytrace.WithServiceName("tagent-wechat-bot"),
		)
		if err != nil {
			log.Warnf("Failed to start OTLP tracing (non-fatal): %v", err)
		} else {
			defer otelCleanup()
			fmt.Printf("  OTLP:        %s\n", otlpEndpoint)
		}
	}
	fmt.Println("===========================================")

	// 8. Create WeChat bot
	slogLogger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	bot := wechat.NewBot(wechat.WithLogger(slogLogger))

	// 9. Login
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

	// 10. Register message handler
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

		// Run tagent agent via persistent event loop (sequential processing).
		// tagent's built-in loop logging provides full observability:
		//   [Loop.Batch#N] TOOL_CALL / TOOL_RESULT / FINAL_RESPONSE
		//   [Loop.Batch#N] completed duration=... events=... tokens=...
		// OTLP spans are exported when OTEL_EXPORTER_OTLP_ENDPOINT is set.
		msgMu.Lock()
		response, err := generateResponse(reqCtx, ta, outputCh, text)
		msgMu.Unlock()
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

	// 11. Run
	fmt.Println("Bot is running. Press Ctrl+C to stop.")
	if err := bot.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Bot stopped with error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Bot stopped gracefully.")
}

// generateResponse injects the user message into tagent's persistent event loop
// and collects the final output from the output channel.
//
// Logging and OTLP tracing are handled by tagent's built-in loop() —
// this function only reads events to extract the final text response.
func generateResponse(ctx context.Context, ta *agent.TagentAgent, outputCh <-chan *event.Event, userMessage string) (string, error) {
	// Inject message into the persistent event loop
	ta.InjectMessage(model.Message{
		Role:    model.RoleUser,
		Content: userMessage,
	})

	// Consume events until final response (tagent logs all details internally)
	var finalOutput string
loop:
	for {
		select {
		case evt, ok := <-outputCh:
			if !ok {
				break loop // channel closed
			}

			// Extract final text response (no tool calls = final answer)
			if evt.Response != nil && len(evt.Response.Choices) > 0 {
				choice := evt.Response.Choices[len(evt.Response.Choices)-1]
				if choice.Message.Content != "" && len(choice.Message.ToolCalls) == 0 {
					finalOutput = choice.Message.Content
				}
			}

			// Final response for this batch — stop consuming
			if evt.IsFinalResponse() {
				break loop
			}

		case <-ctx.Done():
			break loop
		}
	}

	if finalOutput == "" {
		finalOutput = "No response generated"
	}

	return finalOutput, nil
}

// ---------------------------------------------------------------------------
// WeChat config (minimal — extracted from tagent.yaml's app.wechat section)
// ---------------------------------------------------------------------------

// WechatAppConfig holds WeChat-specific configuration.
type WechatAppConfig struct {
	ConfigDir       string `json:"config_dir"`
	TokenFile       string `json:"token_file"`
	ContextTokenDir string `json:"context_token_dir"`
}

// loadWechatConfig extracts WeChat config from tagent.yaml's app.wechat section.
func loadWechatConfig(app map[string]any) WechatAppConfig {
	cfg := WechatAppConfig{
		ConfigDir:       ".wechat-config",
		TokenFile:       "token.json",
		ContextTokenDir: ".wechat-context-tokens",
	}
	if app == nil {
		return cfg
	}
	if raw, ok := app["wechat"].(map[string]any); ok {
		if v, ok := raw["config_dir"].(string); ok {
			cfg.ConfigDir = v
		}
		if v, ok := raw["token_file"].(string); ok {
			cfg.TokenFile = v
		}
		if v, ok := raw["context_token_dir"].(string); ok {
			cfg.ContextTokenDir = v
		}
	}
	return cfg
}

// EnsureDirs creates necessary WeChat directories.
func (c *WechatAppConfig) EnsureDirs() error {
	for _, dir := range []string{c.ConfigDir, c.ContextTokenDir} {
		if dir == "" {
			continue
		}
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create dir %s: %w", dir, err)
		}
	}
	return nil
}
