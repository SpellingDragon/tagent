package workspace // import "github.com/SpellingDragon/tagent/workspace"

Package workspace centralizes tagent's on-disk scratch space (oversized tool
outputs) under one root, and provides a periodic cleaner that bounds the
accumulated files (by age and count).

Layout (under Root):

    <root>/tool-output/   oversized tool outputs (OutputLimitTool, ActionTool)

Command working directories are NOT part of the scratch space: exec inherits
the process working directory so its relative paths stay consistent with the

CONSTANTS

const DefaultRoot = ".tagent-workspace"
    DefaultRoot 是默认的工作区根目录（相对进程工作目录）。

const (
	ToolOutputDir = "tool-output"
)
    ToolOutputDir 是根目录下存放超大工具输出的子目录名。

FUNCTIONS

func Root(root string) string
    Root 归一化工作区根目录：为空时回落到 DefaultRoot。

func ToolOutputPath(root string) string
    ToolOutputPath 返回超大工具输出的目录。

TYPES

type Cleaner struct {
	// Has unexported fields.
}
    Cleaner 周期性把工作区根目录下的文件按「年龄 ＋ 数量」两个维度限幅。

func NewCleaner(root string, interval, maxAge time.Duration, maxFiles int) *Cleaner
    NewCleaner 构造 Cleaner。interval/maxAge 非正表示关掉对应维度，maxFiles<=0 关掉数量上限。

func (c *Cleaner) Run(ctx context.Context)
    Run is Start's blocking form: it returns once ctx is cancelled and the
    ticker is released. The agent's close sequence waits on it, so "the
    maintenance producer stopped" is a fact the host can confirm rather than an
    assumption about a goroutine it cannot see.

func (c *Cleaner) RunOnce()
    RunOnce performs a single cleanup pass (exported for tests / manual
    trigger).

func (c *Cleaner) Start(ctx context.Context)
    Start 在自己的 goroutine 里按 ticker 清理，直到 ctx 取消；需要观察协程退出的调用方用 Run。
