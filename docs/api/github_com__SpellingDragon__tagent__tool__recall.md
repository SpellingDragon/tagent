package recall // import "github.com/SpellingDragon/tagent/tool/recall"

memory_recall: the recall protocol implementation, internal to the unified
recall entry.

- Pure deterministic paths with no LLM in the route; items take precedence over
query. - Items resolve by batch GetEvent in original order; misses are reported,
never hallucinated.

recall: the unified recall entry — parameters are the router.

  - orchestrate: true opts into the RecallAgent engine explicitly; an unwired
    engine returns guidance instead of a silent deterministic fallback.
  - items: batch GetEvent in original order, zero hallucination.
  - turn_key: causal-chain walk back to the turn's external_input; an incomplete
    walk reports what it did read plus the named reason it stopped for.
  - query with filters: retrieval-layer search; the entry protocol stays when
    the layer evolves.

FUNCTIONS

func NewAgent(cfg Config) (*agent.TagentAgent, error)
    NewAgent creates a TagentAgent configured for intelligent memory recall.
    The returned *agent.TagentAgent implements agent.Agent and can be wrapped
    via agenttool.NewTool() to become a CallableTool for the main TagentAgent.

func NewMemoryRecallTool(accessor tagenttool.MemoryStoreAccessor, readPartitionIDs []int) tool.Tool
    NewMemoryRecallTool 构造协议级召回工具（纯函数，无子 agent 绕行）：items 票据形态优先于 query 形态。
    检索分层、降级与诚实回报的契约见文档。

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

func NewRecallTraceTool(accessor tagenttool.MemoryStoreAccessor) tool.Tool
    NewRecallTraceTool creates a tool that traces the causal chain
    backward from an event. Traverses ParentKey links by repeatedly calling
    GetEvent(parentKey).

func NewTool(cfg Config) (tagenttool.Tool, error)
    NewTool is a convenience function that creates a RecallAgent and wraps it as
    a CallableTool.

    - An empty Description with DescriptionFile set loads the text relative to
    PromptDir; both empty falls back to a built-in default. - The wrapper is a
    plain AgentToolWrapper without event_key resolution; full support comes from
    the root-package constructor.

func RegisterSubTools()
    RegisterSubTools registers all recall sub-tools as plain tools in the global
    registry.

    - recall is the unified entry for the deterministic route; memory_recall
    and memory_turn are retired names outside the registry. - recall_query
    / recall_get / recall_recent / recall_trace serve the RecallAgent
    orchestration branch only.

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

    - RecallAgent runs an internal LLM ReAct loop over the recall sub-tools and
    synthesizes; the deterministic entry stays separate. - Assembly: RecallAgent
    → TagentAgent → agent.Tool.

type PromptConfig = prompt.CompositeConfig
    PromptConfig describes how to load a system prompt (bootstrap style).
    Re-exported from prompt package for use by sub-packages.
