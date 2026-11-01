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
	ConfigDir       string `yaml:"config_dir" json:"config_dir"`
	TokenFile       string `yaml:"token_file" json:"token_file"`
	ContextTokenDir string `yaml:"context_token_dir" json:"context_token_dir"`
	WorkspaceDir    string `yaml:"workspace_dir" json:"workspace_dir"`
	// OpsSession declares the chat identity the two startup notices are delivered
	// to; empty means this host's own ops session (see opsSessionAddress).
	OpsSession string   `yaml:"ops_session" json:"ops_session,omitempty"`
	Approvers  []string `yaml:"approvers" json:"approvers,omitempty"`
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
	return addr, h.drive(ctx, addr, "user", in)
}

// Wake drives the ops session once for the notices that already landed in its
// inbox: no user input opens this round, so the notice is the whole material of
// the turn. The address is the fixed target those notices were admitted for, and
// the source is the one they were admitted from — an incomplete target is refused
// by name instead of driving whichever session spoke last.
func (h *hostRuntime) Wake(ctx context.Context, addr event.Address, source string) error {
	if !addr.IsComplete() {
		return fmt.Errorf("%w: %v", ErrOpsWakeAddressMissing, addr)
	}
	return h.drive(ctx, addr, source, inboundMessage{})
}

// drive hands one round of the named session to the host: the notices the session
// still owes beside the text of the delivery that opened it. Every round this
// runtime drives — a chat delivery and the wakeup of the ops session — leaves
// through here, so the notices read under a round are confirmed by the same three
// steps whichever arrival opened it, and there is no second wake path.
// - A host that did not take the material leaves the notices admitted, so the next delivery reads them again instead of losing them beside a turn that never ran.
// - A host that took the material confirms them: the records retire and the consumed markers are renamed.
// - A failed terminal still took the material — the turn ran and read the notices — so only a host error leaves them owed.
func (h *hostRuntime) drive(ctx context.Context, addr event.Address, source string, in inboundMessage) error {
	input := h.intake(addr, in)
	if len(input.msgs) == 0 {
		return nil
	}
	if h.invoke == nil {
		h.inbox.Release(input.ids)
		return errors.New("wechat-bot: the host runtime is not bound")
	}
	failed, err := h.invoke(ctx, assembly.InvokeRequest{
		InvocationID:  h.router.NextInvocation(addr),
		Address:       addr,
		TriggerSource: source,
		Input:         input.msgs,
		InputMeta:     input.meta(),
	})
	if err != nil {
		h.inbox.Release(input.ids)
		return err
	}
	h.inbox.Confirm(input.ids)
	if failed {
		log.Debugf("[host] the turn of %s ended failed; the failure notice is delivered by the host", addr.SessionID)
	}
	return nil
}

// intakeBatch is the material one delivery admits: the notices the session still
// has to read beside the admissions they were read under, so the confirmation
// after the turn names the records it retires.
type intakeBatch struct {
	msgs    []tagentmodel.Message
	ids     []string
	sources []string
}

// intake takes the material one delivery admits: the notices the session still
// has to read beside the text of this delivery, read and composed under the
// intake lock of the session so two deliveries of one chat never read the inbox
// for each other. The turn is not held under that lock: the host orders the
// drives of the session and takes a message that arrives beside a running turn
// onto that turn's chain.
func (h *hostRuntime) intake(addr event.Address, in inboundMessage) intakeBatch {
	unlock := h.lockAddress(addr)
	defer unlock()
	msgs, ids := h.inbox.take(addr)
	sources := make([]string, 0, len(ids)+1)
	for _, id := range ids {
		sources = append(sources, h.inbox.sourceOf(id))
	}
	if text := strings.TrimSpace(in.Text); text != "" {
		msgs = append(msgs, tagentmodel.Message{Role: tagentmodel.RoleUser, Content: text})
		sources = append(sources, "")
	}
	return intakeBatch{msgs: msgs, ids: ids, sources: sources}
}

