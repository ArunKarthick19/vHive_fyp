#!/usr/bin/env bash
# chain_p3.sh — start Phase 3 as soon as run_p2b.sh (Phase 2, fibpy) has finished. Deadline: 2.5 h from start, never later than 03:55 MST
# (leaves ~30 min before the 04:24 MST = 19:24 SGT hard stop for the report).
while pgrep -f "bash run_p2b[.]sh" >/dev/null; do sleep 5; done
d=$(( $(date +%s) + 150*60 )); hs=$(date -d '2026-11-21 03:55' +%s); [ $d -gt $hs ] && d=$hs
echo "$(date '+%F %T') chain_p3: Phase 2 finished, starting Phase 3 with deadline $(date -d @$d '+%T')"
DEADLINE=$d FNS="fibgo fibpy aesgo authgo" bash $HOME/fc-vm/vswarm/run_p3.sh
