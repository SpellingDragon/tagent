// wechat_notice.go 是两条一次性启动通知：保险链换装的转世通报与上一世重启失败的系统告警。
// 两者都把一条内部消息塞进宿主的收件箱，让第一回合读到；传输是微信宿主的启动语义，
// 与装配无关。
//
// 契约: docs/wiki/examples/wechat-bot-runtime.md#module-layout
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	tagentmodel "github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/memory"
	"trpc.group/trpc-go/trpc-agent-go/log"
)

const (
	// restartDoneFreshWindow 是新鲜度窗：标记文件新于此窗内的启动才算转世，否则按冷启动处理。
	restartDoneFreshWindow = 10 * time.Minute

	// walTailEventLimit 与 walTailSummaryMaxChars 是通报的令牌预算：尾现场块只取最新的有限条、
	// 每条摘要截断，绝不带事件全文——通报要进上下文，不是日志。
	walTailEventLimit      = 12
	walTailSummaryMaxChars = 160

	// noticeConsumedSuffix 是消费标记的重命名后缀。
	noticeConsumedSuffix = ".notified"

	// systemAlertConsumedSuffix 是系统告警消费标记的重命名后缀。
	systemAlertConsumedSuffix = ".consumed"
)

// inboxInjector 是两条启动通知共用到的最小接口面：把一条通报交给宿主的内部收件箱。
type inboxInjector interface {
	InjectInternal(addr event.Address, source string, msg tagentmodel.Message)
}

// detectReincarnation 判定本次启动是否为转世：只看换装标记是否存在且足够新鲜。标记由保险链在
// 派生新进程之前写好，故无竞态；冷启动从不产生标记，故不会假阳性。
func detectReincarnation(noticePath string, now time.Time) bool {
	st, err := os.Stat(noticePath)
	if err != nil {
		return false
	}
	return now.Sub(st.ModTime()) <= restartDoneFreshWindow
}

// readNoticeMetadata 解析标记的键值档案；缺失或读不到返回 nil，由通报正文显式标注降级（不静默）。
func readNoticeMetadata(path string) map[string]string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	meta := make(map[string]string)
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if i := strings.Index(line, ":"); i > 0 {
			k := strings.TrimSpace(line[:i])
			v := strings.TrimSpace(line[i+1:])
			if k != "" {
				meta[k] = v
			}
		}
	}
	if len(meta) == 0 {
		return nil
	}
	return meta
}

// tailQuerier 只声明取尾事件真正用到的能力：完整存储与测试替身都无需实现一堆无关方法即可满足。
type tailQuerier interface {
	QueryEvents(ctx context.Context, opts memory.QueryOptions) ([]memory.EventReference, error)
}

// fetchWALTail 按时间倒序取该 agent 最新的有限条事件；查询错误一律上抛以走显式降级。
func fetchWALTail(ctx context.Context, store tailQuerier, agentName string, limit int) ([]memory.EventReference, error) {
	if store == nil {
		return nil, fmt.Errorf("memstore unavailable")
	}
	pid := memory.PartitionIDFromName(agentName)
	return store.QueryEvents(ctx, memory.QueryOptions{
		PartitionIDs: []int{pid},
		Limit:        limit,
		OrderBy:      memory.QueryOrderByDesc,
	})
}

// hasOpenBreakpoint 判断上一世是否中断于回合中途：事件按新到旧取回，最新一条即最后动作。
func hasOpenBreakpoint(refs []memory.EventReference) bool {
	if len(refs) == 0 {
		return false
	}
	switch refs[0].Type {
	case "thinking_plan", "action_command":
		return true
	default:
		return false
	}
}

// buildNoticeText 组装通报三段：元数据、尾现场块、断点判定。降级阶梯每一级都必须在正文里
// 显式写出，不得用一份看起来完整的通报掩盖数据缺失。
func buildNoticeText(meta map[string]string, refs []memory.EventReference, walErr error) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[转世通报] 检测到保险链自替换换装（D1 命中：REINCARNATION_NOTICE 新鲜；PID 详情见元数据段）\n\n")

	b.WriteString("== 元数据 ==\n")
	if meta == nil {
		b.WriteString("（NOTICE 档案缺失——降级：检测依据 NOTICE 自身 mtime，详情查 logs/restart.log）\n")
	} else {
		keys := make([]string, 0, len(meta))
		for k := range meta {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(&b, "%s: %s\n", k, meta[k])
		}
	}

	b.WriteString("\n== WAL 尾现场块（最近事件，时间倒序）==\n")
	switch {
	case walErr != nil:
		fmt.Fprintf(&b, "（现场块不可用：%v——降级，禁静默）\n", walErr)
	case len(refs) == 0:
		b.WriteString("（事件存储为空或查询无结果——降级，禁静默）\n")
	default:
		for _, r := range refs {
			summary := r.Summary
			if len(summary) > walTailSummaryMaxChars {
				summary = summary[:walTailSummaryMaxChars] + "…"
			}
			summary = strings.ReplaceAll(summary, "\n", " ")
			fmt.Fprintf(&b, "- [evt_%s|%s] %s (%s)\n",
				r.Key, r.Type, summary, time.UnixMilli(r.Timestamp).Format("15:04:05"))
		}
		if hasOpenBreakpoint(refs) {
			b.WriteString("\n断点判定：最后事件止于回合中途且无收尾 agent_output → 上一世最后一回合**中断于此**；\n续作前先核验该时刻的承诺是否已兑现（对照 WAL 与系统实态），勿凭通报默认成功。\n")
		} else {
			b.WriteString("\n断点判定：最后事件为收尾输出 → 上一世回合已闭环，无未兑现承诺迹象。\n")
		}
	}
	return b.String()
}

