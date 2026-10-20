// host_memory_test 钉住 宿主内存装配的红线：声明的 memory 段合并成一份共享存储层与一个语义引擎，
// 引擎的嵌入连接由核心默认实现装配（provider/model/宽度/端点即声明），缺 key 在装配期具名拒绝，
// 未声明的宿主不接存储层、不建嵌入器、不读调参。
// 契约: docs/wiki/examples/wechat-bot-runtime.md#startup-and-routing
package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/SpellingDragon/tagent/api/config"
	coreembedder "github.com/SpellingDragon/tagent/memory/embedder"
	"github.com/SpellingDragon/tagent/memory/ledger"
)

// TestShippedMemoryEngineWiresTheSemanticRetrieval pins the shipped memory declaration as the one the host reads.
//   - the memory sections merge into one localfile tier over the existing data root
//   - the engine keeps the zhipu embedding-3 connection, its 512 width and the tuning
//   - the assembled embedder is the core provider-backed one, not the hash stand-in
//   - read_namespaces widen the recall authorization beside the agent's own namespace
func TestShippedMemoryEngineWiresTheSemanticRetrieval(t *testing.T) {
	t.Setenv("ZAI_API_KEY", "test-key")
	doc, err := loadHostDocument("tagent.yaml")
	if err != nil {
		t.Fatalf("load the shipped document: %v", err)
	}
	settings, err := doc.memorySettings()
	if err != nil {
		t.Fatalf("merge the memory declarations: %v", err)
	}
	if settings == nil {
		t.Fatal("the shipped document declares memory but merged into none")
	}
	want := ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: ".wechat-config/data"}
	if settings.storage != want {
		t.Fatalf("storage = %+v, want %+v", settings.storage, want)
	}
	engine := settings.engine
	if engine == nil {
		t.Fatal("the shipped document declares no semantic engine")
	}
	if engine.Backend != "memory" || engine.Embedding.Provider != "zhipu" ||
		engine.Embedding.Model != "embedding-3" || engine.Embedding.APIKeyEnv != "ZAI_API_KEY" ||
		engine.Embedding.Dimensions != 512 {
		t.Fatalf("engine = %+v, want the zhipu embedding-3 connection of 512 dimensions", engine)
	}
	if engine.VectorTopK != 20 || engine.KeywordTopK != 20 || engine.RRFK != 60 {
		t.Fatalf("engine tuning = %d/%d/%d, want 20/20/60", engine.VectorTopK, engine.KeywordTopK, engine.RRFK)
	}

	embedded, err := doc.embedder()
	if err != nil {
		t.Fatalf("assemble the declared embedder: %v", err)
	}
	provider, ok := embedded.(*coreembedder.Embedder)
	if !ok {
		t.Fatalf("embedder = %T, want the core provider-backed embedder rather than the hash stand-in", embedded)
	}
	connection := provider.Connection()
	if connection.Model != "embedding-3" || connection.Dimensions != 512 || connection.Provider != "zhipu" {
		t.Fatalf("connection = %+v, want the declared zhipu embedding-3 binding", connection)
	}
	if want := "https://open.bigmodel.cn/api/paas/v4"; connection.APIEndpoint != want {
		t.Fatalf("embedder endpoint = %q, want the zhipu default %q", connection.APIEndpoint, want)
	}

	vectorTopK, keywordTopK, rrfK, err := doc.retrievalTuning()
	if err != nil {
		t.Fatalf("read the retrieval tuning: %v", err)
	}
	if vectorTopK != 20 || keywordTopK != 20 || rrfK != 60 {
		t.Fatalf("retrieval tuning = %d/%d/%d, want 20/20/60", vectorTopK, keywordTopK, rrfK)
	}

	names := doc.readNamespaces("recall")
	if len(names) != 2 || names[0] != "recall" || names[1] != "tagent" {
		t.Fatalf("recall read namespaces = %v, want the own namespace beside the declared tagent read", names)
	}
}

// TestMemoryEngineRefusesWithoutTheKey refuses an engine whose key variable holds nothing by name.
func TestMemoryEngineRefusesWithoutTheKey(t *testing.T) {
	t.Setenv("ZAI_API_KEY", "")
	doc, err := loadHostDocument("tagent.yaml")
	if err != nil {
		t.Fatalf("load the shipped document: %v", err)
	}
	_, err = doc.embedder()
	if !errors.Is(err, ErrEmbeddingKey) {
		t.Fatalf("assemble the embedder beside an empty key: error %v, want ErrEmbeddingKey", err)
	}
	if !strings.Contains(err.Error(), "ZAI_API_KEY") {
		t.Fatalf("the refusal %q does not name the empty variable", err)
	}
}

// TestMemoryDeclarationRefusesInconsistentShapes pins the merge refusals:
// - two agents naming different storage tiers are refused by name
// - an engine beside no storage tier is refused by name
// - an unknown engine backend is refused by name
func TestMemoryDeclarationRefusesInconsistentShapes(t *testing.T) {
	cases := []struct {
		name string
		doc  *hostDocument
		want error
	}{
		{
			name: "two tiers",
			doc: &hostDocument{
				Entry: "a",
				Agents: map[string]agentEntry{
					"a": {Memory: config.MemorySpec{Type: "localfile", Path: "one"}},
					"b": {Memory: config.MemorySpec{Type: "localfile", Path: "two"}},
				},
			},
			want: ErrMemoryDeclaration,
		},
		{
			name: "engine beside no tier",
			doc: &hostDocument{
				Entry: "a",
				Agents: map[string]agentEntry{
					"a": {Memory: config.MemorySpec{Engine: config.EngineSpec{Backend: "memory"}}},
				},
			},
			want: ErrMemoryDeclaration,
		},
		{
			name: "unknown backend",
			doc: &hostDocument{
				Entry: "a",
				Agents: map[string]agentEntry{
					"a": {Memory: config.MemorySpec{
						Type: "localfile",
						Path: "one",
						Engine: config.EngineSpec{
							Backend:   "kv",
							Embedding: config.EmbeddingSpec{Provider: "zhipu", Model: "embedding-3", APIKeyEnv: "ZAI_API_KEY", Dimensions: 512},
						},
					}},
				},
			},
			want: ErrMemoryEngine,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.doc.memorySettings()
			if !errors.Is(err, tc.want) {
				t.Fatalf("merge the declarations: error %v, want %v", err, tc.want)
			}
		})
	}
}

// TestMemoryDeclarationZeroValueKeepsTheHostSilent wires no tier, embedder or tuning beside no declaration.
func TestMemoryDeclarationZeroValueKeepsTheHostSilent(t *testing.T) {
	doc := &hostDocument{Entry: "a", Agents: map[string]agentEntry{"a": {Model: "m"}}}
	if cfg, err := doc.storageConfig(); err != nil || cfg != nil {
		t.Fatalf("storage config = %+v, %v, want nil beside no declaration", cfg, err)
	}
	if embedded, err := doc.embedder(); err != nil || embedded != nil {
		t.Fatalf("embedder = %v, %v, want nil beside no declaration", embedded, err)
	}
	if v, k, r, err := doc.retrievalTuning(); err != nil || v != 0 || k != 0 || r != 0 {
		t.Fatalf("retrieval tuning = %d/%d/%d, %v, want the zero tuning", v, k, r, err)
	}
}
