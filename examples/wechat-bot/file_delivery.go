package main

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/SpellingDragon/wechat-robot-go/wechat"
	"trpc.group/trpc-go/trpc-agent-go/log"
)

// FileSender 是 wechat-bot 投递层依赖的窄接口。
// *wechat.Bot 天然满足该接口（方法签名一致），测试时可用 mock 替代，
// 从而脱离真实微信登录 / CDN / LLM 进行单元测试。
type FileSender interface {
	SendTextToUser(ctx context.Context, toUserID, text string) error
	SendImageFromPath(ctx context.Context, toUserID, imagePath string) error
	SendVoiceFromPath(ctx context.Context, toUserID, voicePath string, duration int) error
	SendVideoFromPath(ctx context.Context, toUserID, videoPath string) error
	SendFileFromPath(ctx context.Context, toUserID, filePath string) error
}

// 编译期断言：*wechat.Bot 满足 FileSender 接口（无需运行真实微信即可验证）。
var _ FileSender = (*wechat.Bot)(nil)

// executableExts 为拒绝发送的可执行文件扩展名（与权限位检查互补）。
// 硬性规则，不可配置（见 design.md D7）。
var executableExts = map[string]bool{
	".exe": true, ".dll": true, ".bat": true, ".cmd": true, ".sh": true,
	".bin": true, ".out": true, ".app": true, ".msi": true, ".com": true, ".run": true,
}

// pathCandidateRE 匹配以扩展名结尾的路径候选（绝对或相对）。
// 具体是否为真实文件由 os.Stat 二次校验，故误匹配（如版本号 v1.2、域名 example.com）
// 会被过滤；无目录信息的裸文件名（如 report.pdf）因不含 '/' 也不视为路径。
var pathCandidateRE = regexp.MustCompile(`[A-Za-z0-9_./-]+\.[A-Za-z0-9]{1,10}`)

// ExtractFilePaths 从 agent 回复文本中解析本地文件路径。
//
// 识别规则：
//  1. 匹配绝对路径（以 '/' 起头）或相对路径（含 '/' 或以 './'、'../' 起头）。
//  2. 排除以 '://' 开头的 URL（http/https/ftp 等）。
//  3. 候选必须以文件扩展名结尾（正则已保证）。
//  4. 必须通过 os.Stat 确认存在且为普通文件；相对路径先按 workspaceDir 解析为绝对路径
//     （workspaceDir 为空则跳过相对路径）。
//  5. 硬性排除可执行文件（具有任意可执行权限位或扩展名在拒绝列表中）。
//  6. 结果去重并保持首次出现顺序。
func ExtractFilePaths(text, workspaceDir string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, cand := range pathCandidateRE.FindAllString(text, -1) {
		if strings.Contains(cand, "://") {
			continue // 排除 URL
		}
		var abs string
		switch {
		case strings.HasPrefix(cand, "/"):
			abs = cand // 绝对路径
		case strings.HasPrefix(cand, "./"), strings.HasPrefix(cand, "../"), strings.Contains(cand, "/"):
			if workspaceDir == "" {
				continue // 未配置工作区，跳过相对路径
			}
			abs = filepath.Join(workspaceDir, cand)
		default:
			continue // 无目录信息的裸文件名不视为路径
		}
		info, err := os.Stat(abs)
		if err != nil {
			continue
		}
		if !info.Mode().IsRegular() {
			continue
		}
		if isExecutable(info, abs) {
			continue // 硬性排除可执行文件
		}
		if seen[abs] {
			continue
		}
		seen[abs] = true
		result = append(result, abs)
	}
	return result
}

// isExecutable 判断文件是否为可执行文件：具有任意可执行权限位，或扩展名在拒绝列表中。
func isExecutable(info os.FileInfo, path string) bool {
	if info.Mode()&0111 != 0 {
		return true
	}
	if executableExts[strings.ToLower(filepath.Ext(path))] {
		return true
	}
	return false
}

// fileSendFn 封装"按类型选择的具体发送动作"，便于 DeliverFiles 统一调用与测试。
type fileSendFn func(sender FileSender, ctx context.Context, toUserID, path string) error

// selectSendFn 根据扩展名选择微信发送接口。
// image/voice/video 分别使用对应接口（voice 的 duration 传 0，由 SDK/微信侧处理），
// 其余扩展名统一使用 SendFileFromPath。
func selectSendFn(ext string) fileSendFn {
	switch strings.ToLower(ext) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp":
		return func(s FileSender, ctx context.Context, toUserID, path string) error {
			return s.SendImageFromPath(ctx, toUserID, path)
		}
	case ".mp3", ".wav", ".amr", ".m4a":
		return func(s FileSender, ctx context.Context, toUserID, path string) error {
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

// DeliverFiles 将文本中解析出的本地文件按类型发送给用户。
//
// 文本发送（含 >2000 字长文本分片）由消费者负责，本函数仅负责文件投递，
// 以保证既有 SendLongText 逻辑不被破坏（SendLongText 为包级函数，无法纳入 FileSender 接口）。
// 任一文件发送失败仅记录日志并继续，不阻断其余文件。
func DeliverFiles(sender FileSender, ctx context.Context, chatID, content, workspaceDir string) error {
	for _, p := range ExtractFilePaths(content, workspaceDir) {
		fn := selectSendFn(filepath.Ext(p))
		if err := fn(sender, ctx, chatID, p); err != nil {
			log.Errorf("发送文件失败 %s: %v", p, err)
		}
	}
	return nil
}
