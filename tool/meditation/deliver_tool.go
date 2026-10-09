// Package meditation 给冥想 agent 的反思回合一个回流宿主的面：deliver 把反思卡片投给自己的
// deliver_to 白名单目标。投递本体由组合根投递缝（授权、观察面、寻址、存活四道门）承担，
// 执行经装配期注入的 DeliverFunc 闭包完成；本包是该面的消费者，不持任何投递状态，
// 投递方身份在工具之外由装配固定。
// 契约: docs/wiki/reliability/durable-delivery.md#lineage-visibility
package meditation

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// contentMaxBytes 是投递内容的字节上界：卡片是凝练物而非语料。
const contentMaxBytes = 8 * 1024

// DeliverFunc 是组合根注入的投递缝：把一条消息送进 target agent 的活动循环，
// typed error 携带具名拒绝原因，由工具层转写为结果文本。
type DeliverFunc func(ctx context.Context, target string, msg model.Message) error

// DeliverTool 是模型可见的投递调用面。
//
//   - allowed 是装配期登记的 deliver_to 白名单：能力面授予的结论，拒绝文案据此点名现状；
//   - deliver 是装配完成后绑定的投递缝；未绑定时的调用得到具名拒绝。
type DeliverTool struct {
	deliver DeliverFunc
	allowed []string
}

var _ tool.CallableTool = (*DeliverTool)(nil)

// New 构造投递工具面：allowed 为该 agent 的 deliver_to 白名单；
// deliver 传 nil 表示延后绑定，装配尾段经 SetDeliver 补上。
func New(allowed []string, deliver DeliverFunc) *DeliverTool {
	return &DeliverTool{deliver: deliver, allowed: append([]string(nil), allowed...)}
}

// SetDeliver 绑定投递缝：本实例身份由装配闭包固定，不经参数出入。
func (t *DeliverTool) SetDeliver(deliver DeliverFunc) { t.deliver = deliver }

// deliverArgs 是 LLM 可见的参数形态：目标名加卡片正文，仅此两项。
type deliverArgs struct {
	Target  string `json:"target"`
	Content string `json:"content"`
}

// Declaration implements tool.CallableTool.
func (t *DeliverTool) Declaration() *tool.Declaration {
	return &tool.Declaration{
		Name:        "deliver",
		Description: "把反思卡片投递到白名单内 agent 的活动 session：target 是目标 agent 名（必须属于你的白名单），content 是卡片正文（上限 8KiB）。投递方身份由宿主固定，不是参数；投递缝在调用时复核白名单、观察面、寻址与存活，拒绝与失败都以结果文本回给你。",
		InputSchema: &tool.Schema{
			Type: "object",
			Properties: map[string]*tool.Schema{
				"target":  {Type: "string", Description: "目标 agent 名，必须属于你的 deliver_to 白名单"},
				"content": {Type: "string", Description: "反思卡片正文，UTF-8，上限 8KiB"},
			},
			Required: []string{"target", "content"},
		},
	}
}

// Call implements tool.CallableTool.
//
//   - 一切拒绝与投递失败都作为结果文本返回给模型（反思回合继续），白名单现状随文案点名；
//   - err 只留给解不出参数形态的协议错，此时回合交由框架处置。
func (t *DeliverTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(jsonArgs, &probe); err != nil {
		return nil, fmt.Errorf("deliver: malformed arguments: %w", err)
	}
	if unknown := unknownFields(probe); len(unknown) > 0 {
		return t.denied("参数含声明之外的字段 " + strings.Join(unknown, ", ") +
			"——deliver 只接受 target 与 content，投递方身份由宿主固定、不可指定或覆写"), nil
	}
	var args deliverArgs
	if err := json.Unmarshal(jsonArgs, &args); err != nil {
		return nil, fmt.Errorf("deliver: malformed arguments: %w", err)
	}
	target := strings.TrimSpace(args.Target)
	if target == "" {
		return t.denied("target 不能为空：从白名单里挑一个名字"), nil
	}
	if args.Content == "" {
		return t.denied("content 不能为空：空卡片没有可读之物"), nil
	}
	if len(args.Content) > contentMaxBytes {
		return t.denied(fmt.Sprintf("content exceeds 8KiB 上界（实际 %d 字节）：凝练成卡片再投递", len(args.Content))), nil
	}
	if t.deliver == nil {
		return t.denied("宿主未接线投递缝，本次投递无法执行"), nil
	}
	msg := model.Message{Role: model.RoleUser, Content: args.Content}
	if err := t.deliver(ctx, target, msg); err != nil {
		return t.denied(err.Error()), nil
	}
	log.Debugf("[deliver] delivered target=%s bytes=%d", target, len(args.Content))
	return "[delivery_ok] 已投递 target=" + target +
		"；卡片已进入其 mailbox（同批用户输入时让位），目标下一回合可见", nil
}

// denied 把一次拒绝渲染为结果文本：具名原因加白名单现状，run 级 debug 日志即观测面。
func (t *DeliverTool) denied(reason string) string {
	log.Debugf("[deliver] refused: %s", reason)
	return fmt.Sprintf("[delivery_denied] %s；你的白名单是 %v", reason, t.allowed)
}

// unknownFields 点名声明之外的参数字段，排序保证文案可复现。
func unknownFields(probe map[string]json.RawMessage) []string {
	var out []string
	for k := range probe {
		if k != "target" && k != "content" {
			out = append(out, "'"+k+"'")
		}
	}
	sort.Strings(out)
	return out
}
