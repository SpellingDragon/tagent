package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/SpellingDragon/tagent"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/rl"
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
	swappableModel := rl.NewSwappableModel(entryModel)

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
	httpAPI := rl.NewHTTPAPI(ta)
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
	//     Consumer uses metadata (chat_id, user_name) from StateDelta to route
	//     responses to the correct user. This eliminates the need for replyTarget
	//     and lastUser tracking, and fixes the responseCh deadlock bug.
	typingActive := sync.Map{} // chat_id -> time.Time
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

			// Parse the event metadata contract (storage identifiers, trigger
			// source, passthrough routing metadata) via the framework API —
			// consumers never read raw StateDelta keys. (unified-event-projection D4)
			meta := tagentevent.ParseEventMeta(evt)
			eventType := meta.EventType
			// Trigger source values: "user", "task" (delivered to originating
			// session), "meditation" (internal, not delivered).
			triggerSource := meta.TriggerSource
			if triggerSource == "" {
				triggerSource = "user"
			}
			chatID := meta.Meta["chat_id"]
			userName := meta.Meta["user_name"]

			// Check for final response (agent_output — no tool calls)
			if evt.IsFinalResponse() && evt.Response != nil && len(evt.Response.Choices) > 0 {
				choice := evt.Response.Choices[len(evt.Response.Choices)-1]
				content := choice.Message.Content
				// Surface the real execution error instead of the framework's
				// generic fallback ("An error occurred during execution. Please
				// contact the service provider."), which Runner substitutes when
				// an event carries Response.Error but empty content. The
				// structured Response.Error is left intact and holds the reason.
				if evt.Response.Error != nil && evt.Response.Error.Message != "" {
					content = fmt.Sprintf("执行出错：%s", evt.Response.Error.Message)
				}
				if content == "" {
					// Degenerate empty final: nothing to deliver. Drop instead of
					// fabricating "(empty response)". The framework already
					// suppresses the empty agent_output echo; this is the
					// consumer-side half. (async-result-delivery.)
					log.Debugf("[Agent] 丢弃空 final 响应 (trigger=%s)", triggerSource)
					continue
				}

				// Single decision point: route based on trigger_source and chat_id
				switch triggerSource {
				case "meditation":
					// Meditation: internal output, don't send to user.
					log.Infof("[Agent][meditation] 冥想输出: %s", truncateLog(content))
				case "error":
					// Error: log only, don't send to user.
					log.Infof("[Agent][error] 错误输出: %s", truncateLog(content))
				case "user", "task":
					// User input, or a background task result reclaimed into a
					// turn: both deliver to the originating session (meta_chat_id).
					// A settled task fulfilling the user's async request is a
					// first-class user-visible reply. (async-result-delivery.)
					if chatID == "" {
						log.Warnf("[Agent][%s] 无 meta_chat_id，无法发送: %s", triggerSource, truncateLog(content))
						continue
					}

					// Stop typing indicator for this user
					if startTime, ok := typingActive.Load(chatID); ok {
						if t, ok := startTime.(time.Time); ok && time.Since(t) < 60*time.Second {
							_ = bot.StopTyping(ctx, chatID)
						}
						typingActive.Delete(chatID)
					}

					// Log the response
					userLabel := chatID
					if userName != "" {
						userLabel = fmt.Sprintf("%s(%s)", userName, chatID)
					}
					log.Infof("[Agent][%s->%s] %s", triggerSource, userLabel, truncateLog(content))

					// 原始文本（用于文件解析，避免被长文本截断逻辑破坏路径）
					originalContent := content

					// Send reply — use SendTextToUser to get the latest context token
					// (interim messages may have refreshed the token). Long text
					// (>2000) is split / converted to file via SendLongText.
					textSent := false
					if len(content) > 2000 {
						token, _ := bot.GetContextToken(chatID)
						if token != "" {
							if _, err := wechat.SendLongText(ctx, bot.Client(), bot.Media(), chatID, content, token); err == nil {
								textSent = true // 长文本已发送，跳过 SendTextToUser
							} else {
								// Fall through to SendTextToUser with truncated text
								content = content[:2000] + "\n\n[Message truncated]"
							}
						} else {
							content = content[:2000] + "\n\n[Message truncated]"
						}
					}
					if !textSent {
						if err := bot.SendTextToUser(ctx, chatID, content); err != nil {
							log.Errorf("SendTextToUser failed for %s: %v", chatID, err)
						}
					}

					// Deliver any local files referenced in the original reply.
					// Files are sent via WeChat using the persisted context_token,
					// attaching to the correct conversation thread. Text sending is
					// handled above; DeliverFiles only sends files. Per-file failures
					// are logged and isolated inside DeliverFiles (non-fatal), so it
					// always returns nil and needs no error check here.
					DeliverFiles(bot, ctx, chatID, originalContent, wechatCfg.WorkspaceDir)
				default:
					// Unknown trigger source: log only
					log.Warnf("[Agent][%s] 未知触发源，输出: %s", triggerSource, truncateLog(content))
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
						}
						for _, tc := range msg.ToolCalls {
							log.Infof("[Agent][%s] 调用工具: %s(%s)", evtLabel, tc.Function.Name, string(tc.Function.Arguments))
						}
					} else if msg.Content != "" {
						log.Infof("[Agent][%s] 回复: %s", evtLabel, msg.Content)
					}
				case "tool":
					// action_command: tool execution result
					if msg.Content != "" {
						log.Infof("[Agent][%s] 工具结果: %s", evtLabel, msg.Content)
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

	bot.OnMessage(func(ctx context.Context, msg *wechat.Message) error {
		kind := ClassifyInbound(msg, wechatCfg.WorkspaceDir != "")
		if kind == InboundIgnore {
			return nil
		}

		// Run handler in goroutine to avoid blocking Poller's serial loop.
		// This allows subsequent user messages to be injected into persistentBus
		// while the agent is still processing the current message.
		go func() {
			// Show typing indicator and track it by chat_id
			_ = bot.SendTyping(ctx, msg.FromUserID)
			typingActive.Store(msg.FromUserID, time.Now())

			injectText := msg.Text()
			now := time.Now()

			switch kind {
			case InboundMedia:
				if wechatCfg.WorkspaceDir == "" {
					// 未配置 workspace：附件降级为不支持，伴随文本仍按原样注入。
					_ = bot.SendTextToUser(ctx, msg.FromUserID, "暂不支持附件接收（未配置 workspace）")
				} else {
					outcome := IntakeMedia(ctx, bot, bot.CDNBaseURL(), wechatCfg.WorkspaceDir, msg.FromUserID, msg, now)
					for _, reason := range outcome.Rejects {
						_ = bot.SendTextToUser(ctx, msg.FromUserID, reason)
					}
					injectText = ComposeMediaInject(outcome, msg.Text(), wechatCfg.WorkspaceDir, msg.FromUserID, now)
				}
			case InboundLongText:
				if _, inject, err := SaveLongText(wechatCfg.WorkspaceDir, msg.FromUserID, injectText, "input", now); err == nil {
					injectText = inject
				} else {
					// 落盘失败降级为原样注入，不丢消息。
					log.Errorf("[Intake] 长文本落盘失败 (chat=%s): %v", msg.FromUserID, err)
				}
			}

			if injectText == "" {
				// 全部被拒且无伴随文本：无事可注入，收回 typing。
				_ = bot.StopTyping(ctx, msg.FromUserID)
				typingActive.Delete(msg.FromUserID)
				return
			}

			// Inject message with metadata into the persistent event loop.
			// Metadata (chat_id, user_name) will be propagated through StateDelta
			// and used by the consumer to route responses to the correct user.
			ta.InjectMessageWithMetadata("user", model.Message{
				Role:    model.RoleUser,
				Content: injectText,
			}, map[string]string{
				"chat_id":   msg.FromUserID,
				"user_name": msg.FromUserID, // Use FromUserID as user_name (no FromUserName field available)
			})

			// Handler returns immediately — consumer will send response when ready.
			// No need to wait for responseCh or manage typing indicator here.
		}()

		return nil
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
	WorkspaceDir    string `json:"workspace_dir"`
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
		if v, ok := raw["workspace_dir"].(string); ok {
			cfg.WorkspaceDir = v
		}
	}
	// 容器内根文件系统只读，tmpfs 挂载点与本地目录名不同，用环境变量覆盖。
	if v := os.Getenv("TAGENT_WECHAT_WORKSPACE_DIR"); v != "" {
		cfg.WorkspaceDir = v
	}
	return cfg
}

// EnsureDirs creates necessary WeChat directories.
func (c *WechatAppConfig) EnsureDirs() error {
	dirs := []string{c.ConfigDir, c.ContextTokenDir}
	if c.WorkspaceDir != "" {
		dirs = append(dirs, filepath.Join(c.WorkspaceDir, uploadsSubdir))
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
