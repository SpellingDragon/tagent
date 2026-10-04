// 契约: docs/wiki/memory/memory-architecture.md#consolidation
package memory

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/SpellingDragon/tagent/event"
)

const (
	// MetaReceiptKeys 等是 consolidation 事件的 Metadata 收据 schema 键（键名即存储字段）。
	MetaReceiptKeys = "receipt_keys"
	// MetaReceiptFingerprint 服务器侧算得的源内容指纹；客户端提交的同名值不参与校验。
	MetaReceiptFingerprint = "receipt_fingerprint"
	// MetaConsolidationKind 巩固产物种类：蒸馏或经验总结。
	MetaConsolidationKind = "consolidation_kind"
	// MetaConsolidationTrigger 触发来源，供审计回溯该产物为何产生。
	MetaConsolidationTrigger = "consolidation_trigger"
	// MetaSourceCount 声明的源事件条数，与收据实际条数互相校验。
	MetaSourceCount = "source_count"
)

// ComputeReceiptFingerprint 服务端指纹：对排序后的 (key, type, content) 逐条滚动 SHA1。
// 覆盖 content 使「源事件被篡改/重写入」可检出（防漂移）。确定性：同输入同指纹。
func ComputeReceiptFingerprint(events []FullEvent) string {
	sorted := make([]FullEvent, len(events))
	copy(sorted, events)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].EventKey < sorted[j].EventKey })
	h := sha1.New()
	for _, e := range sorted {
		fmt.Fprintf(h, "%d:%s:", e.EventKey, e.EventType)
		h.Write([]byte(e.Content))
		h.Write([]byte{0})
	}
	return "sha1:" + hex.EncodeToString(h.Sum(nil))
}

// ReceiptVerdict 是巩固事件收据的回放验证裁决。
type ReceiptVerdict struct {
	Total            int
	Resolved         int
	Tombstoned       int
	Missing          int
	FingerprintMatch bool
	Detail           string
}

// VerifyConsolidation 回放验证：解析收据 key → GetEvents 取源事件 → 重算指纹比对。
func VerifyConsolidation(store MemoryStore, evt FullEvent) ReceiptVerdict {
	v := ReceiptVerdict{}
	keysHex := evt.Metadata[MetaReceiptKeys]
	if strings.TrimSpace(keysHex) == "" {
		v.Detail = "无收据（非巩固事件或收据缺失）"
		return v
	}
	parts := strings.Split(keysHex, ",")
	v.Total = len(parts)
	keys := make([]int64, 0, len(parts))
	for _, hx := range parts {
		k, err := event.ParseEventKey(strings.TrimSpace(hx))
		if err != nil || k == 0 {
			v.Missing++
			continue
		}
		keys = append(keys, k)
	}
	found, _ := store.GetEvents(keys)
	v.Resolved = len(found)
	v.Tombstoned = len(keys) - len(found)
	if v.Resolved == v.Total && v.Missing == 0 && v.Tombstoned == 0 {
		v.FingerprintMatch = ComputeReceiptFingerprint(found) == evt.Metadata[MetaReceiptFingerprint]
	}
	v.Detail = fmt.Sprintf("收据 %d: 取回 %d, 墓碑 %d, 缺失 %d, 指纹%s",
		v.Total, v.Resolved, v.Tombstoned, v.Missing,
		map[bool]string{true: "匹配", false: "不匹配/不可判"}[v.FingerprintMatch])
	return v
}

// BuildConsolidationEvent 服务端构造巩固事件：拉取源事件、算指纹、封装收据 Metadata。
// 指纹由本函数（服务端）计算，LLM 无法伪造。返回待存储的 FullEvent（正 key、TTL 豁免
// 经注册表声明）与构造时的验证裁决。调用方（memory_consolidate 工具）负责 StoreEvent。
func BuildConsolidationEvent(store MemoryStore, partitionID int, content, kind, trigger string, sourceKeys []int64, minSources int) (FullEvent, ReceiptVerdict, error) {
	if strings.TrimSpace(content) == "" {
		return FullEvent{}, ReceiptVerdict{}, fmt.Errorf("consolidation content is empty")
	}
	uniq := make(map[int64]bool, len(sourceKeys))
	dedup := make([]int64, 0, len(sourceKeys))
	for _, k := range sourceKeys {
		if k > 0 && !uniq[k] {
			uniq[k] = true
			dedup = append(dedup, k)
		}
	}
	sort.Slice(dedup, func(i, j int) bool { return dedup[i] < dedup[j] })

	sources, err := store.GetEvents(dedup)
	if err != nil {
		return FullEvent{}, ReceiptVerdict{}, fmt.Errorf("fetch source events: %w", err)
	}
	if minSources > 0 && len(sources) < minSources {
		return FullEvent{}, ReceiptVerdict{}, fmt.Errorf(
			"consolidation rejected: only %d/%d source events resolved (min_source_events=%d) — refuse to fabricate memory from missing evidence",
			len(sources), len(dedup), minSources)
	}
	hexes := make([]string, 0, len(sources))
	for _, s := range sources {
		hexes = append(hexes, event.FormatEventKey(s.EventKey))
	}
	sort.Strings(hexes)
	fp := ComputeReceiptFingerprint(sources)

	key := NewSnowflakeEventKey(partitionID, 0)
	evt := FullEvent{
		EventKey:     key,
		PartitionID:  partitionID,
		EventType:    event.TypeConsolidation,
		EventSummary: summarizeConsolidation(content),
		Content:      content,
		Timestamp:    time.Now().UnixMilli(),
		Metadata: map[string]string{
			MetaReceiptKeys:          strings.Join(hexes, ","),
			MetaReceiptFingerprint:   fp,
			MetaConsolidationKind:    kind,
			MetaConsolidationTrigger: trigger,
			MetaSourceCount:          strconv.Itoa(len(sources)),
		},
	}
	verdict := ReceiptVerdict{
		Total: len(dedup), Resolved: len(sources),
		Tombstoned: len(dedup) - len(sources), FingerprintMatch: true,
		Detail: fmt.Sprintf("巩固 %d 源事件（请求 %d，取回 %d）", len(sources), len(dedup), len(sources)),
	}
	return evt, verdict, nil
}

// summarizeConsolidation 生成巩固事件的摘要视图（首行/截断，不折损 Content 本体）。
func summarizeConsolidation(content string) string {
	const maxRunes = 200
	if i := strings.IndexByte(content, '\n'); i >= 0 {
		content = content[:i]
	}
	r := []rune(strings.TrimSpace(content))
	if len(r) > maxRunes {
		return string(r[:maxRunes]) + "…"
	}
	return string(r)
}
