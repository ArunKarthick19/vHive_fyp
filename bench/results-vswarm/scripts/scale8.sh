#!/usr/bin/env bash
# scale8.sh — load sweep with autoscaling enabled (services already updated to max-scale 8 / target 4; this script does NOT
# deploy, update or scale anything: it only reads ksvc URLs and pods).
# Same method and ghz invocation as sweep2.sh (run_load copied verbatim): every cell from ZERO pods (node-wide idle check),
# 30 s warm-up (includes the cold start) then 30 s measured, closed loop, c workers / c connections; stock then mine per cell.
# Added: CYCLES loop (cycle-major: cycle -> fn -> c -> mode), podsamp.py at 1 Hz per cell (pods / ready / host busy cores),
# pod Ready timestamps, pods at end of warm-up and end of measurement (from the sampler, plus a direct kubectl at the end).
#   OUT=<dir> [FNS="aesgo authgo"] [CONCS="1 2 4 8 16 32"] [CYCLES=3] DEADLINE=<epoch> bash scale8.sh
set -u
OUT=${OUT:?}; DEADLINE=${DEADLINE:?}; FNS=${FNS:-aesgo authgo}; CONCS=${CONCS:-1 2 4 8 16 32}; CYCLES=${CYCLES:-3}
WARM_S=${WARM_S:-30}; MEASURE_S=${MEASURE_S:-30}; KIND=grpc
IDLE_TIMEOUT_S=600; STABLE_POLLS=3; POLL_S=2; SETTLE_S=5
V=$HOME/fc-vm/vswarm; PY=$HOME/fc-vm/tools/venv/bin/python; GHZ=$HOME/fc-vm/tools/ghz
mkdir -p "$OUT"; LOG=$OUT/sweep.log; CSV=$OUT/cells.csv
say() { echo "$(date '+%F %T') $*" | tee -a "$LOG"; }
INGRESS_IP=$(kubectl -n istio-system get svc istio-ingressgateway -o jsonpath='{.spec.clusterIP}')
source $V/fn.sh
say "start fns='$FNS' concs='$CONCS' cycles=$CYCLES warm=${WARM_S}s measure=${MEASURE_S}s ingress=$INGRESS_IP deadline=$(date -d @$DEADLINE '+%T')"
[ -s "$CSV" ] || echo "cycle,fn,svc,mode,conc,idle_ok,idle_wait_s,start_epoch,warm_end_epoch,meas_end_epoch,warm_rps,warm_ok,warm_errors,warm_p99_ms,rps,p50_ms,p99_ms,errors,ok,err_breakdown,client_cpu_s,elapsed_s,pods_end_kubectl,vms_end,fc_cpu_s,host_busy_cores_meas_avg,note" > "$CSV"

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
cpusnap() {
  local fc=0 p
  for p in $(pgrep -x firecracker); do fc=$((fc + $(awk '{print $14+$15}' /proc/$p/stat 2>/dev/null || echo 0))); done
  echo "$fc $(awk '/^cpu /{print $2+$3+$4+$7+$8+$9, $5+$6}' /proc/stat)"
}
# run_load: verbatim from sweep2.sh (grpc branch)
run_load() {
  local d=$1 c=$2 m=$3 o=$4
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
}

for cyc in $(seq 1 $CYCLES); do
 for fn in $FNS; do
  fn_params $fn; declare -A HOST
  for m in stock mine; do u=$(kubectl get ksvc $fn-$m -o jsonpath='{.status.url}'); HOST[$m]=${u#*://}; done
  for c in $CONCS; do
   for m in stock mine; do
    s=$fn-$m; d=$OUT/cyc$cyc/$fn; mkdir -p $d; r=$d/c$c-$m
    left=$((DEADLINE - $(date +%s)))
    if [ "$left" -le $((WARM_S + MEASURE_S + 150)) ]; then
      say "SKIP cyc$cyc $s c=$c: only ${left}s before the deadline"; echo "$cyc,$fn,$s,$m,$c,,,,,,,,,,,,,,,,,,,,,,skipped_deadline" >> "$CSV"; continue; fi
    say "cyc$cyc $s c=$c: waiting for idle"; wait_idle
    if [ "$IDLE_OK" != 1 ]; then echo "$cyc,$fn,$s,$m,$c,0,$IDLE_WAIT,,,,,,,,,,,,,,,,,,,,not_idle" >> "$CSV"; continue; fi
    $PY $V/podsamp.py $s $r & SP=$!
    sleep 1.5   # sampler has its first (zero-pod) sample before load starts
    st=$(date +%s.%N)
    w=$(run_load $WARM_S $c $m $r.warmup); we_t=$(date +%s.%N)
    wx=$(echo "$w" | cut -d, -f3)
    c0=$(cpusnap); l=$(run_load $MEASURE_S $c $m $r.load); c1=$(cpusnap); me_t=$(date +%s.%N)
    pe=$(kubectl get pods --no-headers -l serving.knative.dev/service=$s 2>/dev/null | wc -l); ve=$(pgrep -x firecracker | wc -l)
    kubectl get pods -o wide -l serving.knative.dev/service=$s > $r.pods_end.txt 2>&1
    kill $SP; wait $SP 2>/dev/null
    cpu=$(echo "$c0 $c1" | awk -v n=$(nproc) '{fc=($4-$1)/100; b=$5-$2; i=$6-$3; printf "%.2f,%.2f", fc, (b+i>0? b/(b+i)*n : 0)}')
    IFS=, read -r wr wp50 wp99 werr wok wb wcpu wel <<< "$w"
    echo "$cyc,$fn,$s,$m,$c,1,$IDLE_WAIT,$st,$we_t,$me_t,$wr,$wok,$werr,$wx,$l,$pe,$ve,$cpu," >> "$CSV"
    say "cyc$cyc $s c=$c: warm-up rps=$wr ok=$wok err=$werr | measured rps,p50,p99,err,ok,breakdown,cpu,elapsed = $l | pods_end=$pe vms=$ve fc_cpu_s,host_busy_cores=$cpu"
   done
  done
 done
done
say "done"
