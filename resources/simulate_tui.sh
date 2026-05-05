#!/bin/bash
# Controlled TUI simulation for tmux monitor testing
echo -e "\033[2J\033[H"
while true; do
  echo "╔════════════════════════════════╗"
  echo "║   Simulated TUI Dashboard      ║"
  echo "╠════════════════════════════════╣"
  echo "║ Time:  $(date '+%H:%M:%S')     ║"
  echo "║ PID:   $$                      ║"
  echo "║ Frame: $(( $(date +%s) % 100 )) ║"
  echo "╚════════════════════════════════╝"
  sleep 1
done
