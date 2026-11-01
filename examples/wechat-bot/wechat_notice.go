// wechat_notice.go 是两条一次性启动通知：保险链换装的转世通报与上一世重启失败的系统告警。
// 两者都把一条内部消息塞进宿主的收件箱，让第一回合读到；传输是微信宿主的启动语义，
// 与装配无关。
//
// 契约: docs/wiki/examples/wechat-bot-runtime.md#module-layout
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	tagentmodel "github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
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

// inboxInjector is the minimal face the two startup notices share: admit one
// notice in the host's internal inbox beside the marker file whose consumed
// suffix the inbox writes when the turn that read the notice is confirmed.
type inboxInjector interface {
	AdmitNotice(addr event.Address, source string, msg tagentmodel.Message, marker, suffix string) (string, error)
}

// noticeWaker is the path that drives the fixed target once a notice is in its
// inbox: the same host drive face a user delivery takes, so there is no second
// wake path.
type noticeWaker interface {
	Wake(ctx context.Context, addr event.Address, source string) error
}

// noticeOps is the fixed target of the two startup notices: the ops session
// address they wait for and the path that wakes it. A nil wake admits the notice
// without driving a round: the record stays owed and the next turn of the
// session reads it.
type noticeOps struct {
	address event.Address
	wake    noticeWaker
}

// wakeOpsNotice drives the ops session once for the notice that just landed in
// its inbox. The wake degrades, it never drops: the admission is already durable,
// so a wake the host refuses leaves the notice owed for the next turn of the
// session instead of losing it.
func wakeOpsNotice(ctx context.Context, ops noticeOps, admitted, source string) {
	if ops.wake == nil {
		return
	}
	if err := ops.wake.Wake(ctx, ops.address, source); err != nil {
		log.Warnf("[%s] the wake of the ops session %s failed: %v — the notice %s stays owed in the inbox",
			source, ops.address.SessionID, err, admitted)
	}
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

// errSceneUnwired 是“本进程的 run 不观察任何存储层”的具名降级因：通报必须说出原因，
// 而不是印一个空现场块。
var errSceneUnwired = errors.New("wechat-bot: the assembled run observes no storage tier")

// sceneFace 是现场块的读面：一次观察读（本进程 run 能看到的事实链尾部）。读面缺席时
// 按名拒绝，故通报走具名降级而非空块。
type sceneFace struct {
	read func(ctx context.Context, opts memory.QueryOptions) ([]memory.EventReference, error)
}

func (s sceneFace) QueryEvents(ctx context.Context, opts memory.QueryOptions) ([]memory.EventReference, error) {
	if s.read == nil {
		return nil, errSceneUnwired
	}
	return s.read(ctx, opts)
}

// opsScene 是两条启动通知在生产装配里读的现场面：装配好那个 run 能看到的事实链尾部，
// 永不为 nil。本进程旁的 run 不观察任何存储层时，拿到的仍是一个按名拒绝的读面，所以
// “忘了接线”不可能静默地省掉现场块。
func opsScene(host *assembly.Host) tailQuerier {
	if host == nil {
		return sceneFace{}
	}
	run := host.Run()
	if run == nil {
		return sceneFace{}
	}
	return sceneFace{read: run.ObservedEvents}
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

// maybeInjectReincarnationNotice 是一次性启动钩子：轮询标记、判新鲜、组装通报、入箱、唤醒。
// ops 是固定目标（预定会址 + 唤醒路）；tail 为事件尾查询面，nil 表示本进程未接事件存储，
// 通报正文显式写出该降级。每一步都记日志，任何一步都不允许让机器人起不来。
// - 已消费标记由读到通报的那一轮的确认写出，不在这里写：入箱时重命名它，进程在回合接手前死掉就把通报丢了。
// - 通报已入箱且 ops 会址空闲时立刻驱动它那一轮，不等下一条用户消息把通报捎进来。
func maybeInjectReincarnationNotice(inj inboxInjector, ops noticeOps, tail tailQuerier, agentName, runDir string, noticeWait time.Duration) {
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
	id, err := inj.AdmitNotice(ops.address, "reincarnation", tagentmodel.Message{
		Role:    tagentmodel.RoleUser,
		Content: text,
	}, noticePath, noticeConsumedSuffix)
	if err != nil {
		log.Errorf("[reincarnation] notice admission failed at %s: %v", noticePath, err)
		return
	}
	log.Infof("[reincarnation] notice admitted as %s for the ops session %s via reincarnation source (%d WAL events, meta=%v); the consumed marker follows the confirmation", id, ops.address.SessionID, len(refs), meta != nil)
	wakeOpsNotice(context.Background(), ops, id, "reincarnation")
}

// maybeConsumeSystemAlert is the one-shot startup hook: wait for the runtime to
// settle, admit any pending restart failure for the fixed target, wake it once and
// leave the consumed marker to the confirmation. Every step is logged; nothing here
// may crash the bot.
func maybeConsumeSystemAlert(inj inboxInjector, ops noticeOps, runDir string, delay time.Duration) {
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
	id, err := inj.AdmitNotice(ops.address, "system_alert", tagentmodel.Message{
		Role:    tagentmodel.RoleUser,
		Content: text,
	}, alertPath, systemAlertConsumedSuffix)
	if err != nil {
		log.Errorf("[system-alert] alert admission failed at %s: %v", alertPath, err)
		return
	}
	log.Infof("[system-alert] staged failure admitted as %s for the ops session %s via system_alert source (%d bytes); the consumed marker follows the confirmation", id, ops.address.SessionID, len(data))
	wakeOpsNotice(context.Background(), ops, id, "system_alert")
}
