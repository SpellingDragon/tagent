package memory

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// RelationStore 维护事件间的因果关联图。
// 全量常驻内存，变更通过 WAL 持久化。
type RelationStore interface {
	// SetParent 设置/更新 parentKey（建立或修改因果链）
	SetParent(childKey, parentKey int64) error

	// GetParent 获取 parentKey（0 = 无前驱）
	GetParent(childKey int64) (int64, error)

	// GetChildren 获取所有直接后继（反向查询）
	GetChildren(parentKey int64) ([]int64, error)

	// GetParents 批量获取（memory_trace 热路径优化）
	GetParents(keys []int64) (map[int64]int64, error)

	// RemoveRelations 删除某事件的所有关联（逐出时调用）
	RemoveRelations(key int64) error

	// === 生命周期 ===

	// Snapshot 创建全量快照
	Snapshot() (map[int64]int64, error)

	// LoadSnapshot 从快照恢复
	LoadSnapshot(data map[int64]int64) error

	// ReplayJournal 重放 WAL（启动恢复）
	ReplayJournal(entries []JournalEntry) error

	// EventsCount 返回当前记录的事件数
	EventsCount() int
}

// JournalEntry 表示一条 WAL 日志记录。
type JournalEntry struct {
	Op        string // "+1" = SetParent, "-1" = RemoveRelations
	ChildKey  int64  // meaningful for SetParent
	ParentKey int64  // meaningful for SetParent
	EventKey  int64  // meaningful for RemoveRelations
}

// InMemRelationStore 实现了 RelationStore 接口。
// 内存双图（childToParent + parentToChildren）+ WAL journal。
type InMemRelationStore struct {
	mu sync.RWMutex

	// childKey → parentKey（正向，用于 GetParent / GetParents）
	childToParent map[int64]int64

	// parentKey → []childKey（反向索引，用于 GetChildren）
	parentToChildren map[int64][]int64

	// WAL 相关
	dataDir string
	journal *os.File
}

// NewInMemRelationStore 创建并初始化 InMemRelationStore。
// dataDir 用于存储 journal 和 snapshot 文件。
func NewInMemRelationStore(dataDir string) (*InMemRelationStore, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create relation store dir %s: %w", dataDir, err)
	}

	rs := &InMemRelationStore{
		childToParent:    make(map[int64]int64),
		parentToChildren: make(map[int64][]int64),
		dataDir:          dataDir,
	}

	// 打开或创建 journal 文件（append-only）
	journalPath := filepath.Join(dataDir, "relations.journal")
	f, err := os.OpenFile(journalPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open relations journal %s: %w", journalPath, err)
	}
	rs.journal = f

	// 尝试从快照 + journal 恢复
	if err := rs.recover(); err != nil {
		// 恢复失败时以空状态启动，打印错误日志但不阻止创建
		fmt.Fprintf(os.Stderr, "[memory] relation store recovery warning: %v\n", err)
	}

	return rs, nil
}

// journalPath 返回 journal 文件路径。
func (rs *InMemRelationStore) journalPath() string {
	return filepath.Join(rs.dataDir, "relations.journal")
}

// snapPath 返回 snapshot 文件路径。
func (rs *InMemRelationStore) snapPath() string {
	return filepath.Join(rs.dataDir, "relations.snap")
}

// recover 尝试从 snapshot + journal 恢复。
func (rs *InMemRelationStore) recover() error {
	// 1. 加载 snapshot（如果存在）
	snapFile := rs.snapPath()
	if _, err := os.Stat(snapFile); err == nil {
		data, err := os.ReadFile(snapFile)
		if err != nil {
			return fmt.Errorf("failed to read snapshot: %w", err)
		}
		var snapshotData map[int64]int64
		if err := json.Unmarshal(data, &snapshotData); err != nil {
			return fmt.Errorf("failed to unmarshal snapshot: %w", err)
		}
		if err := rs.LoadSnapshot(snapshotData); err != nil {
			return fmt.Errorf("failed to load snapshot: %w", err)
		}
	}

	// 2. 重放 journal 增量
	journalPath := rs.journalPath()
	if _, err := os.Stat(journalPath); err == nil {
		f, err := os.Open(journalPath)
		if err != nil {
			return fmt.Errorf("failed to open journal for replay: %w", err)
		}
		defer f.Close()

		// 获取文件大小以确定是否有 snapshot 后的增量
		info, _ := f.Stat()
		if info.Size() == 0 {
			return nil
		}

		var entries []JournalEntry
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}
			entry, err := parseJournalLine(line)
			if err != nil {
				// 不完整行：journal 末尾可能因崩溃被截断，忽略最后一行
				continue
			}
			entries = append(entries, entry)
		}

		if len(entries) > 0 {
			if err := rs.ReplayJournal(entries); err != nil {
				return fmt.Errorf("failed to replay journal: %w", err)
			}
		}
	}

	return nil
}

