// 契约: docs/wiki/examples/wechat-bot-runtime.md#startup-and-routing
package main

import (
	"context"
	"errors"
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
	"github.com/SpellingDragon/tagent/agent/governance"
	tagentevent "github.com/SpellingDragon/tagent/event"
	mengine "github.com/SpellingDragon/tagent/memory/engine"
	"github.com/SpellingDragon/tagent/rl"
	"github.com/SpellingDragon/wechat-robot-go/wechat"
	openaiopt "github.com/openai/openai-go/option"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/skill"
	telemetrytrace "trpc.group/trpc-go/trpc-agent-go/telemetry/trace"
)

// meditatorAgentName is the meditator's key in the resident table, matching its
// agents declaration in the config: assembly puts an agent in that table only
// when the entry reaches it through the tool graph, so a missing key means
// this process declares no meditation-line.
const meditatorAgentName = "meditator"

// meditationLineSession is the reserved session name of the meditation line: 冥想线固定单 session,
// 上下文增长由 meditator 自身的 compress_threshold 在同一 session 内折叠,不参与宿主的会话
// 路由。业务线与入口自察共用 StartLoop 的那条 session(TAGENT_SESSION_ID,缺省 wechat-session)。
const meditationLineSession = "meditation"

// endpointPolicyFromEnv reads the dynamic-endpoint redirect policy from the
// environment: enable flag plus the
// exact-host allowlist (any port). Shared by the HTTPAPI endpoint policy and
// the LLM client's per-hop CheckRedirect guard so the two can never drift.
func endpointPolicyFromEnv() (enabled bool, allowlist []string) {
	enabled = os.Getenv("TAGENT_RL_ALLOW_LLM_REDIRECT") == "1"
	allowlist = []string{}
	if v := os.Getenv("TAGENT_RL_ENDPOINT_ALLOWLIST"); v != "" {
		for _, hst := range strings.Split(v, ",") {
			if hst = strings.TrimSpace(strings.ToLower(hst)); hst != "" {
				allowlist = append(allowlist, hst)
			}
		}
	}
	return enabled, allowlist
}

// resolveTriggerSource applies the delivery-gate policy to a raw
// trigger_source value read from an output event.
// Policy is FAIL-CLOSED from the SINGLE-SOURCE whitelist
// (event.DeliverableLineage — shared with the fold externalization check,
// D9): an output event with NO stamped lineage, or with any lineage outside
// the whitelist, is internal and must NOT reach the user chat. Every
// legitimate user-visible turn stamps its own source at the RunFlow
// forwarding block (agent/context_manager.go), so real user turns always
// carry "user" and are unaffected. This closes the fail-open hole where
// unstamped events (e.g. a task settled from a pre-lineage spawn) were
// coerced to "user" and delivered.
func resolveTriggerSource(raw string) (source string, deliverable bool) {
	if raw == "" {
		return "internal-unstamped", false
	}
	return raw, tagentevent.DeliverableLineage(raw)
}

// resolveDeliveryTarget is the single chat-target rule of the delivery
// switch: the stamped meta_chat_id — carried verbatim from the
// receiving turn through to the send — wins; a deliverable output without a
// stamp (settled task reclaimed into a turn) falls back to the most recent
// active user chat; nothing known is HELD, never broadcast, never guessed.
func resolveDeliveryTarget(metaChatID, lastActive string) (target string, ok bool) {
	if metaChatID != "" {
		return metaChatID, true
	}
	if lastActive != "" {
		return lastActive, true
	}
	return "", false
}

