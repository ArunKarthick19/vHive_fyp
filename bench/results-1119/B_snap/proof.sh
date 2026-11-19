#!/usr/bin/env bash
# proof.sh N — N manual cold starts of snap-stock (snapshots daemon), each from an idle node, with evidence:
# the firecracker-containerd + containerd panes (sliced to this cycle's time window), the vhive log slice, and
# /fccd/snapshots before/after. Window per cycle = from the request until the node is idle again (so it includes
# the scale-down, where vHive creates the snapshot). Output: ~/fc-vm/bench/snap/proof/p<i>/.
set -u
N=${1:-3}; H=$HOME/fc-vm/bench/snap; D0=$H/proof; mkdir -p "$D0"
VLOG=/tmp/vhive-logs/vhive_log.txt
IP=$(kubectl -n istio-system get svc istio-ingressgateway -o jsonpath='{.spec.clusterIP}')
HOST=$(kubectl get ksvc snap-stock -o jsonpath='{.status.url}'); HOST=${HOST#*://}
say() { echo "$(date '+%F %T') $*"; }
busy() { local b=""; [ "$(kubectl get pods --no-headers 2>/dev/null | wc -l)" = 0 ] || b="$b pods"; pgrep -x firecracker >/dev/null && b="$b firecracker"
  ip -o link | grep -q ': fcweb' && b="$b tap"; ls /run/vhive-fcweb/*/fc.sock >/dev/null 2>&1 && b="$b sock"; echo "$b"; }
wait_idle() { local t0=$SECONDS ok=0; while :; do if [ -z "$(busy)" ]; then ok=$((ok+1)); [ $ok -ge 3 ] && return 0; else ok=0; fi; [ $((SECONDS-t0)) -ge 600 ] && return 1; sleep 2; done; }
snaps() { sudo find /fccd/snapshots -maxdepth 2 -printf '%TT %M %10s %p\n' 2>&1 | sort -k4; echo "du: $(sudo du -sh --apparent-size /fccd/snapshots 2>&1 | cut -f1) apparent, $(sudo du -sh /fccd/snapshots 2>&1 | cut -f1) on disk"; }
for i in $(seq 1 "$N"); do
  D=$D0/p$i; mkdir -p "$D"
  wait_idle || { say "p$i: not idle after 600 s"; echo not_idle > "$D/FAILED"; continue; }
  snaps > "$D/snapshots_before.txt"
  off=$(wc -l < "$VLOG"); t0=$(date +%s.%N)
  res=$(curl -s -m 120 -o "$D/body" -w '%{http_code} %{time_total}' -H "Host: $HOST" "http://$IP/")
  say "p$i: HTTP $res"
  sleep 4   # early capture (boot / restore lines), as night-1111 phase C did
  tmux capture-pane -pJ -t firecracker:0.0 -S -2000 > "$D/firecracker_containerd.early.full.txt"
  python3 -I $H/window.py "$D/firecracker_containerd.early.full.txt" "$((${t0%.*}-1))" > "$D/firecracker_containerd.early.txt"
  wait_idle; t1=$(date +%s.%N)
  sleep 2
  tmux capture-pane -pJ -t firecracker:0.0 -S -2000 > "$D/firecracker_containerd.full.txt"
  tmux capture-pane -pJ -t containerd:0.0 -S -2000 > "$D/containerd.full.txt"
  python3 -I $H/window.py "$D/firecracker_containerd.full.txt" "$((${t0%.*}-1))" > "$D/firecracker_containerd.txt"
  python3 -I $H/window.py "$D/containerd.full.txt" "$((${t0%.*}-1))" > "$D/containerd.txt"
  tail -n +"$((off+1))" "$VLOG" > "$D/vhive.txt"
  snaps > "$D/snapshots_after.txt"
  echo "send_epoch=$t0 idle_again_epoch=$t1 http=${res% *} time_total=${res#* } body_sha16=$(sha256sum < "$D/body" | cut -c1-16)" > "$D/meta.txt"
  { echo "## p$i evidence counts (firecracker-containerd window / vhive slice)"
    for pat in 'create VM request' 'startInstance successful' 'successfully started the VM' 'LoadSnapshot|load snapshot|Loading snapshot' 'CreateSnapshot|create snapshot|Creating snapshot' 'PauseVM|Pausing' 'ResumeVM|Resuming'; do
      echo "fc-ctrd early $(grep -ciE "$pat" "$D/firecracker_containerd.early.txt")  late $(grep -ciE "$pat" "$D/firecracker_containerd.txt")  /$pat/"; done
    for pat in 'httpd started' 'response:200' 'failed' 'level=error'; do echo "vhive    $(grep -ciE "$pat" "$D/vhive.txt")  /$pat/"; done; } > "$D/evidence.txt"
  cat "$D/evidence.txt"
done