// SetParent 设置/更新 parentKey。
func (rs *InMemRelationStore) SetParent(childKey, parentKey int64) error {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	// 获取旧的 parentKey（如果存在）
	oldParent, hadOld := rs.childToParent[childKey]

	// 如果父级相同，跳过（幂等）
	if hadOld && oldParent == parentKey {
		return nil
	}

	// 从旧的 parent 的 children 列表中移除
	if hadOld {
		rs.removeFromChildren(oldParent, childKey)
	}

	// 更新正向映射
	rs.childToParent[childKey] = parentKey

	// 更新反向映射
	rs.parentToChildren[parentKey] = append(rs.parentToChildren[parentKey], childKey)

	// 写入 journal
	if err := rs.appendJournal(fmt.Sprintf("+1:%d:%d\n", childKey, parentKey)); err != nil {
		return err
	}

	return nil
}

// GetParent 获取 childKey 的 parentKey。
func (rs *InMemRelationStore) GetParent(childKey int64) (int64, error) {
	rs.mu.RLock()
	defer rs.mu.RUnlock()

	parentKey, ok := rs.childToParent[childKey]
	if !ok {
		return 0, nil
	}
	return parentKey, nil
}

// GetChildren 获取 parentKey 的所有直接后继。
func (rs *InMemRelationStore) GetChildren(parentKey int64) ([]int64, error) {
	rs.mu.RLock()
	defer rs.mu.RUnlock()

	children, ok := rs.parentToChildren[parentKey]
	if !ok || len(children) == 0 {
		return []int64{}, nil
	}

	// 返回副本，防止外部修改
	result := make([]int64, len(children))
	copy(result, children)
	return result, nil
}

// GetParents 批量获取 parentKey。
func (rs *InMemRelationStore) GetParents(keys []int64) (map[int64]int64, error) {
	rs.mu.RLock()
	defer rs.mu.RUnlock()

	result := make(map[int64]int64, len(keys))
	for _, key := range keys {
		if parentKey, ok := rs.childToParent[key]; ok {
			result[key] = parentKey
		} else {
			result[key] = 0
		}
	}
	return result, nil
}

// RemoveRelations 删除某事件的所有关联。
func (rs *InMemRelationStore) RemoveRelations(key int64) error {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	// 1. 从 parent 的 children 列表中移除
	if parentKey, ok := rs.childToParent[key]; ok {
		rs.removeFromChildren(parentKey, key)
	}

	// 2. 从 childToParent 中移除
	delete(rs.childToParent, key)

	// 3. 移除反向索引中作为 parent 的条目
	delete(rs.parentToChildren, key)

	// 4. 写入 journal
	if err := rs.appendJournal(fmt.Sprintf("-1:%d\n", key)); err != nil {
		return err
	}

	return nil
}

// Snapshot 创建全量快照。
func (rs *InMemRelationStore) Snapshot() (map[int64]int64, error) {
	rs.mu.RLock()
	defer rs.mu.RUnlock()

	// 复制 childToParent map
	snapshot := make(map[int64]int64, len(rs.childToParent))
	for k, v := range rs.childToParent {
		snapshot[k] = v
	}
	return snapshot, nil
}

// LoadSnapshot 从快照恢复。
func (rs *InMemRelationStore) LoadSnapshot(data map[int64]int64) error {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	// 重置
	rs.childToParent = make(map[int64]int64, len(data))
	rs.parentToChildren = make(map[int64][]int64)

	for child, parent := range data {
		rs.childToParent[child] = parent
		rs.parentToChildren[parent] = append(rs.parentToChildren[parent], child)
	}

	return nil
}

// ReplayJournal 重放 WAL。
func (rs *InMemRelationStore) ReplayJournal(entries []JournalEntry) error {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	for _, entry := range entries {
		switch entry.Op {
		case "+1":
			// SetParent
			oldParent, hadOld := rs.childToParent[entry.ChildKey]
			if hadOld {
				rs.removeFromChildren(oldParent, entry.ChildKey)
			}
			rs.childToParent[entry.ChildKey] = entry.ParentKey
			rs.parentToChildren[entry.ParentKey] = append(rs.parentToChildren[entry.ParentKey], entry.ChildKey)

		case "-1":
			// RemoveRelations
			if parentKey, ok := rs.childToParent[entry.EventKey]; ok {
				rs.removeFromChildren(parentKey, entry.EventKey)
			}
			delete(rs.childToParent, entry.EventKey)
			delete(rs.parentToChildren, entry.EventKey)

		default:
			return fmt.Errorf("unknown journal operation: %s", entry.Op)
		}
	}

	return nil
}

