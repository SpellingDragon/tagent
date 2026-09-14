# Knowledge backend (app-level override fragment)

Active backend: **ima cloud knowledge base** (single source of truth since 2026-09-14).

Routing:
1. `skill_search` with keywords like "ima knowledge base", then `skill_load` its SKILL.md.
2. Use the MCP tools listed there (prefer `search_knowledge`); if the MCP channel is unavailable, surface the exact `mcp_call` invocation for the caller.

Historical note: the deployment previously used a local `knowledge_base/` directory tree — it is retired; do not read it (fails with ENOENT). Write-side constraints (add-only, citation-link rule) live in the ima SKILL.md.
