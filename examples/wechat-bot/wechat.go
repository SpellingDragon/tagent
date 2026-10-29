// wechat.go 是微信宿主的适配文件：SDK 生命周期、收发、路由、去重、send 端口、审批与启动通知
// 都是微信协议语义的一部分。装配（声明→agents→models→工具面→storage→meditation→注入面）
// 一律走 assembly.Boot；应用侧只填 Boot 留出的两个端口：出站消息面与审批面。
//
// 契约: docs/wiki/examples/wechat-bot-runtime.md#module-layout
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/SpellingDragon/tagent/api/event"
	tagentmodel "github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/wechat-robot-go/wechat"
	"gopkg.in/yaml.v3"
	"trpc.group/trpc-go/trpc-agent-go/log"
)

// FileSender 是投递层依赖的窄接口；*wechat.Bot 天然满足，测试以替身顶上。
type FileSender interface {
	SendTextToUser(ctx context.Context, toUserID, text string) error
	SendImageFromPath(ctx context.Context, toUserID, imagePath string) error
	SendVoiceFromPath(ctx context.Context, toUserID, voicePath string, duration int) error
	SendVideoFromPath(ctx context.Context, toUserID, videoPath string) error
	SendFileFromPath(ctx context.Context, toUserID, filePath string) error
}

// MediaDownloader 是入站接收层的窄接口：SDK 边下边解密写入 Writer，MaxSize 由 SDK 在传输中强制。
type MediaDownloader interface {
	DownloadImageFromItemTo(ctx context.Context, cdnBaseURL string, img *wechat.ImageItem, w io.Writer, opts wechat.DownloadOptions) (int64, error)
	DownloadVoiceTo(ctx context.Context, voice *wechat.VoiceItem, cdnBaseURL string, w io.Writer, opts wechat.DownloadOptions) (int64, error)
	DownloadFileFromItemTo(ctx context.Context, file *wechat.FileItem, cdnBaseURL string, w io.Writer, opts wechat.DownloadOptions) (int64, error)
	DownloadVideoFromItemTo(ctx context.Context, video *wechat.VideoItem, cdnBaseURL string, w io.Writer, opts wechat.DownloadOptions) (int64, error)
}

// wechatOutbound 是宿主发送通路对通道实例的窄依赖面：文本、媒体、typing 与 context-token。
type wechatOutbound interface {
	FileSender
	StopTyping(ctx context.Context, toUserID string) error
	GetContextToken(userID string) (string, error)
}

// wechatChannel 是挂点注册与驱动对通道实例的窄依赖面：出网面与入站媒体下载面之外，
// 再加"入站钩子、typing 入口与 CDN 根"。*wechat.Bot 天然满足，测试以替身顶上。
type wechatChannel interface {
	wechatOutbound
	MediaDownloader
	OnMessage(handler wechat.MessageHandler)
	SendTyping(ctx context.Context, toUserID string) error
	CDNBaseURL() string
}

var (
	_ FileSender      = (*wechat.Bot)(nil)
	_ MediaDownloader = (*wechat.Bot)(nil)
	_ wechatOutbound  = (*wechat.Bot)(nil)
	_ wechatChannel   = (*wechat.Bot)(nil)
)

// longTextFunc 是超长正文（>2000 字节）的分片投递动作，真机经 context-token 走微信长文接口。
type longTextFunc func(ctx context.Context, chatID, content, token string) error

// longTextViaContextToken 是真机一侧的长文投递实现。
func longTextViaContextToken(bot *wechat.Bot) longTextFunc {
	return func(ctx context.Context, chatID, content, token string) error {
		_, err := wechat.SendLongText(ctx, bot.Client(), bot.Media(), chatID, content, token)
		return err
	}
}

// slogLevel reports the slog level TAGENT_LOG_LEVEL names: debug turns both the
// app logger and the framework logger to debug, anything else stays at info.
func slogLevel() slog.Level {
	if os.Getenv("TAGENT_LOG_LEVEL") == "debug" {
		return slog.LevelDebug
	}
	return slog.LevelInfo
}

