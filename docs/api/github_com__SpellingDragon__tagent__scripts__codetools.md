check.go implements the two comparison modes the repository gates need:
comment-check proves a file changed only in comments; merge-check proves
a consolidation batch is lossless (body identical plus assertion count not
lowered).

- Paths are module-relative and must exist under both roots (comment-check) or
in the head root (merge-check); the shell wrappers materialize the baseline

Command codetools emits the mechanical facts the repository comment and
test-file policies are checked against: strip prints a file with comments
removed in canonical go/printer form, decls prints one JSON object per top-level
declaration.

- Both subcommands exit non-zero on unreadable or unparsable input so a silent
