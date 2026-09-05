// Package evolution 承载 tagent 的热配置与自进化治理原语（TC0 热配置 + T-EVO 自进化）。
//
// TC0 交付（本文件 + source.go）：不可变 Bundle 快照 + BundleStore（内容寻址、原子
// active 指针、任意版本回滚）+ VersionedSource（实现 prompt.Getter，从 active bundle
// 读提示词，回合边界生效）。这是「运行期切换 prompt/参数/模型版本而无需重启」的地基，
// 也是 T-EVO 发布状态机（draft→…→active）与后验回滚的存储层。
//
// 设计纪律（报告 D1）：bundle 写后冻结（不可变）；激活 = 原子切换 active 指针；基线
// bundle（v0）由启动物化、永不改写；治理数据用普通文件（JSON），不进 MemoryStore
// （要求永久、可人工 inspect，不受 LSM 合并/TTL 影响）。
package evolution

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// BundleParams 是 bundle 携带的数值参数（指针区分「未设置」与「零值」）。
type BundleParams struct {
	Temperature *float64 `json:"temperature,omitempty"`
	MaxTokens   *int     `json:"max_tokens,omitempty"`
	// 后续可扩展（top_p 等）；bundle v1 不含工具集（装配期约束，报告 D1 Non-goal）。
}

// ModelRef 是 bundle 携带的模型引用。
type ModelRef struct {
	Name     string `json:"name,omitempty"`
	Provider string `json:"provider,omitempty"`
}

// Bundle 是不可变的版本快照。写后冻结——任何「修改」都是派生一个新 bundle。
type Bundle struct {
	ID        string            `json:"id"`         // 内容寻址短哈希
	ParentID  string            `json:"parent_id"`  // 派生自哪个 bundle（空 = 基线）
	Prompts   map[string]string `json:"prompts"`    // 逻辑名 → 提示词内容快照（不可变）
	Params    BundleParams      `json:"params"`
	Model     ModelRef          `json:"model"`
	Hash      string            `json:"hash"`       // 内容寻址哈希（校验冻结完整性）
	CreatedMs int64             `json:"created_ms"`
	CreatedBy string            `json:"created_by"` // "baseline"|"refine"|"manual"
	Note      string            `json:"note"`
}

// BundleStore 管理不可变 bundle 目录 + active 指针。
// 布局：<dir>/bundles/<id>.json（冻结快照）+ <dir>/active.json（{"id": "..."}）。
// 并发安全：active 指针切换经 rename 原子化（同目录 rename 原子）；读经 RWMutex + 内存缓存。
type BundleStore struct {
	dir string

	mu     sync.RWMutex
	active *Bundle // 内存缓存的当前 active（nil = 未初始化）
}

// NewBundleStore 打开（或创建）bundle 存储目录。
func NewBundleStore(dir string) (*BundleStore, error) {
	if dir == "" {
		return nil, fmt.Errorf("evolution: bundle dir is empty")
	}
	if err := os.MkdirAll(filepath.Join(dir, "bundles"), 0o755); err != nil {
		return nil, fmt.Errorf("evolution: mkdir bundle store: %w", err)
	}
	s := &BundleStore{dir: dir}
	// 加载既有 active（重启恢复）。
	if b, err := s.readActiveFromDisk(); err == nil && b != nil {
		s.active = b
	}
	return s, nil
}

// Create 派生一个不可变 bundle（写后冻结），不改 active。parent 可为 nil（基线）。
func (s *BundleStore) Create(parent *Bundle, prompts map[string]string, params BundleParams, model ModelRef, createdBy, note string) (*Bundle, error) {
	parentID := ""
	if parent != nil {
		parentID = parent.ID
	}
	// 复制 prompts（防调用方后续改动污染冻结快照）。
	promptsCopy := make(map[string]string, len(prompts))
	for k, v := range prompts {
		promptsCopy[k] = v
	}
	b := &Bundle{
		ParentID:  parentID,
		Prompts:   promptsCopy,
		Params:    params,
		Model:     model,
		CreatedMs: time.Now().UnixMilli(),
		CreatedBy: createdBy,
		Note:      note,
	}
	b.Hash = computeBundleHash(b)
	b.ID = b.Hash[:16] // 内容寻址短 id

	if err := s.writeBundle(b); err != nil {
		return nil, err
	}
	return b, nil
}

// InitBaseline 物化基线 bundle（v0）并设为 active（若尚无 active）。幂等：已有 active
// 则返回现有 active，不覆盖（基线不可变，报告 D1）。
func (s *BundleStore) InitBaseline(prompts map[string]string, params BundleParams, model ModelRef) (*Bundle, error) {
	s.mu.RLock()
	cur := s.active
	s.mu.RUnlock()
	if cur != nil {
		return cur, nil
	}
	b, err := s.Create(nil, prompts, params, model, "baseline", "启动物化基线")
	if err != nil {
		return nil, err
	}
	if err := s.SetActive(b.ID); err != nil {
		return nil, err
	}
	return b, nil
}

// Active 返回当前激活 bundle（内存缓存，nil = 未初始化）。
func (s *BundleStore) Active() *Bundle {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.active
}

// SetActive 原子切换 active 指针到指定 bundle（激活/回滚同一机制）。
func (s *BundleStore) SetActive(id string) error {
	b, err := s.Get(id)
	if err != nil {
		return err
	}
	if err := s.writeActivePointer(id); err != nil {
		return err
	}
	s.mu.Lock()
	s.active = b
	s.mu.Unlock()
	return nil
}