// WechatAppConfig is the WeChat-specific application section of the document.
type WechatAppConfig struct {
	ConfigDir       string   `yaml:"config_dir" json:"config_dir"`
	TokenFile       string   `yaml:"token_file" json:"token_file"`
	ContextTokenDir string   `yaml:"context_token_dir" json:"context_token_dir"`
	WorkspaceDir    string   `yaml:"workspace_dir" json:"workspace_dir"`
	Approvers       []string `yaml:"approvers" json:"approvers,omitempty"`
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
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create dir %s: %w", dir, err)
		}
	}
	return nil
}

// wechatConfig is the application-side view of the one configuration source: the
// entry agent, the approval section and the WeChat section. Everything else is
// read by assembly.Boot, which refuses an unknown key before anything starts.
type wechatConfig struct {
	Entry      string          `yaml:"entry"`
	Governance governanceEntry `yaml:"governance"`
	App        struct {
		Wechat WechatAppConfig `yaml:"wechat"`
	} `yaml:"app"`
}

// loadWechatConfig reads the sections this application owns. The whole document
// is decoded by Boot; here only the WeChat section is read strictly, so a
// misspelt channel key refuses the startup instead of running on a default.
func loadWechatConfig(path string) (wechatConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return wechatConfig{}, fmt.Errorf("wechat-bot: read %s: %w", path, err)
	}
	var source struct {
		Entry      string          `yaml:"entry"`
		Governance governanceEntry `yaml:"governance"`
		App        yaml.Node       `yaml:"app"`
	}
	if err := yaml.Unmarshal(raw, &source); err != nil {
		return wechatConfig{}, fmt.Errorf("wechat-bot: %w", err)
	}
	cfg := wechatConfig{Entry: strings.TrimSpace(source.Entry), Governance: source.Governance}
	if source.App.Kind != 0 {
		section, err := strictWechatSection(source.App)
		if err != nil {
			return wechatConfig{}, fmt.Errorf("wechat-bot: app section: %w", err)
		}
		cfg.App.Wechat = section
	}
	if cfg.App.Wechat.ConfigDir == "" {
		cfg.App.Wechat.ConfigDir = ".wechat-config"
	}
	if cfg.App.Wechat.TokenFile == "" {
		cfg.App.Wechat.TokenFile = "token.json"
	}
	if cfg.App.Wechat.ContextTokenDir == "" {
		cfg.App.Wechat.ContextTokenDir = ".wechat-context-tokens"
	}
	if workspace := os.Getenv("TAGENT_WECHAT_WORKSPACE_DIR"); workspace != "" {
		cfg.App.Wechat.WorkspaceDir = workspace
	}
	return cfg, nil
}

// strictWechatSection decodes the application section with unknown keys refused.
func strictWechatSection(node yaml.Node) (WechatAppConfig, error) {
	raw, err := yaml.Marshal(&node)
	if err != nil {
		return WechatAppConfig{}, err
	}
	var section struct {
		Wechat WechatAppConfig `yaml:"wechat"`
	}
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&section); err != nil {
		return WechatAppConfig{}, err
	}
	return section.Wechat, nil
}

// configPath reports the configuration source this process was started with.
func configPath() string {
	if envPath := os.Getenv("TAGENT_CONFIG"); envPath != "" {
		return envPath
	}
	return "tagent.yaml"
}

// dieIf reports a startup failure and ends the process; a nil error passes.
func dieIf(err error, what string) {
	if err == nil {
		return
	}
	fmt.Fprintf(os.Stderr, "%s failed: %v\n", what, err)
	os.Exit(1)
}

// hostRuntime is the application side of one assembled host: the one outbound
// path, the session router and the internal inbox the channel drives. The
// runtime itself (models, tools, storage, meditation, injection face) belongs to
// assembly.Host.
type hostRuntime struct {
	invoke invokeFunc
	router *SessionRouter
	inbox  *internalInbox
	sender *hostSender
	seen   *SeenStore
	gates  sync.Map
}

// invokeFunc drives one admitted delivery through the assembled host and reports
// whether its terminal was a failure.
type invokeFunc func(ctx context.Context, req assembly.InvokeRequest) (failed bool, err error)

