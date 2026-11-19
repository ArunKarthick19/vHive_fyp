#!/usr/bin/env bash
# phaseC.sh — night-1111 phase C (stock VM-stage breakdown) re-run on ksvc bb-stock.
# Same method as ~/vhive/bench/night-1111/night.sh phase_C: wait for zero, sleep 3, one curl, sleep 4, capture the
# firecracker-containerd and containerd tmux panes (-S -2000), slice the vhive log, save pod json + meta, then run
# night-1111's stock_breakdown.py (unchanged) on the folder. Differences from night-1111: service bb-stock (not
# fcmicro-fn), ingress ClusterIP read from kubectl, idle = cold.sh's stricter check (no pods in the namespace, no firecracker
# process, no fcweb tap, no fcweb socket), and after the last cycle the live stock VM's memory is read (see MEMCHECK).
# Launch: nohup bash ~/fc-vm/bench/phaseC.sh > ~/fc-vm/bench/phaseC.log 2>&1 &
set -u
SVC=${SVC:-bb-stock}
C_CYCLES=${C_CYCLES:-6}
D=${D:-$HOME/fc-vm/bench/out/C_stock_breakdown}
VLOG=/tmp/vhive-logs/vhive_log.txt
FC_PANE=firecracker:0.0
CTRD_PANE=containerd:0.0
BREAKDOWN=$HOME/vhive/bench/night-1111/stock_breakdown.py
mkdir -p "$D"
say() { echo "$(date '+%F %T') $*"; }
now() { date +%s.%N; }
INGRESS_IP=$(kubectl -n istio-system get svc istio-ingressgateway -o jsonpath='{.spec.clusterIP}')
HOST=$(kubectl get ksvc "$SVC" -o jsonpath='{.status.url}'); HOST=${HOST#*://}
say "phase C start svc=$SVC host=$HOST ingress=$INGRESS_IP cycles=$C_CYCLES out=$D"
busy() {
  local b="" n
  n=$(kubectl get pods --no-headers 2>/dev/null | wc -l);   [ "$n" = 0 ] || b="$b pods=$n"
  n=$(pgrep -x firecracker | wc -l);                        [ "$n" = 0 ] || b="$b firecracker=$n"
  n=$(ip -o link show 2>/dev/null | grep -c ': fcweb');     [ "$n" = 0 ] || b="$b tap=$n"
  n=$(ls /run/vhive-fcweb/*/fc.sock 2>/dev/null | wc -l);   [ "$n" = 0 ] || b="$b sock=$n"
  echo "$b"
}
wait_idle() { local t0=$SECONDS ok=0 b; while :; do b=$(busy); if [ -z "$b" ]; then ok=$((ok+1)); [ $ok -ge 3 ] && return 0; else ok=0; fi
  [ $((SECONDS-t0)) -ge 600 ] && { say "WARN not idle after 600 s:$b"; return 1; }; sleep 2; done; }

for c in $(seq 1 "$C_CYCLES"); do
  t0=$SECONDS
  if ! wait_idle; then echo "cycle=$c status=not_idle" > "$D/c${c}_notidle.txt"; say "cycle $c: FAILURE not idle, no request"; continue; fi
  idle=$((SECONDS-t0))
  sleep 3
  off=$(wc -l < "$VLOG")
  ts=$(now)
  out=$(curl -s -o "$D/c${c}_body" -m 60 -w '%{http_code} %{time_total}' -H "Host: $HOST" "http://$INGRESS_IP/")
  tr=$(now)
  sleep 4
  tmux capture-pane -pJ -t "$FC_PANE" -S -2000 > "$D/c${c}_firecracker_containerd.txt"
  tmux capture-pane -pJ -t "$CTRD_PANE" -S -2000 > "$D/c${c}_containerd.txt"
  tail -n +"$off" "$VLOG" > "$D/c${c}_vhive.txt"
  p=$(kubectl get pods -l serving.knative.dev/service=$SVC -o name | head -1)
  [ -n "$p" ] && kubectl get "$p" -o json > "$D/c${c}_pod.json"
  echo "cycle=$c send_epoch=$ts response_epoch=$tr http=${out% *} curl_total_s=${out#* } pod=${p#pod/} idle_wait_s=$idle body_sha16=$(sha256sum < "$D/c${c}_body" | cut -c1-16)" > "$D/c${c}_meta.txt"
  say "cycle $c: HTTP ${out% *} in ${out#* }s (idle wait ${idle}s)"
  if [ "$c" = "$C_CYCLES" ]; then
    # MEMCHECK: the live stock uVM of this cycle (pod still running). (1) process + cmdline, unprivileged;
    # (2) listening sockets from `ss -xl` (unprivileged, for the record); (3) ONE sudo GET /machine-config via /proc/<pid>/cwd;
    # (4) the MachineCfg of the create-VM request in the firecracker-containerd debug log (already captured).
    M=$D/memcheck.txt
    { echo "# memcheck $(date -Is), cycle $c, pod ${p#pod/}"
      echo "## stock firecracker processes (pgrep -a -f '^/usr/local/bin/firecracker --api-sock firecracker.sock'), owner:"
      for pid in $(pgrep -f '^/usr/local/bin/firecracker --api-sock firecracker.sock'); do ps -o pid=,user=,lstart=,args= -p "$pid"; done
      echo "## listening firecracker API sockets (ss -xl)"
      ss -xl | grep -o '[^ ]*firecracker\.sock' | sort -u
    } > "$M" 2>&1
    pids=$(pgrep -f '^/usr/local/bin/firecracker --api-sock firecracker.sock')
    npid=$(echo "$pids" | grep -c .)
    if [ "$npid" = 1 ]; then
      # the API socket is relative to the process's cwd (root-only shim dir), so it is reached via /proc/<pid>/cwd
      echo "## sudo -n curl -s --unix-socket /proc/$pids/cwd/firecracker.sock http://localhost/machine-config   (GET, read-only)" >> "$M"
      sudo -n curl -s -m 5 --unix-socket "/proc/$pids/cwd/firecracker.sock" http://localhost/machine-config >> "$M" 2>&1; echo " (exit $?)" >> "$M"
    else
      echo "## NOT queried: expected exactly 1 stock firecracker process, found $npid" >> "$M"
    fi
    echo "## create VM request MachineCfg in the firecracker-containerd capture of this cycle (last occurrence)" >> "$M"
    grep -o 'create VM request: VMID:"[^"]*" MachineCfg:<[^>]*>' "$D/c${c}_firecracker_containerd.txt" | tail -1 >> "$M"
    say "memcheck written: $(tr '\n' ' ' < "$M" | grep -oE '"mem_size_mib": *[0-9]+|MemSizeMib:[0-9]+' | tr '\n' ' ')"
  fi
done
python3 "$BREAKDOWN" "$D" > "$D/stock_breakdown.stdout" 2>&1; r=$?
cat "$D/stock_breakdown.stdout"
say "phase C done (stock_breakdown.py exit $r)"
