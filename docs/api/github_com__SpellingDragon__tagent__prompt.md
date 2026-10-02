package prompt // import "github.com/SpellingDragon/tagent/prompt"

Package prompt 装配 agent 的系统提示词：从磁盘文件与目录读取、按固定顺序拼接， 并在磁盘未命中时回退到内嵌默认值。

提示词文件是唯一真源，改动经热重载即时生效，无需重启进程。

契约: docs/wiki/prompt/prompt-architecture.md#file-layout

VARIABLES

var BootstrapLoadOrder = []string{
	"AGENTS.md",
	"SOUL.md",
	"USER.md",
	"TOOLS.md",
	"HEARTBEAT.md",
	"MEMORY.md",
}
    BootstrapLoadOrder 是装配文档的加载顺序，也是该序列的唯一真源。


FUNCTIONS

func SplitCSV(s string) []string
    SplitCSV 按逗号切分并去除各元素首尾空白，空元素丢弃；入参为空串时返回 nil。


TYPES

type CompositeConfig struct {
	Inline string   `json:"inline,omitempty" yaml:"inline,omitempty"`
	Files  []string `json:"files,omitempty"  yaml:"files,omitempty"`
	Dir    string   `json:"dir,omitempty"    yaml:"dir,omitempty"`
}
    CompositeConfig 描述一份提示词由哪些来源组成。组装顺序固定为 inline → Files（按给定 顺序）→ Dir，各段以空行拼接。

    契约: docs/wiki/prompt/prompt-architecture.md#load-composite

func (pc CompositeConfig) IsEmpty() bool
    IsEmpty 报告是否未指定任何提示词来源。

type Getter interface {
	Get() (string, error)
	IsEmpty() bool
}
    Getter 是提示词源的运行期抽象：消费方依赖它，而不依赖任何具体实现。

    Get 返回当前生效的提示词内容；实现必须支持运行期变更（文件热重载或版本切换）。 IsEmpty 报告是否未配置任何提示词源。 Get
    会在每个回合被调用，实现须自身保证并发安全。

    契约: docs/wiki/prompt/prompt-architecture.md#getter

type Loader struct {
	// BaseDir 是相对路径的解析基准目录，空串表示不做解析。
	BaseDir string

	// Has unexported fields.
}
    Loader 从文件或目录读取提示词；配置了内嵌回退 FS 时，磁盘缺失者由该 FS 补齐。

    契约: docs/wiki/prompt/prompt-architecture.md#loader-methods

func NewLoader(baseDir string, opts ...LoaderOption) *Loader
    NewLoader 创建提示词加载器。不传选项时只读磁盘 `BaseDir`。

func (l *Loader) LoadBootstrap(dir string) (string, error)
    LoadBootstrap 按 BootstrapLoadOrder 给定的顺序装配目录中的文档，顺序表之外的 .md
    追加在末尾；条目不存在时跳过该条，目录不存在或其他读取失败整体中止并返回该错误。

func (l *Loader) LoadComposite(inline string, files []string, dir string) (string, error)
    LoadComposite 按 inline → files → dir 的顺序加载各来源并以空行拼接；缺省的来源跳过。

func (l *Loader) LoadFiles(paths []string) (string, error)
    LoadFiles 按序读取多个提示词文件并以空行拼接。空路径与空内容跳过；磁盘上不存在的文件
    也跳过（可选上下文文件在干净检出中合理地缺失），但真实读错误仍向上传播。

    契约: docs/wiki/prompt/prompt-architecture.md#load-files

func (l *Loader) LoadFromDir(dir string) (string, error)
    LoadFromDir 读取目录一层的 .md 提示词并按文件名排序，子目录跳过，非 .md 文件忽略。 目录内无 .md 时返回错误。

func (l *Loader) LoadFromFile(path string) (string, error)
    LoadFromFile 读取单个提示词文件；相对路径按 BaseDir 解析。 空文件返回空串而非错误；磁盘未命中且配置了内嵌 FS 时由该 FS
    补齐，绝对路径不回退。 读失败时以 %w 包裹 os 错误，调用方可用 errors.Is 判别 os.ErrNotExist。

type LoaderOption func(*Loader)
    LoaderOption configures a Loader.

func WithFallback(fsys fs.FS, prefix string) LoaderOption
    WithFallback 指定内嵌提示词 FS 及其中的根路径：磁盘未命中时由它补齐，磁盘命中项始终优先。

type Source struct {
	// Has unexported fields.
}
    Source 是一组提示词文件的运行期视图：按配置读取并拼接文件，文件更新后自动重读。 可并发使用；未配置文件时内容固定不变。

    契约: docs/wiki/prompt/prompt-architecture.md#source-hotreload

func NewSource(loader *Loader, config CompositeConfig) *Source
    NewSource 返回监听 config 所列文件的提示词源。config 不含文件时内容只加载一次。

func NewStaticSource(content string) *Source
    NewStaticSource 返回内容固定的提示词源：不监听文件，不做重读。

func (s *Source) Get() (string, error)
    Get 返回当前生效的提示词内容。

    所有配置文件的 mtime 均不晚于上次成功载入时刻时命中缓存，不重读磁盘；任一文件更晚 则重读并刷新缓存。静态源（loader 为
    nil）直接返回固定内容。 stat 或重读失败时返回既有缓存（若存在），无缓存才返回错误。 nil 接收者返回空内容。

func (s *Source) IsEmpty() bool
    IsEmpty 报告是否未配置任何提示词内容或文件。nil 接收者视为空。

