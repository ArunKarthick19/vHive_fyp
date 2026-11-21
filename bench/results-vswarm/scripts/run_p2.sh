#!/usr/bin/env bash
# run_p2.sh — Phase 2: cold starts (coldg.sh, 11 cycles, stock then mine each round) on the WORKS-BOTH vSwarm functions, in this order.
set -u
V=$HOME/fc-vm/vswarm; O=$V/out/p2; DEADLINE=${DEADLINE:?}
echo "$(date '+%F %T') run_p2 start; deadline $(date -d @$DEADLINE '+%T')"
for f in ${FNS:-fibgo fibpy aesgo authgo}; do
  left=$((DEADLINE - $(date +%s)))
  if [ "$left" -le 600 ]; then echo "$(date '+%F %T') SKIP $f: only ${left}s left"; continue; fi
  echo "$(date '+%F %T') === $f start (${left}s left)"
  FN=$f WORKLOAD=$f SERVICES="$f-stock $f-mine" CYCLES=${CYCLES:-11} OUT=$O/$f timeout "$left" bash $V/coldg.sh
  echo "$(date '+%F %T') === $f coldg.sh exit=$?"
  python3 $V/statsg.py $O/$f/cold.csv $f-stock $f-mine > $O/$f/summary.md 2>&1; head -5 $O/$f/summary.md
done
echo "$(date '+%F %T') run_p2 done"
