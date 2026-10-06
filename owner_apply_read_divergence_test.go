// owner_apply_read_divergence_test.go 钉住「提交点记为 applied ⇒ 消费读到同值」：
// 数值提交被结构性停在唯一提交点时，运维同步入口返回的那一刻消费侧必须已读到 applied 的值。
package tagent

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// syncEntryObservation 是同步运维入口返回那一刻消费侧报上来的读数。
type syncEntryObservation struct {
	elapsed    time.Duration
	keep       int
	sourceKeep int
	sourceOK   bool
	ttl        time.Duration
}

// TestApplyReadDivergence_AppliedAndConsumedSameCommit 把一次纯数值提交停在唯一提交点上，再问运维同步入口是否在提交落地前就宣布完成。
// - 停驻由既有测试专用提交屏障造成（生产恒为空），认领者走的是懒检测同一条 reload，不是耗时或重试门；
// - 同步入口按 §十四 绑定「等整次构建与发布完成」，故它返回时提交点的 applied 值必须已是消费读到的值；
// - 入口返回那一刻的源解析值与消费读值必须同值同号，提交放行后仍为同一权威值（§四 唯一真源、§五 读面与代际同临界区整体轮转）。
// 契约: docs/wiki/platform/org-hot-reload.md#trigger-timing
func TestApplyReadDivergence_AppliedAndConsumedSameCommit(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := writeGateConfig(t, yamlPath, ownerBYAML(false, 2, "1m"), time.Now())

	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	m := &delegModel{}
	entry, err := New(*cfg, WithModel(m), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	tick = writeGateConfig(t, yamlPath, ownerBYAML(true, 2, "1m"), tick)
	entry.CheckOrgReload()
	ownerB := residentCacheForTest(entry)[g24B]
	require.NotNil(t, ownerB, "B became a resident owner")
	require.Equal(t, 2, ownerB.OrgKeepRecent())

	var once sync.Once
	parked := make(chan struct{})
	release := make(chan struct{})
	unpark := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unpark)
	hook := func() { close(parked); <-release }
	orgCommitBarrier.Store(&hook)

	writeGateConfig(t, yamlPath, ownerBYAML(true, 7, "5m"), tick)

	claimantDone := make(chan struct{})
	go func() {
		defer close(claimantDone)
		entry.CheckOrgReload()
	}()
	select {
	case <-parked:
	case <-time.After(10 * time.Second):
		t.Fatal("the numeric-only commit never reached the commit barrier")
	}

	syncReturn := make(chan syncEntryObservation, 1)
	go func() {
		start := time.Now()
		entry.CheckOrgReload()
		hp, ok := ownerB.HotSnapshot()
		syncReturn <- syncEntryObservation{
			elapsed:    time.Since(start),
			keep:       ownerB.OrgKeepRecent(),
			sourceKeep: hp.KeepRecentTasks,
			sourceOK:   ok,
			ttl:        ownerB.TaskManager().TerminalTTL(),
		}
	}()

	var obs syncEntryObservation
	entryWaited := false
	select {
	case obs = <-syncReturn:
	case <-time.After(300 * time.Millisecond):
		entryWaited = true
	}

	unpark()
	select {
	case <-claimantDone:
	case <-time.After(10 * time.Second):
		t.Fatal("the parked commit never finished after release")
	}
	if entryWaited {
		select {
		case obs = <-syncReturn:
		case <-time.After(10 * time.Second):
			t.Fatal("the parked synchronous entry never returned after release")
		}
	}
	keepAfterCommit := ownerB.OrgKeepRecent()

	t.Logf("observed: entryWaited=%v entryElapsed=%v keepAtEntryReturn=%d sourceAtEntryReturn=(keep=%d,ok=%v) ttlAtEntryReturn=%s keepAfterCommit=%d",
		entryWaited, obs.elapsed, obs.keep, obs.sourceKeep, obs.sourceOK, obs.ttl, keepAfterCommit)

	require.Equal(t, 7, obs.keep,
		"applied⇒consumable：同步入口返回时提交点已记 g24_b applied=7，消费读必须同值（入口在提交停驻期间%s，耗时 %v）",
		map[bool]string{true: "按约等待", false: "提前返回"}[entryWaited], obs.elapsed)
	require.Equal(t, 7, obs.sourceKeep, "入口返回时源解析值也必须是 applied 的那个值")
	require.Equal(t, 7, keepAfterCommit, "提交落地后仍是同一权威值，不存在第二份可独立漂移的真值")
}
