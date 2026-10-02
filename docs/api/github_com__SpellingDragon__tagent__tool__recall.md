package recall // import "github.com/SpellingDragon/tagent/tool/recall"

memory_recall: the recall PROTOCOL implementation, now internal — the
model-facing entry is the unified `recall` tool (recall.go) which routes
items/query through recallByItems/recallByQuery below.

Index cards are recall tickets. PURE FUNCTION paths — no LLM in the
deterministic route. Input-shape dispatch (items take precedence):

items: [{key, hint?}] → engineering recall: batch GetEvent, original order, zero
hallucination, misses reported query + filters → semantic recall: QueryOptions
keyword search (the retrieval layer may evolve independently — keyword → vector
— the entry protocol stays)

recall: the UNIFIED recall entry.

One tool — parameters ARE the router. Deterministic shapes never touch an LLM:

orchestrate: true → explicit opt-in for the RecallAgent LLM orchestration engine
(checked first); when the engine is not wired it returns explicit guidance,
never silently falling back to a deterministic path items: [{key, hint?}] →
engineering recall: batch GetEvent, original order, zero hallucination, misses
reported turn_key → causal-chain turn reconstruction: walk back to the turn's
external_input (recovers HOW a past task was executed, incl. compressed tool
steps) query + filters → retrieval-layer recall: QueryOptions keyword search
(may evolve to vector; protocol unchanged)

Supersedes the retired memory_recall / memory_turn tool names — the model side
sees one tool; the output protocol ({key, type, summary, content, time} entries)
is unchanged across shapes.

FUNCTIONS

func NewAgent(cfg Config) (*agent.TagentAgent, error)
    NewAgent creates a TagentAgent configured for intelligent memory recall.
    The returned *agent.TagentAgent implements agent.Agent and can be wrapped
    via agenttool.NewTool() to become a CallableTool for the main TagentAgent.

func NewMemoryRecallTool(accessor tagenttool.MemoryStoreAccessor, readPartitionIDs []int) tool.Tool
    NewMemoryRecallTool 构造协议级召回工具（纯函数，无子 agent 绕行）：items 票据形态优先于 query 形态。
    检索分层、降级与诚实回报的契约见文档。

    契约: docs/wiki/tool/tool-architecture.md#recall-contract

func NewMemoryTurnTool(accessor tagenttool.MemoryStoreAccessor) tool.Tool
    NewMemoryTurnTool reconstructs a task turn's execution process (compress-
    digest-reconnect). Given a boundary event key (usually an agent_output
    card), it walks the causal chain backward via GetParent until the turn's
    external_input (inclusive), returning all events in the turn — including the
    thinking_plan/action_command steps that skeleton compression dropped from
    the timeline — in chronological order. This is how the model recovers HOW a
    past task was executed: the dropped tool events never leave the MemoryStore,
    and the causal chain (independent of compression) anchors them to the kept
    boundary cards. No forward traversal is needed — the backward walk from
    agent_output to external_input bounds exactly one turn.

func NewRecallGetTool(accessor tagenttool.MemoryStoreAccessor) tool.Tool
    NewRecallGetTool creates a tool that retrieves full event details by key.
    This is a sub-tool used by RecallAgent for detailed memory retrieval.

func NewRecallQueryTool(accessor tagenttool.MemoryStoreAccessor, readPartitionIDs []int) tool.Tool
    NewRecallQueryTool creates a tool that queries historical events from
    memory. This is a sub-tool used by RecallAgent for memory retrieval.
    readPartitionIDs lists additional partition IDs to include in queries
    (injected from config).

func NewRecallRecentTool(accessor tagenttool.MemoryStoreAccessor, readPartitionIDs []int) tool.Tool
    NewRecallRecentTool creates a tool that retrieves the most recent events.
    This is a sub-tool used by RecallAgent for quick recent memory access.
    readPartitionIDs lists additional partition IDs to include in queries
    (injected from config).

func NewRecallTool(accessor tagenttool.MemoryStoreAccessor, readPartitionIDs []int) tool.Tool
    NewRecallTool 构造统一召回入口：确定性形态是纯函数，orchestrate 是显式的 LLM 编排 opt-in。 未接线的
    orchestrate 必须显式回报并给出确定性迭代路径，不得静默降级，见文档。

    契约: docs/wiki/tool/tool-architecture.md#recall-contract

func NewRecallTraceTool(accessor tagenttool.MemoryStoreAccessor) tool.Tool
    NewRecallTraceTool creates a tool that traces the causal chain
    backward from an event. Traverses ParentKey links by repeatedly calling
    GetEvent(parentKey).

func NewTool(cfg Config) (tagenttool.Tool, error)
    NewTool is a convenience function that creates a RecallAgent and wraps it as
    a CallableTool ready for registration.

    If cfg.Description is empty and cfg.DescriptionFile is set, the description
    is loaded from the file (relative to cfg.PromptDir). If both are empty,
    a hardcoded default is used for backward compatibility.

    Note: This wraps with a simple AgentToolWrapper without event_key
    resolution. For full event_key support, use tagent.New() which builds agents
    from Config.

func RegisterSubTools()
    RegisterSubTools registers all recall sub-tools as plain tools in the global
    tool registry. Called by tagent.RegisterBuiltinTools().

    Registered tools: - recall: the UNIFIED recall entry (items tickets /
    turn_key causal chain / query semantic search / orchestrate reserved
    form) — supersedes the retired memory_recall and memory_turn tool names
    (stable-context- compaction D7) - recall_query / recall_get / recall_recent
    / recall_trace: RecallAgent orchestration sub-tools (internal to the
    orchestrate branch; not for direct top-level assembly)


TYPES

type Config struct {
	// Model Required: LLM model for the internal React loop
	Model model.Model

	// MemStore Required: agent's own MemoryStore (writes via MemoryPlugin, reads via sub-tools)
	MemStore memory.MemoryStore

	// ReadPartitionIDs lists PartitionIDs this agent is allowed to read in addition
	// to its own namespace. Injected from ToolAgentFactoryConfig.ReadPartitionIDs.
	ReadPartitionIDs []int

	// Tools are the sub-tools available to this agent (e.g., recall_query, recall_get).
	// In the config-driven path, these are injected by buildAgent from the agent's
	// config tools list. If empty, buildRecallSubTools is called for backward compatibility.
	Tools []tagenttool.Tool

	// PromptDir Optional: base directory for prompt files (default: "resources/prompts")
	PromptDir string

	// Prompt loading (bootstrap style)
	// Optional: overrides PromptDir + "recall_agent.md" if set
	Prompt PromptConfig

	// Description Tool description shown to the parent agent's LLM
	// Optional: inline description (overrides default)
	Description string
	// DescriptionFile Optional: description loaded from file (relative to PromptDir)
	DescriptionFile string

	// MaxToolIterations Optional overrides
	// Default: 5
	MaxToolIterations int
	// MaxTokens Default: 4096
	MaxTokens int
}
    Config holds configuration for creating the Recall Agent.

    RecallAgent is a TagentAgent instance configured for intelligent memory
    recall. Unlike the simple RecallTool, RecallAgent uses an internal LLM
    React loop to understand user queries and synthesize memory into coherent
    responses.

    Architecture: RecallAgent → TagentAgent (agent.Agent) → agent.Tool
    (CallableTool)

type PromptConfig = prompt.CompositeConfig
    PromptConfig describes how to load a system prompt (bootstrap style).
    Re-exported from prompt package for use by sub-packages.