func main() {
	configPath := "tagent.yaml"
	if envPath := os.Getenv("TAGENT_CONFIG"); envPath != "" {
		configPath = envPath
	}

	tagentCfg, err := tagent.LoadConfig(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Load config failed: %v\n", err)
		os.Exit(1)
	}

	if len(os.Args) >= 3 && (os.Args[1] == "approve" || os.Args[1] == "reject") {
		approvalsDir := filepath.Join(tagentCfg.Governance.Dir, "approvals")
		if tagentCfg.Governance.Dir == "" {
			fmt.Fprintln(os.Stderr, "governance.dir 未配置——审批文件通道不可用")
			os.Exit(1)
		}
		msg, err := governance.RespondFile(approvalsDir, os.Args[2], os.Args[1] == "approve", "cli")
		if err != nil {
			fmt.Fprintf(os.Stderr, "审批失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(msg)
		return
	}

	wechatCfg := loadWechatConfig(tagentCfg.App)
	if err := wechatCfg.EnsureDirs(); err != nil {
		fmt.Fprintf(os.Stderr, "Create dirs failed: %v\n", err)
		os.Exit(1)
	}

	seenStore := NewSeenStore(wechatCfg.ConfigDir, slog.Default())

	log.SetLevel(tagentCfg.LogLevel)

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

	redirectEnabled, endpointAllowlist := endpointPolicyFromEnv()
	if !redirectEnabled {
		endpointAllowlist = nil
	}
	guardedClient := rl.NewEndpointGuardedClient(endpointAllowlist)
	modelHTTP := openai.WithOpenAIOptions(openaiopt.WithHTTPClient(guardedClient))

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
		modelHTTP,
	)

	entryEndpoint, entryKeyEnv, err := tagentCfg.ResolveAgentProvider(tagentCfg.Entry)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Resolve entry agent provider failed: %v\n", err)
		os.Exit(1)
	}
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
		modelHTTP,
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
	// swap the LLM endpoint at runtime.
	// Other agents with model/provider fields are resolved internally by tagent.New()
	// via the provider.Model() factory (supports multi-vendor: openai/anthropic/gemini/etc).
	var approvalCh *wechatApprovalChannel
	opts := []tagent.Option{
		tagent.WithModel(globalModel),
		tagent.WithModelOverrides(map[string]model.Model{
			tagentCfg.Entry: swappableModel,
		}),
		tagent.WithSkillRepo(skillRepo),
		tagent.WithConfigPath(configPath),
	}
	if n := len(wechatCfg.Approvers); n > 0 {
		approvalCh = &wechatApprovalChannel{to: wechatCfg.Approvers[0]}
		opts = append(opts, tagent.WithApprovalChannel(approvalCh))
	}

	ta, err := tagent.New(*tagentCfg, opts...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Create tagent agent failed: %v\n", err)
		os.Exit(1)
	}
	defer ta.Close()

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

	// 旁路冥想线（meditator）是常驻表里的第二个反思主体,自己的循环跑在保留 session 上
	// (meditationLineSession)。它的输出只落日志:反思文本是 agent 的内部叙述,转给微信使用者等于
	// 泄漏推理过程;要回流业务 agent 的结论走组合根投递面,由 deliver_to 白名单授权。常驻表
	// 没有 meditator 或未起循环都只降级为日志,业务线不受影响;meditationLineStop 在入口 Close
	// （级联关闭其余常驻 owner）之前调用,冥想回合的在途 drain 不被入口收尾抢跑。
	var meditationLineStop = func() {}
	if meditator := ta.ResidentTable()[meditatorAgentName]; meditator != nil {
		meditatorOut, meditatorErr := meditator.StartLoop(loopUser, meditationLineSession)
		if meditatorErr != nil {
			log.Warnf("[meditation-line] 冥想线循环未起 session=%s: %v", meditationLineSession, meditatorErr)
		} else {
			meditationLineStop = meditator.StopLoop
			go func() {
				for evt := range meditatorOut {
					if evt == nil || !evt.IsFinalResponse() || evt.Response == nil || len(evt.Response.Choices) == 0 {
						continue
					}
					content := evt.Response.Choices[len(evt.Response.Choices)-1].Message.Content
					if content == "" {
						continue
					}
					log.Infof("[meditation-line] 冥想卡片(仅日志,不回灌业务线): %s", truncateLogN(content, 400))
				}
				log.Info("[meditation-line] 冥想线输出流已关闭")
			}()
			log.Infof("[meditation-line] 冥想线循环已起 user=%s session=%s", loopUser, meditationLineSession)
		}
	} else {
		log.Infof("[meditation-line] 常驻表无 %q,本进程不起冥想线", meditatorAgentName)
	}

	go maybeInjectReincarnationNotice(ta, tagentCfg.Entry, filepath.Join("run"), noticeWaitMax)
	go maybeConsumeSystemAlert(ta, filepath.Join("run"), 5*time.Second)

	httpPort := os.Getenv("TAGENT_HTTP_PORT")
	if httpPort == "" {
		httpPort = "8089"
	}
	httpAPI := rl.NewHTTPAPI(ta)
	httpAPI.SetFeedbackStore(ta.MemStore())
	diagStore := ta.MemStore()
	httpAPI.SetDiagnosticsFn(func() any {
		return mengine.NewMemoryDiagnostics(nil, diagStore).Snapshot()
	})
	httpAPI.SetModelUpdateFn(func(baseURL string) {
		newModel := openai.New(
			effectiveModel,
			openai.WithAPIKey(entryAPIKey),
			openai.WithBaseURL(baseURL),
			modelHTTP,
		)
		swappableModel.Swap(newModel)
		if tr := ta.TrajectoryRecorder(); tr != nil {
			tr.SetModelEndpoint(baseURL)
		}
		log.Infof("[HTTPAPI] LLM base URL updated to %s", baseURL)
	})
	if redirectEnabled {
		httpAPI.SetEndpointPolicy(true, endpointAllowlist)
		log.Infof("[HTTPAPI] dynamic llm_base_url redirect ENABLED (allowlist=%v, per-hop redirect guard ON)", endpointAllowlist)
	} else {
		httpAPI.SetEndpointPolicy(false, nil)
	}

	listenAddr := ":" + httpPort
	srv := rl.NewHTTPServer(listenAddr, httpAPI)
	stopHTTP := make(chan struct{})
	httpDone := make(chan struct{})
	go func() {
		defer close(httpDone)
		rlToken := rl.AuthTokenFromEnv()
		httpAPI.SetAuthToken(rlToken)
		if err := rl.ValidateListenAddr(listenAddr, rlToken); err != nil {
			listenAddr = "127.0.0.1:" + httpPort
			srv.Addr = listenAddr
			log.Warnf("%v — falling back to %s (LAN access requires the token)", err, listenAddr)
		}
		fmt.Printf("  HTTPAPI:     http://%s\n", listenAddr)
		for attempt := 1; ; attempt++ {
			listenErr := srv.ListenAndServe()
			if listenErr == nil || errors.Is(listenErr, http.ErrServerClosed) {
				return
			}
			wait := time.Duration(attempt) * 5 * time.Second
			if wait > 60*time.Second {
				wait = 60 * time.Second
			}
			log.Errorf("HTTPAPI attempt %d failed: %v — retrying in %v", attempt, listenErr, wait)
			select {
			case <-stopHTTP:
				return
			case <-time.After(wait):
			}
		}
	}()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	usr2 := make(chan os.Signal, 1)
	signal.Notify(usr2, syscall.SIGUSR2)
	go func() {
		for range usr2 {
			log.Infof("[SIGUSR2] rollback requested — rebuilding executor from ring-2 previous generation")
			ta.Rollback()
		}
	}()

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

	slogLogger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	cursorStore := wechat.NewFileCursorStore(filepath.Join(wechatCfg.ConfigDir, "cursor.txt"))
	bot := wechat.NewBot(
		wechat.WithLogger(slogLogger),
		wechat.WithCursorStore(cursorStore),
	)
	if approvalCh != nil {
		approvalCh.SetBot(bot)
	}

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

	typingActive := sync.Map{}
	lastActiveChat := sync.Map{}
	seedLastActiveChat(&lastActiveChat, runDir())
	go func() {
		for evt := range outputCh {
			if evt == nil {
				continue
			}

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

			meta := tagentevent.ParseEventMeta(evt)
			eventType := meta.EventType
			triggerSource, deliverable := resolveTriggerSource(meta.TriggerSource)
			chatID := meta.Meta["chat_id"]
			if triggerSource == "user" && chatID != "" {
				lastActiveChat.Store("latest", chatID)
				persistLastActiveChat(chatID, runDir())
			}
			userName := meta.Meta["user_name"]

			if evt.IsFinalResponse() && evt.Response != nil && len(evt.Response.Choices) > 0 {
				choice := evt.Response.Choices[len(evt.Response.Choices)-1]
				content := choice.Message.Content
				if evt.Response.Error != nil && evt.Response.Error.Message != "" {
					content = fmt.Sprintf("执行出错：%s", evt.Response.Error.Message)
				}
				if content == "" {
					log.Debugf("[Agent] 丢弃空 final 响应 (trigger=%s)", triggerSource)
					continue
				}

				if !deliverable {
					log.Infof("[Agent][gate] 未盖章输出，内部消化 (source=%s): %s", triggerSource, truncateLog(content))
					reportWithheld(ta, triggerSource, false, "", content)
					continue
				}

				switch triggerSource {
				case "meditation":
					log.Infof("[Agent][meditation] 冥想输出: %s", truncateLog(content))
					reportWithheld(ta, triggerSource, true, "", content)
				case "error":
					log.Infof("[Agent][error] 错误输出: %s", truncateLog(content))
					reportWithheld(ta, triggerSource, true, "", content)
				case "user", "task", "reincarnation", "system_alert":
					if chatID == "" {
						raw, _ := lastActiveChat.Load("latest")
						lastActive, _ := raw.(string)
						target, hasTarget := resolveDeliveryTarget(chatID, lastActive)
						if !hasTarget {
							log.Warnf("[Agent][%s] 无 meta_chat_id，无可回退会话，扣留: %s", triggerSource, truncateLog(content))
							emitReceipt(ta, "no-target", "WARN", triggerSource, "", content)
							continue
						}
						log.Infof("[Agent][%s] 无 meta_chat_id，回退最近活跃会话 %s", triggerSource, target)
						chatID = target
					}
					if startTime, ok := typingActive.Load(chatID); ok {
						if t, ok := startTime.(time.Time); ok && time.Since(t) < 60*time.Second {
							_ = bot.StopTyping(ctx, chatID)
						}
						typingActive.Delete(chatID)
					}

					userLabel := chatID
					if userName != "" {
						userLabel = fmt.Sprintf("%s(%s)", userName, chatID)
					}
					log.Infof("[Agent][%s->%s] %s", triggerSource, userLabel, truncateLog(content))

					originalContent := content

					textSent := false
					if len(content) > 2000 {
						token, _ := bot.GetContextToken(chatID)
						if token != "" {
							if _, err := wechat.SendLongText(ctx, bot.Client(), bot.Media(), chatID, content, token); err == nil {
								textSent = true
							} else {
								content = content[:2000] + "\n\n[Message truncated]"
							}
						} else {
							content = content[:2000] + "\n\n[Message truncated]"
						}
					}
					if !textSent {
						if err := bot.SendTextToUser(ctx, chatID, content); err != nil {
							log.Errorf("SendTextToUser failed for %s: %v", chatID, err)
							emitReceipt(ta, "send-failed", "ERROR", triggerSource, chatID, content)
						}
					}

					DeliverFiles(bot, ctx, chatID, originalContent, wechatCfg.WorkspaceDir)
				default:
					log.Warnf("[Agent][%s] 未知触发源，输出: %s", triggerSource, truncateLog(content))
				}
				continue
			}

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

	bot.OnMessage(func(ctx context.Context, msg *wechat.Message) error {
		if seenStore != nil && !seenStore.CheckAndMark(DedupKey(msg)) {
			log.Warnf("[Dedup] duplicate message dropped (chat=%s)", msg.FromUserID)
			return nil
		}
		if digest, approve, ok := governance.ParseApprovalReply(msg.Text()); ok && tagentCfg.Governance.Dir != "" {
			if !wechatCfg.IsApprover(msg.FromUserID) {
				_ = bot.SendTextToUser(ctx, msg.FromUserID,
					"你没有审批权限（不在 app.wechat.approvers 白名单）。请由审批人经 CLI（wechat-bot approve <digest>）执行。")
				return nil
			}
			approvalsDir := filepath.Join(tagentCfg.Governance.Dir, "approvals")
			reply, err := governance.RespondFile(approvalsDir, digest, approve, "wechat:"+msg.FromUserID)
			if err != nil {
				reply = "审批失败: " + err.Error()
			}
			_ = bot.SendTextToUser(ctx, msg.FromUserID, reply)
			return nil
		}

		kind := ClassifyInbound(msg, wechatCfg.WorkspaceDir != "")
		if kind == InboundIgnore {
			return nil
		}

		go func() {
			_ = bot.SendTyping(ctx, msg.FromUserID)
			typingActive.Store(msg.FromUserID, time.Now())

			injectText := msg.Text()
			now := time.Now()

			switch kind {
			case InboundMedia:
				if wechatCfg.WorkspaceDir == "" {
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
					log.Errorf("[Intake] 长文本落盘失败 (chat=%s): %v", msg.FromUserID, err)
				}
			}

			if injectText == "" {
				_ = bot.StopTyping(ctx, msg.FromUserID)
				typingActive.Delete(msg.FromUserID)
				return
			}

			ta.InjectMessageWithMetadata("user", model.Message{
				Role:    model.RoleUser,
				Content: injectText,
			}, map[string]string{
				"chat_id":   msg.FromUserID,
				"user_name": msg.FromUserID,
			})

		}()

		return nil
	})

	fmt.Println("Bot is running. Press Ctrl+C to stop.")
	if err := bot.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Bot stopped with error: %v\n", err)
		meditationLineStop()
		close(stopHTTP)
		_ = srv.Shutdown(context.Background())
		os.Exit(1)
	}
	meditationLineStop()
	close(stopHTTP)
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Warnf("HTTPAPI shutdown: %v", err)
	} else {
		<-httpDone
	}
	fmt.Println("Bot stopped gracefully.")
}

