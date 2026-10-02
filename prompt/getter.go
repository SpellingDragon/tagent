package prompt

// Getter 是提示词源的运行期抽象：消费方依赖它，而不依赖任何具体实现。
//
// Get 返回当前生效的提示词内容；实现必须支持运行期变更（文件热重载或版本切换）。
// IsEmpty 报告是否未配置任何提示词源。
// Get 会在每个回合被调用，实现须自身保证并发安全。
//
// 契约: docs/wiki/prompt/prompt-architecture.md#getter
type Getter interface {
	Get() (string, error)
	IsEmpty() bool
}

var _ Getter = (*Source)(nil)