// inboundMessage is one channel delivery the host admits.
type inboundMessage struct {
	Key      string
	ChatID   string
	UserName string
	Text     string
}

// Admit claims the dedup identity of one channel delivery.
func (h *hostRuntime) Admit(key string) error {
	if h.seen == nil || key == "" {
		return nil
	}
	if !h.seen.CheckAndMark(key) {
		return fmt.Errorf("wechat-bot: the inbound message was already admitted: %s", key)
	}
	return nil
}

// Drive routes one admitted delivery to its session and lets the host deliver
// the turn's output through the sender port. A failed turn is answered by the
// host's own failure notice: this layer holds no cause and writes no second
// wording, so the user reads one raw error instead of a canned sentence.
//
// The per-session lock covers the intake alone: the notices a session still has
// to read and the text of this delivery are taken into one request under it, so
// two deliveries of one chat never read the inbox for each other. The turn itself
// runs outside the lock, because the host's own address gate orders the drives
// and takes a message that arrives while one runs onto the chain of that turn: a
// channel lock around the whole turn would park the second message behind the
// first one again, which is the queue this runtime does not keep. A delivery the
// host accepted beside a running turn answers no decision and no error, and that
// silence is the accepted message: it is never read here as a failed turn.
func (h *hostRuntime) Drive(ctx context.Context, in inboundMessage) (event.Address, error) {
	addr, err := h.router.AddressFor(in.ChatID)
	if err != nil {
		return event.Address{}, err
	}
	if err := h.router.Bind(addr, in.ChatID); err != nil {
		return event.Address{}, err
	}
	input := h.intake(addr, in)
	if len(input) == 0 {
		return addr, nil
	}
	if h.invoke == nil {
		return addr, errors.New("wechat-bot: the host runtime is not bound")
	}
	failed, err := h.invoke(ctx, assembly.InvokeRequest{
		InvocationID:  h.router.NextInvocation(addr),
		Address:       addr,
		TriggerSource: "user",
		Input:         input,
	})
	if err != nil {
		return addr, err
	}
	if failed {
		log.Debugf("[host] the turn of %s ended failed; the failure notice is delivered by the host", addr.SessionID)
	}
	return addr, nil
}

// intake takes the material one delivery admits: the notices the session still
// has to read beside the text of this delivery, read and composed under the
// intake lock of the session so two deliveries of one chat never read the inbox
// for each other. The turn is not held under that lock: the host orders the
// drives of the session and takes a message that arrives beside a running turn
// onto that turn's chain.
func (h *hostRuntime) intake(addr event.Address, in inboundMessage) []tagentmodel.Message {
	unlock := h.lockAddress(addr)
	defer unlock()
	input := h.inbox.Drain(addr)
	if text := strings.TrimSpace(in.Text); text != "" {
		input = append(input, tagentmodel.Message{Role: tagentmodel.RoleUser, Content: text})
	}
	return input
}

