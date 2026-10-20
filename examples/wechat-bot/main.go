// 契约: docs/wiki/examples/wechat-bot-runtime.md#startup-and-routing
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/wechat-robot-go/wechat"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"

	"github.com/SpellingDragon/tagent/memory/ledger"
)

// wechatOutbound 是宿主发送通路对通道实例的窄依赖面：文本、媒体、typing 与 context-token。
// *wechat.Bot 天然满足该接口，测试以替身顶上（与 FileSender 同一纪律：不在测试里构造真实通道）。
type wechatOutbound interface {
	FileSender
	StopTyping(ctx context.Context, toUserID string) error
	GetContextToken(userID string) (string, error)
}

// 编译期断言：*wechat.Bot 满足发送面（无需运行真实微信即可验证）。
var _ wechatOutbound = (*wechat.Bot)(nil)

// longTextFunc 是超长正文（>2000 字）的分片投递动作，真机一侧经 context-token 走微信长文接口。
type longTextFunc func(ctx context.Context, chatID, content, token string) error

// longTextViaContextToken 是真机一侧的长文投递实现。
func longTextViaContextToken(bot *wechat.Bot) longTextFunc {
	return func(ctx context.Context, chatID, content, token string) error {
		_, err := wechat.SendLongText(ctx, bot.Client(), bot.Media(), chatID, content, token)
		return err
	}
}

// sendToolLineage 是主动通道回执的血统标注：与投递终态回执同族，落在门内——只留痕，不自达。
const sendToolLineage = "send-tool"

// errNoSendTarget 点名"该会址没有绑定的送达目标"：发送缝据此回模型具名拒绝，扣留而非猜测。
var errNoSendTarget = errors.New("无可解析的送达目标（本会址未绑定会话）")

// sendRequest 描述一次送达意图：正文、该次送达的会址与血统标注。
//   - addr 是本次调用的显式会址；目标只经路由表按精确会址解析，不经任何全局最近会话回退；
//   - triggerSource 是日志与回执的血统标注；userName 只参与目标标签渲染。
type sendRequest struct {
	addr          event.Address
	userName      string
	triggerSource string
	content       string
}

// hostSender 承载宿主唯一的出网发送通路：被动通道（回合结束的交付）与主动通道
// （send 工具）汇合于 sendToUser，typing 清理、长文分片、附件投递、失败回执一条不漏。
type hostSender struct {
	bot          wechatOutbound
	longText     longTextFunc
	typingActive *sync.Map
	router       *SessionRouter
	inj          receiptInjector
	workspaceDir string
	replicas     resultReplicaSource
	sends        atomic.Uint64
}

// sendCalls 报告 sendToUser 被到访几次（目标落空的拒绝也算一次）：两路共用同一实现的可执行断言位。
func (h *hostSender) sendCalls() uint64 { return h.sends.Load() }

// sendToUser 把一次送达意图落成实际投递，返回实际送达目标的标签。
//   - 目标按精确会址解析：会址须完整且已绑定会话，否则具名拒绝（不广播、不猜测、不回退他会话）；
//   - 文本发送失败记 send-failed 回执并把原因随 error 回调用方，附件投递照常发生。
func (h *hostSender) sendToUser(ctx context.Context, req sendRequest) (string, error) {
	h.sends.Add(1)

	target := ""
	if req.addr.IsComplete() && h.router != nil {
		target, _ = h.router.Target(req.addr)
	}
	if target == "" {
		log.Warnf("[Agent][%s] 会址未绑定会话，无可回退目标，扣留: %s", req.triggerSource, truncateLog(req.content))
		emitReceipt(h.inj, req.addr, "no-target", "WARN", req.triggerSource, "", req.content)
		return "", errNoSendTarget
	}
	if startTime, ok := h.typingActive.Load(target); ok {
		if t, ok := startTime.(time.Time); ok && time.Since(t) < 60*time.Second {
			_ = h.bot.StopTyping(ctx, target)
		}
		h.typingActive.Delete(target)
	}

	userLabel := target
	if req.userName != "" {
		userLabel = fmt.Sprintf("%s(%s)", req.userName, target)
	}
	log.Infof("[Agent][%s->%s] %s", req.triggerSource, userLabel, truncateLog(req.content))

	originalContent := req.content
	content := req.content
	textSent := false
	if len(content) > 2000 {
		token, _ := h.bot.GetContextToken(target)
		if token != "" && h.longText != nil {
			if err := h.longText(ctx, target, content, token); err == nil {
				textSent = true
			} else {
				content = content[:2000] + "\n\n[Message truncated]"
			}
		} else {
			content = content[:2000] + "\n\n[Message truncated]"
		}
	}
	var sendErr error
	if !textSent {
		if err := h.bot.SendTextToUser(ctx, target, content); err != nil {
			log.Errorf("SendTextToUser failed for %s: %v", target, err)
			emitReceipt(h.inj, req.addr, "send-failed", "ERROR", req.triggerSource, target, content)
			sendErr = err
		}
	}

	h.deliverFiles(ctx, req, target, originalContent)
	return userLabel, sendErr
}

