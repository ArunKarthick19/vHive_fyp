#!/usr/bin/env bash
# sweep.sh — load variation, stock vs mine, every cell from ZERO pods (same idle check as ~/fc-vm/bench/cold.sh).
#
#   KIND=http|grpc WL=<label> PREFIX=<ksvc prefix> [HTTP_PATH=/] [CONCS="1 2 4 8 16"] [VARY=1] OUT=<dir> DEADLINE=<epoch> bash sweep.sh
#
# (a) for c in CONCS: for mode in stock mine (alternating, so drift hits both):
#       wait idle -> warm-up WARM_S (closed loop, c workers; this includes the cold start) -> measured MEASURE_S (closed loop, c workers)
#     http: ~/fc-vm/bench/load2.py (keep-alive, one connection per worker)
#     grpc: ghz (c workers, c connections), proto from vSwarm-proto; client CPU from /usr/bin/time
# (b) VARY=1: for mode in stock mine: wait idle -> vary.py open-loop steps STEPS (default 1,5,20,5,1 rps x 30 s)
# Every cell writes one row; a cell that cannot start (not idle / deadline) is recorded, never silently dropped.
set -u
KIND=${KIND:?}; WL=${WL:?}; PREFIX=${PREFIX:?}; OUT=${OUT:?}; DEADLINE=${DEADLINE:?}
HTTP_PATH=${HTTP_PATH:-/}; CONCS=${CONCS:-1 2 4 8 16}; VARY=${VARY:-1}; STEPS=${STEPS:-1:30,5:30,20:30,5:30,1:30}
WARM_S=${WARM_S:-30}; MEASURE_S=${MEASURE_S:-30}
IDLE_TIMEOUT_S=600; STABLE_POLLS=3; POLL_S=2; SETTLE_S=5
V=$HOME/fc-vm/vswarm; B=$HOME/fc-vm/bench; PY=$HOME/fc-vm/tools/venv/bin/python; GHZ=$HOME/fc-vm/tools/ghz
mkdir -p "$OUT"; LOG=$OUT/sweep.log; CSV=$OUT/sweep.csv
say() { echo "$(date '+%F %T') $*" | tee -a "$LOG"; }
INGRESS_IP=$(kubectl -n istio-system get svc istio-ingressgateway -o jsonpath='{.spec.clusterIP}')
declare -A HOST
for m in stock mine; do u=$(kubectl get ksvc $PREFIX-$m -o jsonpath='{.status.url}'); HOST[$m]=${u#*://}; done
[ "$KIND" = grpc ] && { source $V/fn.sh; fn_params $PREFIX; }
say "start kind=$KIND wl=$WL prefix=$PREFIX concs='$CONCS' vary=$VARY steps=$STEPS warm=${WARM_S}s measure=${MEASURE_S}s ingress=$INGRESS_IP deadline=$(date -d @$DEADLINE '+%T')"
[ -s "$CSV" ] || echo "wl,svc,mode,conc,idle_ok,idle_wait_s,start_epoch,warm_rps,warm_ok,warm_errors,warm_err_breakdown,warm_p99_ms,rps,p50_ms,p99_ms,errors,ok,err_breakdown,client_cpu_s,elapsed_s,pods_end,vms_end,fc_cpu_s,host_busy_cores,note" > "$CSV"

busy() {
  local b="" n
  n=$(kubectl get pods --no-headers 2>/dev/null | wc -l); [ "$n" = 0 ] || b="$b pods=$n"
  n=$(pgrep -x firecracker | wc -l);                     [ "$n" = 0 ] || b="$b firecracker=$n"
  n=$(ip -o link show 2>/dev/null | grep -c ': fcweb');  [ "$n" = 0 ] || b="$b tap=$n"
  n=$(ls /run/vhive-fcweb/*/fc.sock 2>/dev/null | wc -l); [ "$n" = 0 ] || b="$b sock=$n"
  echo "$b"
}
wait_idle() {
  local t0=${1:-$SECONDS} ok=0 b last=""
  while :; do
    b=$(busy)
    if [ -z "$b" ]; then ok=$((ok+1)); [ "$ok" -ge "$STABLE_POLLS" ] && break; else ok=0; last=$b; fi
    if [ $((SECONDS-t0)) -ge "$IDLE_TIMEOUT_S" ]; then IDLE_OK=0; IDLE_WAIT=$((SECONDS-t0)); say "    not idle after ${IDLE_WAIT}s:$last"; return; fi
    sleep "$POLL_S"
  done
  sleep "$SETTLE_S"
  b=$(busy); if [ -n "$b" ]; then say "    busy again after settle:$b"; wait_idle "$t0"; return; fi
  IDLE_OK=1; IDLE_WAIT=$((SECONDS-t0))
}

# cpusnap -> "<firecracker utime+stime ticks summed over all firecracker processes> <host busy ticks> <host idle ticks>" (CLK_TCK=100)
cpusnap() {
  local fc=0 p
  for p in $(pgrep -x firecracker); do fc=$((fc + $(awk '{print $14+$15}' /proc/$p/stat 2>/dev/null || echo 0))); done
  echo "$fc $(awk '/^cpu /{print $2+$3+$4+$7+$8+$9, $5+$6}' /proc/stat)"
}
# run_load <seconds> <conc> <mode> <outprefix>  -> prints "rps,p50,p99,errors,ok,breakdown,cpu,elapsed"
run_load() {
  local d=$1 c=$2 m=$3 o=$4
  if [ "$KIND" = http ]; then
    timeout -k 10 $((d + 120)) python3 -I $B/load2.py "$INGRESS_IP" 80 "$d" "$c" "$HTTP_PATH" "${HOST[$m]}" --json "$o.json" > "$o.csv" 2> "$o.err"
    sed -n 2p "$o.csv" | awk -F, '{print $2","$3","$4","$5","$6","$11","$12","$8}'
  else
    /usr/bin/time -f '%U %S' -o "$o.time" timeout -k 10 $((d + 120)) $GHZ --insecure --proto "$PROTO" --import-paths "$(dirname $PROTO)" \
      --call "$METHOD" -d "$DATA" --authority "${HOST[$m]}" -c "$c" --connections "$c" -z "${d}s" --timeout 60s \
      --format json -o "$o.json" "$INGRESS_IP:80" 2> "$o.err"
    $PY - "$o.json" "$o.time" <<'EOF'
import json, sys
try: j = json.load(open(sys.argv[1]))
except Exception as e: print(f",,,,,ghz_output_error:{type(e).__name__},,"); sys.exit()
cpu = sum(float(x) for x in open(sys.argv[2]).read().split()[-2:])
ld = {d['percentage']: d['latency'] / 1e6 for d in (j.get('latencyDistribution') or [])}
ok = j.get('statusCodeDistribution', {}).get('OK', 0)
errs = {k: v for k, v in (j.get('statusCodeDistribution') or {}).items() if k != 'OK'}
for k, v in (j.get('errorDistribution') or {}).items(): errs['err:' + k[:60].replace(',', ' ').replace(';', ' ')] = v
tot_s = j['total'] / 1e9
p50 = f"{ld[50]:.2f}" if 50 in ld else ''; p99 = f"{ld[99]:.2f}" if 99 in ld else ''
nerr = sum(v for k, v in (j.get('statusCodeDistribution') or {}).items() if k != 'OK')
print(f"{ok / tot_s:.1f},{p50},{p99},{nerr},{ok},{';'.join(f'{k}={v}' for k, v in sorted(errs.items()))},{cpu:.2f},{tot_s:.2f}")
EOF
  fi
}

for c in $CONCS; do
  for m in stock mine; do
    s=$PREFIX-$m; r=$OUT/c$c-$m
    left=$((DEADLINE - $(date +%s)))
    if [ "$left" -le $((WARM_S + MEASURE_S + 150)) ]; then
      say "SKIP $s c=$c: only ${left}s before the deadline"; echo "$WL,$s,$m,$c,,,,,,,,,,,,,,,,,,,skipped_deadline" >> "$CSV"; continue; fi
    say "$s c=$c: waiting for idle"; wait_idle
    if [ "$IDLE_OK" != 1 ]; then echo "$WL,$s,$m,$c,0,$IDLE_WAIT,,,,,,,,,,,,,,,,,not_idle" >> "$CSV"; continue; fi
    st=$(date +%s.%N)
    w=$(run_load $WARM_S $c $m $r.warmup)
    if [ "$KIND" = http ]; then wx=$(python3 -c "import json;j=json.load(open('$r.warmup.json'));print(j['p99_ms'])" 2>/dev/null); else wx=$(echo "$w" | cut -d, -f3); fi
    c0=$(cpusnap); l=$(run_load $MEASURE_S $c $m $r.load); c1=$(cpusnap)
    cpu=$(echo "$c0 $c1" | awk -v n=$(nproc) '{fc=($4-$1)/100; b=$5-$2; i=$6-$3; printf "%.2f,%.2f", fc, (b+i>0? b/(b+i)*n : 0)}')
    pe=$(kubectl get pods --no-headers -l serving.knative.dev/service=$s 2>/dev/null | wc -l); ve=$(pgrep -x firecracker | wc -l)
    IFS=, read -r wr wp50 wp99 we wok wb wcpu wel <<< "$w"
    echo "$WL,$s,$m,$c,1,$IDLE_WAIT,$st,$wr,$wok,$we,$wb,$wx,$l,$pe,$ve,$cpu," >> "$CSV"
    say "$s c=$c: warm-up rps=$wr ok=$wok err=$we [$wb] | measured rps,p50,p99,err,ok,breakdown,cpu,elapsed = $l | pods=$pe vms=$ve fc_cpu_s,host_busy_cores=$cpu"
  done
done

if [ "$VARY" = 1 ]; then
  for m in stock mine; do
    s=$PREFIX-$m; r=$OUT/vary-$m
    left=$((DEADLINE - $(date +%s)))
    if [ "$left" -le 300 ]; then say "SKIP vary $s: only ${left}s before the deadline"; continue; fi
    say "vary $s: waiting for idle"; wait_idle
    [ "$IDLE_OK" = 1 ] || { say "vary $s: not idle, skipped"; continue; }
    if [ "$KIND" = http ]; then sel="--http $HTTP_PATH"; else sel="--grpc $PREFIX"; fi
    timeout -k 10 600 $PY $V/vary.py --ip "$INGRESS_IP" --host "${HOST[$m]}" --svc "$s" --steps "$STEPS" --out "$r" $sel > "$r.out" 2> "$r.err"
    say "vary $s: exit=$? $(head -c 600 $r.out)"
  done
fi
say "done wl=$WL"