// meta names the source each message was admitted from, one entry per message
// and in the same order, so a notice reads in the facts of its turn by the
// source it came from while the round trigger source is left as declared. A
// message the delivery itself carried names no source and stamps nothing.
func (b intakeBatch) meta() []map[string]string {
	meta := make([]map[string]string, len(b.msgs))
	for i, source := range b.sources {
		if i >= len(meta) || source == "" {
			continue
		}
		meta[i] = map[string]string{event.MetaKeyNoticeSource: source}
	}
	return meta
}

// lockAddress serializes the intake of one session: the take of the notices it
// still has to read beside the text of the delivery that is being admitted.
func (h *hostRuntime) lockAddress(addr event.Address) func() {
	value, _ := h.gates.LoadOrStore(addr, &sync.Mutex{})
	mu := value.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// noticeInboxDir is the directory under the configuration directory that keeps
// the admitted notices: one file per admission, removed only when the turn that
// read the notice is confirmed.
const noticeInboxDir = "notice-inbox"

// noticeRecordSuffix is the suffix of one durable admission record.
const noticeRecordSuffix = ".json"

var (
	// ErrNoticeAddressIncomplete names the refusal of an admission that names no
	// complete address: a notice no session is named for has no reader, and the
	// pending queue the arrival order decided is the mechanism the fixed target
	// deleted.
	ErrNoticeAddressIncomplete = errors.New("wechat-bot: the notice names no complete address")

	// ErrOpsWakeAddressMissing names the refusal to wake an ops target that is
	// not a complete address.
	ErrOpsWakeAddressMissing = errors.New("wechat-bot: the ops session is declared beside no complete address")
)

// internalInbox holds the internal notices a session has to read on its next
// turn. Every notice is admitted as a durable record under a stable identity —
// the source it came from, a digest of its body and the admission ordinal — so a
// notice admitted before a stop is still in the inbox after the next start. The
// three steps are separate: an admitted notice is not delivered, a delivered
// notice is not consumed, and only the confirmation of the turn that read it
// retires the record and writes the consumed marker. An admission is therefore
// retryable: a redelivery answers the record already in the inbox instead of
// opening a second one.
type internalInbox struct {
	mu      sync.Mutex
	dir     string
	records map[string]*noticeRecord
	order   []string
	byAddr  map[event.Address][]string
	claimed map[string]bool
	seq     int

	// observe is the fault-injection seam one crash check reads at each durable
	// step; production leaves it nil and pays one nil check per step.
	observe func(step string)
}

// noticeRecord is one durable admission: the stable identity of a notice, its
// body, the address it waits for and the marker file its confirmation retires.
type noticeRecord struct {
	ID      string           `json:"id"`
	Source  string           `json:"source"`
	Seq     int              `json:"seq"`
	Role    tagentmodel.Role `json:"role,omitempty"`
	Content string           `json:"content"`
	Agent   string           `json:"agent,omitempty"`
	Session string           `json:"session,omitempty"`
	Marker  string           `json:"marker,omitempty"`
	Suffix  string           `json:"suffix,omitempty"`
}

// addressOf reports the address the record waits for. A record that names an
// incomplete address is one a previous version admitted without a fixed target:
// it is readable by no session and the load warns about it by name.
func (rec *noticeRecord) addressOf() event.Address {
	return event.Address{AgentID: event.AgentID(rec.Agent), SessionID: event.SessionID(rec.Session)}
}

// newInternalInbox opens the inbox under the named directory: the records a
// previous process left behind are read back, and an empty directory keeps the
// inbox in memory alone.
func newInternalInbox(dir string) (*internalInbox, error) {
	in := &internalInbox{
		dir:     dir,
		records: map[string]*noticeRecord{},
		byAddr:  map[event.Address][]string{},
		claimed: map[string]bool{},
	}
	if dir == "" {
		return in, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("wechat-bot: prepare the notice inbox %s: %w", dir, err)
	}
	if err := in.load(); err != nil {
		return nil, err
	}
	return in, nil
}

// load reads back the admissions a previous process left behind, in the order
// their ordinals name, so the next start reads the notices the last one owed.
func (in *internalInbox) load() error {
	entries, err := os.ReadDir(in.dir)
	if err != nil {
		return fmt.Errorf("wechat-bot: read the notice inbox %s: %w", in.dir, err)
	}
	records := make([]*noticeRecord, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), noticeRecordSuffix) {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(in.dir, entry.Name()))
		if err != nil {
			return fmt.Errorf("wechat-bot: read the notice record %s: %w", entry.Name(), err)
		}
		rec := &noticeRecord{}
		if err := json.Unmarshal(raw, rec); err != nil {
			return fmt.Errorf("wechat-bot: parse the notice record %s: %w", entry.Name(), err)
		}
		records = append(records, rec)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Seq < records[j].Seq })
	for _, rec := range records {
		in.index(rec)
		if rec.Seq > in.seq {
			in.seq = rec.Seq
		}
	}
	return nil
}

