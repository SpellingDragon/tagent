// send 工具给任何谱系的回合一个显式送达用户的面：正文经宿主唯一的发送通路送进可解析的会话，
// 通道与目标在装配期由闭包固定，模型只给正文。投递本体由 hostSender 承担（与被动通道同一实现），
// 本文件是该面的消费者，不持任何发送状态，发送方身份在工具之外由装配固定。
// 契约: docs/wiki/examples/wechat-bot-runtime.md#outbound-delivery
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/SpellingDragon/tagent/agent"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// contentMaxBytes 是单次送达正文的字节上界：一条微信消息不是语料库。
const contentMaxBytes = 8 * 1024

// sendToolID 是注册表登记 id 与 yaml ToolRef 的 id 共用的工具名。
const sendToolID = "send"

// SendFunc 是组合根注入的发送缝：把一条正文送进宿主唯一的发送通路，
// 返回实际送达目标的标签；error 携带具名拒绝原因，由工具层转写为结果文本。
type SendFunc func(ctx context.Context, content string) (targetLabel string, err error)

// SendTool 是模型可见的主动送达面。
//
//   - send 是装配完成后绑定的发送缝；未绑定时的调用得到具名拒绝。
type SendTool struct {
	send SendFunc
}

var _ tool.CallableTool = (*SendTool)(nil)

// NewSendTool 构造送达面：send 传 nil 表示延后绑定，装配尾段经 SetSend 补上。
func NewSendTool(send SendFunc) *SendTool {
	return &SendTool{send: send}
}

// SetSend 绑定发送缝：通道与目标定位由装配闭包固定，不经参数出入。
func (t *SendTool) SetSend(send SendFunc) { t.send = send }

// sendArgs 是 LLM 可见的参数形态：正文一位，仅此一项。
type sendArgs struct {
	Content string `json:"content"`
}

// Declaration implements tool.CallableTool.
func (t *SendTool) Declaration() *tool.Declaration {
	return &tool.Declaration{
		Name:        "send",
		Description: "把内容显式送达微信用户：content 是正文（上限 8KiB）。这是与回合触发源无关的主动通道——你在非用户回合想对用户说的话，不经此调用不会自动送达。发送目标与发送通道由宿主固定（本会话盖章 > 最近活跃会话），不是参数；失败以 [send_denied] 结果文本回给你，回合照常继续。",
		InputSchema: &tool.Schema{
			Type: "object",
			Properties: map[string]*tool.Schema{
				"content": {Type: "string", Description: "送达用户的正文，UTF-8，上限 8KiB"},
			},
			Required: []string{"content"},
		},
	}
}

// Call implements tool.CallableTool.
//
//   - 一切拒绝与发送失败都作为结果文本返回给模型（回合继续），目标规则随文案点名；
//   - err 只留给解不出参数形态的协议错，此时回合交由框架处置。
func (t *SendTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(jsonArgs, &probe); err != nil {
		return nil, fmt.Errorf("send: malformed arguments: %w", err)
	}
	if unknown := unknownFields(probe); len(unknown) > 0 {
		return t.denied("参数含声明之外的字段 " + strings.Join(unknown, ", ") +
			"——send 只接受 content，发送目标与通道由宿主固定、不可指定或覆写"), nil
	}
	var args sendArgs
	if err := json.Unmarshal(jsonArgs, &args); err != nil {
		return nil, fmt.Errorf("send: malformed arguments: %w", err)
	}
	if args.Content == "" {
		return t.denied("content 不能为空：空正文没有可送达之物"), nil
	}
	if len(args.Content) > contentMaxBytes {
		return t.denied(fmt.Sprintf("content exceeds 8KiB 上界（实际 %d 字节）：凝练成一条消息再发", len(args.Content))), nil
	}
	if t.send == nil {
		return t.denied("宿主未接线发送缝，本次发送无法执行"), nil
	}
	label, err := t.send(ctx, args.Content)
	if err != nil {
		return t.denied(err.Error()), nil
	}
	log.Debugf("[send] delivered target=%s bytes=%d", label, len(args.Content))
	return "[send_ok] 已送达（目标 " + label + "）；通道=宿主唯一发送通路，长文拆分与附件投递由其一并完成", nil
}

// denied 把一次拒绝渲染为结果文本：具名原因加目标规则现状，run 级 debug 日志即观测面。
func (t *SendTool) denied(reason string) string {
	log.Debugf("[send] refused: %s", reason)
	return fmt.Sprintf("[send_denied] %s；目标规则：本会话盖章 > 最近活跃", reason)
}

// unknownFields 点名声明之外的参数字段，排序保证文案可复现。
func unknownFields(probe map[string]json.RawMessage) []string {
	var out []string
	for k := range probe {
		if k != "content" {
			out = append(out, "'"+k+"'")
		}
	}
	sort.Strings(out)
	return out
}

// sendToolFactory 产出注册表侧的工厂：工具实例只持发送缝，宿主通路经缝晚绑定。
// 工厂按 id 登记：只有 yaml 声明了 {kind: tool, id: send} 的 agent 看得见它——授予即授权。
func sendToolFactory(seam *hostSendSeam) agent.PlainToolFactory {
	return func(agent.PlainToolFactoryConfig) (tool.CallableTool, error) {
		return NewSendTool(seam.send), nil
	}
}
