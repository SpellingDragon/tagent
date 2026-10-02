// 契约: docs/wiki/platform/cognitive-asset-guard.md
package tagent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SpellingDragon/tagent/evolution"
	"trpc.group/trpc-go/trpc-agent-go/log"
)

// assetAuditInterval 是周期比对间隔：常量而非配置（零新旋钮，防线不可被部署遗忘）。
const assetAuditInterval = 10 * time.Minute

// assetSnapshotName 是快照文件名（存 WorkingDir/.tagent/ 下，跨重启保留）。
const assetSnapshotName = "cognitive-assets.snapshot.json"

// FileEntry 是单个资产文件的内容指纹。
type FileEntry struct {
	Hash  string `json:"hash"`
	Size  int64  `json:"size"`
	Mtime int64  `json:"mtime"`
}

// AssetChange 是一次漂移的最小证据单元：旧/新内容指纹 + 检测时刻（unix ms）。
// OldHash 为空表示新增，NewHash 为空表示删除。
type AssetChange struct {
	File      string
	OldHash   string
	NewHash   string
	Size      int64
	Timestamp int64
}

// assetSnapshot 是持久化的文件集指纹基线。
type assetSnapshot struct {
	Files map[string]FileEntry `json:"files"`
}

// AssetAuditor 周期扫描认知资产并比对基线；漂移经 report 回调入事实链。
// report 为 nil 时仅日志（降级安全）。并发约定：scanAndReport 由 ticker 与启动
// 路径先后调用，内部以 mu 串行化快照读写。
type AssetAuditor struct {
	wd        string
	extra     []string
	paths     []string
	snap      string
	interval  time.Duration
	report    func([]AssetChange)
	stop      chan struct{}
	done      chan struct{}
	mu        sync.Mutex
	once      sync.Once
	startOnce sync.Once
	started   atomic.Bool
}

// NewAssetAuditor 构造审计器。wd 为空回退进程 cwd；patterns 为受控清单
// （evolution.DefaultProtectedPaths 同源传入）；extraFiles 是清单外补充文件
// （主配置 ConfigPath，可空）。interval<=0 时使用 assetAuditInterval。
func NewAssetAuditor(wd string, patterns, extraFiles []string, report func([]AssetChange)) *AssetAuditor {
	if wd == "" {
		wd, _ = os.Getwd()
	}
	a := &AssetAuditor{
		wd:       wd,
		extra:    extraFiles,
		interval: assetAuditInterval,
		report:   report,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
	for _, pat := range patterns {
		if strings.HasSuffix(pat, "/**") {
			a.paths = append(a.paths, strings.TrimSuffix(pat, "/**"))
		} else if !strings.ContainsAny(pat, "*?") {
			a.extra = append(a.extra, pat)
		}
	}
	a.snap = filepath.Join(wd, ".tagent", assetSnapshotName)
	return a
}

// Start 起后台循环：先做一次启动比对（上一代快照 → 漂移事件 → 新基线；无历史
// 静默建基线），随后进入周期 ticker。初始比对放在 goroutine 内——审计不得阻塞
// agent 构造路径（文件哈希是有界 I/O，不该串进装配关键路径）。返回 error 仅用于
// 保留签名兼容（当前不产生）。
func (a *AssetAuditor) Start() error {
	a.startOnce.Do(func() {
		a.started.Store(true)
		go func() {
			defer close(a.done)
			if a.stopped() {
				return
			}
			a.scanAndReport(true)
			a.loop()
		}()
	})
	return nil
}

// Close 停止后台循环并同步等待其完全退出（幂等）。Close 返回后保证无任何快照写入，
// 避免调用方的资源清理（如 t.TempDir）与尾随写竞态。
func (a *AssetAuditor) Close() error {
	a.once.Do(func() { close(a.stop) })
	if a.started.Load() {
		<-a.done
	}
	return nil
}

// stopped 报告 stop 是否已关闭（非阻塞）。
func (a *AssetAuditor) stopped() bool {
	select {
	case <-a.stop:
		return true
	default:
		return false
	}
}

func (a *AssetAuditor) loop() {
	t := time.NewTicker(a.interval)
	defer t.Stop()
	for {
		select {
		case <-a.stop:
			return
		case <-t.C:
			a.scanAndReport(false)
		}
	}
}

// scanAndReport 扫当前文件集→与基线 Diff→非空则产事件+Info 日志→落新基线。
// atStartup=true 时容忍无历史快照（静默建基线，不算漂移）。
func (a *AssetAuditor) scanAndReport(atStartup bool) {
	a.mu.Lock()
	defer a.mu.Unlock()

	current := a.scan()
	prev := a.loadSnapshot()
	if prev == nil {
		if !atStartup {
			return
		}
		a.saveSnapshot(current)
		return
	}
	changes := DiffAssetSnapshots(prev, current)
	if len(changes) > 0 {
		a.emit(changes)
	}
	a.saveSnapshot(current)
}

// emit 产出一条 cognitive_asset_changed 批次事件 + Info 日志。
func (a *AssetAuditor) emit(changes []AssetChange) {
	var lines []string
	for _, c := range changes {
		lines = append(lines, c.evidenceLine())
	}
	log.Infof("[asset-drift-audit] %d cognitive asset change(s):\n%s", len(changes), strings.Join(lines, "\n"))
	if a.report != nil {
		a.report(changes)
	}
}

// evidenceLine 是单条漂移的证据行：file old8->new8 size=... mtime=...
func (c AssetChange) evidenceLine() string {
	old8, new8 := c.OldHash, c.NewHash
	if len(old8) > 8 {
		old8 = old8[:8]
	}
	if len(new8) > 8 {
		new8 = new8[:8]
	}
	if c.OldHash == "" {
		old8 = "-"
	}
	if c.NewHash == "" {
		new8 = "(deleted)"
	}
	return fmt.Sprintf("%s %s->%s size=%d mtime=%s", c.File, old8, new8, c.Size,
		time.UnixMilli(c.Timestamp).Format(time.RFC3339))
}

// scan 枚举资产文件集并计算指纹。单文件读失败（权限/瞬时）跳过并 Warn，
// 不因个别文件整体失败——审计目标是驻留的有害固化，可用性优先于完备性。
func (a *AssetAuditor) scan() map[string]FileEntry {
	out := map[string]FileEntry{}
	for _, dir := range a.paths {
		root := filepath.Join(a.wd, dir)
		_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if fe, ok := fingerprint(p); ok {
				rel, rerr := filepath.Rel(a.wd, p)
				if rerr != nil {
					rel = p
				}
				out[filepath.ToSlash(rel)] = fe
			}
			return nil
		})
	}
	for _, f := range a.extra {
		if f == "" {
			continue
		}
		p := f
		if !filepath.IsAbs(p) {
			p = filepath.Join(a.wd, p)
		}
		if fe, ok := fingerprint(p); ok {
			rel, rerr := filepath.Rel(a.wd, p)
			if rerr != nil {
				rel = p
			}
			out[filepath.ToSlash(rel)] = fe
		}
	}
	return out
}

