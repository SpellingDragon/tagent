// deliver_tool_test 钉住 投递工具面的模型可见契约：参数形态封闭、尺寸上界、拒绝与失败转结果文本、成功回执语义。
// 契约: docs/wiki/reliability/durable-delivery.md#lineage-visibility
package meditation

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// seamCall 记录投递缝收到的一次调用，供断言身份固定与消息形态。
type seamCall struct {
	target string
	msg    model.Message
}

// recordingSeam 返回缝替身与记录本：deliver 按 given 返回错误。
func recordingSeam(given error) (DeliverFunc, *[]seamCall) {
	var calls []seamCall
	seam := func(_ context.Context, target string, msg model.Message) error {
		calls = append(calls, seamCall{target: target, msg: msg})
		return given
	}
	return seam, &calls
}

// TestDeliverToolDeclaration 钉住 声明面只教模型两件事：投给谁、投什么。
// - 工具名 deliver，required 恰为 target 与 content；
// - 属性表里没有身份位：投递方由装配固定。
func TestDeliverToolDeclaration(t *testing.T) {
	d := New([]string{"target"}, nil).Declaration()
	require.Equal(t, "deliver", d.Name)
	require.ElementsMatch(t, []string{"target", "content"}, d.InputSchema.Required)
	require.Len(t, d.InputSchema.Properties, 2)
	require.NotContains(t, d.InputSchema.Properties, "from")
	require.NotContains(t, d.InputSchema.Properties, "source")
}

// TestDeliverToolContentBound 钉住 8KiB 上界的两侧：等界通过、超一字节具名拒绝且缝未被触碰。
func TestDeliverToolContentBound(t *testing.T) {
	seam, calls := recordingSeam(nil)
	dt := New([]string{"assistant"}, seam)
	ctx := context.Background()

	res, err := dt.Call(ctx, []byte(`{"target":"assistant","content":"`+strings.Repeat("x", contentMaxBytes+1)+`"}`))
	require.NoError(t, err, "超限拒绝不得断回合")
	require.Contains(t, res, "[delivery_denied] content exceeds 8KiB 上界（实际 8193 字节）")
	require.Empty(t, *calls, "超限不得触缝")

	res, err = dt.Call(ctx, []byte(`{"target":"assistant","content":"`+strings.Repeat("x", contentMaxBytes)+`"}`))
	require.NoError(t, err)
	require.Contains(t, res, "[delivery_ok]")
	require.Len(t, *calls, 1, "等界内容合法")
}

// TestDeliverToolRefusalTextCarriesAllowlist 钉住 拒绝文案的自纠料：具名原因加白名单现状。
func TestDeliverToolRefusalTextCarriesAllowlist(t *testing.T) {
	notAllowed := errors.New("tagent: delivery is not authorized by the sender's deliver_to allowlist, " +
		"allowlist [target] excludes target \"ghost\"")
	seam, _ := recordingSeam(notAllowed)
	dt := New([]string{"target"}, seam)

	res, err := dt.Call(context.Background(), []byte(`{"target":"ghost","content":"卡片"}`))
	require.NoError(t, err, "白名单外拒绝是结果文本，不是回合错误")
	require.Contains(t, res, "[delivery_denied]")
	require.Contains(t, res, "not authorized", "typed error 的具名原因须原样可读")
	require.Contains(t, res, "；你的白名单是 [target]")
}

// TestDeliverToolSuccessReceipt 钉住 成功语义与消息形态：正文原样进缝、角色补齐、回执点名目标。
func TestDeliverToolSuccessReceipt(t *testing.T) {
	seam, calls := recordingSeam(nil)
	dt := New([]string{"target"}, seam)

	res, err := dt.Call(context.Background(), []byte(`{"target":"target","content":"跨域巩固卡片正文"}`))
	require.NoError(t, err)
	require.Contains(t, res, "[delivery_ok] 已投递 target=target")
	require.Contains(t, res, "已进入其 mailbox（同批用户输入时让位）")
	require.Len(t, *calls, 1)
	require.Equal(t, "target", (*calls)[0].target)
	require.Equal(t, model.RoleUser, (*calls)[0].msg.Role)
	require.Equal(t, "跨域巩固卡片正文", (*calls)[0].msg.Content)
}

// TestDeliverToolForgedSenderRefused 钉住 身份不可伪造：夹带来源字段的调用具名拒绝，缝未被触碰。
func TestDeliverToolForgedSenderRefused(t *testing.T) {
	seam, calls := recordingSeam(nil)
	dt := New([]string{"target"}, seam)

	res, err := dt.Call(context.Background(), []byte(`{"target":"target","content":"c","from":"victim"}`))
	require.NoError(t, err, "夹带身份字段的拒绝同样不得断回合")
	require.Contains(t, res, "[delivery_denied]")
	require.Contains(t, res, "'from'")
	require.Empty(t, *calls, "被拒调用不得触缝")
}

// TestDeliverToolUnboundSeamRefuses 钉住 延后绑定窗口的失败模式：缝未接线时具名拒绝而非 panic。
func TestDeliverToolUnboundSeamRefuses(t *testing.T) {
	dt := New([]string{"target"}, nil)
	res, err := dt.Call(context.Background(), []byte(`{"target":"target","content":"c"}`))
	require.NoError(t, err)
	require.Contains(t, res, "[delivery_denied] 宿主未接线投递缝")

	seam, calls := recordingSeam(nil)
	dt.SetDeliver(seam)
	res, err = dt.Call(context.Background(), []byte(`{"target":"target","content":"c"}`))
	require.NoError(t, err)
	require.Contains(t, res, "[delivery_ok]")
	require.Len(t, *calls, 1, "SetDeliver 之后调用直达投递缝")
}

// TestDeliverToolProtocolError 钉住 err 的留守领地：解不出参数形态才是协议错。
func TestDeliverToolProtocolError(t *testing.T) {
	dt := New([]string{"target"}, nil)
	res, err := dt.Call(context.Background(), []byte(`{"target":`))
	require.Nil(t, res)
	require.Error(t, err)
	require.Contains(t, err.Error(), "deliver: malformed arguments")
}
