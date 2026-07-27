Action executor — perform behavioral actions on real-world resources (shell commands, builds, services, scripts).

Describe in natural language what you want done and this tool runs it. Use this AFTER knowledge has found the right approach.

**Execution model (async task layer):**
- Quick commands settle within a short window and return their result **inline** (the usual synchronous feel — typically a few seconds).
- Long-running commands cross the sync→async boundary and return an **ack** ("running in background"); their result arrives later via a `task_settled` event. Do NOT retry the same command while waiting.
- Service-type processes (long-lived, output stabilizes) report "ready" once, then run detached until they exit or you cancel them.

**Working directory (IMPORTANT):**
- Each `action` call runs in a **fresh shell** at the workspace root. `cd` does **NOT** carry over between **separate** `action` calls.
- To work inside a subdirectory in one call, chain (`cd sub/dir && …`) or use paths relative to the workspace root / absolute paths.
- **Re-entry is different**: a still-running session (a long-running command or interactive shell that is alive) is re-entered by `resume_task` into that **same shell** — so `cd`, exported variables and other shell state **DO persist across resumes**.
- A command that has already exited has its session reaped; `resume_task` then fails — use `relaunch_task` instead.
- For destructive commands (`rm`/`mv`) prefer workspace-rooted or absolute paths regardless of mode.

**Usage:**
- Skills live in `./skills/<name>/` — run them via shell (e.g. `./skills/url-fetcher/url_fetcher.js`).
- Chain commands with `&&` or pipe with `|` as needed.
- Manage background tasks with `list_tasks` / `get_task_result` / `cancel_task` / `relaunch_task`.

Knowledge discovers how; action executes.