// Rollback 回滚到历史 bundle（= SetActive，基线不可变故任意历史版本皆可回）。
func (s *BundleStore) Rollback(toID string) error { return s.SetActive(toID) }

// Get 按 id 读取 bundle（先内存 active，后磁盘）。
func (s *BundleStore) Get(id string) (*Bundle, error) {
	s.mu.RLock()
	if s.active != nil && s.active.ID == id {
		b := s.active
		s.mu.RUnlock()
		return b, nil
	}
	s.mu.RUnlock()
	raw, err := os.ReadFile(s.bundlePath(id))
	if err != nil {
		return nil, fmt.Errorf("evolution: bundle %s not found: %w", id, err)
	}
	var b Bundle
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, fmt.Errorf("evolution: parse bundle %s: %w", id, err)
	}
	return &b, nil
}

// List 返回全部 bundle（按创建时间升序）。
func (s *BundleStore) List() ([]*Bundle, error) {
	entries, err := os.ReadDir(filepath.Join(s.dir, "bundles"))
	if err != nil {
		return nil, err
	}
	out := make([]*Bundle, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(s.dir, "bundles", e.Name()))
		if err != nil {
			continue
		}
		var b Bundle
		if err := json.Unmarshal(raw, &b); err == nil {
			out = append(out, &b)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedMs < out[j].CreatedMs })
	return out, nil
}

// Diff 返回两 bundle 的差异摘要（refine 工具 diff op 消费，报告 D1）。
type BundleDiff struct {
	PromptsChanged   []string `json:"prompts_changed"`
	PromptsAdded     []string `json:"prompts_added"`
	PromptsRemoved   []string `json:"prompts_removed"`
	ParamsChanged    bool     `json:"params_changed"`
	ModelChanged     bool     `json:"model_changed"`
}

// Diff 计算 from→to 的差异。
func Diff(from, to *Bundle) BundleDiff {
	d := BundleDiff{}
	if from == nil || to == nil {
		return d
	}
	for k, v := range to.Prompts {
		old, ok := from.Prompts[k]
		if !ok {
			d.PromptsAdded = append(d.PromptsAdded, k)
		} else if old != v {
			d.PromptsChanged = append(d.PromptsChanged, k)
		}
	}
	for k := range from.Prompts {
		if _, ok := to.Prompts[k]; !ok {
			d.PromptsRemoved = append(d.PromptsRemoved, k)
		}
	}
	sort.Strings(d.PromptsChanged)
	sort.Strings(d.PromptsAdded)
	sort.Strings(d.PromptsRemoved)
	d.ParamsChanged = !paramsEqual(from.Params, to.Params)
	d.ModelChanged = from.Model != to.Model
	return d
}

func paramsEqual(a, b BundleParams) bool {
	return f64PtrEqual(a.Temperature, b.Temperature) && intPtrEqual(a.MaxTokens, b.MaxTokens)
}
func f64PtrEqual(a, b *float64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
func intPtrEqual(a, b *int) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// === 内部：持久化 ===

func (s *BundleStore) bundlePath(id string) string {
	return filepath.Join(s.dir, "bundles", id+".json")
}

func (s *BundleStore) writeBundle(b *Bundle) error {
	raw, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return fmt.Errorf("evolution: marshal bundle: %w", err)
	}
	path := s.bundlePath(b.ID)
	if _, err := os.Stat(path); err == nil {
		return nil // 内容寻址：同 id 已存在 = 同内容，幂等
	}
	// 原子写：临时文件 + rename。
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return fmt.Errorf("evolution: write bundle: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("evolution: commit bundle: %w", err)
	}
	return nil
}

func (s *BundleStore) writeActivePointer(id string) error {
	raw, _ := json.Marshal(map[string]string{"id": id})
	path := filepath.Join(s.dir, "active.json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return fmt.Errorf("evolution: write active pointer: %w", err)
	}
	return os.Rename(tmp, path) // 同目录 rename 原子
}

func (s *BundleStore) readActiveFromDisk() (*Bundle, error) {
	raw, err := os.ReadFile(filepath.Join(s.dir, "active.json"))
	if err != nil {
		return nil, err
	}
	var ptr struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &ptr); err != nil || ptr.ID == "" {
		return nil, fmt.Errorf("evolution: bad active pointer")
	}
	braw, err := os.ReadFile(s.bundlePath(ptr.ID))
	if err != nil {
		return nil, err
	}
	var b Bundle
	if err := json.Unmarshal(braw, &b); err != nil {
		return nil, err
	}
	return &b, nil
}

// computeBundleHash 对 bundle 内容做规范哈希（内容寻址 + 冻结完整性校验）。
func computeBundleHash(b *Bundle) string {
	h := sha256.New()
	// prompts 按 key 排序，保证规范化（map 迭代序不定）。
	keys := make([]string, 0, len(b.Prompts))
	for k := range b.Prompts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(h, "prompt:%s=%s\n", k, b.Prompts[k])
	}
	if b.Params.Temperature != nil {
		fmt.Fprintf(h, "param:temperature=%v\n", *b.Params.Temperature)
	}
	if b.Params.MaxTokens != nil {
		fmt.Fprintf(h, "param:max_tokens=%d\n", *b.Params.MaxTokens)
	}
	fmt.Fprintf(h, "model:%s/%s\n", b.Model.Provider, b.Model.Name)
	fmt.Fprintf(h, "parent:%s\n", b.ParentID)
	return hex.EncodeToString(h.Sum(nil))
}
