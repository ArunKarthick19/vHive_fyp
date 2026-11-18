#!/usr/bin/env bash
# run_all.sh — runs cold.sh for busybox, nginx, httpbin in that order (11 cycles, stock then mine within each round),
# then after each workload: copies results to ~/fc-vm/results-1118/<w>/, commits them on branch results-1118 under
# ~/vhive/bench/results-1118/<w>/ (local only, no push), and rebuilds ~/results-1118.tgz.
# Launch: nohup bash ~/fc-vm/bench/run_all.sh > ~/fc-vm/bench/run_all.log 2>&1 &
set -u
B=$HOME/fc-vm/bench
R=$HOME/fc-vm/results-1118
G=$HOME/vhive/bench/results-1118
DEADLINE=${DEADLINE:-$(date -d '2026-11-18 14:40' +%s)}   # ~6 h after the 08:44 start
CYCLES=${CYCLES:-11}
say() { echo "$(date '+%F %T') $*"; }
say "run_all start; deadline $(date -d @$DEADLINE '+%F %T'); vhive log 'built rootfs during cold start' count before: $(grep -c 'built rootfs during cold start' /tmp/vhive-logs/vhive_log.txt)"
for pair in "busybox bb" "nginx nginx" "httpbin httpbin"; do
  set -- $pair; w=$1; p=$2
  left=$((DEADLINE - $(date +%s)))
  if [ "$left" -le 600 ]; then say "SKIP $w: only ${left}s left before the deadline"; continue; fi
  say "=== $w start (${left}s left)"
  WORKLOAD=$w SERVICES="$p-stock $p-mine" CYCLES=$CYCLES OUT=$B/out/$w timeout "$left" bash $B/cold.sh
  rc=$?; say "=== $w cold.sh exit=$rc"
  python3 $B/stats.py $B/out/$w/cold.csv $p-stock $p-mine > $B/out/$w/summary.md 2>&1
  cp $B/yaml/$p-stock.yaml $B/yaml/$p-mine.yaml $B/out/$w/
  kubectl get ksvc $p-stock $p-mine -o wide > $B/out/$w/ksvc.txt 2>&1
  mkdir -p $R/$w $G/$w
  cp -r $B/out/$w/. $R/$w/
  cp -r $B/out/$w/. $G/$w/
  cp $B/cold.sh $B/stats.py $B/run_all.sh $B/check-1118.sh $B/check-1118.run1.log $R/ 2>/dev/null
  cp $B/cold.sh $B/stats.py $B/run_all.sh $B/check-1118.sh $B/check-1118.run1.log $G/ 2>/dev/null
  ( cd $HOME/vhive && git add bench/results-1118 && \
    git -c user.name="Arun Karthick" -c user.email="$(git log -1 --format=%ae)" commit -q \
      -m "bench: results-1118 $w cold starts (stock vs image512, $CYCLES cycles, cycle 1 warm-up)" \
      -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>" && git log --oneline -1 ) || say "git commit for $w failed"
  tar -C $HOME/fc-vm -czf $HOME/results-1118.tgz results-1118 && say "tgz updated ($(du -h $HOME/results-1118.tgz | cut -f1))"
  cat $B/out/$w/summary.md | head -8
done
say "run_all done; 'built rootfs during cold start' count after: $(grep -c 'built rootfs during cold start' /tmp/vhive-logs/vhive_log.txt)"
