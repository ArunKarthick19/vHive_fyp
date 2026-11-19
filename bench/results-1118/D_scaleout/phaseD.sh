#!/usr/bin/env bash
# phaseD.sh — scale-out, busybox (crccheck-hello-world) only, modes stock and mine (firecracker-vm://image, FC_MEM_MIB=512).
# Two NEW ksvcs from ~/fc-vm/night-1112/scale-template.yaml (__MEM__=512 for both; max-scale 8, target 4, as the template).
# Cells, in order: for c in LEVELS: stock keep-alive, mine keep-alive, and at c in CHURN_LEVELS also stock --new-conn,
# mine --new-conn. Every cell starts from zero (no pods, no firecracker, no fcweb tap/socket), then WARM_S of load
# (not measured), then MEASURE_S of load (measured). `ss -s` is saved before the warm-up, between, and after.
# Launch: nohup bash ~/fc-vm/bench/phaseD.sh > ~/fc-vm/bench/phaseD.log 2>&1 &
set -u
B=$HOME/fc-vm/bench
D=${D:-$B/out/D_scaleout}
LEVELS=${LEVELS:-"1 2 4 8 16 32"}
CHURN_LEVELS=${CHURN_LEVELS:-"8 16"}
WARM_S=${WARM_S:-30}
MEASURE_S=${MEASURE_S:-30}
TEMPLATE=$HOME/fc-vm/night-1112/scale-template.yaml
DEADLINE=${DEADLINE:-$(date -d '2026-11-18 22:15' +%s)}
declare -A NAME=([stock]=d19-stock [mine]=d19-mine)
declare -A GIMG=([stock]=ghcr.io/vhive-serverless/crccheck-hello-world:latest [mine]=firecracker-vm://image)
mkdir -p "$D"
say() { echo "$(date '+%F %T') $*"; }
now() { date +%s.%N; }
tw() { ss -s | sed -n 's/.*timewait \([0-9]*\).*/\1/p' | head -1; }

for m in stock mine; do
  sed -e "s#__NAME__#${NAME[$m]}#" -e "s#__GUEST_IMAGE__#${GIMG[$m]}#" -e "s#__PORT__#8000#g" -e "s#__MEM__#512#" "$TEMPLATE" > "$D/${NAME[$m]}.yaml"
done
diff "$D/d19-stock.yaml" "$D/d19-mine.yaml" > "$D/yaml_differences.txt"
for m in stock mine; do kubectl apply -f "$D/${NAME[$m]}.yaml" 2>&1 | grep -v '^Warning'; done
for m in stock mine; do kubectl wait ksvc/${NAME[$m]} --for=condition=Ready --timeout=300s || say "ksvc ${NAME[$m]} NOT Ready"; done
INGRESS_IP=$(kubectl -n istio-system get svc istio-ingressgateway -o jsonpath='{.spec.clusterIP}')
declare -A HOST
for m in stock mine; do u=$(kubectl get ksvc ${NAME[$m]} -o jsonpath='{.status.url}'); HOST[$m]=${u#*://}; done
say "phase D start ingress=$INGRESS_IP levels=[$LEVELS] churn=[$CHURN_LEVELS] warm=${WARM_S}s measure=${MEASURE_S}s deadline $(date -d @$DEADLINE '+%T')"
for m in stock mine; do say "  $m: ${NAME[$m]} Host ${HOST[$m]}"; done

CSV=$D/scale.csv
echo "mode,conn,conc,rps,p50_ms,p99_ms,errors,err_breakdown,ok,attempts,conns_opened,client_cpu_s,max_pods_meas,end_pods,warm_rps,warm_errors,warm_err_breakdown,tw_before,tw_mid,tw_after,idle_wait_s,start_epoch,note" > "$CSV"

busy() {
  local b="" n
  n=$(kubectl get pods --no-headers 2>/dev/null | wc -l);   [ "$n" = 0 ] || b="$b pods=$n"
  n=$(pgrep -x firecracker | wc -l);                        [ "$n" = 0 ] || b="$b firecracker=$n"
  n=$(ip -o link show 2>/dev/null | grep -c ': fcweb');     [ "$n" = 0 ] || b="$b tap=$n"
  n=$(ls /run/vhive-fcweb/*/fc.sock 2>/dev/null | wc -l);   [ "$n" = 0 ] || b="$b sock=$n"
  echo "$b"
}
wait_idle() { local t0=$SECONDS ok=0 b last=""; while :; do b=$(busy); if [ -z "$b" ]; then ok=$((ok+1)); [ $ok -ge 3 ] && { sleep 5; IDLE=$((SECONDS-t0)); return 0; }; else ok=0; last=$b; fi
  [ $((SECONDS-t0)) -ge 900 ] && { say "  WARN not idle after 900 s:$last"; IDLE=$((SECONDS-t0)); return 1; }; sleep 2; done; }
field() { sed -n 2p "$1" | cut -d, -f"$2"; }   # load2.py row: 1 conc 2 rps 3 p50 4 p99 5 errors 6 ok 7 attempts 8 elapsed 9 conns 10 mode 11 breakdown 12 cpu

cell() {  # $1 mode  $2 conc  $3 keepalive|new-conn
  local m=$1 c=$2 cm=$3 r="$D/$1/$3/c$2" flag="" note=""
  [ "$cm" = new-conn ] && flag=--new-conn
  if [ $(( $(date +%s) + WARM_S + MEASURE_S + 120 )) -gt "$DEADLINE" ]; then
    say "SKIP $m $cm c=$c: deadline"; echo "$m,$cm,$c,,,,,,,,,,,,,,,,,,,,skipped_deadline" >> "$CSV"; return; fi
  mkdir -p "$r"
  say "$m $cm c=$c: waiting for zero"
  wait_idle || note="not_idle_at_start"
  ss -s > "$r/ss_before.txt"; local twb; twb=$(tw)
  local st; st=$(now)
  ( while :; do echo "$(date +%s.%N),$(kubectl get pods -l serving.knative.dev/service=${NAME[$m]} --no-headers 2>/dev/null | awk '{t++; split($2,a,"/"); if(a[1]==a[2]) r++} END{print t+0","r+0}')"; sleep 1; done ) > "$r/pods.csv" &
  local sp=$!
  timeout -k 10 $((WARM_S + 60)) python3 -I "$B/load2.py" "$INGRESS_IP" 80 "$WARM_S" "$c" / "${HOST[$m]}" $flag --json "$r/warmup.json" > "$r/warmup.csv" 2> "$r/warmup.err"
  local wr=$?
  ss -s > "$r/ss_mid.txt"; local twm; twm=$(tw)
  local tm; tm=$(now)
  timeout -k 10 $((MEASURE_S + 60)) python3 -I "$B/load2.py" "$INGRESS_IP" 80 "$MEASURE_S" "$c" / "${HOST[$m]}" $flag --json "$r/load.json" > "$r/load.csv" 2> "$r/load.err"
  local lr=$?
  ss -s > "$r/ss_after.txt"; local twa; twa=$(tw)
  kill $sp 2>/dev/null; wait $sp 2>/dev/null
  [ $wr = 0 ] || note="${note:+$note;}warmup_rc_$wr"
  [ $lr = 0 ] || note="${note:+$note;}load_rc_$lr"
  local maxp endp
  maxp=$(awk -F, -v t="$tm" '$1>=t && $2>m {m=$2} END{print m+0}' "$r/pods.csv")
  endp=$(tail -1 "$r/pods.csv" | cut -d, -f2)
  echo "$m,$cm,$c,$(field "$r/load.csv" 2),$(field "$r/load.csv" 3),$(field "$r/load.csv" 4),$(field "$r/load.csv" 5),$(field "$r/load.csv" 11),$(field "$r/load.csv" 6),$(field "$r/load.csv" 7),$(field "$r/load.csv" 9),$(field "$r/load.csv" 12),$maxp,$endp,$(field "$r/warmup.csv" 2),$(field "$r/warmup.csv" 5),$(field "$r/warmup.csv" 11),$twb,$twm,$twa,$IDLE,$st,$note" >> "$CSV"
  say "$m $cm c=$c: $(field "$r/load.csv" 2) rps p50 $(field "$r/load.csv" 3) p99 $(field "$r/load.csv" 4) ms, errors $(field "$r/load.csv" 5) [$(field "$r/load.csv" 11)], conns $(field "$r/load.csv" 9), pods max $maxp | warm-up errors $(field "$r/warmup.csv" 5) [$(field "$r/warmup.csv" 11)] | TIME_WAIT $twb/$twm/$twa ${note:+NOTE=$note}"
}

for c in $LEVELS; do
  cell stock "$c" keepalive
  cell mine  "$c" keepalive
  case " $CHURN_LEVELS " in *" $c "*) cell stock "$c" new-conn; cell mine "$c" new-conn;; esac
done
kubectl get ksvc -o wide > "$D/ksvc_end.txt" 2>&1
say "phase D done"