// deliverFiles 把一条回复命名的文件交给通道：每一笔拒绝与失败都以具名回执回流该会话，
// 已投递的正文不受影响，一个文件失败也不阻断其余文件。
func (h *hostSender) deliverFiles(ctx context.Context, req sendRequest, target, content string) fileDeliveryReport {
	return fileDelivery{
		sender:        h.bot,
		chatID:        target,
		content:       content,
		workspaceDir:  h.workspaceDir,
		addr:          req.addr,
		triggerSource: req.triggerSource,
		receipts:      h.inj,
		replicas:      h.replicas,
	}.deliver(ctx)
}

// hostSendFunc is the late-bound send seam the tool instances carry: one
// explicit address beside one body.
type hostSendFunc func(ctx context.Context, addr event.Address, content string) (string, error)

// hostSendSeam 是主动通道的晚绑定发送缝：工具实例先于 bot 产出，绑定完成后调用直达 hostSender。
type hostSendSeam struct {
	mu   sync.RWMutex
	send hostSendFunc
}

// bind registers the host-side delivery function of this process.
func (s *hostSendSeam) bind(send hostSendFunc) {
	s.mu.Lock()
	s.send = send
	s.mu.Unlock()
}

// Send delivers one body at the explicit address the call carries. An unbound
// seam refuses by name instead of panicking.
func (s *hostSendSeam) Send(ctx context.Context, addr event.Address, content string) (string, error) {
	s.mu.RLock()
	send := s.send
	s.mu.RUnlock()
	if send == nil {
		return "", errors.New("宿主发送通路尚未就绪（bot 未完成装配），本次发送无法执行")
	}
	return send(ctx, addr, content)
}

// WechatAppConfig holds WeChat-specific configuration.
type WechatAppConfig struct {
	ConfigDir       string `yaml:"config_dir" json:"config_dir"`
	TokenFile       string `yaml:"token_file" json:"token_file"`
	ContextTokenDir string `yaml:"context_token_dir" json:"context_token_dir"`
	WorkspaceDir    string `yaml:"workspace_dir" json:"workspace_dir"`
	// Approvers：可经消息通道批准 critical 操作的用户 ID 白名单。
	// 空 = 消息通道批准关闭（安全默认），批准仅经 CLI（wechat-bot approve <digest>）。
	Approvers []string `yaml:"approvers" json:"approvers,omitempty"`
}

// EnsureDirs creates the directories the channel and the workspace need.
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

