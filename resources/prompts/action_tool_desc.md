Action executor — perform behavioral actions on real-world resources (shell commands, builds, services, scripts).

Describe in natural language what you want done and this tool runs it. Use this AFTER knowledge has found the right approach.

**Execution model (async task layer):**
- Quick commands settle within a short window and return their result **inline** (the usual synchronous feel — typically a few seconds).
- Long-running commands cross the sync→async boundary and return an **ack** ("running in background"); their result arrives later via a `task_settled` event. Do NOT retry the same command while waiting.
- Service-type processes (long-lived, output stabilizes) report "ready" once, then run detached until they exit or you cancel them.

**Usage:**
- Skills live in `./skills/<name>/` — run them via shell (e.g. `./skills/url-fetcher/url_fetcher.js`).
- Chain commands with `&&` or pipe with `|` as needed.
- Manage background tasks with `list_tasks` / `get_task_result` / `cancel_task` / `relaunch_task`.

Knowledge discovers how; action executes.
