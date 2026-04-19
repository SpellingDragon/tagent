Knowledge acquisition and translation tool. Acquires knowledge needed to complete tasks and translates it into executable plans.

Supports:
1. Capability discovery: search local skills and MCP tools
2. Factual knowledge: web search for facts, concepts, documentation
3. Translation: convert skill content into executable commands (ExecutionPlan)
4. Historical knowledge: query past knowledge events from memory

When the result contains execution_plan, use the command tool to execute it:
- execution_plan.function="exec" → command(mode="exec", command=execution_plan.command)
- execution_plan.function="tmux_exec" → command(mode="tmux_exec", command=execution_plan.command)
- execution_plan.function="mcp_call" → command(mode="exec", command=execution_plan.command)