// runDir resolves the runtime state directory, anchored to the executable so
// cwd drift across launchers cannot misplace the persistence file.
func runDir() string {
	if exe, err := os.Executable(); err == nil {
		return filepath.Join(filepath.Dir(exe), "run")
	}
	return "run"
}

// persistLastActiveChat atomically writes the latest active chat_id to
// run/last_active_chat (tmp+rename). Errors are logged and non-fatal: losing
// the anchor degrades to the old in-memory-only behavior, never blocks the
// event loop. (fix-lastactive-chat-reincarnation-drop.)
func persistLastActiveChat(chatID, dir string) {
	if chatID == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Warnf("[Agent] persistLastActiveChat: mkdir %s: %v", dir, err)
		return
	}
	final := filepath.Join(dir, "last_active_chat")
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, []byte(chatID), 0o644); err != nil {
		log.Warnf("[Agent] persistLastActiveChat: write tmp: %v", err)
		return
	}
	if err := os.Rename(tmp, final); err != nil {
		log.Warnf("[Agent] persistLastActiveChat: rename: %v", err)
	}
}

// seedLastActiveChat restores the last-active-chat anchor from disk at boot.
// After a hot-swap/restart the in-memory anchor starts empty, so the first
// post-reincarnation output
// would be silently dropped. Missing file is silent
// (cold start); unreadable/corrupt content warns and continues.
// (fix-lastactive-chat-reincarnation-drop.)
func seedLastActiveChat(m *sync.Map, dir string) {
	b, err := os.ReadFile(filepath.Join(dir, "last_active_chat"))
	if err != nil {
		if !os.IsNotExist(err) {
			log.Warnf("[Agent] seedLastActiveChat: %v", err)
		}
		return
	}
	chatID := strings.TrimSpace(string(b))
	if chatID == "" {
		log.Warnf("[Agent] seedLastActiveChat: empty content, skip")
		return
	}
	m.Store("latest", chatID)
	log.Infof("[Agent] seedLastActiveChat: 回种最近活跃会话 %s", chatID)
}
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
	}
}

