#!/usr/bin/env bash
# run_p2b.sh — continuation of run_p2.sh: wait for fibgo's coldg.sh to finish, summarize it, then fibpy with CYCLES=6 (n=5), no deadline cut.
set -u
V=$HOME/fc-vm/vswarm; O=$V/out/p2
while kill -0 3519765 2>/dev/null; do sleep 5; done
echo "$(date '+%F %T') === fibgo coldg.sh finished"
python3 $V/statsg.py $O/fibgo/cold.csv fibgo-stock fibgo-mine > $O/fibgo/summary.md 2>&1; head -5 $O/fibgo/summary.md
echo "$(date '+%F %T') === fibpy start (CYCLES=6)"
FN=fibpy WORKLOAD=fibpy SERVICES="fibpy-stock fibpy-mine" CYCLES=6 OUT=$O/fibpy timeout 3000 bash $V/coldg.sh
echo "$(date '+%F %T') === fibpy coldg.sh exit=$?"
python3 $V/statsg.py $O/fibpy/cold.csv fibpy-stock fibpy-mine > $O/fibpy/summary.md 2>&1; head -5 $O/fibpy/summary.md
echo "$(date '+%F %T') run_p2b done"
