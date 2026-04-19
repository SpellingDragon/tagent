package testutil

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// LoadAPIKey loads API key from environment or ~/.zshrc
func LoadAPIKey() (string, error) {
	// 1. Try environment variable first
	apiKey := os.Getenv("ZAI_API_KEY")
	if apiKey != "" {
		return apiKey, nil
	}

	// 2. Source ~/.zshrc and extract ZAI_API_KEY
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

// LoadConfig loads configuration from environment or ~/.zshrc
type Config struct {
	APIKey    string
	Endpoint  string
	ModelName string
}

func LoadConfig() (*Config, error) {
	cfg := &Config{}

	// Load API key
	apiKey, err := LoadAPIKey()
	if err != nil {
		return nil, err
	}
	cfg.APIKey = apiKey

	// Load endpoint
	cfg.Endpoint = os.Getenv("TRPC_CLAW_API_ENDPOINT")
	if cfg.Endpoint == "" {
		cfg.Endpoint = "https://open.bigmodel.cn/api/paas/v4"
	}

	// Load model name
	cfg.ModelName = os.Getenv("TRPC_CLAW_MODEL_NAME")
	if cfg.ModelName == "" {
		cfg.ModelName = "glm-4.7" // Default to glm-4.7 for reliability (flash is unstable)
	}

	return cfg, nil
}

// RetryWithBackoff retries a function with exponential backoff
func RetryWithBackoff(maxRetries int, delay time.Duration, fn func() error) error {
	var lastErr error

	for i := 0; i < maxRetries; i++ {
		err := fn()
		if err == nil {
			return nil // Success
		}

		lastErr = err

		// Check if it's a rate limit error (429)
		if strings.Contains(err.Error(), "429") || strings.Contains(err.Error(), "速率限制") {
			// Wait longer for rate limit
			waitTime := delay * time.Duration(i+1) * 2
			fmt.Printf("Rate limit hit, retry %d/%d after %v...\n", i+1, maxRetries, waitTime)
			time.Sleep(waitTime)
		} else {
			// Regular backoff
			waitTime := delay * time.Duration(i+1)
			fmt.Printf("Error, retry %d/%d after %v...\n", i+1, maxRetries, waitTime)
			time.Sleep(waitTime)
		}
	}

	return fmt.Errorf("failed after %d retries: %w", maxRetries, lastErr)
}
