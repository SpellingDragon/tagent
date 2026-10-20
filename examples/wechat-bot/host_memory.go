// 契约: docs/wiki/memory/memory-architecture.md#read-paths
//
// The memory assembly of the host: the memory declaration names one shared
// storage tier, the semantic engine behind the recall vocabulary and the
// namespaces one agent may read across partitions, and this file merges those
// declarations into the faces the run and the recall tools are wired over. The
// embedding connection itself is the core default implementation; the host only
// names the declaration it assembles.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/embedder"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/memory/replay"
)

// ErrMemoryDeclaration reports a memory section this host cannot merge into
// one storage tier and one semantic engine: a declaration that names half a
// storage tier, two agents that name different tiers or engines, or an engine
// beside no tier at all.
var ErrMemoryDeclaration = errors.New("wechat-bot: the memory declaration is inconsistent")

// ErrMemoryEngine reports a semantic engine declaration this host cannot
// assemble: an unknown backend or provider, a connection missing its model,
// key variable or vector width, a negative retrieval bound, or an endpoint
// that is not an absolute http(s) URL.
var ErrMemoryEngine = errors.New("wechat-bot: the memory engine declaration is invalid")

// ErrEmbeddingKey reports an embedding connection whose key variable names no
// value in the environment.
var ErrEmbeddingKey = embedder.ErrEmbeddingKey

// memoryEngineBackend is the only engine backend this host wires: the semantic
// retrieval of the memory side. A document naming another backend is refused
// by name instead of being read as this one.
const memoryEngineBackend = "memory"

// memorySettings is the merged memory declaration of one document: the one
// storage tier every declaring agent shares, and the one semantic engine its
// recall vocabulary generates embeddings under.
type memorySettings struct {
	storage ledger.StorageConfig
	engine  *config.EngineSpec
}

// declaresEngine reports whether a memory engine section names anything.
func declaresEngine(engine config.EngineSpec) bool {
	return strings.TrimSpace(engine.Backend) != "" ||
		strings.TrimSpace(engine.Embedding.Provider) != "" ||
		strings.TrimSpace(engine.Embedding.Model) != "" ||
		strings.TrimSpace(engine.Embedding.APIKeyEnv) != "" ||
		engine.Embedding.Dimensions != 0 ||
		strings.TrimSpace(engine.Embedding.APIEndpoint) != "" ||
		engine.VectorTopK != 0 || engine.KeywordTopK != 0 || engine.RRFK != 0
}

// memorySettings merges every agent's memory declaration into the one storage
// tier and the one semantic engine the run is assembled with. Every declaring
// agent must name the same tier and the same engine: a shared store read under
// two different engines would answer one query two ways. An engine beside no
// tier is refused by name, because an embedding with nowhere to persist its
// vectors is no engine at all.
func (d *hostDocument) memorySettings() (*memorySettings, error) {
	settings := &memorySettings{}
	declaredBy := ""
	engineBy := ""
	for _, id := range d.agentOrder() {
		entry := d.Agents[id].Memory
		storageType := strings.TrimSpace(entry.Type)
		storagePath := strings.TrimSpace(entry.Path)
		storageDeclared := storageType != "" || storagePath != ""
		if storageDeclared && (storageType == "" || storagePath == "") {
			return nil, fmt.Errorf("%w: agent %q names half a storage tier (type and path are both required)", ErrMemoryDeclaration, id)
		}
		if storageDeclared {
			candidate := ledger.StorageConfig{Type: ledger.StorageType(storageType), Root: storagePath}
			if declaredBy == "" {
				settings.storage = candidate
				declaredBy = id
			} else if settings.storage != candidate {
				return nil, fmt.Errorf("%w: agents %q and %q name different storage tiers", ErrMemoryDeclaration, declaredBy, id)
			}
		}
		if declaresEngine(entry.Engine) {
			engine := entry.Engine
			if engineBy == "" {
				settings.engine = &engine
				engineBy = id
			} else if *settings.engine != engine {
				return nil, fmt.Errorf("%w: agents %q and %q name different semantic engines", ErrMemoryDeclaration, engineBy, id)
			}
		}
	}
	topType := strings.TrimSpace(d.Storage.Type)
	topRoot := strings.TrimSpace(d.Storage.Root)
	if topType != "" {
		top := ledger.StorageConfig{Type: ledger.StorageType(topType), Root: topRoot}
		if declaredBy != "" && settings.storage != top {
			return nil, fmt.Errorf("%w: the top-level storage disagrees with the memory tier agent %q declares", ErrMemoryDeclaration, declaredBy)
		}
		if declaredBy == "" {
			settings.storage = top
		}
	}
	if settings.storage.Type == "" && settings.engine == nil {
		return nil, nil
	}
	if settings.storage.Type == "" {
		return nil, fmt.Errorf("%w: the semantic engine requires a storage tier", ErrMemoryDeclaration)
	}
	if settings.engine != nil {
		if err := validateEngine(*settings.engine); err != nil {
			return nil, err
		}
	}
	return settings, nil
}

