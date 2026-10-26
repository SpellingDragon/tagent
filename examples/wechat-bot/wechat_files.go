// wechat_files.go 是微信文件面：入站媒体的分类、落盘与注入文本，出站回复命名的
// 本地文件投递，以及两条路径共用的命名与大小规则。全部是微信协议语义的一部分，
// 与宿主装配无关。
//
// 契约: docs/wiki/examples/wechat-bot-runtime.md#module-layout
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/wechat-robot-go/wechat"
	"trpc.group/trpc-go/trpc-agent-go/log"
)

// executableExts 为拒绝发送的可执行文件扩展名（与权限位检查互补）。硬性规则，不可配置。
var executableExts = map[string]bool{
	".exe": true, ".dll": true, ".bat": true, ".cmd": true, ".sh": true,
	".bin": true, ".out": true, ".app": true, ".msi": true, ".com": true, ".run": true,
}

// pathCandidateRE 匹配以扩展名结尾的路径候选（绝对或相对）。字符类使用 \p{L}/\p{N}
// 以正确识别中文等非 ASCII 文件名；具体是否为真实文件由 os.Stat 二次校验。
var pathCandidateRE = regexp.MustCompile(`[\p{L}\p{N}_./-]+\.[\p{L}\p{N}-]{1,10}`)

// executableRefusal 是拒绝投递可执行文件的固定规则：可执行权限位或拒绝扩展名命中即拒绝，
// 没有任何配置面能放宽它。
const executableRefusal = "可执行文件不投递（权限位或拒绝扩展名命中，硬规则不可配置）"

// fileCandidate 是回复文本命名的一个本地文件，连同决定它能否投递的规则：
// refusal 为空表示可投递，非空表示宿主在发送前按具名规则拒绝。
type fileCandidate struct {
	Path    string
	Refusal string
}

// classifyFilePaths 按六条识别规则读出回复文本命名的本地文件（本地路径形态、排除 URL、
// 必须带扩展名、必须存在且为普通文件、拒绝可执行、去重保序），保持首次出现顺序。
func classifyFilePaths(text, workspaceDir string) []fileCandidate {
	seen := make(map[string]bool)
	var result []fileCandidate
	for _, cand := range pathCandidateRE.FindAllString(text, -1) {
		if strings.Contains(cand, "://") {
			continue
		}
		var abs string
		switch {
		case strings.HasPrefix(cand, "/"):
			abs = cand
		case strings.HasPrefix(cand, "./"), strings.HasPrefix(cand, "../"), strings.Contains(cand, "/"):
			if workspaceDir == "" {
				continue
			}
			abs = filepath.Join(workspaceDir, cand)
		default:
			continue
		}
		info, err := os.Stat(abs)
		if err != nil || !info.Mode().IsRegular() || seen[abs] {
			continue
		}
		seen[abs] = true
		candidate := fileCandidate{Path: abs}
		if isExecutable(info, abs) {
			candidate.Refusal = executableRefusal
		}
		result = append(result, candidate)
	}
	return result
}

// voiceMaxBytes is the 5MB cap for audio sent over the voice channel:
// audio larger than this falls back to a plain file message (voice CDN rejects long audio).
const voiceMaxBytes = 5 << 20

// isExecutable 判断文件是否为可执行文件：具有任意可执行权限位，或扩展名在拒绝列表中。
func isExecutable(info os.FileInfo, path string) bool {
	if info.Mode()&0o111 != 0 {
		return true
	}
	return executableExts[strings.ToLower(filepath.Ext(path))]
}

// fileSendFn 封装"按类型选择的具体发送动作"，便于统一调用与测试。
type fileSendFn func(sender FileSender, ctx context.Context, toUserID, path string) error