// lockAddress serializes the intake of one session: the take of the notices it
// still has to read beside the text of the delivery that is being admitted.
func (h *hostRuntime) lockAddress(addr event.Address) func() {
	value, _ := h.gates.LoadOrStore(addr, &sync.Mutex{})
	mu := value.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// internalInbox holds the internal notices a session has to read on its next turn.
type internalInbox struct {
	mu      sync.Mutex
	byAddr  map[event.Address][]tagentmodel.Message
	pending []tagentmodel.Message
}

func newInternalInbox() *internalInbox {
	return &internalInbox{byAddr: map[event.Address][]tagentmodel.Message{}}
}

// InjectInternal queues one internal notice for the named address; an incomplete
// address queues it for the first turn of any session.
func (in *internalInbox) InjectInternal(addr event.Address, source string, msg tagentmodel.Message) {
	if in == nil {
		return
	}
	if msg.Role == "" {
		msg.Role = tagentmodel.RoleUser
	}
	log.Debugf("[inbox] queue %s notice for %s", source, addr.SessionID)
	in.mu.Lock()
	defer in.mu.Unlock()
	if !addr.IsComplete() {
		in.pending = append(in.pending, msg)
		return
	}
	in.byAddr[addr] = append(in.byAddr[addr], msg)
}

// Drain returns the notices waiting for the named address beside the notices
// waiting for any session, and records them as consumed.
func (in *internalInbox) Drain(addr event.Address) []tagentmodel.Message {
	if in == nil {
		return nil
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	out := append([]tagentmodel.Message(nil), in.byAddr[addr]...)
	delete(in.byAddr, addr)
	out = append(out, in.pending...)
	in.pending = nil
	return out
}

// sendToolLineage 是主动通道回执的血统标注：与投递终态回执同族，落在门内——只留痕，不自达。
const sendToolLineage = "send-tool"

// errNoSendTarget 点名"该会址没有绑定的送达目标"：发送端口据此回具名拒绝，扣留而非猜测。
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

// hostSender 承载宿主唯一的出网发送通路：Boot 的回合交付与 send 工具都汇合于 Send，
// typing 清理、长文分片、附件投递、失败回执一条不漏。
type hostSender struct {
	bot          wechatOutbound
	longText     longTextFunc
	typingActive *sync.Map
	router       *SessionRouter
	inj          receiptInjector
	workspaceDir string
	sends        atomic.Uint64
}

// Send implements assembly.MessageSender: one body to the address the turn carries.
func (h *hostSender) Send(ctx context.Context, addr event.Address, content string) error {
	return h.sendWithLineage(ctx, addr, sendToolLineage, content)
}

// sendWithLineage lands one body with the given trigger-source stamp.
func (h *hostSender) sendWithLineage(ctx context.Context, addr event.Address, lineage, content string) error {
	_, err := h.sendToUser(ctx, sendRequest{addr: addr, triggerSource: lineage, content: content})
	return err
}

// sendToUser 把一次送达意图落成实际投递，返回实际送达目标的标签。
//   - 目标按精确会址解析：会址须完整且已绑定会话，否则具名拒绝（不广播、不猜测、不回退他会话）；
//   - 文本发送失败记 send-failed 回执并把原因随 error 回调用方，附件投递照常发生。
func (h *hostSender) sendToUser(ctx context.Context, req sendRequest) (string, error) {
	if h.bot == nil {
		return "", errors.New("wechat-bot: the wechat sender is not bound")
	}
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
	if h.typingActive != nil {
		if startTime, ok := h.typingActive.Load(target); ok {
			if t, ok := startTime.(time.Time); ok && time.Since(t) < 60*time.Second {
				_ = h.bot.StopTyping(ctx, target)
			}
			h.typingActive.Delete(target)
		}
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
				content = truncateBytes(content, 2000) + "\n\n[Message truncated]"
			}
		} else {
			content = truncateBytes(content, 2000) + "\n\n[Message truncated]"
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
	}.deliver(ctx)
}

// receiptLineage labels the internal delivery receipts fed back to the agent. It
// stays outside every deliverable lineage: a receipt turn is internal, its own
// sending never receipts, and a withheld body travels with the receipt of the
// address that produced it.
const receiptLineage = "delivery_receipt"

// receiptInjector is the narrow host-side view of the inbox a receipt is queued
// into; *internalInbox satisfies it.
type receiptInjector interface {
	InjectInternal(addr event.Address, source string, msg tagentmodel.Message)
}

// emitReceipt renders one receipt, logs it at its grade and queues it for the
// address whose delivery ended without reaching the user. A receipt of the
// receipt lineage itself is dropped: the no-recursion red line.
func emitReceipt(inj receiptInjector, addr event.Address, reason, level, triggerSource, target, content string) {
	if inj == nil || triggerSource == receiptLineage {
		return
	}
	line := fmt.Sprintf("[delivery-receipt] 投递终态=%s 血统=%s 目标=%s：%s",
		reason, triggerSource, orNoTarget(target), truncateLog(content))
	if level == "ERROR" {
		log.Errorf("%s", line)
	} else {
		log.Warnf("%s", line)
	}
	inj.InjectInternal(addr, receiptLineage, tagentmodel.Message{
		Role:    tagentmodel.RoleUser,
		Content: line + "\n该内容未达用户；若属用户等待的交付，请在用户回合投递。",
	})
}

// orNoTarget names the missing delivery target of one receipt.
func orNoTarget(target string) string {
	if target == "" {
		return "(无目标)"
	}
	return target
}

// truncateLog shortens a body for one log line.
func truncateLog(s string) string { return truncateLogN(s, 120) }

// truncateLogN shortens a body to at most n bytes for one log line.
func truncateLogN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return truncateBytes(s, n) + "..."
}

