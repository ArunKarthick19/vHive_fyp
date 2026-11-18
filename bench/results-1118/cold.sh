#!/usr/bin/env bash
# cold.sh — standalone cold-start benchmark: one request per cycle, each from a fully idle node.
#
#   WORKLOAD=busybox SERVICES="bb-stock bb-mine" CYCLES=11 OUT=~/fc-vm/bench/out/busybox bash ~/fc-vm/bench/cold.sh
#
# Rounds c = 1..CYCLES; within a round the SERVICES are run in the given order (stock first, then mine), so
# node drift hits both. Cycle 1 is flagged warmup=1 (kept in the CSV, left out of the statistics later).
#
# Per cycle:
#   1. wait until idle: no pods in the namespace (so neither service of the pair has a pod), no firecracker process,
#      no tap fcweb*, no /run/vhive-fcweb/*/fc.sock; the condition must hold on STABLE_POLLS polls in a row,
#      then SETTLE_S more seconds. If not idle within IDLE_TIMEOUT_S the cycle is recorded as a failure
#      (note=not_idle) and no request is sent.
#   2. one curl to the Istio ingress ClusterIP with Host: <ksvc URL host>; record epoch send time, node time,
#      HTTP code, curl exit code, time_total, body size and sha256.
#   3. keep the vhive log lines written during the cycle and a pod snapshot.
# Nothing is ever dropped: every cycle writes exactly one CSV row.
set -u
WORKLOAD=${WORKLOAD:?set WORKLOAD}
SERVICES=${SERVICES:?set SERVICES (space separated, run in this order within each round)}
CYCLES=${CYCLES:-11}
NS=${NS:-default}
OUT=${OUT:?set OUT}
IDLE_TIMEOUT_S=${IDLE_TIMEOUT_S:-600}
STABLE_POLLS=${STABLE_POLLS:-3}
POLL_S=${POLL_S:-2}
SETTLE_S=${SETTLE_S:-5}
CURL_MAX_S=${CURL_MAX_S:-300}
VHIVE_LOG=${VHIVE_LOG:-/tmp/vhive-logs/vhive_log.txt}

mkdir -p "$OUT/bodies" "$OUT/cycles"
CSV=$OUT/cold.csv
LOG=$OUT/cold.log
say() { echo "$(date '+%F %T') $*" | tee -a "$LOG"; }

