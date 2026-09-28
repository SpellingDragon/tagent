package testutil

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// LoadAPIKey 读取 ZAI_API_KEY：先看进程环境，为空时退回 source shell 配置（~/.zshrc）取值；
// 两处都取不到即返回错误，不返回空串冒充"可用"。仅用于测试。
func LoadAPIKey() (string, error) {
	apiKey := os.Getenv("ZAI_API_KEY")
	if apiKey != "" {
		return apiKey, nil
	}
	cmd := exec.Command("zsh", "-c", "source ~/.zshrc && echo $ZAI_API_KEY")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to source ~/.zshrc: %w", err)
	}

	apiKey = strings.TrimSpace(string(output))
	if apiKey != "" {
		return apiKey, nil
	}

	return "", fmt.Errorf("ZAI_API_KEY not found in environment or ~/.zshrc")
}

// Config 是测试装配结果：凭据、端点与模型名。
type Config struct {
	// APIKey 是测试用凭据，由 LoadAPIKey 解析。
	APIKey string
	// Endpoint 是模型服务地址；环境变量为空时取公共端点默认值。
	Endpoint string
	// ModelName 是测试用模型名；为空时取默认值——默认刻意选稳定优先的型号，因为更快的那个
	// 型号输出不稳定，会让真实调用测试的结果不可解释。
	ModelName string
}

// LoadConfig 装配 Config：凭据必须可得（否则直接返回错误），端点与模型名有默认值兜底。
func LoadConfig() (*Config, error) {
	cfg := &Config{}

	apiKey, err := LoadAPIKey()
	if err != nil {
		return nil, err
	}
	cfg.APIKey = apiKey

	cfg.Endpoint = os.Getenv("TRPC_CLAW_API_ENDPOINT")
	if cfg.Endpoint == "" {
		cfg.Endpoint = "https://open.bigmodel.cn/api/coding/paas/v4"
	}

	cfg.ModelName = os.Getenv("TRPC_CLAW_MODEL_NAME")
	if cfg.ModelName == "" {
		cfg.ModelName = "glm-4.7"
	}

	return cfg, nil
}

// RetryWithBackoff 以退避方式重试：命中限流（429 或限流文案）时等待时长翻倍，其余错误线性退避；
// 重试用尽后包装最后一次的错误返回，而不是只报"失败了"。
func RetryWithBackoff(maxRetries int, delay time.Duration, fn func() error) error {
	var lastErr error

	for i := 0; i < maxRetries; i++ {
		err := fn()
		if err == nil {
			return nil
		}

		lastErr = err

		if strings.Contains(err.Error(), "429") || strings.Contains(err.Error(), "速率限制") {
			waitTime := delay * time.Duration(i+1) * 2
			fmt.Printf("Rate limit hit, retry %d/%d after %v...\n", i+1, maxRetries, waitTime)
			time.Sleep(waitTime)
		} else {
			waitTime := delay * time.Duration(i+1)
			fmt.Printf("Error, retry %d/%d after %v...\n", i+1, maxRetries, waitTime)
			time.Sleep(waitTime)
		}
	}

	return fmt.Errorf("failed after %d retries: %w", maxRetries, lastErr)
}