// replyInterim sends an interim message to the user if a reply target is set.
// This is called from the continuous consumer goroutine for thinking_plan and
// action_command events, allowing the user to see the agent's progress in real-time.
func replyInterim(target *atomic.Pointer[string], bot *wechat.Bot, content string) {
	userID := target.Load()
	if userID == nil {
		return
	}
	_ = bot.SendTextToUser(context.Background(), *userID, content)
}

// WechatAppConfig holds WeChat-specific configuration.
type WechatAppConfig struct {
	ConfigDir       string `json:"config_dir"`
	TokenFile       string `json:"token_file"`
	ContextTokenDir string `json:"context_token_dir"`
	WorkspaceDir    string `json:"workspace_dir"`
	// Approvers：可经消息通道批准 critical 操作的用户 ID 白名单。
	// 空 = 消息通道批准关闭（安全默认——digest 随请求明文送达，任意可达者可批准），
	// 批准仅经 CLI（wechat-bot approve <digest>）。
	Approvers []string `json:"approvers,omitempty"`
}

// wechatApprovalChannel：审批请求直投微信（不经 agent
// 转述）。晚绑定 bot（构造时序：ta 先于 bot），SetBot 后可用。
type wechatApprovalChannel struct {
	mu  sync.Mutex
	bot *wechat.Bot
	to  string
}