// truncateBytes cuts s to at most n bytes without splitting a UTF-8 rune.
func truncateBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// SeenStore provides message-level idempotency across restarts.
//
//   - Dedup key layering: ClientID when non-empty, otherwise uid plus a text hash prefix.
//   - seen.json sits next to the bot config, loads at start, prunes by TTL and capacity, writes
//     atomically; a corrupted file degrades to an empty set so dedup never blocks the pipeline.
type SeenStore struct {
	path     string
	ttl      time.Duration
	capacity int
	mu       sync.Mutex
	seen     map[string]int64
	logger   *slog.Logger
}

// NewSeenStore loads (or initializes) the seen set from dir/seen.json.
func NewSeenStore(dir string, logger *slog.Logger) *SeenStore {
	s := &SeenStore{
		path:     filepath.Join(dir, "seen.json"),
		ttl:      24 * time.Hour,
		capacity: 10000,
		seen:     make(map[string]int64),
		logger:   logger,
	}
	s.load()
	return s
}

func (s *SeenStore) load() {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if !os.IsNotExist(err) {
			s.logger.Warn("[Dedup] failed to read seen.json, starting empty", "error", err)
		}
		return
	}
	var m map[string]int64
	if err := json.Unmarshal(data, &m); err != nil {
		s.logger.Warn("[Dedup] seen.json corrupted, starting empty", "error", err)
		_ = os.Rename(s.path, s.path+".corrupt")
		return
	}
	now := time.Now().Unix()
	for k, ts := range m {
		if now-ts <= int64(s.ttl.Seconds()) {
			s.seen[k] = ts
		}
	}
	s.logger.Info("[Dedup] seen.json restored", "entries", len(s.seen))
}

// CheckAndMark returns true if the key was new (message should be processed),
// false if it was already seen (duplicate — drop). Marking is persisted
// immediately (atomic write) so a crash right after cannot replay the message.
func (s *SeenStore) CheckAndMark(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.seen[key]; ok {
		s.seen[key] = time.Now().Unix()
		return false
	}
	s.seen[key] = time.Now().Unix()
	s.persistLocked()
	return true
}

