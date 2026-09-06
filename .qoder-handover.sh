#!/bin/sh
set -x
cd /Users/pengweiye/Documents/codes/tagent || exit 1
git add openspec/changes/LEDGER.md openspec/changes/tagent-evolution-roadmap/execution-dag.md
git diff --cached --name-only
git commit -m "docs(openspec): pre-handover deep review — verdict + 6 majors with evidence into execution-dag §8"
echo "COMMIT_EXIT=$?"
git log --oneline -4
git push origin main
echo "PUSH_EXIT=$?"
git status --short
echo "SCRIPT_DONE"
