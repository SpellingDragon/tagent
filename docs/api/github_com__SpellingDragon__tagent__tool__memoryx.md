package memoryx // import "github.com/SpellingDragon/tagent/tool/memoryx"

Package memoryx 提供记忆策展工具（T-D）：memory_consolidate（证据门控巩固）与
memory_health（维度锚定诊断）。二者是 agent 面向的记忆策展入口——巩固让 LLM 提交 {content, source_keys}
由服务端算指纹入库（防伪造），诊断让 LLM 查询记忆健康度。

FUNCTIONS

func NewConsolidateTool(store memory.MemoryStore, partitionID int) tool.Tool
    NewConsolidateTool 构建 memory_consolidate 工具（证据门控巩固）。 服务端构造：工具自己 GetEvents
    拉源事件算指纹后入库——LLM 在 content 里手写任何 "fingerprint" 都无意义（Metadata 由工具构造，防伪造）。
    NewConsolidateTool 构造巩固工具（minSources=0：不校验源数，现状兼容）。

func NewConsolidateToolWithGate(store memory.MemoryStore, partitionID, minSources int) tool.Tool
    NewConsolidateToolWithGate带 min_source_events 硬门控：实际取回源不足时
    memory.BuildConsolidationEvent 显式拒绝。

func NewHealthTool(engine memory.MemoryEngine, store memory.MemoryStore) tool.Tool
    NewHealthTool 构建 memory_health 工具（维度锚定诊断，读引擎+store 实时态）。 engine/store 可为
    nil（对应维度省略）。

func RegisterSubTools()
    RegisterSubTools 注册记忆策展工具到全局注册表（memory_consolidate/memory_health）。