// index files one admitted record in the order and the address queue it belongs
// to. A record that names no complete address is filed in no queue: the pending
// queue the arrival order decided was deleted with the fixed target, so such a
// record is readable by no session and the warning names it instead.
func (in *internalInbox) index(rec *noticeRecord) {
	in.records[rec.ID] = rec
	in.order = append(in.order, rec.ID)
	addr := rec.addressOf()
	if !addr.IsComplete() {
		log.Warnf("[inbox] the record %s names no complete address; no session reads it", rec.ID)
		return
	}
	in.byAddr[addr] = append(in.byAddr[addr], rec.ID)
}

// InjectInternal admits one internal notice for the named address without a
// consumed marker. An address that is not complete is refused by name: a notice
// no session is named for has no reader, and the pending queue the arrival order
// decided is gone with the fixed target. A notice the inbox already holds under
// the same identity is not admitted twice.
func (in *internalInbox) InjectInternal(addr event.Address, source string, msg tagentmodel.Message) {
	if _, err := in.AdmitNotice(addr, source, msg, "", ""); err != nil {
		log.Errorf("[inbox] admit the %s notice for %s: %v", source, addr.SessionID, err)
	}
}

// AdmitNotice admits one internal notice for the named address and names the
// marker file whose consumed suffix is written when the notice is confirmed. The
// address is the fixed target the notice waits for, so an address that is not
// complete is refused by name instead of being admitted for whichever session
// reads first. A redelivery answers the record already in the inbox — the same
// marker for a startup notice, the same source beside the same body otherwise —
// so a notice relanded after a stop opens no second record.
func (in *internalInbox) AdmitNotice(addr event.Address, source string, msg tagentmodel.Message, marker, suffix string) (string, error) {
	if in == nil {
		return "", nil
	}
	if !addr.IsComplete() {
		return "", fmt.Errorf("%w: the %s notice names %v", ErrNoticeAddressIncomplete, source, addr)
	}
	if msg.Role == "" {
		msg.Role = tagentmodel.RoleUser
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	if id := in.find(source, msg.Content, marker); id != "" {
		log.Infof("[inbox] the %s notice is already admitted as %s; no second record", source, id)
		return id, nil
	}
	rec := &noticeRecord{
		ID:      fmt.Sprintf("%s-%s-%d", noticeSourceSlug(source), noticeDigest(msg.Content), in.seq+1),
		Source:  source,
		Seq:     in.seq + 1,
		Role:    msg.Role,
		Content: msg.Content,
		Marker:  marker,
		Suffix:  suffix,
		Agent:   string(addr.AgentID),
		Session: string(addr.SessionID),
	}
	if err := in.persist(rec); err != nil {
		return "", err
	}
	in.seq = rec.Seq
	in.index(rec)
	log.Infof("[inbox] admitted the %s notice for %s as %s", source, addr.SessionID, rec.ID)
	in.step("admitted")
	return rec.ID, nil
}

// find answers the record a redelivery answers: the same marker in the inbox, or
// the same source beside a body of the same digest.
func (in *internalInbox) find(source, content, marker string) string {
	if marker != "" {
		for _, id := range in.order {
			if rec := in.records[id]; rec != nil && rec.Marker == marker {
				return id
			}
		}
	}
	digest := noticeDigest(content)
	for _, id := range in.order {
		rec := in.records[id]
		if rec == nil || rec.Source != source {
			continue
		}
		if noticeDigest(rec.Content) == digest {
			return id
		}
	}
	return ""
}

// persist lands one admission on disk; an inbox without a directory keeps the
// record in memory alone. The record is written beside its final name and
// renamed into place, so a stop mid-write leaves no half record to read back.
func (in *internalInbox) persist(rec *noticeRecord) error {
	if in.dir == "" {
		return nil
	}
	body, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("wechat-bot: encode the notice record %s: %w", rec.ID, err)
	}
	path := in.path(rec.ID)
	if err := os.WriteFile(path+".tmp", body, 0o644); err != nil {
		return fmt.Errorf("wechat-bot: write the notice record %s: %w", rec.ID, err)
	}
	if err := os.Rename(path+".tmp", path); err != nil {
		return fmt.Errorf("wechat-bot: land the notice record %s: %w", rec.ID, err)
	}
	return nil
}