// storageConfig reports the storage tier the document names, or nil when it
// names none.
func (d *hostDocument) storageConfig() (*ledger.StorageConfig, error) {
	settings, err := d.memorySettings()
	if err != nil || settings == nil {
		return nil, err
	}
	cfg := settings.storage
	return &cfg, nil
}

// embedder reports the embedding binding the recall retrieval of this host
// generates its vectors under: the core default implementation over the
// configured semantic engine, and none at all when the document declares no
// engine (the keyword-only retrieval).
func (d *hostDocument) embedder() (memory.Embedder, error) {
	settings, err := d.memorySettings()
	if err != nil {
		return nil, err
	}
	if settings == nil || settings.engine == nil {
		return nil, nil
	}
	built, err := embedder.New(settings.engine.Embedding)
	if err != nil {
		return nil, err
	}
	return built, nil
}

// retrievalTuning reports the two leg bounds and the rank constant the merged
// engine declares. A document that declares no engine reports the zero tuning,
// which keeps the retrieval exactly as it stood.
func (d *hostDocument) retrievalTuning() (int, int, int, error) {
	settings, err := d.memorySettings()
	if err != nil {
		return 0, 0, 0, err
	}
	if settings == nil || settings.engine == nil {
		return 0, 0, 0, nil
	}
	return settings.engine.VectorTopK, settings.engine.KeywordTopK, settings.engine.RRFK, nil
}

// readNamespaces reports the partitions one declared agent may read through
// its recall tools: its own namespace first, then every cross-namespace read
// its memory declaration grants, deduplicated in declaration order.
func (d *hostDocument) readNamespaces(id string) []string {
	names := []string{id}
	seen := map[string]bool{id: true}
	for _, name := range d.Agents[id].Memory.ReadNamespaces {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		names = append(names, trimmed)
	}
	return names
}

// relations reports the causal relation ledger the recall trace of this host
// walks: the journal under the storage root when the document declares one, and
// an in-process ledger otherwise. The runtime wires no relation ledger, so this
// face belongs to the host.
func (d *hostDocument) relations() (*replay.MemoryRelations, error) {
	cfg, err := d.storageConfig()
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return replay.NewMemoryRelations(), nil
	}
	root := filepath.Join(cfg.Root, "relations")
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("wechat-bot: create the relation journal %s: %w", root, err)
	}
	relations, err := replay.OpenMemoryRelations(root)
	if err != nil {
		return nil, fmt.Errorf("wechat-bot: open the relation journal %s: %w", root, err)
	}
	return relations, nil
}

// validateEngine validates one merged semantic engine: the backend must be the
// one this host wires, the retrieval bounds must not be negative, and the
// embedding connection must name everything its provider needs.
func validateEngine(engine config.EngineSpec) error {
	backend := strings.TrimSpace(engine.Backend)
	if backend == "" {
		return fmt.Errorf("%w: the engine names no backend", ErrMemoryEngine)
	}
	if backend != memoryEngineBackend {
		return fmt.Errorf("%w: backend %q is not a known engine backend", ErrMemoryEngine, backend)
	}
	if engine.VectorTopK < 0 || engine.KeywordTopK < 0 || engine.RRFK < 0 {
		return fmt.Errorf("%w: the engine carries a negative retrieval bound (vector_top_k=%d keyword_top_k=%d rrf_k=%d)",
			ErrMemoryEngine, engine.VectorTopK, engine.KeywordTopK, engine.RRFK)
	}
	if err := embedder.Validate(engine.Embedding); err != nil {
		return fmt.Errorf("%w: %w", ErrMemoryEngine, err)
	}
	return nil
}
