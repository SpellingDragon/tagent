package tagent

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/stretchr/testify/require"
)

// §2.3（design D3「调度裁决」/ spec swappable-executor「触发时机与观测」）最后一腿：
// 短锁必须由**阻塞屏障**证明，不能由平均耗时证明（本轮同时撤回 bench 里
// “一次热更均值 < 10ms 即短锁成立”的完成声明）。
//
// 被测契约（reload 全程在 `mu` 之下；业务 turn 的获取路径 requestCheck→currentRunner
// 只碰 os.Stat 与 executorMu，绝不碰 `mu`）：
//   - 候选构建停在可控屏障（orgBuildBarrier，停在 mu 之下）时，业务 turn 仍立即取得
//     当时已发布的旧 effective，不等待构建；解除屏障并发布后开始的 turn 才用新代。
//   - 运维同步 CheckOrgReload 停在屏障里等待结果时，业务获取路径不被封住。
//   - Close 先停调度再排空在途构建（drain 抢 mu，被 parked 构建挡住）时，业务获取仍
//     不被阻塞——「请求获取不等构建或 Close」。
//
// fail-before 语义：若获取路径错误地经 `mu` 串行化（历史形态或在途回归），下面每一例
// 的“有界获取”都会在 parked 构建释放前永久阻塞，acquireWithin 直接判红；而
// park.waitEntered 保证构建确实停在 mu 之下，杜绝“测了个寂寞”。

// buildPark arms orgBuildBarrier so a background reload parks while holding the
// reload mutex, and hands the test a deterministic enter/release handshake.
type buildPark struct {
	entered  chan struct{}
	release  chan struct{}
	relOnce  sync.Once
	enterSig sync.Once
}

func newBuildPark() *buildPark {
	p := &buildPark{entered: make(chan struct{}), release: make(chan struct{})}
	h := func() {
		// Signal entry exactly once, then block until released — all while the
		// caller (reload) holds `mu`.
		p.enterSig.Do(func() { close(p.entered) })
		<-p.release
	}
	orgBuildBarrier.Store(&h)
	return p
}

// waitEntered fails the test if no build actually reached the barrier (i.e. the
// build is genuinely parked under `mu`), so the bounded-acquire assertions below
// would otherwise measure nothing.
func (p *buildPark) waitEntered(t *testing.T) {
	t.Helper()
	select {
	case <-p.entered:
	case <-time.After(10 * time.Second):
		p.letGo()
		t.Fatal("the candidate build never parked at the barrier — this run would measure nothing")
	}
}

func (p *buildPark) letGo() { p.relOnce.Do(func() { close(p.release) }) }

// disarm clears the global seam and, if the test bailed out early, frees any
// parked build so Close-style drains cannot hang the package.
func (p *buildPark) disarm() {
	orgBuildBarrier.Store(nil)
	p.letGo()
}

// acquireWithin runs a business-turn acquire on its own goroutine and fails if
// it does not return within `within`. A correct acquire touches only os.Stat and
// executorMu, never the reload mutex, so it must never be held by a parked build
// or a Close drain.
func acquireWithin(t *testing.T, ta *agent.TagentAgent, within time.Duration) (got any) {
	t.Helper()
	ch := make(chan any, 1)
	go func() {
		r, release := ta.ContextManager().BeginTurn()
		ch <- r
		release()
	}()
	select {
	case v := <-ch:
		return v
	case <-time.After(within):
		t.Fatalf("business acquire blocked >%s while a build/Close held the reload mutex — "+
			"the acquire path is NOT the short lock D3 requires (it must never touch `mu`)", within)
		return nil
	}
}

func genOf(ta *agent.TagentAgent) int64 {
	g, _ := ta.OrgDiagnostics()["generation"].(int64)
	return g
}

// TestOrgLazyCheck_BuildParkedDoesNotBlockTurnAcquire pins the「懒检测不等待候选构建」
// scenario: the turn that witnesses the edit schedules the build and keeps serving
// on the old effective; a later turn that starts after the publish uses the new gen.
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

	// Prime lastSeenMtime with a synchronous check (no barrier armed) so the only
	// build this test schedules is the deliberate structural one below.
	ta.CheckOrgReload()
	require.EqualValues(t, 0, genOf(ta), "priming is a numeric no-op — no generation yet")

	oldRunner := acquireWithin(t, ta, 2*time.Second) // startup effective, no build scheduled (mtime unchanged)

	park := newBuildPark()
	defer park.disarm()

	// Structural edit → the next acquire schedules a background build that parks under mu.
	writeBumped(t, yamlPath, ownerYAMLWithModel(t, "lazy-b", []string{"sub1", "sub2"}, sub2MemDefault(t)), &tick)
	during := acquireWithin(t, ta, 2*time.Second) // fires requestCheck → spawns the parked build
	park.waitEntered(t)

	// The build is genuinely parked under `mu`; a request acquire still returns the OLD
	// effective immediately — it never waits for the candidate.
	require.Same(t, oldRunner, during, "the witnessing turn keeps serving on the old effective")
	require.EqualValues(t, 0, genOf(ta), "nothing publishes while the build is parked")
	later := acquireWithin(t, ta, 2*time.Second)
	require.Same(t, oldRunner, later, "requests started before the publish also keep the old effective")

	// Release the barrier → the build completes and publishes as a new generation.
	park.letGo()
	require.Eventually(t, func() bool { return genOf(ta) == 1 }, 10*time.Second, 20*time.Millisecond,
		"the parked build must publish after release")

	after := acquireWithin(t, ta, 2*time.Second)
	require.NotSame(t, oldRunner, after, "a turn starting after the publish uses the new generation")
}