INGRESS_IP=$(kubectl -n istio-system get svc istio-ingressgateway -o jsonpath='{.spec.clusterIP}')
[ -n "$INGRESS_IP" ] || { say "FATAL: no istio-ingressgateway ClusterIP"; exit 1; }
declare -A HOST
for s in $SERVICES; do
  u=$(kubectl -n "$NS" get ksvc "$s" -o jsonpath='{.status.url}')
  [ -n "$u" ] || { say "FATAL: ksvc $s has no URL"; exit 1; }
  HOST[$s]=${u#*://}
done
say "start workload=$WORKLOAD services='$SERVICES' cycles=$CYCLES ingress=$INGRESS_IP"
for s in $SERVICES; do say "  $s -> Host: ${HOST[$s]}"; done
say "  settings: IDLE_TIMEOUT_S=$IDLE_TIMEOUT_S STABLE_POLLS=$STABLE_POLLS POLL_S=$POLL_S SETTLE_S=$SETTLE_S CURL_MAX_S=$CURL_MAX_S"

[ -s "$CSV" ] || echo "workload,service,cycle,warmup,idle_ok,idle_wait_s,send_epoch,node_time,http_code,curl_exit,time_total_s,body_bytes,body_sha256_16,pods_after,vm_boot_line,note" > "$CSV"

# What still blocks "idle" (empty string = idle).
busy() {
  local b="" n
  n=$(kubectl -n "$NS" get pods --no-headers 2>/dev/null | wc -l); [ "$n" = 0 ] || b="$b pods=$n"
  n=$(pgrep -x firecracker | wc -l);                              [ "$n" = 0 ] || b="$b firecracker=$n"
  n=$(ip -o link show 2>/dev/null | grep -c ': fcweb');           [ "$n" = 0 ] || b="$b tap=$n"
  n=$(ls /run/vhive-fcweb/*/fc.sock 2>/dev/null | wc -l);         [ "$n" = 0 ] || b="$b sock=$n"
  echo "$b"
}

wait_idle() {  # sets IDLE_OK, IDLE_WAIT; $1 = start time when restarted after a failed settle
  local t0=${1:-$SECONDS} ok=0 b last=""
  while :; do
    b=$(busy)
    if [ -z "$b" ]; then ok=$((ok+1)); [ "$ok" -ge "$STABLE_POLLS" ] && break
    else ok=0; last=$b; fi
    if [ $((SECONDS-t0)) -ge "$IDLE_TIMEOUT_S" ]; then IDLE_OK=0; IDLE_WAIT=$((SECONDS-t0)); say "    not idle after ${IDLE_WAIT}s:$last"; return; fi
    sleep "$POLL_S"
  done
  sleep "$SETTLE_S"
  b=$(busy)
  if [ -n "$b" ]; then say "    busy again after settle:$b"; wait_idle "$t0"; return; fi   # something appeared during the settle time: start over
  IDLE_OK=1; IDLE_WAIT=$((SECONDS-t0))
}

for c in $(seq 1 "$CYCLES"); do
  warm=0; [ "$c" = 1 ] && warm=1
  for s in $SERVICES; do
    tag="$s.c$c"
    say "cycle $c $s: waiting for idle"
    wait_idle
    l0=$(wc -l < "$VHIVE_LOG")
    if [ "$IDLE_OK" != 1 ]; then
      echo "$WORKLOAD,$s,$c,$warm,0,$IDLE_WAIT,,$(date -Is),,,,,,$(kubectl -n "$NS" get pods --no-headers 2>/dev/null | wc -l),,not_idle" >> "$CSV"
      kubectl -n "$NS" get pods -o wide > "$OUT/cycles/$tag.pods.txt" 2>&1
      say "cycle $c $s: FAILURE not_idle, no request sent"
      continue
    fi
    send=$(date +%s.%N); nodet=$(date -Is)
    res=$(curl -s -m "$CURL_MAX_S" -o "$OUT/bodies/$tag" -w '%{http_code} %{time_total}' -H "Host: ${HOST[$s]}" "http://$INGRESS_IP/")
    cx=$?
    code=${res%% *}; tt=${res#* }
    bytes=$(stat -c %s "$OUT/bodies/$tag" 2>/dev/null || echo 0)
    sha=$(sha256sum < "$OUT/bodies/$tag" 2>/dev/null | cut -c1-16)
    sleep 1
    kubectl -n "$NS" get pods -o wide > "$OUT/cycles/$tag.pods.txt" 2>&1
    np=$(grep -c -v '^NAME' "$OUT/cycles/$tag.pods.txt")
    tail -n +"$((l0+1))" "$VHIVE_LOG" > "$OUT/cycles/$tag.vhive.log"
    boot=$(grep -m1 'Firecracker VM booted' "$OUT/cycles/$tag.vhive.log" | grep -oE 'firecrackerVM=[^ ]+|launchToHTTP200=[^ ]+|probe=[^ ]+' | tr '\n' ' ' | sed 's/ $//; s/,/;/g; s/"//g')
    note=""
    [ "$code" = 200 ] || note="http_$code"
    [ "$cx" = 0 ] || note="${note:+$note;}curl_exit_$cx"
    echo "$WORKLOAD,$s,$c,$warm,1,$IDLE_WAIT,$send,$nodet,$code,$cx,$tt,$bytes,$sha,$np,$boot,$note" >> "$CSV"
    say "cycle $c $s: HTTP $code curl_exit=$cx time_total=${tt}s bytes=$bytes idle_wait=${IDLE_WAIT}s ${boot:+[$boot]} ${note:+NOTE=$note}"
  done
done
say "done workload=$WORKLOAD"
