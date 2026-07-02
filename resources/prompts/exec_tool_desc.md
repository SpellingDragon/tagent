Direct shell command execution.

Use this for simple commands that don't require specialized reading or writing logic:
- System commands (ls, pwd, whoami, ps, df)
- One-off script execution
- Quick file inspection (cat, head, wc)
- Process management

Modes:
- exec: Run synchronously, wait for completion, return result (default)
- tmux_exec: Run in a persistent tmux session (survives disconnections, ideal for long-running tasks)

For complex tasks that need content understanding or result verification, use the read or write sub-agents instead.