// persistLocked prunes (TTL, then capacity) and atomically writes the set.
// Caller must hold s.mu.
func (s *SeenStore) persistLocked() {
	now := time.Now().Unix()
	ttlSec := int64(s.ttl.Seconds())
	for k, ts := range s.seen {
		if now-ts > ttlSec {
			delete(s.seen, k)
		}
	}
	if len(s.seen) > s.capacity {
		type kv struct {
			k string
			v int64
		}
		list := make([]kv, 0, len(s.seen))
		for k, v := range s.seen {
			list = append(list, kv{k, v})
		}
		sort.Slice(list, func(i, j int) bool { return list[i].v < list[j].v })
		for i := 0; i < len(list)-s.capacity; i++ {
			delete(s.seen, list[i].k)
		}
	}

	data, err := json.Marshal(s.seen)
	if err != nil {
		s.logger.Warn("[Dedup] marshal seen.json failed", "error", err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		s.logger.Warn("[Dedup] ensure seen dir failed", "error", err)
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".seen-*")
	if err != nil {
		s.logger.Warn("[Dedup] create seen tmp failed", "error", err)
		return
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		s.logger.Warn("[Dedup] write seen tmp failed", "error", err)
		return
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		s.logger.Warn("[Dedup] close seen tmp failed", "error", err)
		return
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		os.Remove(tmpName)
		s.logger.Warn("[Dedup] rename seen.json failed", "error", err)
		return
	}
}

// DedupKey builds the layered dedup key for an inbound message.
func DedupKey(msg *wechat.Message) string {
	if msg.ClientID != "" {
		return "cid:" + msg.ClientID
	}
	sum := sha256.Sum256([]byte(msg.Text()))
	return fmt.Sprintf("uid:%s#%s", msg.FromUserID, hex.EncodeToString(sum[:8]))
}

// ErrNoChatAddress reports an inbound message that names no chat identity. The
// refusal is the whole answer: a message without an address is never routed to
// a session that belongs to somebody else.
var ErrNoChatAddress = errors.New("wechat-bot: the inbound message names no chat identity")

// ErrSessionPrefixMissing reports a router constructed without the explicit
// session prefix its address spelling starts with.
var ErrSessionPrefixMissing = errors.New("wechat-bot: the session router carries no session prefix")

// ErrTargetRebind reports a second chat identity claimed for an address that
// already delivered to another chat. The bound target is the delivery identity
// of that session and is never rewritten by a later claim.
var ErrTargetRebind = errors.New("wechat-bot: the address is already bound to another chat identity")

// SessionRouter maps one explicit chat identity to the session address the
// runtime is driven at, and holds the delivery identity of each address it
// admitted.
//
// The mapping is injective in its spelling: one chat identity has exactly one
// session identity and two chat identities never share one, so a restart
// re-derives the same addresses instead of inventing a fresh history.
type SessionRouter struct {
	agentID event.AgentID
	prefix  string

	mu      sync.Mutex
	seq     uint64
	targets map[event.Address]string
}

// NewSessionRouter returns the router of one declared agent beside the explicit
// session prefix its addresses are spelled with. A missing agent or prefix is
// refused at construction, so no address is ever built from a default.
func NewSessionRouter(agentID event.AgentID, prefix string) (*SessionRouter, error) {
	if agentID == "" {
		return nil, ErrNoChatAddress
	}
	if prefix == "" {
		return nil, ErrSessionPrefixMissing
	}
	return &SessionRouter{
		agentID: agentID,
		prefix:  prefix,
		targets: map[event.Address]string{},
	}, nil
}

// AddressFor returns the explicit address of one chat identity: the declared
// agent beside the session identity that identity spells. The chat identity is
// percent-escaped, so the spelling is injective over every byte a channel may
// carry and two chats never collapse onto one session.
func (r *SessionRouter) AddressFor(chatID string) (event.Address, error) {
	trimmed := strings.TrimSpace(chatID)
	if trimmed == "" {
		return event.Address{}, ErrNoChatAddress
	}
	escaped := url.PathEscape(trimmed)
	if escaped == "" {
		return event.Address{}, ErrNoChatAddress
	}
	return event.Address{AgentID: r.agentID, SessionID: event.SessionID(r.prefix + escaped)}, nil
}

// Bind records the chat identity a drive was admitted for as the delivery
// identity of its address: a later explicit send from that address reaches the
// chat its own drive came from. A rebound address keeps its first identity and
// reports the conflict instead of silently retargeting the session.
func (r *SessionRouter) Bind(addr event.Address, chatID string) error {
	trimmed := strings.TrimSpace(chatID)
	if trimmed == "" {
		return ErrNoChatAddress
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if bound, ok := r.targets[addr]; ok && bound != trimmed {
		return fmt.Errorf("%w: %s", ErrTargetRebind, addr.SessionID)
	}
	r.targets[addr] = trimmed
	return nil
}

// Target reports the chat identity bound to exactly this address. An address no
// drive ever bound answers not-ok: the delivery face then refuses instead of
// resolving a most-recent chat of another session.
func (r *SessionRouter) Target(addr event.Address) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	chatID, ok := r.targets[addr]
	return chatID, ok
}

// BoundAddresses reports the addresses of the named agent that a drive bound to
// a chat identity, in session order.
func (r *SessionRouter) BoundAddresses(agentID event.AgentID) []event.Address {
	if agentID == "" || agentID != r.agentID {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	addresses := make([]event.Address, 0, len(r.targets))
	for addr := range r.targets {
		addresses = append(addresses, addr)
	}
	sort.Slice(addresses, func(i, j int) bool { return addresses[i].SessionID < addresses[j].SessionID })
	return addresses
}

// NextInvocation mints the responsibility range identity of one drive of the
// address. The identity is unique per drive and derived from the address, so a
// range never collides with the range of another session.
func (r *SessionRouter) NextInvocation(addr event.Address) event.InvocationID {
	r.mu.Lock()
	r.seq++
	n := r.seq
	r.mu.Unlock()
	return event.InvocationID(string(addr.SessionID) + "#" + strconv.FormatUint(n, 10))
}

// sendToolID 是声明侧 ToolRef 与核心工具实例共用的工具名。
const sendToolID = "send"

// sessionPrefixOf reports the session identity prefix the document's addresses
// are spelled with.
func sessionPrefixOf(entry string) string {
	label := strings.TrimSpace(os.Getenv("TAGENT_SESSION_ID"))
	if label == "" {
		label = "wechat-session"
	}
	return label + ":" + strings.TrimSpace(entry) + ":"
}

// newWechatBot builds the channel instance of this process: the file cursor and
// token stores live beside the configured config directory.
func newWechatBot(cfg WechatAppConfig) *wechat.Bot {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slogLevel()}))
	if os.Getenv("TAGENT_LOG_LEVEL") == "debug" {
		log.SetLevel(log.LevelDebug)
		logger.Info("log level: debug (TAGENT_LOG_LEVEL=debug)")
	}
	cursorStore := wechat.NewFileCursorStore(filepath.Join(cfg.ConfigDir, "cursor.txt"))
	return wechat.NewBot(
		wechat.WithLogger(logger),
		wechat.WithCursorStore(cursorStore),
		wechat.WithTokenFile(cfg.TokenFile),
	)
}

