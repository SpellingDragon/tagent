Shell command executor. Run commands in the execution environment.

Modes:
- exec: Run synchronously, wait for completion, return result (default)
- tmux_exec: Run in a persistent tmux session (survives disconnections, ideal for long-running tasks)

Use this AFTER knowledge has found the right approach. When knowledge returns a skill or workflow, run it as a command:
- Skills live in ./skills/<name>/ directory — e.g. ./skills/url-fetcher/url_fetcher.js
- The command runs via shell, so you can chain commands with && or pipe with |
- Use mode="exec" for typical tasks; use mode="tmux_exec" for long-running or interactive processes

Knowledge discovers how; command executes.