func (c *wechatApprovalChannel) SetBot(b *wechat.Bot) {
	c.mu.Lock()
	c.bot = b
	c.mu.Unlock()
}

func (c *wechatApprovalChannel) Deliver(req *governance.ApprovalRequest) error {
	c.mu.Lock()
	b := c.bot
	c.mu.Unlock()
	if b == nil {
		return fmt.Errorf("wechat bot not ready")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	d := req.ArgsDigest
	if len(d) > 8 {
		d = d[:8]
	}
	text := fmt.Sprintf("⚠ 审批请求（tool=%s）\ndigest: %s\n批准回复: approve %s ｜ 拒绝: reject %s",
		req.ToolName, d, d, d)
	return b.SendTextToUser(ctx, c.to, text)
}

// IsApprover reports whether the user id may approve via the message
// channel (8.2: empty whitelist denies everyone — approval goes via CLI).
func (c WechatAppConfig) IsApprover(userID string) bool {
	for _, id := range c.Approvers {
		if id == userID {
			return true
		}
	}
	return false
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
		if rawList, ok := raw["approvers"].([]any); ok {
			cfg.Approvers = nil
			for _, item := range rawList {
				if id, ok := item.(string); ok && id != "" {
					cfg.Approvers = append(cfg.Approvers, id)
				}
			}
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