// login drives the login flow and reports the QR code the user has to scan.
func login(ctx context.Context, bot *wechat.Bot) error {
	fmt.Println("Logging in to WeChat...")
	if err := bot.Login(ctx, func(qrCode string) {
		fmt.Println("\nPlease scan the QR code with WeChat:")
		fmt.Println("----------------------------------------")
		if strings.HasPrefix(qrCode, "http") {
			fmt.Println(qrCode)
		} else {
			fmt.Println("[QR code image content]")
		}
		fmt.Println("----------------------------------------")
	}); err != nil {
		return err
	}
	fmt.Println("Login successful!")
	return nil
}

// bindChannel binds the one outbound path of this process: passive turn
// delivery, the send tool, files and long text all leave through it.
func bindChannel(host *hostRuntime, bot wechatChannel, cfg WechatAppConfig) *sync.Map {
	typingActive := &sync.Map{}
	// The context-token long-text path is a property of the real channel; a
	// stand-in channel records the truncation fallback instead.
	var longText longTextFunc
	if concrete, ok := bot.(*wechat.Bot); ok {
		longText = longTextViaContextToken(concrete)
	}
	host.sender.bot = bot
	host.sender.longText = longText
	host.sender.typingActive = typingActive
	host.sender.workspaceDir = cfg.WorkspaceDir
	return typingActive
}