// TestOrgSyncCheck_WaitsButDoesNotBlockBusinessAcquire pins「手动检查同步等待但不封住
// 业务获取」: ops drives the synchronous reload (which parks at the barrier and holds
// `mu`), yet business acquires keep returning the old effective until the sync call's
// publish lands.
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

	ta.CheckOrgReload() // prime
	require.EqualValues(t, 0, genOf(ta))
	oldRunner := acquireWithin(t, ta, 2*time.Second)

	park := newBuildPark()
	defer park.disarm()
	writeBumped(t, yamlPath, ownerYAMLWithModel(t, "sync-b", []string{"sub1", "sub2"}, sub2MemDefault(t)), &tick)

	// Ops: synchronous check that will park mid-build holding `mu` and only return after publish.
	done := make(chan struct{})
	go func() { ta.CheckOrgReload(); close(done) }()
	park.waitEntered(t)

	// While the management call waits, a business acquire returns the old effective promptly.
	acq := acquireWithin(t, ta, 2*time.Second)
	require.Same(t, oldRunner, acq, "a business request still gets the old effective while ops waits")
	require.EqualValues(t, 0, genOf(ta), "the sync call has not published yet")

	// Release → the management call's publish lands and it returns.
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

// TestOrgBuildBarrierControl_MuTakersAreGated is the negative control that makes
// the three non-blocking assertions above meaningful rather than vacuous: it proves
// the parked build really holds the reload mutex. A mutex-taking op (a second
// synchronous CheckOrgReload → reload) MUST be gated by the barrier and time out,
// while the mu-free business acquire returns. If the barrier were not holding `mu`
// — or the acquire secretly took `mu` — this control and its counterpart together
// would go red, so the suite cannot pass by "measuring nothing".
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

	ta.CheckOrgReload() // prime lastSeenMtime
	park := newBuildPark()
	defer park.disarm()
	writeBumped(t, yamlPath, ownerYAMLWithModel(t, "ctl-b", []string{"sub1", "sub2"}, sub2MemDefault(t)), &tick)

	acquireWithin(t, ta, 2*time.Second) // spawn the background build
	park.waitEntered(t)                 // it holds `mu` now

	// A mutex-taking op must be gated by the parked build.
	muTaken := make(chan struct{})
	go func() { ta.CheckOrgReload(); close(muTaken) }()
	select {
	case <-muTaken:
		park.letGo()
		t.Fatal("control: a synchronous (mu-taking) reload returned while a build was parked — " +
			"the barrier is not holding `mu`, so the non-blocking tests would prove nothing")
	case <-time.After(500 * time.Millisecond):
		// expected: still blocked behind the parked build's `mu`
	}

	// The mu-free business acquire is NOT gated by the same parked build.
	acquireWithin(t, ta, 2*time.Second)

	park.letGo()
	select {
	case <-muTaken:
	case <-time.After(10 * time.Second):
		t.Fatal("the gated mu-taker never returned after the barrier released")
	}
}

// TestOrgCloseDrainDoesNotBlockBusinessAcquire pins「请求获取不等构建或 Close」: with a
// background build parked under `mu` and a Close already draining (blocked on `mu`
// behind the build), a business acquire still returns immediately rather than waiting
// for the teardown.
func TestOrgCloseDrainDoesNotBlockBusinessAcquire(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	writeBumped(t, yamlPath, ownerYAMLWithModel(t, "close-a", []string{"sub1", "sub2"}, sub2MemDefault(t)), &tick)

	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	ta, err := New(*cfg, WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	// No deferred Close here: this test drives Close itself.

	ta.CheckOrgReload() // prime
	park := newBuildPark()
	defer func() { park.disarm(); _ = ta.Close() }()
	writeBumped(t, yamlPath, ownerYAMLWithModel(t, "close-b", []string{"sub1", "sub2"}, sub2MemDefault(t)), &tick)

	acquireWithin(t, ta, 2*time.Second) // trigger the lazy build
	park.waitEntered(t)                 // it now holds `mu`

	// Close begins: it flips `stopped`, then its drain blocks on `mu` behind the parked build.
	closed := make(chan struct{})
	go func() { _ = ta.Close(); close(closed) }()

	// Even with Close pending a drain and a build parked, a business acquire is not blocked.
	acquireWithin(t, ta, 2*time.Second)

	// Release → the build finishes, the drain acquires `mu`, teardown proceeds and Close returns.
	park.letGo()
	select {
	case <-closed:
	case <-time.After(10 * time.Second):
		t.Fatal("Close never returned after the parked build released — the drain is unbounded")
	}
}
