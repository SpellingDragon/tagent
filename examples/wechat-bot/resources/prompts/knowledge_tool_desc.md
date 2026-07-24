Information and workflow discovery tool. Searches local skills, the web, and memory to find how to accomplish tasks.

Use this BEFORE action when:
- You need an approach for a task (fetch a URL, process a file, deploy, etc.) — knowledge searches local skills for ready-made automation workflows
- You need facts, documentation, or current information — knowledge searches the web
- You need to recall past knowledge or earlier decisions — knowledge queries historical memory

Knowledge discovers how to do it; action executes it. Always check knowledge first for any non-trivial task.

## event_keys parameter
Pass relevant `event_keys` from your conversation context (the `[evt_KEY|type]` prefixes) so the knowledge agent can access full event details. If you pass none, the most recent 5 events are auto-injected as context.
