Action executor — perform behavioral actions on real-world resources (shell commands, builds, services, scripts).

Describe in natural language what you want done and this tool runs it. Use this AFTER knowledge has found the right approach.

**Execution model (async task layer):**
- Quick commands settle within a short window and return their result **inline** (the usual synchronous feel — typically a few seconds).
- Long-running commands cross the sync→async boundary and return an **ack** ("running in background"); their result arrives later via a `task_settled` event. Do NOT retry the same command while waiting.
- Service-type processes (long-lived, output stabilizes) report "ready" once, then run detached until they exit or you cancel them.
- **The framework owns async state management — do not build your own waiting.** You are woken automatically by the settle event; NEVER dispatch `sleep N` probe jobs, poll, or re-check on a timer. Just end your turn after launching; the settle result is your next wakeup.
- An `alive_detached` status is an OBSERVATION, not a failure — quiet output often means the command is simply running (e.g. mid-build, mid-download, or a sleep inside your own command). Wait for its settle; verify by reading the redirected log file, not by re-running.**

**Working directory (IMPORTANT):**
- Each `action` call runs in a **fresh shell** at the workspace root. `cd` does **NOT** carry over between **separate** `action` calls.
- To work inside a subdirectory in one call, chain (`cd sub/dir && …`) or use paths relative to the workspace root / absolute paths.
- **Re-entry is different**: a still-running session (a long-running command or interactive shell that is alive) is re-entered by `resume_task` into that **same shell** — so `cd`, exported variables and other shell state **DO persist across resumes**.
- A command that has already exited has its session reaped; `resume_task` then fails — use `relaunch_task` instead.
- For destructive commands (`rm`/`mv`) prefer workspace-rooted or absolute paths regardless of mode.

**Operational lessons (hard-won, do not relearn):**
- Long-running jobs (builds, downloads, batch inference) run fine as direct managed commands — the framework owns waiting (a settle is the terminal state). Do NOT wrap them in `tmux` shells to dodge "reaping": the historical "process silently died" incident was root-caused to upstream proxy rate-throttling of long connections (speed decay ~6.7→3.2 MB/s with a clean audit.log), NOT platform reaping — and tmux wrapping only bypasses the platform's watch/reconcile governance. For throttled long downloads, use chunked/resumable transfer (`curl -C -`) or lower concurrency instead.
- Redirect long output to a file and read the FILE as the source of truth: heavy streaming output can be truncated in tool results, and a 0-byte file + unchanged mtime means "never started", not "still running".
- `quiet_timeout` must be >= 30s (the stability window); shorter values are rejected outright.
- Prefer many small "check-and-go" probes over one long blocking command: probe, read state, end turn — let settle wake you.
- For state claims (deployed/restarted/file changed), re-verify with an independent read-back; a completed status alone is not proof.

**Usage:**
- Skills live in `./skills/<name>/` — run them via shell (e.g. `./skills/url-fetcher/url_fetcher.js`).
- Chain commands with `&&` or pipe with `|` as needed.

Knowledge discovers how; action executes.
