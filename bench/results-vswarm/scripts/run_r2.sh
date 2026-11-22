#!/usr/bin/env bash
# run_r2.sh FN — second run, one function, both modes, in order:
#  (a) cold starts: coldg.sh CYCLES=8 (stock then mine each round), with pidwatch.py + fccdcap.sh running (teardown timing);
#      after the last cycle wait until idle so the last teardown is logged, then stop both watchers; statsg.py
#  (c) memory: mem.sh (one pod Ready + 5 s, ps RSS)
#  (b) load: sweep2.sh (c=1..16, 30+30 s, from zero pods; ps RSS at the end of each cell) + vary; plot.py
set -u
FN=${1:?}; V=$HOME/fc-vm/vswarm; O=$V/out/r2/$FN; DEADLINE=${DEADLINE:?}; PY=$HOME/fc-vm/tools/venv/bin/python
mkdir -p $O/cold $O/mem $O/load
VL=/tmp/vhive-logs/vhive_log.txt; l0=$(wc -l < $VL)
echo "$(date '+%F %T') run_r2 $FN start; vhive log line $l0; deadline $(date -d @$DEADLINE '+%T')"
$PY $V/pidwatch.py $O/cold/pidwatch.log & PW=$!
bash $V/fccdcap.sh $O/cold/fccd-pane.raw & FC=$!
FN=$FN WORKLOAD=$FN SERVICES="$FN-stock $FN-mine" CYCLES=8 OUT=$O/cold bash $V/coldg.sh
echo "$(date '+%F %T') coldg exit=$?; waiting for the last VM to go away"
for i in $(seq 1 300); do
  [ "$(kubectl get pods --no-headers 2>/dev/null | wc -l)" = 0 ] && [ "$(pgrep -x firecracker | wc -l)" = 0 ] && break; sleep 2; done
sleep 16   # one more pane capture after the last exit
kill $PW $FC; wait $PW $FC 2>/dev/null
echo "$(date '+%F %T') watchers stopped"
python3 $V/statsg.py $O/cold/cold.csv $FN-stock $FN-mine > $O/cold/summary.md 2>&1; head -6 $O/cold/summary.md
FN=$FN PREFIX=$FN OUT=$O/mem bash $V/mem.sh
KIND=grpc WL=$FN PREFIX=$FN OUT=$O/load DEADLINE=$DEADLINE bash $V/sweep2.sh
$PY $V/plot.py $O/load "$FN (vSwarm, gRPC)" 2>&1 | tail -1
tail -n +$((l0+1)) $VL > $O/vhive.log
echo "$(date '+%F %T') run_r2 $FN done"
