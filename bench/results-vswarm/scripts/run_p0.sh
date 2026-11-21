#!/usr/bin/env bash
# run_p0.sh — Phase 0: load sweep (c=1,2,4,8,16) + one open-loop time-varying run, stock vs mine, on busybox, httpbin, nginx (HTTP).
set -u
V=$HOME/fc-vm/vswarm; O=$V/out/p0
DEADLINE=${DEADLINE:?}
echo "$(date '+%F %T') run_p0 start; deadline $(date -d @$DEADLINE '+%T'); vhive-perpod md5 $(md5sum < $HOME/vhive/vhive-perpod | cut -c1-32)"
for p in "busybox bb" "httpbin httpbin" "nginx nginx"; do
  set -- $p
  KIND=http WL=$1 PREFIX=$2 OUT=$O/$1 DEADLINE=$DEADLINE bash $V/sweep.sh
  $HOME/fc-vm/tools/venv/bin/python $V/plot.py $O/$1 "$1" 2>&1 | tail -1
done
echo "$(date '+%F %T') run_p0 done"