// selectSendFn 根据扩展名选择微信发送接口。voice 的 duration 传 0，由 SDK/微信侧处理；
// 超过 5MB 的音频回退为普通文件消息。
func selectSendFn(ext string) fileSendFn {
	switch strings.ToLower(ext) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp":
		return func(s FileSender, ctx context.Context, toUserID, path string) error {
			return s.SendImageFromPath(ctx, toUserID, path)
		}
	case ".mp3", ".wav", ".amr", ".m4a":
		return func(s FileSender, ctx context.Context, toUserID, path string) error {
			if info, err := os.Stat(path); err == nil && info.Size() > voiceMaxBytes {
				return s.SendFileFromPath(ctx, toUserID, path)
			}
			return s.SendVoiceFromPath(ctx, toUserID, path, 0)
		}
	case ".mp4", ".mov", ".avi", ".webm", ".mkv":
		return func(s FileSender, ctx context.Context, toUserID, path string) error {
			return s.SendVideoFromPath(ctx, toUserID, path)
		}
	default:
		return func(s FileSender, ctx context.Context, toUserID, path string) error {
			return s.SendFileFromPath(ctx, toUserID, path)
		}
	}
}

// fileRefusal 是宿主在发送前按具名规则拒绝投递的一个已识别文件：路径与拒绝理由。
type fileRefusal struct {
	Path   string
	Reason string
}

// fileFailure 是通道拒绝投递的一个文件，连同它给出的原因。
type fileFailure struct {
	Path string
	Err  error
}

// fileDeliveryReport 是一次回复的文件投递账目：已送达、宿主按具名规则拒绝、通道投递失败。
type fileDeliveryReport struct {
	Delivered []string
	Refused   []fileRefusal
	Failed    []fileFailure
}

// fileDelivery 是一次回复的出海文件工作：读路径的正文、投递到的会话、唯一的出网发送面。
// 文本投递由调用方负责，因此任一文件失败都不阻断文本；一个文件失败也不阻断其余文件。
type fileDelivery struct {
	sender        FileSender
	chatID        string
	content       string
	workspaceDir  string
	addr          event.Address
	triggerSource string
	receipts      receiptInjector
}

// deliver 投递回复命名的每个文件并记下每一笔拒绝与失败：拒绝在发送前判定，
// 失败连同原因入账且循环继续。
func (d fileDelivery) deliver(ctx context.Context) fileDeliveryReport {
	report := fileDeliveryReport{}
	for _, cand := range classifyFilePaths(d.content, d.workspaceDir) {
		if cand.Refusal != "" {
			report.Refused = append(report.Refused, fileRefusal{Path: cand.Path, Reason: cand.Refusal})
			emitReceipt(d.receipts, d.addr, "file-denied", "WARN", d.triggerSource, d.chatID, cand.Refusal+"："+cand.Path)
			continue
		}
		if err := d.sendFile(ctx, cand.Path); err != nil {
			log.Errorf("发送文件失败 %s: %v", cand.Path, err)
			report.Failed = append(report.Failed, fileFailure{Path: cand.Path, Err: err})
			emitReceipt(d.receipts, d.addr, "file-failed", "ERROR", d.triggerSource, d.chatID, err.Error()+"："+cand.Path)
			continue
		}
		report.Delivered = append(report.Delivered, cand.Path)
	}
	return report
}

// sendFile 把一个文件交给通道：按扩展名选择图片/语音/视频/文件接口。
func (d fileDelivery) sendFile(ctx context.Context, path string) error {
	return selectSendFn(filepath.Ext(path))(d.sender, ctx, d.chatID, path)
}

const (
	uploadsSubdir = "uploads"

	// maxInboundFileSize 为入站单文件上限。容器 workspace 为 256m tmpfs，
	// 且 SDK 下载为全量 []byte，上限过大会同时威胁磁盘与内存。
	maxInboundFileSize = 50 * 1024 * 1024

	// longTextThreshold 超过该字符数（rune 计）的入站文本转存为 .txt 文件。
	longTextThreshold = 1000

	// previewRunes 长文本转文件后注入的预览字符数。
	previewRunes = 200
)

// SavedFile 描述一个已落盘的入站文件。
type SavedFile struct {
	Path     string
	OrigName string
	Size     int
}

