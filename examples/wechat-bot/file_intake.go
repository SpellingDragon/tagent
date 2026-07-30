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

	"github.com/SpellingDragon/wechat-robot-go/wechat"
	"trpc.group/trpc-go/trpc-agent-go/log"
)

// 入站文件接收（file intake）：file_delivery.go 的镜像方向。
// 用户微信发来的附件/超长文本 → 落盘 workspace uploads 目录 → 以相对路径注入 agent。

const (
	uploadsSubdir = "uploads"

	// maxInboundFileSize 为入站单文件上限。容器 workspace 为 256m tmpfs，
	// 且 SDK 下载为全量 []byte，上限过大会同时威胁磁盘与内存（见 design.md D8）。
	maxInboundFileSize = 50 * 1024 * 1024

	// longTextThreshold 超过该字符数（rune 计）的入站文本转存为 .txt 文件。
	longTextThreshold = 1000

	// previewRunes 长文本转文件后注入的预览字符数。
	previewRunes = 200
)

// MediaDownloader 是入站接收层依赖的窄接口（流式：SDK 边下边解密写入 Writer，
// MaxSize 由 SDK 在传输中强制）。*wechat.Bot 天然满足该接口，测试时可用 mock
// 替代（与 FileSender 同一模式）。
type MediaDownloader interface {
	DownloadImageFromItemTo(ctx context.Context, cdnBaseURL string, img *wechat.ImageItem, w io.Writer, opts wechat.DownloadOptions) (int64, error)
	DownloadVoiceTo(ctx context.Context, voice *wechat.VoiceItem, cdnBaseURL string, w io.Writer, opts wechat.DownloadOptions) (int64, error)
	DownloadFileFromItemTo(ctx context.Context, file *wechat.FileItem, cdnBaseURL string, w io.Writer, opts wechat.DownloadOptions) (int64, error)
	DownloadVideoFromItemTo(ctx context.Context, video *wechat.VideoItem, cdnBaseURL string, w io.Writer, opts wechat.DownloadOptions) (int64, error)
}

// 编译期断言：*wechat.Bot 满足 MediaDownloader 接口。
var _ MediaDownloader = (*wechat.Bot)(nil)

// SavedFile 描述一个已落盘的入站文件。
type SavedFile struct {
	Path     string // workspace 相对路径（相对进程 cwd，agent 可直接读取）
	OrigName string // 原始文件名（清洗前，供展示）
	Size     int    // 明文字节数
}

// IntakeOutcome 是一条媒体消息的接收结果。
type IntakeOutcome struct {
	Saved      []SavedFile
	Transcript string   // 语音服务端转写文本（如有）
	Rejects    []string // 面向用户的拒绝/失败原因（不注入 agent）
}

// InboundKind 是入站消息的处理分类。
type InboundKind int

const (
	InboundIgnore    InboundKind = iota // 无文本无媒体，丢弃
	InboundShortText                    // 文本 ≤ 阈值，走既有直接注入路径
	InboundLongText                     // 文本 > 阈值，转文件注入
	InboundMedia                        // 含媒体 item
)

// ClassifyInbound 对入站消息分类。纯函数，便于单测。
// workspaceConfigured 为 false 时长文本降级为 ShortText（保持现状行为）。
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

// sanitizeName 将不可信文件名/用户 ID 清洗为安全路径段：
// 仅保留 [A-Za-z0-9._-]，去除路径分隔符与 ".."，剥掉前导点（防隐藏文件与相对段）。
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
// 不做存在性探测——冲突由 createExclusive 以 O_EXCL 原子解决。
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
// 原子创建消除 Stat-then-Write 的 TOCTOU 竞态（并发同秒同名不会互相覆盖）。
func createExclusive(path string) (*os.File, string, error) {
	ext := filepath.Ext(path)
	stem := strings.TrimSuffix(path, ext)
	candidate := path
	for i := 1; ; i++ {
		f, err := os.OpenFile(candidate, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if err == nil {
			return f, candidate, nil
		}
		if !os.IsExist(err) {
			return nil, "", err
		}
		candidate = stem + "-" + strconv.Itoa(i) + ext
	}
}

// saveInboundFile 校验并落盘一个入站文件，返回 workspace 相对路径。
// 拒绝可执行扩展名；复核实际字节数；权限 0644（无执行位）；
// 写入失败时删除残留的不完整文件。
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
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return SavedFile{}, fmt.Errorf("create upload dir: %w", err)
	}
	f, finalPath, err := createExclusive(path)
	if err != nil {
		return SavedFile{}, fmt.Errorf("create upload file: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(finalPath) // 清理不完整残留（如 tmpfs 写满 ENOSPC）
		return SavedFile{}, fmt.Errorf("write upload file: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(finalPath)
		return SavedFile{}, fmt.Errorf("close upload file: %w", err)
	}
	return SavedFile{Path: finalPath, OrigName: origName, Size: len(data)}, nil
}

// downloadSem 限制全局并发下载数（带宽保护/CDN 友好）。
// 内存风险已由 SDK 流式下载 + MaxSize 闸门消除，此处仅约束出口带宽占用。
var downloadSem = make(chan struct{}, 2)

// IntakeMedia 处理一条含媒体 item 的消息：逐 item 预判大小（体验优化，超限免下载）
// → O_EXCL 创建目标文件 → SDK 流式下载直落盘（MaxSize 传输中强制）。
// 单 item 失败/被拒不阻断其余 item；原因归入 Rejects（面向用户，不注入 agent）；
// 任何失败路径删除不完整残留文件。
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
			continue // 文本 item 由调用方处理
		}

		// 可执行扩展名在下载前拒绝（省流量）；声明大小超限免下载（体验优化，
		// 元数据缺失 declared=0 时正常接收，SDK 流式限额兜底）。
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
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
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
			os.Remove(finalPath) // 中断/失败时清理不完整残留
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

// SaveLongText 将超长文本落盘为 .txt，返回保存结果与注入文本
// （相对路径 + 前 previewRunes 字预览 + 总字数）。
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
// 转写超过阈值时同样转存文件（走 SaveLongText）。
// 无可注入内容（全部被拒且无转写无附言）时返回空串。
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