// EventsCount 返回当前记录的事件数。
func (rs *InMemRelationStore) EventsCount() int {
	rs.mu.RLock()
	defer rs.mu.RUnlock()
	return len(rs.childToParent)
}

// ==================== 内部方法 ====================

// removeFromChildren 从 parent 的 children 列表中移除特定 child。
// 必须在持有 mu.Lock 时调用。
func (rs *InMemRelationStore) removeFromChildren(parentKey, childKey int64) {
	children, ok := rs.parentToChildren[parentKey]
	if !ok {
		return
	}
	for i, c := range children {
		if c == childKey {
			rs.parentToChildren[parentKey] = append(children[:i], children[i+1:]...)
			break
		}
	}
	// 如果 children 列表为空，清理空条目
	if len(rs.parentToChildren[parentKey]) == 0 {
		delete(rs.parentToChildren, parentKey)
	}
}

// appendJournal 追加一行到 journal 文件。
func (rs *InMemRelationStore) appendJournal(line string) error {
	if rs.journal == nil {
		return nil
	}
	if _, err := rs.journal.WriteString(line); err != nil {
		return fmt.Errorf("failed to write journal: %w", err)
	}
	if err := rs.journal.Sync(); err != nil {
		return fmt.Errorf("failed to sync journal: %w", err)
	}
	return nil
}

// SaveSnapshotToFile 将当前关系图的快照保存到文件。
// 同时截断 journal（snapshot 后所有变更已固化）。
func (rs *InMemRelationStore) SaveSnapshotToFile() error {
	snapshot, err := rs.Snapshot()
	if err != nil {
		return err
	}

	data, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("failed to marshal snapshot: %w", err)
	}

	// 原子写入：先写临时文件再 rename
	tmpPath := rs.snapPath() + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write snapshot temp: %w", err)
	}
	if err := os.Rename(tmpPath, rs.snapPath()); err != nil {
		return fmt.Errorf("failed to rename snapshot: %w", err)
	}

	// 截断 journal
	if err := rs.truncateJournal(); err != nil {
		return fmt.Errorf("failed to truncate journal: %w", err)
	}

	return nil
}

// truncateJournal 截断 journal 文件。
func (rs *InMemRelationStore) truncateJournal() error {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	if rs.journal != nil {
		rs.journal.Close()
	}

	journalPath := rs.journalPath()
	f, err := os.OpenFile(journalPath, os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to truncate journal: %w", err)
	}
	rs.journal = f
	return nil
}

// Close 关闭 store，释放资源。
func (rs *InMemRelationStore) Close() error {
	if rs.journal != nil {
		return rs.journal.Close()
	}
	return nil
}

// ==================== Journal 解析 ====================

// parseJournalLine 解析单行 journal 记录。
// 格式: +1:childKey:parentKey 或 -1:eventKey
func parseJournalLine(line string) (JournalEntry, error) {
	if len(line) < 4 {
		return JournalEntry{}, fmt.Errorf("invalid journal line: %s", line)
	}

	op := line[:2]
	rest := line[3:] // skip ":"

	switch op {
	case "+1":
		parts := splitAndParse(rest, ':')
		if len(parts) < 2 {
			return JournalEntry{}, fmt.Errorf("invalid SetParent journal line: %s", line)
		}
		return JournalEntry{
			Op:        op,
			ChildKey:  parts[0],
			ParentKey: parts[1],
		}, nil

	case "-1":
		parts := splitAndParse(rest, ':')
		if len(parts) < 1 {
			return JournalEntry{}, fmt.Errorf("invalid RemoveRelations journal line: %s", line)
		}
		return JournalEntry{
			Op:       op,
			EventKey: parts[0],
		}, nil

	default:
		return JournalEntry{}, fmt.Errorf("unknown journal op: %s", op)
	}
}

// splitAndParse 按分隔符分割字符串并解析为 int64 切片。
func splitAndParse(s string, sep byte) []int64 {
	var result []int64
	current := int64(0)
	found := false
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			result = append(result, current)
			current = 0
			found = false
		} else {
			current = current*10 + int64(s[i]-'0')
			found = true
		}
	}
	if found {
		result = append(result, current)
	}
	return result
}
