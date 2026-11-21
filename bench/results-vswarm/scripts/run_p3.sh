#!/usr/bin/env bash
# run_p3.sh — Phase 3: gRPC load sweep (ghz, c=1,2,4,8,16) + one open-loop time-varying run (vary.py --grpc), stock vs mine.
set -u
V=$HOME/fc-vm/vswarm; O=$V/out/p3; DEADLINE=${DEADLINE:?}
echo "$(date '+%F %T') run_p3 start; deadline $(date -d @$DEADLINE '+%T')"
for f in ${FNS:-fibgo fibpy aesgo authgo}; do
  left=$((DEADLINE - $(date +%s)))
  if [ "$left" -le 600 ]; then echo "$(date '+%F %T') SKIP $f: only ${left}s left"; continue; fi
  KIND=grpc WL=$f PREFIX=$f OUT=$O/$f DEADLINE=$DEADLINE bash $V/sweep.sh
  $HOME/fc-vm/tools/venv/bin/python $V/plot.py $O/$f "$f (vSwarm, gRPC)" 2>&1 | tail -1
done
echo "$(date '+%F %T') run_p3 done"
