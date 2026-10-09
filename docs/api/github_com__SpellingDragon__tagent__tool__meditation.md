package meditation // import "github.com/SpellingDragon/tagent/tool/meditation"

Package meditation 给冥想 agent 的反思回合一个回流宿主的面：deliver 把反思卡片投给自己的
deliver_to 白名单目标。投递本体由组合根投递缝（授权、观察面、寻址、存活四道门）承担， 执行经装配期注入的

TYPES

type DeliverFunc func(ctx context.Context, target string, msg model.Message) error
    DeliverFunc 是组合根注入的投递缝：把一条消息送进 target agent 的活动循环， typed error
    携带具名拒绝原因，由工具层转写为结果文本。

type DeliverTool struct {
	// Has unexported fields.
}
    DeliverTool 是模型可见的投递调用面。

      - allowed 是装配期登记的 deliver_to 白名单：能力面授予的结论，拒绝文案据此点名现状；
      - deliver 是装配完成后绑定的投递缝；未绑定时的调用得到具名拒绝。

func New(allowed []string, deliver DeliverFunc) *DeliverTool
    New 构造投递工具面：allowed 为该 agent 的 deliver_to 白名单； deliver 传 nil 表示延后绑定，装配尾段经
    SetDeliver 补上。

func (t *DeliverTool) Call(ctx context.Context, jsonArgs []byte) (any, error)
    Call implements tool.CallableTool.

      - 一切拒绝与投递失败都作为结果文本返回给模型（反思回合继续），白名单现状随文案点名；
      - err 只留给解不出参数形态的协议错，此时回合交由框架处置。

func (t *DeliverTool) Declaration() *tool.Declaration
    Declaration implements tool.CallableTool.

func (t *DeliverTool) SetDeliver(deliver DeliverFunc)
    SetDeliver 绑定投递缝：本实例身份由装配闭包固定，不经参数出入。
