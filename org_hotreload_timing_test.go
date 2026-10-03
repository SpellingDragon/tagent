// org_hotreload_timing 触发时机域：检测不等待构建，构建不封住业务 acquire。
// 契约: docs/wiki/platform/org-hot-reload.md#trigger-timing
package tagent

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestOrgLazyCheck_BuildParkedDoesNotBlockTurnAcquire 钉住 懒检测不等候选构建，业务获取照常拿旧 effective。
// - 见证编辑的那一回合负责调度构建，自己继续用当前生效的一代；构建停在屏障上期间不得有任何发布；
// - 发布之前启动的回合同样拿旧 effective，代际切换只对发布之后启动的回合可见；
// - 屏障放行后构建必须完成并发布新一代，此后启动的回合拿到新执行器——否则"不阻塞"可能只是构建根本没跑。
// 契约: docs/wiki/platform/org-hot-reload.md#trigger-timing
func TestOrgLazyCheck_BuildParkedDoesNotBlockTurnAcquire(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	writeBumped(t, yamlPath, ownerYAMLWithModel(t, "lazy-a", []string{"sub1", "sub2"}, sub2MemDefault(t)), &tick)

	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	ta, err := New(*cfg, WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = ta.Close() }()

	ta.CheckOrgReload()
	require.EqualValues(t, 0, genOf(ta), "priming is a numeric no-op — no generation yet")

	oldRunner := acquireWithin(t, ta, 2*time.Second)

	park := newBuildPark()
	defer park.disarm()

	writeBumped(t, yamlPath, ownerYAMLWithModel(t, "lazy-b", []string{"sub1", "sub2"}, sub2MemDefault(t)), &tick)
	during := acquireWithin(t, ta, 2*time.Second)
	park.waitEntered(t)

	require.Same(t, oldRunner, during, "the witnessing turn keeps serving on the old effective")
	require.EqualValues(t, 0, genOf(ta), "nothing publishes while the build is parked")
	later := acquireWithin(t, ta, 2*time.Second)
	require.Same(t, oldRunner, later, "requests started before the publish also keep the old effective")

	park.letGo()
	require.Eventually(t, func() bool { return genOf(ta) == 1 }, 10*time.Second, 20*time.Millisecond,
		"the parked build must publish after release")

	after := acquireWithin(t, ta, 2*time.Second)
	require.NotSame(t, oldRunner, after, "a turn starting after the publish uses the new generation")
}

// TestOrgSyncCheck_WaitsButDoesNotBlockBusinessAcquire 钉住 手动同步检查等构建与发布，但不封住业务获取。
// - 运维驱动的同步检查停在屏障上并持有重载互斥量，返回时机必须是发布落地；
// - 它等待期间业务获取仍必须立刻拿回当前生效的那一代，且代际尚未轮转；
// - 屏障放行后同步检查必须自己返回（有界）、代际加一，此后启动的回合拿新执行器。
// 契约: docs/wiki/platform/org-hot-reload.md#trigger-timing
func TestOrgSyncCheck_WaitsButDoesNotBlockBusinessAcquire(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	writeBumped(t, yamlPath, ownerYAMLWithModel(t, "sync-a", []string{"sub1", "sub2"}, sub2MemDefault(t)), &tick)

	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	ta, err := New(*cfg, WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = ta.Close() }()

	ta.CheckOrgReload()
	require.EqualValues(t, 0, genOf(ta))
	oldRunner := acquireWithin(t, ta, 2*time.Second)

	park := newBuildPark()
	defer park.disarm()
	writeBumped(t, yamlPath, ownerYAMLWithModel(t, "sync-b", []string{"sub1", "sub2"}, sub2MemDefault(t)), &tick)

	done := make(chan struct{})
	go func() { ta.CheckOrgReload(); close(done) }()
	park.waitEntered(t)

	acq := acquireWithin(t, ta, 2*time.Second)
	require.Same(t, oldRunner, acq, "a business request still gets the old effective while ops waits")
	require.EqualValues(t, 0, genOf(ta), "the sync call has not published yet")

	park.letGo()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the synchronous management call never returned after the barrier released")
	}
	require.EqualValues(t, 1, genOf(ta), "the management call published on return")

	after := acquireWithin(t, ta, 2*time.Second)
	require.NotSame(t, oldRunner, after, "turns started after the management call use the new generation")
}

