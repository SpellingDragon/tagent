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

// ==================== 证据门控巩固（T-D · 记忆策展）====================
//
// 巩固产物（冥想蒸馏/经验总结）必须携带源事件 EventKey 收据 + 服务端指纹，可回放验证。
// 借鉴 OpenSquilla Memory Dream（可回放收据 + 指纹钉住）+ MemoHarness（双层经验库 E+G：
// E 层=原始事件流可被 TTL 遗忘，G 层=consolidation 事件 TTL 豁免长存）。
//
// 防伪造核心：指纹只由服务端（BuildConsolidationEvent）计算——LLM 提交 {content, 源keys}，
// 工具自己 GetEvents 拉源事件算 SHA1 后写入 Metadata。LLM 在 content 里手写任何
// "fingerprint" 字符串都无意义（Metadata 由工具构造，非 LLM）。源事件不可变（事件溯源
// 无 Update 路径），故指纹长期有效；源事件被 TTL 删除后收据进入 Tombstoned 状态——这是
// 诚实的衰减信号而非错误（诊断维度 receipt_integrity 直接度量它）。

// consolidation 事件的 Metadata 收据 schema 键（报告 D2 §4.4.1）。
const (
	MetaReceiptKeys          = "receipt_keys"          // 源事件 EventKey hex 逗号列表
	MetaReceiptFingerprint   = "receipt_fingerprint"   // 服务端 SHA1 指纹
	MetaConsolidationKind    = "consolidation_kind"    // meditation_digest/experience_distill/manual
	MetaConsolidationTrigger = "consolidation_trigger" // capacity/value/meditation/manual
	MetaSourceCount          = "source_count"          // 收据条数（冗余，便于诊断不解析列表）
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
	Total            int    // 收据条数
	Resolved         int    // 成功取回的源事件数
	Tombstoned       int    // 解析成功但已被 TTL/墓碑删除（诚实衰减，非错误）
	Missing          int    // key 无法解析/从未存在
	FingerprintMatch bool   // 全部 resolve 时重算指纹比对；有缺失则 false
	Detail           string // 人类可读裁决（供工具返回）
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
	found, _ := store.GetEvents(keys) // GetEvents 跳过缺失/墓碑
	v.Resolved = len(found)
	v.Tombstoned = len(keys) - len(found)
	// 指纹比对仅当全部收据都取回才有意义（否则重算集合不同）。
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
func BuildConsolidationEvent(store MemoryStore, partitionID int, content, kind, trigger string, sourceKeys []int64) (FullEvent, ReceiptVerdict, error) {
	if strings.TrimSpace(content) == "" {
		return FullEvent{}, ReceiptVerdict{}, fmt.Errorf("consolidation content is empty")
	}
	// 去重 + 排序源 key（确定性收据）。
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
	// 收据 hex 列表基于**实际取回**的源事件（墓碑/缺失的不入收据，诚实）。
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
