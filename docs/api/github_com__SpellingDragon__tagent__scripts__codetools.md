check.go implements the two comparison modes the repository gates need:

    codetools comment-check --base-root DIR --head-root DIR <file>...
    codetools merge-check   --base-root DIR --head-root DIR [--map FILE] <dir>...

Paths are module-relative and must exist under both roots (comment-check) or in
the head root (merge-check); the shell wrappers materialize the baseline with
git archive. Both exit non-zero when a violation is printed.

Command codetools emits the mechanical facts the repository's comment and
test-file policies are checked against.

Usage:

    codetools strip <file>...           print each file with comments removed
    codetools decls <file>...           print one JSON object per top-level declaration

strip output is canonical (go/printer), so it can be diffed across revisions
to prove that only comments changed. decls reports, per declaration,
whether it is a test or benchmark, the comment-stripped hash of its body,
the number of assertion calls it contains and the number of t.Parallel calls;
both gates consume it as JSON lines.

Both subcommands exit non-zero on unreadable or unparsable input so that a
silent skip cannot masquerade as a pass.