// path names the file one record is kept under.
func (in *internalInbox) path(id string) string {
	return filepath.Join(in.dir, id+noticeRecordSuffix)
}

// take returns the notices waiting for the named address as copies beside
// the records they were read under, and marks them read by this turn. The
// records stay admitted until Confirm retires them, so a stop between the read
// and the confirmation leaves the notice to be read again by the next turn.
func (in *internalInbox) take(addr event.Address) ([]tagentmodel.Message, []string) {
	if in == nil {
		return nil, nil
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	var out []tagentmodel.Message
	var ids []string
	collect := func(id string) {
		rec := in.records[id]
		if rec == nil || in.claimed[id] {
			return
		}
		role := rec.Role
		if role == "" {
			role = tagentmodel.RoleUser
		}
		out = append(out, tagentmodel.Message{Role: role, Content: rec.Content})
		in.claimed[id] = true
		ids = append(ids, id)
	}
	for _, id := range in.byAddr[addr] {
		collect(id)
	}
	if len(ids) > 0 {
		in.step("drained")
	}
	return out, ids
}

// sourceOf answers the source one admitted record names, so the material read
// from it carries that source into the facts of its turn instead of reading as
// one anonymous input beside the text of the delivery it came with.
func (in *internalInbox) sourceOf(id string) string {
	if in == nil {
		return ""
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	if rec := in.records[id]; rec != nil {
		return rec.Source
	}
	return ""
}

// Drain returns the notices waiting for the named address as copies. It retires
// nothing: the records stay admitted until Confirm, which is what keeps a notice
// owed across a failed or stopped turn. An incomplete address names no session
// and therefore reads nothing back.
func (in *internalInbox) Drain(addr event.Address) []tagentmodel.Message {
	msgs, _ := in.take(addr)
	return msgs
}

// Release hands back the notices a turn read but never handed over to the host:
// the records stay admitted and the next turn reads them again.
func (in *internalInbox) Release(ids []string) {
	if in == nil {
		return
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	for _, id := range ids {
		delete(in.claimed, id)
	}
	if len(ids) > 0 {
		in.step("released")
	}
}

// Confirm retires the records the named notices were read under. The consumed
// marker is renamed first and the record is removed only beside a marker that is
// already gone: a rename that fails keeps the notice admitted for the next turn
// instead of losing it, and a confirmation repeated after a stop retires the
// record instead of reporting the rename twice.
func (in *internalInbox) Confirm(ids []string) {
	if in == nil {
		return
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	for _, id := range ids {
		delete(in.claimed, id)
		rec := in.records[id]
		if rec == nil {
			continue
		}
		if !in.retireMarker(rec) {
			continue
		}
		in.remove(rec)
	}
}

// retireMarker renames the marker the notice was admitted for and reports
// whether the record may retire: a marker an earlier confirmation already
// renamed reads as done, because the consumed suffix is the only completion
// word this inbox writes.
func (in *internalInbox) retireMarker(rec *noticeRecord) bool {
	if rec.Marker == "" || rec.Suffix == "" {
		return true
	}
	if _, err := os.Stat(rec.Marker); err != nil {
		if os.IsNotExist(err) {
			return true
		}
		log.Warnf("[inbox] notice %s stays admitted: the marker %s is unreadable: %v", rec.ID, rec.Marker, err)
		return false
	}
	consumed := rec.Marker + rec.Suffix
	if err := os.Rename(rec.Marker, consumed); err != nil {
		log.Warnf("[inbox] notice %s stays admitted: the consumed marker rename failed: %v", rec.ID, err)
		return false
	}
	log.Infof("[inbox] notice %s renamed its marker → %s", rec.ID, consumed)
	in.step("marker-renamed")
	return true
}

// remove drops one confirmed record from the index and from the disk; a removal
// the disk refuses is reported, and the record then reads back on the next start
// rather than being lost.
func (in *internalInbox) remove(rec *noticeRecord) {
	delete(in.records, rec.ID)
	delete(in.claimed, rec.ID)
	for i, id := range in.order {
		if id == rec.ID {
			in.order = append(in.order[:i], in.order[i+1:]...)
			break
		}
	}
	addr := rec.addressOf()
	if addr.IsComplete() {
		in.byAddr[addr] = dropNoticeID(in.byAddr[addr], rec.ID)
		if len(in.byAddr[addr]) == 0 {
			delete(in.byAddr, addr)
		}
	}
	if in.dir != "" {
		if err := os.Remove(in.path(rec.ID)); err != nil && !os.IsNotExist(err) {
			log.Warnf("[inbox] the confirmed record %s was not removed from %s: %v", rec.ID, in.dir, err)
		}
	}
	log.Infof("[inbox] notice %s confirmed", rec.ID)
	in.step("retired")
}

// step hands one durable step to the fault-injection seam; production runs with
// no seam and reaches no branch here.
func (in *internalInbox) step(name string) {
	if in.observe != nil {
		in.observe(name)
	}
}

// dropNoticeID removes one identity from a queue and keeps the queue order.
func dropNoticeID(ids []string, id string) []string {
	for i, got := range ids {
		if got == id {
			return append(ids[:i], ids[i+1:]...)
		}
	}
	return ids
}

// noticeDigest is the body digest of the identity: two notices of one source
// with the same body answer the same identity and are admitted once.
func noticeDigest(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])[:12]
}

// noticeSourceSlug keeps the source readable in the record name: every byte a
// file name does not take becomes a dash.
func noticeSourceSlug(source string) string {
	slug := make([]byte, 0, len(source))
	for i := 0; i < len(source); i++ {
		c := source[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
			slug = append(slug, c)
		default:
			slug = append(slug, '-')
		}
	}
	if len(slug) == 0 {
		return "notice"
	}
	return string(slug)
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

// opsSessionLabel is the session label of the host's own ops session: the fixed
// target of the two startup notices when the document declares none.
const opsSessionLabel = "ops"

// opsSessionAddress reports the fixed target of the two startup notices: the chat
// identity the document declares as its ops session, spelled by the one rule
// every chat address of this host is spelled with — so a document that names one
// of its chats as its ops session lands the notices in that chat — or this host's
// own ops session when the document names none.
func opsSessionAddress(entry, declared string) event.Address {
	label := strings.TrimSpace(declared)
	if label == "" {
		label = opsSessionLabel
	}
	return event.Address{
		AgentID:   event.AgentID(entry),
		SessionID: event.SessionID(sessionPrefixOf(entry) + url.PathEscape(label)),
	}
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
	inbox, err := newInternalInbox(filepath.Join(cfg.App.Wechat.ConfigDir, noticeInboxDir))
	if err != nil {
		return nil, err
	}
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

// noticeWiring reports the fixed target and the scene face of the two startup
// notices: the ops session this document declares beside this host's own wake
// path, and the tail of the fact chain the assembled run observes. It is the one
// place that decision is made, so the notices can never start beside a nil scene
// face or beside a second wake path.
func (a *wechatApp) noticeWiring(host *assembly.Host) (noticeOps, tailQuerier) {
	return noticeOps{
		address: opsSessionAddress(a.cfg.Entry, a.cfg.App.Wechat.OpsSession),
		wake:    a.host,
	}, opsScene(host)
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
	ops, scene := a.noticeWiring(host)
	go maybeInjectReincarnationNotice(a.host.inbox, ops, scene, a.cfg.Entry, "run", noticeWaitMax)
	go maybeConsumeSystemAlert(a.host.inbox, ops, "run", 5*time.Second)
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
