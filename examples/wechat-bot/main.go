package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/wechat-robot-go/wechat"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/skill"
	telemetrytrace "trpc.group/trpc-go/trpc-agent-go/telemetry/trace"
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

	// Set framework log level
	log.SetLevel(tagentCfg.LogLevel)

	// Resolve entry agent's effective model name
	entryCfg := tagentCfg.Agents[tagentCfg.Entry]
	effectiveModel := entryCfg.Model
	if effectiveModel == "" {
		effectiveModel = tagentCfg.Model
	}

	entryProviderName := entryCfg.Provider
	if entryProviderName == "" {
		entryProviderName = tagentCfg.Provider
	}

	fmt.Println("===========================================")
	fmt.Println("  tagent WeChat Bot")
	fmt.Println("===========================================")
	fmt.Printf("  Agent Name:  %s\n", tagentCfg.Entry)
	fmt.Printf("  Model:       %s\n", effectiveModel)
	fmt.Printf("  Provider:    %s\n", entryProviderName)
	fmt.Printf("  Max Tokens:  %d\n", entryCfg.MaxTokens)
	fmt.Printf("  Log Level:   %s\n", tagentCfg.LogLevel)
	fmt.Printf("  Config:      %s\n", configPath)
	fmt.Println("===========================================")

	// 2. Create LLM models
	// tagent resolves provider endpoints/API keys from Config. The application only
	// wires them into model instances and the SwappableModel used by AReaL/HTTPAPI.

	// 2a. Global fallback model (for sub-agents without explicit model/provider).
	globalEndpoint, globalKeyEnv, err := tagentCfg.ResolveAgentProvider("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Resolve global provider failed: %v\n", err)
		os.Exit(1)
	}
	globalAPIKey := os.Getenv(globalKeyEnv)
	if globalAPIKey == "" {
		fmt.Fprintf(os.Stderr, "API key not set. Set %s environment variable.\n", globalKeyEnv)
		os.Exit(1)
	}
	globalModel := openai.New(
		tagentCfg.Model,
		openai.WithAPIKey(globalAPIKey),
		openai.WithBaseURL(globalEndpoint),
	)

	// 2b. Entry agent model (SwappableModel for AReaL proxy support).
	entryEndpoint, entryKeyEnv, err := tagentCfg.ResolveAgentProvider(tagentCfg.Entry)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Resolve entry agent provider failed: %v\n", err)
		os.Exit(1)
	}
	// TAGENT_API_ENDPOINT overrides config (e.g. AReaL proxy for RL training)
	if envEndpoint := os.Getenv("TAGENT_API_ENDPOINT"); envEndpoint != "" {
		entryEndpoint = envEndpoint
	}
	entryAPIKey := os.Getenv(entryKeyEnv)
	if entryAPIKey == "" {
		fmt.Fprintf(os.Stderr, "API key not set. Set %s environment variable.\n", entryKeyEnv)
		os.Exit(1)
	}
	entryModel := openai.New(
		effectiveModel,
		openai.WithAPIKey(entryAPIKey),
		openai.WithBaseURL(entryEndpoint),
	)
	swappableModel := agent.NewSwappableModel(entryModel)

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
	// - WithSummaryModel: fallback when an agent does not declare compress.summary_model.
	// - WithModelOverrides: entry agent uses SwappableModel so AReaL/HTTPAPI can
	//   swap the LLM endpoint at runtime.
	// Other agents with model/provider fields are resolved internally by tagent.New()
	// via the provider.Model() factory (supports multi-vendor: openai/anthropic/gemini/etc).
	opts := []tagent.Option{
		tagent.WithModel(globalModel),
		tagent.WithSummaryModel(globalModel),
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
			openai.WithAPIKey(entryAPIKey),
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

	// 10. Start continuous event consumer goroutine.
	//     The consumer reads all events from outputCh continuously,
	//     dispatching by event type. This ensures outputCh never fills up
	//     (runEventLoop blocks on writes until consumer reads).
	//
	//     Event dispatch (mirrors prototype's OnEvents switch on EventType):
	//     - agent_output (final response): deliver to waiting user via responseCh
	//     - thinking_plan (assistant + tool_calls): reply interim to user
	//     - action_command (tool result): reply interim to user
	//     - other events: log for visibility
	//
	//     The consumer holds a reference to the current reply target (fromUserID),
	//     set by the message handler when a user message is being processed.
	//     When no user is waiting, interim messages go to log only.
	responseCh := make(chan string, 1) // buffered: at most one pending response
	typingActive := atomic.Bool{}
	// replyTarget tracks the current user to reply to. nil when no user is waiting.
	var replyTarget atomic.Pointer[string]
	// lastUser tracks the most recent user who sent a message.
	// Used to deliver async results (e.g., [action_tool_result] responses)
	// when replyTarget has been cleared after the initial response.
	var lastUser atomic.Pointer[string]
	go func() {
		for evt := range outputCh {
			if evt == nil {
				continue
			}

			// Debug: print full event content
			deltaStr := ""
			if evt.StateDelta != nil {
				for k, v := range evt.StateDelta {
					deltaStr += fmt.Sprintf("%s=%s ", k, string(v))
				}
			}
			respStr := "(nil)"
			if evt.Response != nil && len(evt.Response.Choices) > 0 {
				msg := evt.Response.Choices[len(evt.Response.Choices)-1].Message
				respStr = fmt.Sprintf("role=%s content_len=%d tool_calls=%d", msg.Role, len(msg.Content), len(msg.ToolCalls))
			}
			log.Debugf("[Event] ID=%s Author=%s Tag=%s RequiresCompletion=%v StateDelta[%s] Response{%s}",
				evt.ID, evt.Author, evt.Tag, evt.RequiresCompletion, deltaStr, respStr)

			// Extract event type from StateDelta (written by MemoryPlugin)
			eventType := ""
			if evt.StateDelta != nil {
				if typeBytes, ok := evt.StateDelta["event_type"]; ok && len(typeBytes) > 0 {
					eventType = string(typeBytes)
				}
			}

			// Check for final response (agent_output — no tool calls)
			if evt.IsFinalResponse() && evt.Response != nil && len(evt.Response.Choices) > 0 {
				choice := evt.Response.Choices[len(evt.Response.Choices)-1]
				content := choice.Message.Content
				if content == "" {
					content = "(empty response)"
				}

				// Determine the target user for this response.
				// Priority: replyTarget (user actively waiting) > lastUser (for async results)
				targetUser := replyTarget.Load()
				if targetUser == nil {
					targetUser = lastUser.Load()
				}

				// Check if this is a meditation/internal response
				isMeditation := false
				// Meditation responses originate from [meditation] messages.
				if strings.Contains(content, "[meditation]") {
					isMeditation = true
				}
				// Only treat as meditation if no user at all (not even lastUser)
				if targetUser == nil {
					isMeditation = true
				}

				if isMeditation {
					log.Infof("[Agent] 冥想/内部输出: %s", truncateLog(content))
				} else {
					// Try to deliver to a waiting user
					select {
					case responseCh <- content:
						// Delivered to user message handler
					default:
						// No one waiting — send directly via bot if we have a target
						if targetUser != nil {
							log.Infof("[Agent] 异步结果发送给用户 %s: %s", *targetUser, truncateLog(content))
							_ = bot.SendTextToUser(ctx, *targetUser, content)
						} else {
							log.Infof("[Agent] 冥想/内部输出: %s", truncateLog(content))
						}
					}
					// Clear reply target — user interaction complete
					replyTarget.Store(nil)
				}
				continue
			}

			// Non-final events: dispatch by message role (mirrors prototype switch)
			if evt.Response != nil && len(evt.Response.Choices) > 0 {
				choice := evt.Response.Choices[len(evt.Response.Choices)-1]
				msg := choice.Message
				evtLabel := eventType
				if evtLabel == "" {
					evtLabel = "unknown"
				}

				switch msg.Role {
				case "assistant":
					if len(msg.ToolCalls) > 0 {
						// thinking_plan: LLM decided to call tools
						if msg.Content != "" {
							log.Infof("[Agent][%s] 思考: %s", evtLabel, msg.Content)
							// replyInterim(&replyTarget, bot, fmt.Sprintf("💭 %s", msg.Content))
						}
						for _, tc := range msg.ToolCalls {
							log.Infof("[Agent][%s] 调用工具: %s(%s)", evtLabel, tc.Function.Name, string(tc.Function.Arguments))
							// replyInterim(&replyTarget, bot, fmt.Sprintf("🔧 %s(%s)", tc.Function.Name, string(tc.Function.Arguments)))
						}
					} else if msg.Content != "" {
						log.Infof("[Agent][%s] 回复: %s", evtLabel, msg.Content)
					}
				case "tool":
					// action_command: tool execution result
					if msg.Content != "" {
						log.Infof("[Agent][%s] 工具结果: %s", evtLabel, msg.Content)
						// replyInterim(&replyTarget, bot, fmt.Sprintf("📋 %s", msg.Content))
					}
				case "user":
					if msg.Content != "" {
						log.Infof("[Agent][%s] 用户消息: %s", evtLabel, msg.Content)
					}
				case "system":
					if msg.Content != "" {
						log.Infof("[Agent][%s] 系统消息: %s", evtLabel, msg.Content)
					}
				}
			}
		}
		log.Info("[Consumer] outputCh closed, consumer exiting")
	}()

	// 11. Register message handler
	//     WeChat Poller processes messages serially — if handler A blocks
	//     waiting for agent response, handler B won't execute until A returns.
	//     To allow concurrent message processing (so user B's InjectMessage
	//     reaches persistentBus while Agent is still processing A), we wrap
	//     the handler to run in a goroutine.
	requestTimeout := time.Duration(tagentCfg.RequestTimeoutSeconds) * time.Second

	bot.OnMessage(func(ctx context.Context, msg *wechat.Message) error {
		text := msg.Text()
		if text == "" {
			return nil
		}

		// Run handler in goroutine to avoid blocking Poller's serial loop.
		// This allows subsequent user messages to be injected into persistentBus
		// while the agent is still processing the current message.
		go func() {
			// Show typing indicator
			_ = bot.SendTyping(ctx, msg.FromUserID)
			typingActive.Store(true)

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
						if typingActive.Load() {
							_ = bot.SendTyping(reqCtx, msg.FromUserID)
						}
					}
				}
			}()

			// Set reply target so the consumer can send interim messages to this user
			replyTarget.Store(&msg.FromUserID)
			lastUser.Store(&msg.FromUserID)

			// Inject message into the persistent event loop.
			ta.InjectMessage(model.Message{
				Role:    model.RoleUser,
				Content: text,
			})

			// Wait for the continuous consumer to deliver the agent's final response
			var response string
			select {
			case response = <-responseCh:
			case <-reqCtx.Done():
				if reqCtx.Err() == context.DeadlineExceeded {
					response = fmt.Sprintf("Processing timed out (exceeded %v). Please try again.", requestTimeout)
				} else {
					response = "Sorry, I encountered an error. Please try again."
				}
			}
			close(done)
			typingActive.Store(false)

			// Stop typing
			_ = bot.StopTyping(ctx, msg.FromUserID)

			// Brief delay to avoid WeChat API rate limiting after interim messages
			time.Sleep(500 * time.Millisecond)

			// Send reply — use SendTextToUser to get the latest context token
			// (interim messages may have refreshed the token)
			if len(response) > 2000 {
				// For long text, try SendLongText first
				token, _ := bot.GetContextToken(msg.FromUserID)
				if token != "" {
					_, err := wechat.SendLongText(ctx, bot.Client(), bot.Media(), msg.FromUserID, response, token)
					if err == nil {
						return
					}
					// Fall through to SendTextToUser with truncated text
					response = response[:2000] + "\n\n[Message truncated]"
				}
			}
			if err := bot.SendTextToUser(ctx, msg.FromUserID, response); err != nil {
				log.Errorf("SendTextToUser failed: %v, falling back to Reply", err)
				_ = bot.Reply(ctx, msg, response)
			}
		}()

		return nil // Return immediately so Poller can process next message
	})

	// 12. Run
	fmt.Println("Bot is running. Press Ctrl+C to stop.")
	if err := bot.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Bot stopped with error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Bot stopped gracefully.")
}

// truncateLog truncates a string for log output (max 120 chars).
func truncateLog(s string) string {
	return truncateLogN(s, 120)
}

func truncateLogN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// sendInterim non-blocking sends an interim message to the user.
func sendInterim(ch chan string, msg string) {
	select {
	case ch <- msg:
	default:
		// Channel full, skip — don't block the consumer
	}
}

// replyInterim sends an interim message to the user if a reply target is set.
// This is called from the continuous consumer goroutine for thinking_plan and
// action_command events, allowing the user to see the agent's progress in real-time.
func replyInterim(target *atomic.Pointer[string], bot *wechat.Bot, content string) {
	userID := target.Load()
	if userID == nil {
		return // No user waiting
	}
	_ = bot.SendTextToUser(context.Background(), *userID, content)
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
