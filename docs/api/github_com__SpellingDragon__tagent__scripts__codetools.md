check.go implements the two comparison modes the repository gates need:

    codetools comment-check --base-root DIR --head-root DIR <file>...
    codetools merge-check   --base-root DIR --head-root DIR [--map FILE] [--explain FILE] <dir>...

Paths are module-relative and must exist under both roots (comment-check) or in
the head root (merge-check); the shell wrappers materialize the baseline with
git archive. Both exit non-zero when a violation is printed.

--map and --explain each hold ONE file, for the package being checked: repeating
either flag is a usage error, an --explain entry that exempts no declaration in
the run is rejected, and a table file that cannot be read fails the call instead
of loading as empty. A shadowed table, a waiver that applies to nothing, and a
silently empty table all let a reading pass that the gate never actually earned.
Tables are per-package by construction (a cross-package table applies one
domain's normalization to another's baseline text and manufactures violations),
so a multi-package run invokes merge-check once per package with its own pair.

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