// fingerprint 计算单文件 SHA-256/size/mtime；不可读返回 ok=false。
func fingerprint(path string) (FileEntry, bool) {
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		return FileEntry{}, false
	}
	f, err := os.Open(path)
	if err != nil {
		log.Warnf("[asset-drift-audit] open failed %s: %v", path, err)
		return FileEntry{}, false
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		log.Warnf("[asset-drift-audit] hash failed %s: %v", path, err)
		return FileEntry{}, false
	}
	return FileEntry{Hash: hex.EncodeToString(h.Sum(nil)), Size: st.Size(), Mtime: st.ModTime().UnixMilli()}, true
}

// DiffAssetSnapshots 是纯函数比对引擎：内容 hash 不同=变更；新增/删除文件也算
// 变更（OldHash/NewHash 留空表意）。顺序稳定（按文件名字典序），事件可复现。
func DiffAssetSnapshots(prev, cur map[string]FileEntry) []AssetChange {
	var changes []AssetChange
	for name, nfe := range cur {
		if ofe, ok := prev[name]; !ok || ofe.Hash != nfe.Hash {
			changes = append(changes, AssetChange{File: name, OldHash: ofe.Hash, NewHash: nfe.Hash,
				Size: nfe.Size, Timestamp: nfe.Mtime})
		}
	}
	for name, ofe := range prev {
		if _, ok := cur[name]; !ok {
			changes = append(changes, AssetChange{File: name, OldHash: ofe.Hash, NewHash: "",
				Size: ofe.Size, Timestamp: ofe.Mtime})
		}
	}
	for i := 1; i < len(changes); i++ {
		for j := i; j > 0 && changes[j].File < changes[j-1].File; j-- {
			changes[j], changes[j-1] = changes[j-1], changes[j]
		}
	}
	return changes
}

// loadSnapshot 读上一代基线；不存在/损坏返回 nil（损坏时 Warn，下次 scan 重建）。
func (a *AssetAuditor) loadSnapshot() map[string]FileEntry {
	raw, err := os.ReadFile(a.snap)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Warnf("[asset-drift-audit] snapshot read failed: %v", err)
		}
		return nil
	}
	var s assetSnapshot
	if err := json.Unmarshal(raw, &s); err != nil || s.Files == nil {
		log.Warnf("[asset-drift-audit] snapshot corrupt, rebuilding baseline: %v", err)
		return nil
	}
	return s.Files
}

// saveSnapshot 原子写基线（tmp+rename，对齐 governance/budget.go 模式）：
// 崩溃不留下半快照被误判为全体漂移。
func (a *AssetAuditor) saveSnapshot(files map[string]FileEntry) {
	if err := os.MkdirAll(filepath.Dir(a.snap), 0o755); err != nil {
		log.Warnf("[asset-drift-audit] snapshot dir failed: %v", err)
		return
	}
	raw, err := json.Marshal(assetSnapshot{Files: files})
	if err != nil {
		return
	}
	tmp := a.snap + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		log.Warnf("[asset-drift-audit] snapshot write failed: %v", err)
		return
	}
	if err := os.Rename(tmp, a.snap); err != nil {
		log.Warnf("[asset-drift-audit] snapshot rename failed: %v", err)
	}
}

// DefaultAssetPatterns 返回漂移审计的受控清单（同源真源转发，wiring 唯一入口）。
func DefaultAssetPatterns() []string {
	return evolution.DefaultProtectedPaths
}
