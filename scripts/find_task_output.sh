#!/usr/bin/env sh
# find_task_output.sh <task-id-substring> [head_bytes]
# Locate a spilled tool-output file across known .tagent-workspace bases
# (settle messages use ambiguous relative paths; this removes the guessing).
# Bases ordered by likelihood: app ws > tests ws > repo-root ws > home ws.
set -u
ID="${1:?usage: find_task_output.sh <task-id-substring> [head_bytes]}"
HEADN="${2:-0}"
BASES="/home/lighthouse/tagent/examples/wechat-bot/.tagent-workspace/tool-output /home/lighthouse/tagent/tests/.tagent-workspace/tool-output /home/lighthouse/tagent/.tagent-workspace/tool-output /home/lighthouse/.tagent-workspace/tool-output"
found=0
for b in $BASES; do
  # Match both spill shapes: task settle (task-<id>-*.txt) and action output (output_tagent-<session>.txt)
  for f in "$b"/task-"$ID"-*.txt "$b"/output_*"$ID"*.txt; do
    [ -e "$f" ] || continue
    echo "FOUND: $f"
    found=1
    if [ "$HEADN" -gt 0 ]; then head -c "$HEADN" "$f"; echo; fi
  done
done
[ "$found" -eq 1 ] || { echo "NOT_FOUND id=$ID"; exit 1; }
