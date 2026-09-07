package prompt

// Getter 是热配置提示词源的抽象缝（TC0 热配置 / T-EVO 自进化）。
//
// 设计意图：把消费方（ContextManager / Meditation / AgentToolWrapper）对提示词源的
// 依赖从具体 *Source 抽象为 Getter 接口——这是「运行期可替换 prompt 源而无需重启」
// 的接缝（C6 遗产；git-native 后默认实现=文件热重载 Source）。
// 既有 *Source 天然满足 Getter（Get/IsEmpty），迁移零行为变化。
//
// 契约：
//   - Get 返回当前生效的提示词内容；实现 MUST 支持运行期变更（热载或版本切换）。
//   - IsEmpty 报告是否未配置任何提示词源。
//   - 并发安全：Get 可能被每回合 BeforeModel 调用，实现须自身保证并发安全。
type Getter interface {
	Get() (string, error)
	IsEmpty() bool
}

// 编译期锁定：*Source 满足 Getter（迁移消费方字段类型后行为不变）。
var _ Getter = (*Source)(nil)
