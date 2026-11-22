#!/usr/bin/env bash
# fccdcap.sh OUTFILE — read-only: every 15 s copy the firecracker-containerd tmux pane (history 2000 lines, wrapped lines joined) to OUTFILE.
# No keys are sent to the pane. Later: sort -u of the log lines gives the full sequence.
while :; do echo "=== capture $(date +%s.%N)" >> "$1"; tmux capture-pane -p -J -t firecracker -S -2000 >> "$1"; sleep 15; done
