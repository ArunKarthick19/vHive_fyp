#!/usr/bin/env bash
# mem.sh — FN=<fn> PREFIX=<ksvc prefix> OUT=<dir>: per mode (stock, mine): wait idle (as coldg.sh), one gRPC request (cold1.py) to create
# one pod, poll until the pod is Ready, sleep 5 s (no requests in flight), then snapshot ps -eo pid,ppid,rss,etimes,args of firecracker procs.
set -u
FN=${FN:?}; PREFIX=${PREFIX:?}; OUT=${OUT:?}; mkdir -p "$OUT"; LOG=$OUT/mem.log
V=$HOME/fc-vm/vswarm; PY=$HOME/fc-vm/tools/venv/bin/python
say() { echo "$(date '+%F %T.%3N') $*" | tee -a "$LOG"; }
INGRESS_IP=$(kubectl -n istio-system get svc istio-ingressgateway -o jsonpath='{.spec.clusterIP}')
busy() { local b="" n
  n=$(kubectl get pods --no-headers 2>/dev/null | wc -l); [ "$n" = 0 ] || b="$b pods=$n"
  n=$(pgrep -x firecracker | wc -l); [ "$n" = 0 ] || b="$b firecracker=$n"
  n=$(ip -o link show 2>/dev/null | grep -c ': fcweb'); [ "$n" = 0 ] || b="$b tap=$n"
  n=$(ls /run/vhive-fcweb/*/fc.sock 2>/dev/null | wc -l); [ "$n" = 0 ] || b="$b sock=$n"; echo "$b"; }
wait_idle() { local t0=$SECONDS ok=0
  while [ $((SECONDS-t0)) -lt 600 ]; do if [ -z "$(busy)" ]; then ok=$((ok+1)); [ $ok -ge 3 ] && { sleep 5; [ -z "$(busy)" ] && return 0; ok=0; }; else ok=0; fi; sleep 2; done; return 1; }
for m in stock mine; do
  s=$PREFIX-$m
  say "$s: waiting for idle"; wait_idle || { say "$s: NOT IDLE after 600 s, skipped"; continue; }
  u=$(kubectl get ksvc $s -o jsonpath='{.status.url}')
  r=$(timeout 300 $PY $V/cold1.py "$INGRESS_IP" "${u#*://}" "$FN" "$OUT/$m.body"); say "$s: request -> $r"
  while :; do rd=$(kubectl get pods -l serving.knative.dev/service=$s -o jsonpath='{range .items[*]}{.metadata.name} {.status.conditions[?(@.type=="Ready")].status} {.status.conditions[?(@.type=="Ready")].lastTransitionTime}{"\n"}{end}'); 
    echo "$rd" | grep -q ' True ' && break; sleep 0.2; done
  say "$s: Ready seen: $rd"; sleep 5
  say "$s: snapshot (5 s after Ready seen)"
  { date '+%F %T.%3N'; echo "pods:"; kubectl get pods -o wide --no-headers; echo "ps:"; ps -eo pid,ppid,rss,etimes,args | grep -E '[f]irecracker'; 
    echo "parents:"; for p in $(pgrep -x firecracker); do pp=$(ps -o ppid= -p $p | tr -d ' '); echo "fc pid $p rss_kib=$(ps -o rss= -p $p | tr -d ' ') parent $pp: $(ps -o args= -p $pp | cut -c1-150)"; done; } > "$OUT/$m.ps.txt" 2>&1
  cat "$OUT/$m.ps.txt" | tee -a "$LOG" >/dev/null
  grep '^fc pid' "$OUT/$m.ps.txt" | tee -a "$LOG"
done
say "done"
