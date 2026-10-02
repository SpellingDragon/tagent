package testutil // import "github.com/SpellingDragon/tagent/testutil"

Package testutil 提供真实 API 侧测试的装配助手：从环境（或 shell 配置）读取凭据与
端点，以及带退避的重试。**仅限测试使用**——它把本机 shell 配置当凭据来源，不是 生产配置通路。

FUNCTIONS

func LoadAPIKey() (string, error)
    LoadAPIKey 读取 ZAI_API_KEY：先看进程环境，为空时退回 source shell 配置（~/.zshrc）取值；
    两处都取不到即返回错误，不返回空串冒充"可用"。仅用于测试。

func RetryWithBackoff(maxRetries int, delay time.Duration, fn func() error) error
    RetryWithBackoff 以退避方式重试：命中限流（429 或限流文案）时等待时长翻倍，其余错误线性退避；
    重试用尽后包装最后一次的错误返回，而不是只报"失败了"。

TYPES

type Config struct {
	// APIKey 是测试用凭据，由 LoadAPIKey 解析。
	APIKey string
	// Endpoint 是模型服务地址；环境变量为空时取公共端点默认值。
	Endpoint string
	// ModelName 是测试用模型名；为空时取默认值——默认刻意选稳定优先的型号，因为更快的那个
	// 型号输出不稳定，会让真实调用测试的结果不可解释。
	ModelName string
}
    Config 是测试装配结果：凭据、端点与模型名。

func LoadConfig() (*Config, error)
    LoadConfig 装配 Config：凭据必须可得（否则直接返回错误），端点与模型名有默认值兜底。
