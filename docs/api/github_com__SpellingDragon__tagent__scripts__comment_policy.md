Command comment_policy checks Go sources against the repository's comment
whitelist: documentation comments may only state a contract, and any pointer to
longer-lived documentation must use the index form.

Usage:

    comment_policy [-report] [dir...]

Without -report any violation exits non-zero; -report prints the same findings
and exits 0, which is how the gates are introduced before they become blocking.
A directory argument is scanned recursively, skipping subdirectories that carry
their own go.mod.

Rules are named in the output and documented on the matcher table below. Length
never decides compliance: a long contract comment is legal and a short piece of
design narrative is not, so content shape is the only judge.
