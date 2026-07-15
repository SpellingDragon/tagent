Action executor — perform behavioral actions on real-world resources.

Through natural language, describe the behavior you want to perform on real resources, and this tool triggers execution. It translates your intent into concrete shell commands and runs them asynchronously.

**Execution model: All commands run asynchronously via tmux.**
- When you call this tool, it returns immediately with a session_id
- The actual command result arrives later via `[action_tool_result]` event when the command stabilizes or completes
- You will receive the result automatically — do NOT retry the same command while waiting
- For quick commands (e.g., `ls /tmp`), results typically arrive in 3-5 seconds
- For long-running commands (e.g., `tail -f`, `make build`), results arrive when the command's output stabilizes

**When to use this tool:**
- After knowledge has found the right approach
- When you need to run shell commands, build projects, start services, or perform any system operation
- Skills live in `./skills/<name>/` directory — run them via shell

**Command chaining:** You can chain commands with `&&` or pipe with `|` as needed.

Knowledge discovers how; action executes.
Action executor — perform behavioral actions on real-world resources.

Action is an execution hub agent. It receives behavioral instructions from you (the planner) and orchestrates execution through specialized sub-agents:

- **read**: Read and analyze files, fetch web resources, search code
- **write**: Create, modify, and verify file contents
- **speak**: Voice/text-to-speech output (stub, not yet active)
- **draw**: Image generation (stub, not yet active)
- **exec**: Direct shell command execution for simple commands

Usage: Send a natural language description of the action you want performed. Action will decide the best execution path — delegating to a specialized sub-agent for complex tasks or executing directly for simple commands.

You plan with knowledge and recall; action executes.
Action executor — perform behavioral actions on real-world resources.

Through natural language, describe the behavior you want to perform on real resources, and this tool triggers execution. It translates your intent into concrete shell commands and runs them.

Modes:
- exec: Run synchronously, wait for completion, return result (default)
- tmux_exec: Run in a persistent tmux session (survives disconnections, ideal for long-running tasks)

Use this AFTER knowledge has found the right approach. When knowledge returns a skill or workflow, run it as an action:
- Skills live in ./skills/<name>/ directory — e.g. ./skills/url-fetcher/url_fetcher.js
- The action runs via shell, so you can chain commands with && or pipe with |
- Use mode="exec" for typical tasks; use mode="tmux_exec" for long-running or interactive processes

Knowledge discovers how; action executes.