// IntakeOutcome 是一条媒体消息的接收结果。
type IntakeOutcome struct {
	Saved      []SavedFile
	Transcript string
	Rejects    []string
}

// InboundKind 是入站消息的处理分类。
type InboundKind int

const (
	// InboundIgnore 文本为空且非媒体，不进入会话。
	InboundIgnore InboundKind = iota
	// InboundShortText 其余文本：未超过长文本阈值，或工作区未配置时的降级归类。
	InboundShortText
	// InboundLongText 文本 rune 数超过 longTextThreshold，且 workspaceConfigured 为真。
	InboundLongText
	// InboundMedia 消息含图片/语音/文件/视频之一。
	InboundMedia
)

// ClassifyInbound 对入站消息分类。纯函数。workspaceConfigured 为 false 时长文本降级为
// ShortText（保持现状行为）。
func ClassifyInbound(msg *wechat.Message, workspaceConfigured bool) InboundKind {
	if msg.IsImage() || msg.IsVoice() || msg.IsFile() || msg.IsVideo() {
		return InboundMedia
	}
	text := msg.Text()
	if text == "" {
		return InboundIgnore
	}
	if workspaceConfigured && utf8.RuneCountInString(text) > longTextThreshold {
		return InboundLongText
	}
	return InboundShortText
}

// unsafeNameChars 匹配文件名中需要替换的字符（白名单之外的一切）。
var unsafeNameChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// sanitizeName 将不可信文件名/用户 ID 清洗为安全路径段：仅保留 [A-Za-z0-9._-]，
// 去除路径分隔符与 ".."，剥掉前导点（防隐藏文件与相对段）。
func sanitizeName(name string) string {
	name = unsafeNameChars.ReplaceAllString(name, "_")
	for strings.Contains(name, "..") {
		name = strings.ReplaceAll(name, "..", ".")
	}
	name = strings.TrimLeft(name, ".")
	if name == "" || name == "_" {
		name = "file"
	}
	return name
}

// buildSavePath 构造落盘路径 {workspaceDir}/uploads/{chatID}/{ts}_{name}，
// 规范化后强制校验仍位于 uploads 目录内（防穿越，双保险）。
func buildSavePath(workspaceDir, chatID, name string, now time.Time) (string, error) {
	base := filepath.Clean(filepath.Join(workspaceDir, uploadsSubdir))
	stamped := now.Format("20060102-150405") + "_" + sanitizeName(name)
	path := filepath.Join(base, sanitizeName(chatID), stamped)
	if !strings.HasPrefix(filepath.Clean(path), base+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe path escapes uploads dir: %q", name)
	}
	return path, nil
}

// createExclusive 以 O_CREATE|O_EXCL 原子创建文件，同名冲突时追加序号重试。
func createExclusive(path string) (*os.File, string, error) {
	ext := filepath.Ext(path)
	stem := strings.TrimSuffix(path, ext)
	candidate := path
	for i := 1; ; i++ {
		f, err := os.OpenFile(candidate, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			return f, candidate, nil
		}
		if !os.IsExist(err) {
			return nil, "", err
		}
		candidate = stem + "-" + strconv.Itoa(i) + ext
	}
}

