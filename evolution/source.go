package evolution

import (
	"github.com/SpellingDragon/tagent/prompt"
)

// BundleProvider 是运行期 active bundle 的订阅点（回合边界生效语义的载体）。
// ContextManager 每回合 BeforeModel 经 VersionedSource.Get 读当前 active——激活/回滚
// 在下一回合自然生效（报告 D1：回合边界生效，放弃立即生效避免回合内半新半旧）。
type BundleProvider struct {
	store *BundleStore
}

// NewBundleProvider 构建 active bundle 订阅点。
func NewBundleProvider(store *BundleStore) *BundleProvider {
	return &BundleProvider{store: store}
}

// Active 返回当前激活 bundle（nil = 未初始化）。
func (p *BundleProvider) Active() *Bundle {
	if p == nil || p.store == nil {
		return nil
	}
	return p.store.Active()
}

// ActiveID 返回当前激活 bundle 的 id（空 = 无）——供归因盖章（bundle_id）消费。
func (p *BundleProvider) ActiveID() string {
	if b := p.Active(); b != nil {
		return b.ID
	}
	return ""
}

// VersionedSource 实现 prompt.Getter：从 active bundle 读指定逻辑名的提示词，无 active
// 或无该键时回退 base（文件热载 Source）。每次 Get 读当前 active → 版本切换在下一回合
// 生效。这是 TC0 热配置的核心：把「文件热载」升级为「版本化 bundle 切换」。
type VersionedSource struct {
	provider *BundleProvider
	key      string        // bundle.Prompts 的逻辑名（如 "system"）
	base     prompt.Getter // 回退源（文件热载 Source，可为 nil）
}

// 编译期锁定：VersionedSource 满足 prompt.Getter（可与文件 Source 互换）。
var _ prompt.Getter = (*VersionedSource)(nil)

// NewVersionedSource 构建版本化提示词源。key 是 bundle.Prompts 的逻辑名；base 是无
// active bundle 时的回退（通常传既有文件热载 *prompt.Source）。
func NewVersionedSource(provider *BundleProvider, key string, base prompt.Getter) *VersionedSource {
	return &VersionedSource{provider: provider, key: key, base: base}
}

// Get 返回当前生效提示词：优先 active bundle 的 key，回退 base。
func (v *VersionedSource) Get() (string, error) {
	if v.provider != nil {
		if b := v.provider.Active(); b != nil {
			if content, ok := b.Prompts[v.key]; ok {
				return content, nil
			}
		}
	}
	if v.base != nil {
		return v.base.Get()
	}
	return "", nil
}

// IsEmpty 报告是否无任何提示词源（active bundle 无该键且 base 空）。
func (v *VersionedSource) IsEmpty() bool {
	if v.provider != nil {
		if b := v.provider.Active(); b != nil {
			if _, ok := b.Prompts[v.key]; ok {
				return false
			}
		}
	}
	return v.base == nil || v.base.IsEmpty()
}
