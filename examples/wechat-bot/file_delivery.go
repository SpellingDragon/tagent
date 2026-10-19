// 契约: docs/wiki/examples/wechat-bot-runtime.md#outbound-delivery
package main

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/SpellingDragon/tagent/api/event"
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
// 硬性规则，不可配置。
var executableExts = map[string]bool{
	".exe": true, ".dll": true, ".bat": true, ".cmd": true, ".sh": true,
	".bin": true, ".out": true, ".app": true, ".msi": true, ".com": true, ".run": true,
}

// pathCandidateRE 匹配以扩展名结尾的路径候选（绝对或相对）。
// 字符类使用 \p{L}（任意 Unicode 字母，含中文/日文/韩文等）与 \p{N}（任意 Unicode 数字），
// 以正确识别中文等非 ASCII 文件名（如 彭伟业_面试备战文档.md）；原仅 ASCII 的字符类会导致
// 中文路径被静默截断、文件投递失败。具体是否为真实文件由 os.Stat 二次校验，故误匹配
// （如版本号 v1.2、域名 example.com）会被过滤；无目录信息的裸文件名
// 因不含 '/' 也不视为路径。'-' 置于字符类末尾以避免被当作范围操作符。
var pathCandidateRE = regexp.MustCompile(`[\p{L}\p{N}_./-]+\.[\p{L}\p{N}-]{1,10}`)

// executableRefusal 是拒绝投递可执行文件的固定规则：可执行权限位或拒绝扩展名命中即拒绝，
// 没有任何配置面能放宽它。
const executableRefusal = "可执行文件不投递（权限位或拒绝扩展名命中，硬规则不可配置）"

// fileCandidate 是回复文本命名的一个本地文件，连同决定它能否投递的规则：
// refusal 为空表示可投递，非空表示宿主在发送前按具名规则拒绝。
// 契约: docs/wiki/examples/wechat-bot-runtime.md#outbound-delivery
type fileCandidate struct {
	Path    string
	Refusal string
}

// classifyFilePaths 按六条识别规则读出回复文本命名的本地文件（本地路径形态、排除 URL、
// 必须带扩展名、必须存在且为普通文件、拒绝可执行、去重保序），保持首次出现顺序。
// 通过前四条并被判为可执行的文件保留路径与拒绝理由，使拒绝本身可见而不是被静默丢弃。
// 契约: docs/wiki/examples/wechat-bot-runtime.md#outbound-delivery
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
		if err != nil {
			continue
		}
		if !info.Mode().IsRegular() {
			continue
		}
		if seen[abs] {
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

// ExtractFilePaths 从 agent 回复文本中解析本地文件路径：六条识别规则（本地路径形态、
// 排除 URL、必须带扩展名、必须存在且为普通文件、拒绝可执行、去重保序）全部成立才算。
// 契约: docs/wiki/examples/wechat-bot-runtime.md#outbound-delivery
func ExtractFilePaths(text, workspaceDir string) []string {
	var result []string
	for _, cand := range classifyFilePaths(text, workspaceDir) {
		if cand.Refusal == "" {
			result = append(result, cand.Path)
		}
	}
	return result
}

// voiceMaxBytes is the 5MB cap for audio sent over the voice channel:
// audio larger than this falls back to a plain file message (voice CDN rejects long audio).
const voiceMaxBytes = 5 << 20

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
// 除 Delivered 之外的每一项都是必须回流 agent 的投递终态，空的账目表示这条回复没有命名本地文件。
type fileDeliveryReport struct {
	Delivered []string
	Refused   []fileRefusal
	Failed    []fileFailure
}

// deliveryConsumer 标注本宿主的投递副本使用者：副本路径由它与原文引用共同派生。
const deliveryConsumer = "wechat-delivery"

// fileDelivery 是一次回复的出海文件工作：读路径的正文、投递到的会话、唯一的出网发送面，
// 以及大结果副本所在的 workspace 生命周期（未接线的宿主传 nil，文件按原样投递）。
//
// 文本投递由调用方负责，因此任一文件失败都不阻断文本；一个文件失败也不阻断其余文件。
type fileDelivery struct {
	sender        FileSender
	chatID        string
	content       string
	workspaceDir  string
	addr          event.Address
	triggerSource string
	receipts      receiptInjector
	replicas      resultReplicaSource
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

// sendFile 把一个文件交给通道：大结果交给它的 workspace 副本路径，投递期间该副本由活跃保护
// 覆盖；其余文件在原位交给通道。
func (d fileDelivery) sendFile(ctx context.Context, path string) error {
	sendPath, release, err := d.deliveryPath(ctx, path)
	if err != nil {
		return err
	}
	defer release()
	return selectSendFn(filepath.Ext(sendPath))(d.sender, ctx, d.chatID, sendPath)
}

// deliveryPath 报告一个文件交给通道时的路径，连同覆盖它的保护的释放动作。
func (d fileDelivery) deliveryPath(ctx context.Context, path string) (string, func(), error) {
	noRelease := func() {}
	if d.replicas == nil {
		return path, noRelease, nil
	}
	copied, handled, err := d.replicas.OpenReplica(ctx, path, deliveryConsumer)
	if err != nil {
		return "", noRelease, err
	}
	if !handled {
		return path, noRelease, nil
	}
	if copied.Release == nil {
		return copied.Path, noRelease, nil
	}
	return copied.Path, copied.Release, nil
}

// DeliverFiles 将文本中解析出的本地文件按类型发送给用户。
//
// 文本发送（含 >2000 字长文本分片）由消费者负责，本函数仅负责文件投递，
// 以保证既有 SendLongText 逻辑不被破坏（SendLongText 为包级函数，无法纳入 FileSender 接口）。
// 任一文件发送失败仅记录日志并继续，不阻断其余文件。
func DeliverFiles(sender FileSender, ctx context.Context, chatID, content, workspaceDir string) error {
	fileDelivery{
		sender:       sender,
		chatID:       chatID,
		content:      content,
		workspaceDir: workspaceDir,
	}.deliver(ctx)
	return nil
}
