Command comment_policy checks Go sources against the repository's comment
whitelist: documentation comments may only state a contract, and any pointer to
longer-lived documentation must use the index form.

Usage:

    comment_policy [-baseline F] [-update-baseline] [-strict] [-no-baseline] [dir...]

A run that consults the ratchet exits non-zero on any count above the baseline;
-v prints every finding rather than only the regressions. -strict additionally
requires the converged end state: zero findings.

A directory argument is scanned recursively, skipping subdirectories that carry
their own go.mod — which is why the ratchet refuses to run over a set that
leaves a nested module ungated, or a set other than the one the baseline was
written over. -no-baseline measures a scope without consulting the ratchet at
all.

The scope is the repository, not the working tree: a Go file git ignores is
dropped from the counts, because a checkout would not contain it and a baseline
holding its findings would not be reproducible. See ignoredGoFiles.

scripts/lint.sh owns the canonical directory set, so a batch author and CI scan
the same tree; read and lower the baseline through it rather than invoking this
command with an ad-hoc scope.

Rules are named in the output and documented on the matcher table below. Length
never decides compliance: a long contract comment is legal and a short piece of
design narrative is not, so content shape is the only judge.