// TestOrgBuildBarrierControl_MuTakersAreGated 钉住 对照组：停在屏障上的构建必须真的持有重载互斥量。
// - 持锁操作（第二次同步检查）要被挡住而超时，无锁的业务获取要照常返回——两条同时成立才说明挡的是锁而不是构建没跑；
// - 屏障不持锁、或业务获取偷偷取锁时，这条与它的对照用例一起变红，套件无法靠"什么都没测到"通过；
// - 放行后被挡的持锁操作必须最终完成，不得永久悬空。
// 契约: docs/wiki/platform/org-hot-reload.md#trigger-timing
func TestOrgBuildBarrierControl_MuTakersAreGated(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	writeBumped(t, yamlPath, ownerYAMLWithModel(t, "ctl-a", []string{"sub1", "sub2"}, sub2MemDefault(t)), &tick)

	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	ta, err := New(*cfg, WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = ta.Close() }()

	ta.CheckOrgReload()
	park := newBuildPark()
	defer park.disarm()
	writeBumped(t, yamlPath, ownerYAMLWithModel(t, "ctl-b", []string{"sub1", "sub2"}, sub2MemDefault(t)), &tick)

	acquireWithin(t, ta, 2*time.Second)
	park.waitEntered(t)

	muTaken := make(chan struct{})
	go func() { ta.CheckOrgReload(); close(muTaken) }()
	select {
	case <-muTaken:
		park.letGo()
		t.Fatal("control: a synchronous (mu-taking) reload returned while a build was parked — " +
			"the barrier is not holding `mu`, so the non-blocking tests would prove nothing")
	case <-time.After(500 * time.Millisecond):
	}

	acquireWithin(t, ta, 2*time.Second)

	park.letGo()
	select {
	case <-muTaken:
	case <-time.After(10 * time.Second):
		t.Fatal("the gated mu-taker never returned after the barrier released")
	}
}

// TestOrgCloseDrainDoesNotBlockBusinessAcquire 钉住 请求获取既不等构建也不等关闭排空。
// - 关闭已发起、其排空还堵在被停住的构建后面等锁时，业务获取仍必须立刻返回；
// - 放行后排空必须拿到锁并走完拆除，关闭有界返回——排空无上界即为缺陷。
// - 关闭由本用例自己发起并观察，因此不注册延迟关闭：延迟的第二次关闭会被 closeOnce 吞成 no-op，排空与返回的时序就观察不到了。
// 契约: docs/wiki/platform/org-hot-reload.md#trigger-timing
func TestOrgCloseDrainDoesNotBlockBusinessAcquire(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	writeBumped(t, yamlPath, ownerYAMLWithModel(t, "close-a", []string{"sub1", "sub2"}, sub2MemDefault(t)), &tick)

	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	ta, err := New(*cfg, WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)

	ta.CheckOrgReload()
	park := newBuildPark()
	defer func() { park.disarm(); _ = ta.Close() }()
	writeBumped(t, yamlPath, ownerYAMLWithModel(t, "close-b", []string{"sub1", "sub2"}, sub2MemDefault(t)), &tick)

	acquireWithin(t, ta, 2*time.Second)
	park.waitEntered(t)

	closed := make(chan struct{})
	go func() { _ = ta.Close(); close(closed) }()

	acquireWithin(t, ta, 2*time.Second)

	park.letGo()
	select {
	case <-closed:
	case <-time.After(10 * time.Second):
		t.Fatal("Close never returned after the parked build released — the drain is unbounded")
	}
}
