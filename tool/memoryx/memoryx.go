// Package memoryx 提供记忆策展工具（T-D）：memory_consolidate（证据门控巩固）与
// memory_health（维度锚定诊断）。二者是 agent 面向的记忆策展入口——巩固让 LLM 提交
// {content, source_keys} 由服务端算指纹入库（防伪造），诊断让 LLM 查询记忆健康度。
package memoryx

import (
	"context"
	"fmt"
	"strings"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

// consolidateArgs 是 memory_consolidate 的入参（LLM 提交巩固内容 + 源事件票据）。
type consolidateArgs struct {
	Content    string   `json:"content" jsonschema:"description=巩固后的经验/知识内容（蒸馏产物）"`
	SourceKeys []string `json:"source_keys" jsonschema:"description=源事件 EventKey hex 列表（收据；从时间线 [evt_...] 票据取）"`
	Kind       string   `json:"kind,omitempty" jsonschema:"description=巩固类型 meditation_digest/experience_distill/manual（默认 manual）"`
}

// consolidateResult 是巩固结果（含服务端指纹与回放验证裁决）。
type consolidateResult struct {
	Key         string `json:"key"`                   // 巩固事件 EventKey hex
	Fingerprint string `json:"fingerprint"`           // 服务端 SHA1 指纹（LLM 不可伪造）
	Resolved    int    `json:"resolved"`              // 收据取回的源事件数
	Tombstoned  int    `json:"tombstoned"`            // 已遗忘的源事件数（诚实衰减）
	Verified    bool   `json:"verified"`              // 回放验证指纹是否匹配
	Message     string `json:"message"`
}

// NewConsolidateTool 构建 memory_consolidate 工具（证据门控巩固）。
// 服务端构造：工具自己 GetEvents 拉源事件算指纹后入库——LLM 在 content 里手写任何
// "fingerprint" 都无意义（Metadata 由工具构造，防伪造）。
func NewConsolidateTool(store memory.MemoryStore, partitionID int) tool.Tool {
	return function.NewFunctionTool(
		func(ctx context.Context, args consolidateArgs) (consolidateResult, error) {
			if strings.TrimSpace(args.Content) == "" {
				return consolidateResult{}, fmt.Errorf("content 不能为空")
			}
			keys := make([]int64, 0, len(args.SourceKeys))
			for _, hx := range args.SourceKeys {
				k, err := event.ParseEventKey(strings.TrimSpace(hx))
				if err != nil || k == 0 {
					continue // 跳过非法票据（诚实：不伪造收据）
				}
				keys = append(keys, k)
			}
			kind := args.Kind
			if kind == "" {
				kind = "manual"
			}
			evt, _, err := memory.BuildConsolidationEvent(store, partitionID, args.Content, kind, "manual", keys)
			if err != nil {
				return consolidateResult{}, fmt.Errorf("构造巩固事件失败: %w", err)
			}
			if err := store.StoreEvent(evt.EventKey, evt); err != nil {
				return consolidateResult{}, fmt.Errorf("存储巩固事件失败: %w", err)
			}
			v := memory.VerifyConsolidation(store, evt)
			return consolidateResult{
				Key:         event.FormatEventKey(evt.EventKey),
				Fingerprint: evt.Metadata[memory.MetaReceiptFingerprint],
				Resolved:    v.Resolved,
				Tombstoned:  v.Tombstoned,
				Verified:    v.FingerprintMatch,
				Message:     fmt.Sprintf("巩固 %d 源事件，服务端指纹已钉住（%s）", v.Resolved, v.Detail),
			}, nil
		},
		function.WithName("memory_consolidate"),
		function.WithDescription("证据门控记忆巩固：把跨时间的经验/知识蒸馏为一条长期记忆（TTL 豁免）。"+
			"提交 content（蒸馏产物）+ source_keys（源事件 [evt_...] 票据 hex 列表作为收据）。"+
			"服务端拉取源事件计算 SHA1 指纹钉住（防伪造、可回放验证）；源事件被遗忘后收据诚实标记为衰减。"),
	)
}

// healthResult 是 memory_health 的输出（维度锚定诊断快照）。
type healthResult struct {
	IndexHealth   float64 `json:"index_health"`
	VectorCount   int64   `json:"vector_count"`
	VectorIndexed int64   `json:"vector_indexed"`
	VectorDropped int64   `json:"vector_dropped"`
	DimMismatch   int64   `json:"vector_dim_mismatch"`
	CapHybrid     bool    `json:"cap_hybrid"`
	EngineReady   bool    `json:"engine_ready"`
	TotalEvents   int     `json:"total_events"`
	StorageSize   int64   `json:"storage_size"`
}

// NewHealthTool 构建 memory_health 工具（维度锚定诊断，读引擎+store 实时态）。
// engine/store 可为 nil（对应维度省略）。
func NewHealthTool(engine memory.MemoryEngine, store memory.MemoryStore) tool.Tool {
	diag := memory.NewMemoryDiagnostics(engine, store)
	return function.NewFunctionTool(
		func(ctx context.Context, args struct{}) (healthResult, error) {
			s := diag.Snapshot()
			return healthResult{
				IndexHealth: s.IndexHealth, VectorCount: s.VectorCount, VectorIndexed: s.VectorIndexed,
				VectorDropped: s.VectorDropped, DimMismatch: s.VectorDimMismatch, CapHybrid: s.CapHybrid,
				EngineReady: s.EngineReady, TotalEvents: s.TotalEvents, StorageSize: s.StorageSize,
			}, nil
		},
		function.WithName("memory_health"),
		function.WithDescription("查询记忆子系统健康度（维度锚定诊断）：向量索引健康率、检索能力"+
			"（keyword/vector/hybrid）、维度不匹配（换模型信号）、存储规模。用于判断语义召回是否退化。"),
	)
}