// registerHandlers registers the inbound hook of the channel: approval rulings
// first, then dedup, classification, media intake and the drive.
func registerHandlers(host *hostRuntime, bot wechatChannel, gateway *approvalGateway, cfg WechatAppConfig, typingActive *sync.Map) {
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

		kind := ClassifyInbound(msg, cfg.WorkspaceDir != "")
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
				if cfg.WorkspaceDir == "" {
					_ = bot.SendTextToUser(ctx, msg.FromUserID, "暂不支持附件接收（未配置 workspace）")
				} else {
					outcome := IntakeMedia(ctx, bot, bot.CDNBaseURL(), cfg.WorkspaceDir, msg.FromUserID, msg, now)
					for _, reason := range outcome.Rejects {
						_ = bot.SendTextToUser(ctx, msg.FromUserID, reason)
					}
					injectText = ComposeMediaInject(outcome, msg.Text(), cfg.WorkspaceDir, msg.FromUserID, now)
				}
			case InboundLongText:
				if _, inject, err := SaveLongText(cfg.WorkspaceDir, msg.FromUserID, injectText, "input", now); err == nil {
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
}

// wechatApp is the WeChat side of one process: the channel instance, the one
// outbound port and the inbound hook chain. Boot owns everything else.
type wechatApp struct {
	cfg     wechatConfig
	bot     *wechat.Bot
	gateway *approvalGateway
	host    *hostRuntime
	typing  *sync.Map
	face    string
}

// newWechatApp prepares the application side before Boot: the channel instance,
// the outbound port, the session router and the dedup store.
func newWechatApp(cfg wechatConfig, gateway *approvalGateway) (*wechatApp, error) {
	if err := cfg.App.Wechat.EnsureDirs(); err != nil {
		return nil, err
	}
	router, err := NewSessionRouter(event.AgentID(cfg.Entry), sessionPrefixOf(cfg.Entry))
	if err != nil {
		return nil, err
	}
	inbox := newInternalInbox()
	app := &wechatApp{cfg: cfg, bot: newWechatBot(cfg.App.Wechat), gateway: gateway}
	app.host = &hostRuntime{
		router: router,
		inbox:  inbox,
		sender: &hostSender{router: router, typingActive: &sync.Map{}, inj: inbox, workspaceDir: cfg.App.Wechat.WorkspaceDir},
		seen:   NewSeenStore(cfg.App.Wechat.ConfigDir, slog.Default()),
	}
	return app, nil
}

// options reports the ports this application fills on the host.
func (a *wechatApp) options() []assembly.HostOption {
	options := []assembly.HostOption{
		assembly.WithSender(a.host.sender),
	}
	if a.gateway.Enabled() {
		options = append(options, assembly.WithApprover(approvalRuling(a.gateway)))
	}
	return options
}

// bindWechat completes the application side once Boot assembled the runtime: the bot
// logs in, the one outbound path and the inbound hooks are bound, and the
// one-shot startup notices wait beside them.
func (a *wechatApp) bindWechat(ctx context.Context, host *assembly.Host) error {
	a.host.invoke = func(ctx context.Context, req assembly.InvokeRequest) (bool, error) {
		turn, err := host.Invoke(ctx, req)
		if err != nil {
			return false, err
		}
		return turn.Terminal == tagentmodel.KindFailed, nil
	}
	a.face = host.FaceAddr()
	a.gateway.SetSender(a.bot)
	if err := login(ctx, a.bot); err != nil {
		return err
	}
	a.typing = bindChannel(a.host, a.bot, a.cfg.App.Wechat)
	registerHandlers(a.host, a.bot, a.gateway, a.cfg.App.Wechat, a.typing)
	go maybeInjectReincarnationNotice(a.host.inbox, nil, a.cfg.Entry, "run", noticeWaitMax)
	go maybeConsumeSystemAlert(a.host.inbox, "run", 5*time.Second)
	return nil
}

// banner prints the startup banner of this process.
func (a *wechatApp) banner(path string) {
	fmt.Println("===========================================")
	fmt.Println("  tagent WeChat Bot")
	fmt.Println("===========================================")
	fmt.Printf("  Agent Name:  %s\n", a.cfg.Entry)
	fmt.Printf("  Config:      %s\n", path)
	fmt.Printf("  HTTP:        http://%s (/healthz, /task)\n", a.face)
	fmt.Println("===========================================")
}

// run drives the channel until the process context ends.
func (a *wechatApp) run(ctx context.Context) error {
	fmt.Println("Bot is running. Press Ctrl+C to stop.")
	if err := a.bot.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	fmt.Println("Bot stopped gracefully.")
	return nil
}

// close drains the host and reports what stayed open.
func (a *wechatApp) close(host *assembly.Host) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := host.Close(ctx); err != nil {
		log.Warnf("[host] close: %v", err)
	}
}
