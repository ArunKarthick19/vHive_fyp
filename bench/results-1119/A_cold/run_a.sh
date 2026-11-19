#!/usr/bin/env bash
# run_a.sh — REPORT-1119b part A: last night's cold-start benchmark again (same cold.sh, same six services,
# 11 cycles, stock then mine within each round), on the NEW daemon/fcinit, in the order busybox, httpbin, nginx.
# Results: ~/fc-vm/bench/out-1119b/<w>/ -> copied to ~/fc-vm/results-1119b/A_cold/<w>/ and to the results worktree
# ~/fc-vm/wt-results-1118/bench/results-1119/A_cold/<w>/ (committed + pushed after the whole part by hand).
# Launch: nohup bash ~/fc-vm/bench/run_a.sh > ~/fc-vm/bench/run_a.log 2>&1 &
set -u
B=$HOME/fc-vm/bench
R=$HOME/fc-vm/results-1119b/A_cold
W=$HOME/fc-vm/wt-results-1118/bench/results-1119/A_cold
DEADLINE=${DEADLINE:-$(date -d '2026-11-19 01:20' +%s)}   # leaves time for part B before the 02:08 hard stop
CYCLES=${CYCLES:-11}
say() { echo "$(date '+%F %T') $*"; }
say "run_a start; deadline $(date -d @$DEADLINE '+%F %T'); vhive-perpod md5 $(md5sum < $HOME/vhive/vhive-perpod | cut -c1-32); fcinit md5 $(md5sum < $HOME/fc-vm/images/fcinit | cut -c1-32); 'built rootfs during cold start' before: $(grep -c 'built rootfs during cold start' /tmp/vhive-logs/vhive_log.txt)"
for pair in "busybox bb" "httpbin httpbin" "nginx nginx"; do
  set -- $pair; w=$1; p=$2
  left=$((DEADLINE - $(date +%s)))
  if [ "$left" -le 600 ]; then say "SKIP $w: only ${left}s left before the deadline"; continue; fi
  say "=== $w start (${left}s left)"
  WORKLOAD=$w SERVICES="$p-stock $p-mine" CYCLES=$CYCLES OUT=$B/out-1119b/$w timeout "$left" bash $B/cold.sh
  rc=$?; say "=== $w cold.sh exit=$rc"
  python3 $B/stats.py $B/out-1119b/$w/cold.csv $p-stock $p-mine > $B/out-1119b/$w/summary.md 2>&1
  kubectl get ksvc $p-stock $p-mine -o yaml > $B/out-1119b/$w/ksvc.yaml 2>&1
  mkdir -p $R/$w $W/$w
  cp -r $B/out-1119b/$w/. $R/$w/
  cp -r $B/out-1119b/$w/. $W/$w/
  head -6 $B/out-1119b/$w/summary.md
done
cp $B/cold.sh $B/stats.py $B/run_a.sh $R/ && cp $B/cold.sh $B/stats.py $B/run_a.sh $W/
say "run_a done; 'built rootfs during cold start' after: $(grep -c 'built rootfs during cold start' /tmp/vhive-logs/vhive_log.txt)"