// noticeWaitMax 与 noticePollStep 是等待标记出现的预算：必须轮询而非固定 sleep 一次——
// 单次检查会静默错过慢写入者，错过一次等于整世失忆且无人报错。
const (
	noticeWaitMax  = 60 * time.Second
	noticePollStep = 500 * time.Millisecond
)

// waitNoticeAppearance 轮询标记直至出现或预算用尽，返回是否见到（新鲜度由调用方判）。
func waitNoticeAppearance(noticePath string, maxWait, poll time.Duration) bool {
	deadline := time.Now().Add(maxWait)
	for {
		if _, err := os.Stat(noticePath); err == nil {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(poll)
	}
}

// maybeInjectReincarnationNotice 是一次性启动钩子：轮询标记、判新鲜、组装通报、注入、记消费。
// tail 为事件尾查询面；nil 表示本进程未接事件存储，通报正文显式写出该降级。
// 每一步都记日志，任何一步都不允许让机器人起不来。
func maybeInjectReincarnationNotice(inj inboxInjector, tail tailQuerier, agentName, runDir string, noticeWait time.Duration) {
	if !filepath.IsAbs(runDir) {
		if exe, err := os.Executable(); err == nil {
			runDir = filepath.Join(filepath.Dir(exe), runDir)
		}
	}
	noticePath := filepath.Join(runDir, "REINCARNATION_NOTICE")

	if !waitNoticeAppearance(noticePath, noticeWait, noticePollStep) {
		return
	}
	if !detectReincarnation(noticePath, time.Now()) {
		return
	}
	log.Infof("[reincarnation] D1 hit: fresh REINCARNATION_NOTICE at %s", noticePath)

	meta := readNoticeMetadata(noticePath)
	if meta == nil {
		log.Warnf("[reincarnation] NOTICE archive missing at %s — degraded metadata", noticePath)
	}
	refs, walErr := fetchWALTail(context.Background(), tail, agentName, walTailEventLimit)
	if walErr != nil {
		log.Warnf("[reincarnation] WAL tail query failed: %v — degraded scene block", walErr)
	}

	text := buildNoticeText(meta, refs, walErr)
	inj.InjectInternal(event.Address{}, "reincarnation", tagentmodel.Message{
		Role:    tagentmodel.RoleUser,
		Content: text,
	})
	log.Infof("[reincarnation] notice injected via reincarnation source (%d WAL events, meta=%v)", len(refs), meta != nil)

	if err := os.Rename(noticePath, noticePath+noticeConsumedSuffix); err != nil {
		log.Warnf("[reincarnation] consume-marker rename failed (re-notice on next boot): %v", err)
	} else {
		log.Infof("[reincarnation] NOTICE renamed → consumed marker")
	}
}

// maybeConsumeSystemAlert is the one-shot startup hook: wait for the runtime to
// settle, queue any pending restart failure for the first user turn, mark
// consumed. Every step is logged; nothing here may crash the bot.
func maybeConsumeSystemAlert(inj inboxInjector, runDir string, delay time.Duration) {
	if !filepath.IsAbs(runDir) {
		if exe, err := os.Executable(); err == nil {
			runDir = filepath.Join(filepath.Dir(exe), runDir)
		}
	}
	alertPath := filepath.Join(runDir, "SYSTEM_ALERT")

	time.Sleep(delay)

	data, err := os.ReadFile(alertPath)
	if err != nil {
		return
	}

	text := fmt.Sprintf("[System Alert] Detected an unprocessed failure record for the previous generation's restart script (run/SYSTEM_ALERT):\n%s",
		string(data))
	inj.InjectInternal(event.Address{}, "system_alert", tagentmodel.Message{
		Role:    tagentmodel.RoleUser,
		Content: text,
	})
	log.Infof("[system-alert] staged failure queued via system_alert source (%d bytes)", len(data))

	if err := os.Rename(alertPath, alertPath+systemAlertConsumedSuffix); err != nil {
		log.Warnf("[system-alert] consume-marker rename failed: %v", err)
	} else {
		log.Infof("[system-alert] SYSTEM_ALERT renamed -> consumed marker")
	}
}