func main() {
	configPath := "tagent.yaml"
	if envPath := os.Getenv("TAGENT_CONFIG"); envPath != "" {
		configPath = envPath
	}

	doc, err := loadHostDocument(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Load config failed: %v\n", err)
		os.Exit(1)
	}

	wechatCfg := doc.App.Wechat
	if wechatCfg.ConfigDir == "" {
		wechatCfg.ConfigDir = ".wechat-config"
	}
	if wechatCfg.TokenFile == "" {
		wechatCfg.TokenFile = "token.json"
	}
	if wechatCfg.ContextTokenDir == "" {
		wechatCfg.ContextTokenDir = ".wechat-context-tokens"
	}
	if v := os.Getenv("TAGENT_WECHAT_WORKSPACE_DIR"); v != "" {
		wechatCfg.WorkspaceDir = v
	}

	gateway := newApprovalGateway(doc.Governance, wechatCfg.Approvers)
	if len(os.Args) >= 3 && (os.Args[1] == "approve" || os.Args[1] == "reject") {
		_, reply, err := gateway.Resolve("cli", os.Args[1]+" "+os.Args[2])
		if err != nil {
			fmt.Fprintf(os.Stderr, "审批失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(reply)
		return
	}

	if err := wechatCfg.EnsureDirs(); err != nil {
		fmt.Fprintf(os.Stderr, "Create dirs failed: %v\n", err)
		os.Exit(1)
	}

	seenStore := NewSeenStore(wechatCfg.ConfigDir, slog.Default())

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	host, err := assembleHost(ctx, doc, gateway, seenStore)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Assemble runtime failed: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		if err := host.Close(shutdownCtx); err != nil {
			log.Warnf("[host] close: %v", err)
		}
	}()

	fmt.Println("===========================================")
	fmt.Println("  tagent WeChat Bot")
	fmt.Println("===========================================")
	fmt.Printf("  Agent Name:  %s\n", doc.Entry)
	fmt.Printf("  Model:       %s\n", doc.modelName(doc.Entry))
	fmt.Printf("  Provider:    %s\n", doc.Provider)
	fmt.Printf("  Config:      %s\n", configPath)
	fmt.Println("===========================================")
	reportUnwired(doc)

	go maybeInjectReincarnationNotice(host.inbox, nil, doc.Entry, filepath.Join("run"), noticeWaitMax)
	go maybeConsumeSystemAlert(host.inbox, filepath.Join("run"), 5*time.Second)

	slogLogger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	cursorStore := wechat.NewFileCursorStore(filepath.Join(wechatCfg.ConfigDir, "cursor.txt"))
	bot := wechat.NewBot(
		wechat.WithLogger(slogLogger),
		wechat.WithCursorStore(cursorStore),
		wechat.WithTokenFile(wechatCfg.TokenFile),
	)
	gateway.SetSender(bot)

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

	typingActive := &sync.Map{}
	host.sender = &hostSender{
		bot:          bot,
		longText:     longTextViaContextToken(bot),
		typingActive: typingActive,
		router:       host.router,
		inj:          host.inbox,
		workspaceDir: wechatCfg.WorkspaceDir,
	}
	if wechatCfg.WorkspaceDir != "" {
		replicas, err := newWorkspaceReplicas(wechatCfg.WorkspaceDir, deliveryOwner(doc.Entry), ledger.NewMemoryOriginalStore(), 0)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Wire the delivery replicas failed: %v\n", err)
			os.Exit(1)
		}
		if err := replicas.CleanupStartup(ctx); err != nil {
			log.Warnf("[host] replica reclamation at startup: %v", err)
		}
		go func() {
			if err := replicas.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				log.Warnf("[host] replica reclamation ended: %v", err)
			}
		}()
		host.sender.replicas = replicas
	}

	bot.OnMessage(func(ctx context.Context, msg *wechat.Message) error {
		if handled, reply, _ := gateway.Resolve(msg.FromUserID, msg.Text()); handled {
			if reply != "" {
				_ = bot.SendTextToUser(ctx, msg.FromUserID, reply)
			}
			return nil
		}
		if err := host.Admit(DedupKey(msg)); err != nil {
			log.Warnf("[Dedup] duplicate message dropped (chat=%s)", msg.FromUserID)
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

			if strings.TrimSpace(injectText) == "" {
				_ = bot.StopTyping(ctx, msg.FromUserID)
				typingActive.Delete(msg.FromUserID)
				return
			}

			if _, err := host.Drive(ctx, inboundMessage{
				ChatID:   msg.FromUserID,
				UserName: msg.FromUserID,
				Text:     injectText,
			}); err != nil {
				log.Errorf("[host] drive failed (chat=%s): %v", msg.FromUserID, err)
			}
			_ = bot.StopTyping(ctx, msg.FromUserID)
			typingActive.Delete(msg.FromUserID)
		}()

		return nil
	})

	face := newTaskFace(ctx, host, os.Getenv(faceTokenEnv))
	listener, err := face.Listen()
	if err != nil {
		log.Errorf("[host] HTTP 面未装配，探针与注入端点均不可用: %v", err)
	} else {
		fmt.Printf("  HTTP:        http://%s (/healthz, /task)\n", listener.Addr())
		go func() {
			if err := face.Serve(listener); err != nil && !errors.Is(err, context.Canceled) {
				log.Warnf("[host] HTTP 面结束: %v", err)
			}
		}()
	}

	fmt.Println("Bot is running. Press Ctrl+C to stop.")
	if err := bot.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintf(os.Stderr, "Bot stopped with error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Bot stopped gracefully.")
}

// reportUnwired states, at startup, the tool references this host assembles no
// implementation for. Strict decoding bounds the keys a document may carry; this
// report bounds the values: a tool reference the assembly contributes nothing for
// is announced instead of silently resolved to nothing.
func reportUnwired(doc *hostDocument) {
	var items []string
	for _, id := range doc.agentOrder() {
		for _, ref := range doc.Agents[id].Tools {
			switch {
			case ref.Kind == "tool" && ref.ID == sendToolID:
			case ref.Kind == "tool":
				items = append(items, fmt.Sprintf("agent %s: tool %q", id, ref.ID))
			case ref.Agent != "":
				items = append(items, fmt.Sprintf("agent %s: delegation to %q", id, ref.Agent))
			}
		}
	}
	if len(items) == 0 {
		return
	}
	log.Warnf("[host] unwired by this assembly (%d): %s", len(items), strings.Join(items, "; "))
}

// truncateLog shortens a body for one log line.
func truncateLog(s string) string {
	return truncateLogN(s, 120)
}

// truncateLogN shortens a body to at most n bytes for one log line.
func truncateLogN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// runDir resolves the runtime state directory, anchored to the executable so
// cwd drift across launchers cannot misplace the persistence file.
func runDir() string {
	if exe, err := os.Executable(); err == nil {
		return filepath.Join(filepath.Dir(exe), "run")
	}
	return "run"
}

// userMessage is the native user message one inbound delivery carries.
func userMessage(text string) model.Message {
	return model.Message{Role: model.RoleUser, Content: text}
}
