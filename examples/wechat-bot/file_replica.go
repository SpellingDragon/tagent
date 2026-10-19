// 契约: docs/wiki/examples/wechat-bot-runtime.md#outbound-delivery
package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory/replica"

	"github.com/SpellingDragon/tagent/memory/ledger"
)

// resultCopy 是一个大结果的投递副本：通道读取的路径，以及覆盖它的活跃保护的释放动作。
type resultCopy struct {
	// Path 是交给通道的路径，落在 workspace 副本区内。
	Path string
	// Release 结束活跃保护；重复调用只释放一次。
	Release func()
}

// resultReplicaSource 是投递层消费的 workspace 副本生命周期窄面，即运行时唯一的外置路径：
// 原文在任何副本存在前落盘、副本立于 workspace 副本根下并在保护结束后可被回收、
// 原文仍有效时重新材料化，而宿主不自持第二份副本。
//
// handled 报告该路径是否被此生命周期接管；未被接管的路径按原样交给通道。
type resultReplicaSource interface {
	OpenReplica(ctx context.Context, path, consumer string) (resultCopy, bool, error)
}

// replicaArea 是 workspace 副本根在配置 workspace 下的目录名。
const replicaArea = "overflow"

// replicaRetention 是宿主投递原文的保留策略：它界定这些副本所属的读作用域，不是时限。
const replicaRetention = "delivery"

// replicaStore 是投递生命周期需要的原文面：外置原文的提交面，以及材料化副本所用的受限读取面。
type replicaStore interface {
	ledger.OriginalStore
	ledger.ScopedOriginalStore
}

// deliveryOwner 是宿主投递副本归属的会址：宿主进程自身而非某一个会话，
// 使一处副本区服务本进程的全部会话。
func deliveryOwner(entry string) event.Address {
	return event.Address{AgentID: event.AgentID(strings.TrimSpace(entry)), SessionID: "wechat-delivery"}
}

// workspaceReplicas 是投递层对 workspace 副本生命周期的视图：大结果落为原文，
// 副本材料化于副本根下；已立在副本区的副本只取活跃保护。
type workspaceReplicas struct {
	scope        replica.ResultScope
	cleaner      *replica.ReplicaCleaner
	materializer *replica.Materializer
	externalizer *replica.ResultExternalizer
}

var _ resultReplicaSource = (*workspaceReplicas)(nil)

// newWorkspaceReplicas 在一个 workspace 内接线投递生命周期：原文存入 store，
// 材料化经宿主固定的读作用域读取原文，回收器回收没有投递持有的副本。
// owner 是这些副本归属的会址，maxTokens 是外置阈值的模型窗口口径（0 取默认值）。
func newWorkspaceReplicas(workspaceDir string, owner event.Address, store replicaStore, maxTokens int) (*workspaceReplicas, error) {
	scope := replica.ResultScope{
		Owner:           owner,
		Format:          replica.FormatOverflow,
		RetentionPolicy: replicaRetention,
	}
	root := filepath.Join(workspaceDir, replicaArea)
	cleaner, err := replica.NewReplicaCleaner(root)
	if err != nil {
		return nil, err
	}
	readScope := ledger.ReadScope{Owner: scope.Owner, Format: scope.Format, RetentionPolicy: scope.RetentionPolicy}
	materializer, err := replica.NewMaterializer(root, store, readScope, replica.WithCleaner(cleaner))
	if err != nil {
		return nil, err
	}
	externalizer, err := replica.NewResultExternalizer(store, replica.WithMaxTokens(maxTokens))
	if err != nil {
		return nil, err
	}
	return &workspaceReplicas{
		scope:        scope,
		cleaner:      cleaner,
		materializer: materializer,
		externalizer: externalizer,
	}, nil
}

// OpenReplica 把一个文件纳入副本生命周期，返回交给通道的路径与覆盖它的保护。
//
// 副本区内的副本只取活跃保护；超出外置阈值的路径由 workspace 材料化出唯一副本并受保护；
// 其余路径不归此生命周期接管（handled=false）。
func (r *workspaceReplicas) OpenReplica(ctx context.Context, path, consumer string) (resultCopy, bool, error) {
	release, err := r.cleaner.Acquire(path)
	switch {
	case err == nil:
		return resultCopy{Path: path, Release: release}, true, nil
	case errors.Is(err, replica.ErrCleanupTargetOutOfRange):
	default:
		return resultCopy{}, false, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return resultCopy{}, false, err
	}
	if !r.externalizer.Exceeds(int(info.Size())) {
		return resultCopy{}, false, nil
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return resultCopy{}, false, err
	}
	ref := replica.OriginalRefFor(r.scope, body)
	if _, err := r.externalizer.Externalize(ctx, ref, body); err != nil {
		return resultCopy{}, false, err
	}
	active, err := r.materializer.Open(ctx, ref, consumer)
	if err != nil {
		return resultCopy{}, false, err
	}
	return resultCopy{Path: active.Path, Release: active.Close}, true, nil
}

// CleanupStartup 回收副本区内没有投递持有的副本。
func (r *workspaceReplicas) CleanupStartup(ctx context.Context) error {
	return r.cleaner.CleanupStartup(ctx)
}

// Run 按配置周期回收没有投递持有的副本，直到 ctx 结束。
func (r *workspaceReplicas) Run(ctx context.Context) error {
	return r.cleaner.Run(ctx)
}