// saveInboundFile 校验并落盘一个入站文件。拒绝可执行扩展名；复核实际字节数；
// 权限 0644（无执行位）；写入失败时删除残留的不完整文件。
func saveInboundFile(workspaceDir, chatID, origName string, data []byte, now time.Time) (SavedFile, error) {
	if executableExts[strings.ToLower(filepath.Ext(origName))] {
		return SavedFile{}, fmt.Errorf("可执行文件不允许接收: %s", origName)
	}
	if len(data) > maxInboundFileSize {
		return SavedFile{}, fmt.Errorf("文件超过大小上限 %s: %s (%s)",
			formatSize(maxInboundFileSize), origName, formatSize(len(data)))
	}
	path, err := buildSavePath(workspaceDir, chatID, origName, now)
	if err != nil {
		return SavedFile{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return SavedFile{}, fmt.Errorf("create upload dir: %w", err)
	}
	f, finalPath, err := createExclusive(path)
	if err != nil {
		return SavedFile{}, fmt.Errorf("create upload file: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(finalPath)
		return SavedFile{}, fmt.Errorf("write upload file: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(finalPath)
		return SavedFile{}, fmt.Errorf("close upload file: %w", err)
	}
	return SavedFile{Path: finalPath, OrigName: origName, Size: len(data)}, nil
}

// downloadSem 限制全局并发下载数（带宽保护/CDN 友好）。
var downloadSem = make(chan struct{}, 2)

// IntakeMedia 处理一条含媒体 item 的消息：逐 item 预判大小（超限免下载）→ O_EXCL 创建目标文件
// → SDK 流式下载直落盘（MaxSize 传输中强制）。单 item 失败/被拒不阻断其余 item；
// 原因归入 Rejects（面向用户，不注入 agent）；任何失败路径删除不完整残留文件。
func IntakeMedia(ctx context.Context, dl MediaDownloader, cdnBaseURL, workspaceDir, chatID string, msg *wechat.Message, now time.Time) IntakeOutcome {
	var out IntakeOutcome
	opts := wechat.DownloadOptions{MaxSize: maxInboundFileSize}
	for _, item := range msg.ItemList {
		var (
			origName string
			declared int64
			download func(w io.Writer) (int64, error)
		)
		switch {
		case item.Type == wechat.ItemTypeImage && item.ImageItem != nil:
			img := item.ImageItem
			origName = "image.jpg"
			declared = img.DeclaredSize()
			download = func(w io.Writer) (int64, error) {
				return dl.DownloadImageFromItemTo(ctx, cdnBaseURL, img, w, opts)
			}
		case item.Type == wechat.ItemTypeVoice && item.VoiceItem != nil:
			voice := item.VoiceItem
			out.Transcript = voice.Text
			origName = "voice.silk"
			declared = voice.DeclaredSize()
			download = func(w io.Writer) (int64, error) {
				return dl.DownloadVoiceTo(ctx, voice, cdnBaseURL, w, opts)
			}
		case item.Type == wechat.ItemTypeFile && item.FileItem != nil:
			file := item.FileItem
			origName = file.FileName
			if origName == "" {
				origName = "attachment.dat"
			}
			declared = file.DeclaredSize()
			download = func(w io.Writer) (int64, error) {
				return dl.DownloadFileFromItemTo(ctx, file, cdnBaseURL, w, opts)
			}
		case item.Type == wechat.ItemTypeVideo && item.VideoItem != nil:
			video := item.VideoItem
			origName = "video.mp4"
			declared = video.DeclaredSize()
			download = func(w io.Writer) (int64, error) {
				return dl.DownloadVideoFromItemTo(ctx, video, cdnBaseURL, w, opts)
			}
		default:
			continue
		}

		if executableExts[strings.ToLower(filepath.Ext(origName))] {
			out.Rejects = append(out.Rejects, fmt.Sprintf("已拒收可执行文件: %s", origName))
			continue
		}
		if declared > maxInboundFileSize {
			out.Rejects = append(out.Rejects, fmt.Sprintf("文件 %s 超过大小上限 %s（%s），未接收",
				origName, formatSize(maxInboundFileSize), formatSize(int(declared))))
			continue
		}

		path, err := buildSavePath(workspaceDir, chatID, origName, now)
		if err != nil {
			log.Errorf("[Intake] 构造路径失败 %s (chat=%s): %v", origName, chatID, err)
			out.Rejects = append(out.Rejects, fmt.Sprintf("附件 %s 保存失败: %v", origName, err))
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			log.Errorf("[Intake] 创建目录失败 %s (chat=%s): %v", origName, chatID, err)
			out.Rejects = append(out.Rejects, fmt.Sprintf("附件 %s 保存失败: %v", origName, err))
			continue
		}
		f, finalPath, err := createExclusive(path)
		if err != nil {
			log.Errorf("[Intake] 创建文件失败 %s (chat=%s): %v", origName, chatID, err)
			out.Rejects = append(out.Rejects, fmt.Sprintf("附件 %s 保存失败: %v", origName, err))
			continue
		}

		downloadSem <- struct{}{}
		written, err := download(f)
		<-downloadSem
		if err != nil {
			f.Close()
			os.Remove(finalPath)
			if errors.Is(err, wechat.ErrMaxSizeExceeded) {
				out.Rejects = append(out.Rejects, fmt.Sprintf("文件 %s 超过大小上限 %s，未接收",
					origName, formatSize(maxInboundFileSize)))
			} else {
				log.Errorf("[Intake] 下载失败 %s (chat=%s): %v", origName, chatID, err)
				out.Rejects = append(out.Rejects, fmt.Sprintf("附件 %s 下载失败，请重新发送", origName))
			}
			continue
		}
		if err := f.Close(); err != nil {
			os.Remove(finalPath)
			log.Errorf("[Intake] 落盘失败 %s (chat=%s): %v", origName, chatID, err)
			out.Rejects = append(out.Rejects, fmt.Sprintf("附件 %s 保存失败: %v", origName, err))
			continue
		}
		out.Saved = append(out.Saved, SavedFile{Path: finalPath, OrigName: origName, Size: int(written)})
	}
	return out
}

// SaveLongText 将超长文本落盘为 .txt，返回保存结果与注入文本。
func SaveLongText(workspaceDir, chatID, text, nameHint string, now time.Time) (SavedFile, string, error) {
	if nameHint == "" {
		nameHint = "input"
	}
	saved, err := saveInboundFile(workspaceDir, chatID, nameHint+".txt", []byte(text), now)
	if err != nil {
		return SavedFile{}, "", err
	}
	runes := []rune(text)
	preview := string(runes[:min(len(runes), previewRunes)])
	inject := fmt.Sprintf("[用户输入较长，已存为文件]\n文件: %s (共 %d 字)\n预览: %s...",
		saved.Path, len(runes), preview)
	return saved, inject, nil
}

// ComposeMediaInject 拼装媒体消息的注入文本：附件路径列表 + 语音转写 + 用户附言。
// 转写超过阈值时同样转存文件（走 SaveLongText）。无可注入内容时返回空串。
func ComposeMediaInject(outcome IntakeOutcome, userText, workspaceDir, chatID string, now time.Time) string {
	var lines []string
	if len(outcome.Saved) > 0 {
		lines = append(lines, "[用户发来附件]")
		for _, f := range outcome.Saved {
			lines = append(lines, fmt.Sprintf("文件: %s (原名: %s, %s)", f.Path, f.OrigName, formatSize(f.Size)))
		}
	}
	if outcome.Transcript != "" {
		if utf8.RuneCountInString(outcome.Transcript) > longTextThreshold {
			if _, inject, err := SaveLongText(workspaceDir, chatID, outcome.Transcript, "voice_transcript", now); err == nil {
				lines = append(lines, "语音转写较长，已存为文件:", inject)
			} else {
				log.Errorf("[Intake] 转写落盘失败 (chat=%s): %v", chatID, err)
				lines = append(lines, "语音转写: "+outcome.Transcript)
			}
		} else {
			lines = append(lines, "语音转写: "+outcome.Transcript)
		}
	}
	if userText != "" {
		lines = append(lines, userText)
	}
	return strings.Join(lines, "\n")
}

// formatSize 人类可读的字节数。
func formatSize(n int) string {
	switch {
	case n >= 1024*1024:
		return fmt.Sprintf("%.1fMB", float64(n)/(1024*1024))
	case n >= 1024:
		return fmt.Sprintf("%.1fKB", float64(n)/1024)
	default:
		return fmt.Sprintf("%dB", n)
	}
}
