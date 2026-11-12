#!/usr/bin/env bash
# night.sh — overnight benchmark: stock vHive microVM vs shared Firecracker VM vs per-pod Firecracker VM
# vs per-pod Firecracker VM running the REAL image (firecracker-vm://image, mode "image", 256 MiB)
# and the same with 512 MiB like stock (mode "image512").
# night-1112 = night-1111 plus the "image" mode and a MODES knob; see README.md "What changed vs night-1111".
# Phases A..E (see README.md). Each phase runs in its own process under `timeout`; a failing phase is
# logged and the next one starts. Never uses sudo, never restarts daemons, never edits vhive.
#
#   tmux new -d -s night 'bash ~/fc-vm/night-1112/night.sh'
#
# Knobs (environment): CYCLES (11), LEVELS ("1 2 4 8 16 32"), WARM_S (10), MEASURE_S (30),
#   C_CYCLES (6), PHASES ("A B C D E"), OUT_ROOT (~/fc-vm/night-1112), BUDGET_S (24600 = 6h50m),
#   MODES ("stock shared perpod image image512"; e.g. MODES="stock image image512" for only the same-image runs).
set -u

FCVM=${FCVM:-$HOME/fc-vm}
HERE=$(cd "$(dirname "$0")" && pwd)
SELF="$HERE/$(basename "$0")"
OUT_ROOT=${OUT_ROOT:-$FCVM/night-1112}
CYCLES=${CYCLES:-11}
C_CYCLES=${C_CYCLES:-6}
LEVELS=${LEVELS:-"1 2 4 8 16 32"}
WARM_S=${WARM_S:-10}
MEASURE_S=${MEASURE_S:-30}
PHASES=${PHASES:-"A B C D E"}
BUDGET_S=${BUDGET_S:-24600}
MODES=${MODES:-"stock shared perpod image image512"}
# mode "image": same image as stock (crccheck-hello-world), booted by vhive in a per-pod Firecracker VM
IMAGE_REF=ghcr.io/vhive-serverless/crccheck-hello-world:latest
IMAGE_YAML=$FCVM/real/vm.yaml      # ksvc real-vm, used for phase B (VM 256 MiB, vhive's default)
IMAGE512_YAML=$FCVM/real/vm-512.yaml  # ksvc real-vm-512, used for phase B (VM 512 MiB = stock's size)
FC_ROOTFS_CLI=$HOME/vhive/bin/fc-rootfs

INGRESS_IP=10.106.195.52
INGRESS=http://$INGRESS_IP/
DOMAIN=default.192.168.1.240.sslip.io
VLOG=/tmp/vhive-logs/vhive_log.txt
FC_PANE=firecracker:0.0     # firecracker-containerd (debug), read only
CTRD_PANE=containerd:0.0    # containerd (info, CRI calls), read only
MASTER_LOG="$OUT_ROOT/night.log"
export FCVM HERE OUT_ROOT CYCLES C_CYCLES LEVELS WARM_S MEASURE_S INGRESS DOMAIN VLOG FC_PANE CTRD_PANE MASTER_LOG
export MODES IMAGE_REF IMAGE_YAML IMAGE512_YAML FC_ROOTFS_CLI
has_mode() { case " $MODES " in *" $1 "*) return 0;; esac; return 1; }
# phase B service of each mode
bsvc() { case $1 in stock) echo fcmicro-fn;; shared) echo fcweb-fn;; perpod) echo fcpod-fn;; image) echo real-vm;; image512) echo real-vm-512;; esac; }
# wait until every phase-B service of MODES is at zero (pods and VMs)
wait_zero_all() { local m; for m in $MODES; do wait_zero "$(bsvc "$m")" "$m" "$1"; done; }

# phase letter -> output directory and time budget in seconds (sum 24300 s = 6h45m < 7 h)
pdir() { case $1 in A) echo A_preflight;; B) echo B_cold;; C) echo C_stock_breakdown;; D) echo D_scaleout;; E) echo E_analysis;; esac; }
pbudget() { case $1 in A) echo 900;; B) echo 9000;; C) echo 2400;; D) echo 11400;; E) echo 600;; esac; }

log() { echo "$(date '+%F %T') [${PHASE:-main}] $*" | tee -a "$MASTER_LOG"; }
now() { date +%s.%N; }

# ---------------------------------------------------------------- shared helpers
pods_of() { kubectl get pods -l serving.knative.dev/service="$1" --no-headers 2>/dev/null | wc -l; }
stock_vms() { pgrep -f '^/usr/local/bin/firecracker --api-sock firecracker.sock' | wc -l; }
# (perpod, image and image512 share the slots fcweb1..16, so all use slot_vms)
shared_vm() { pgrep -f '^/usr/local/bin/firecracker --api-sock /run/vhive-fcweb/fc.sock' | wc -l; }
slot_vms() { pgrep -f '^/usr/local/bin/firecracker --api-sock /run/vhive-fcweb/[0-9]+/fc.sock' | wc -l; }
# wait until ksvc $1 has no pods and no VM of its kind ($2 = stock|shared|perpod) is left; give up after $3 s
wait_zero() {
  local svc=$1 kind=$2 lim=${3:-600} t0=$SECONDS vm
  while :; do
    case $kind in stock) vm=$(stock_vms);; shared) vm=$(shared_vm);; perpod|image|image512) vm=$(slot_vms);; esac
    [ "$(pods_of "$svc")" = 0 ] && [ "$vm" = 0 ] && return 0
    [ $((SECONDS - t0)) -ge "$lim" ] && { log "WARN: $svc not at zero after ${lim}s (pods=$(pods_of "$svc") vms=$vm)"; return 1; }
    sleep 2
  done
}

# ================================================================ Phase A: preflight
phase_A() {
  local d=$1 ok=0
  kubectl get ksvc -o wide > "$d/ksvc_before.txt" 2>&1 || { log "kubectl get ksvc failed"; ok=1; }
  cat "$d/ksvc_before.txt" | tee -a "$MASTER_LOG"
  kubectl get nodes -o wide > "$d/nodes.txt" 2>&1
  kubectl get pods -A -o wide > "$d/pods_all.txt" 2>&1

  # Ingress reachable: a request with no Host header gets 404 from the gateway without waking any function.
  local code; code=$(curl -s -o /dev/null -m 5 -w '%{http_code}' "$INGRESS")
  echo "ingress $INGRESS -> HTTP $code" > "$d/ingress.txt"
  if [ "$code" = 000 ]; then log "ingress NOT reachable"; ok=1; else log "ingress reachable (HTTP $code without Host header, expected 404)"; fi

  # Stock baseline service
  if kubectl get ksvc fcmicro-fn >/dev/null 2>&1; then
    log "fcmicro-fn already exists"
  else
    log "creating fcmicro-fn from $FCVM/fcmicro-fn.yaml"
    kubectl apply -f "$FCVM/fcmicro-fn.yaml" > "$d/fcmicro_apply.txt" 2>&1 || ok=1
  fi
  if has_mode image || has_mode image512; then
    # The image VM's rootfs must be built BEFORE any measurement (a cache miss would put a ~1 s build
    # into the first cold start). fc-rootfs is a no-op if it is already cached.
    if "$FC_ROOTFS_CLI" build "$IMAGE_REF" > "$d/fc_rootfs.txt" 2>&1; then
      log "image rootfs ready: $(tail -1 "$d/fc_rootfs.txt")"
    else log "fc-rootfs build $IMAGE_REF FAILED (see fc_rootfs.txt)"; ok=1; fi
  fi
  local m y s
  for m in image image512; do
    has_mode $m || continue
    s=$(bsvc $m); y=$IMAGE_YAML; [ $m = image512 ] && y=$IMAGE512_YAML
    if kubectl get ksvc $s >/dev/null 2>&1; then log "$s already exists"
    else log "creating $s from $y"; kubectl apply -f "$y" > "$d/${s}_apply.txt" 2>&1 || ok=1; fi
  done
  for m in $MODES; do
    s=$(bsvc "$m")
    if kubectl wait ksvc/$s --for=condition=Ready --timeout=300s >> "$d/ksvc_wait.txt" 2>&1; then
      log "ksvc $s Ready"
    else log "ksvc $s NOT Ready"; ok=1; fi
  done
  kubectl get ksvc -o wide > "$d/ksvc_after.txt" 2>&1
  for m in $MODES; do s=$(bsvc "$m"); kubectl get ksvc $s -o yaml > "$d/ksvc_$s.yaml" 2>&1; done

  # Versions
  {
    echo "date: $(date -Is)"; echo "host: $(hostname)  kernel: $(uname -r)  cpus: $(nproc)"
    free -m | head -2
    echo "--- vhive git (~/vhive)"
    git -C "$HOME/vhive" rev-parse --abbrev-ref HEAD; git -C "$HOME/vhive" rev-parse HEAD
    git -C "$HOME/vhive" log -1 --format='%ci %s'; git -C "$HOME/vhive" status --short
    echo "--- vhive-perpod binary"
    ls -l --time-style=full-iso "$HOME/vhive/vhive-perpod"; sha256sum "$HOME/vhive/vhive-perpod"
    echo "--- image modes: fcinit and rootfs (one disk shared by image and image512)"
    sha256sum /users/Arun1903/fc-vm/images/fcinit; "$FC_ROOTFS_CLI" path "$IMAGE_REF"
  } > "$d/versions.txt" 2>&1

  # Daemon env. /proc/<pid>/environ of the root vhive process is not readable without sudo, so the FCWEB_*
  # values are read from the visible sudo command line (`sudo FCWEB_...=.. ./vhive-perpod`).
  ps -eo pid,user,lstart,args | grep -E '[v]hive-perpod|[f]irecracker-containerd|[ ]containerd$' > "$d/daemons.txt"
  grep -o 'FCWEB_[A-Z_]*=[^ ]*' "$d/daemons.txt" | sort -u > "$d/fcweb_env.txt"
  log "daemon env: $(tr '\n' ' ' < "$d/fcweb_env.txt")(from the sudo command line; /proc environ needs root)"
  [ -s "$d/fcweb_env.txt" ] || { log "could not find FCWEB_* on the vhive command line"; ok=1; }

  # Log sources the later phases rely on
  { for p in "$FC_PANE" "$CTRD_PANE" vhive:0.0; do
      echo "pane $p history=$(tmux display -p -t "$p" '#{history_size}/#{history_limit}' 2>&1)"; done
    ls -l "$VLOG"; } > "$d/log_sources.txt" 2>&1
  tmux has-session -t firecracker 2>/dev/null || { log "tmux session 'firecracker' missing (needed for B/C stock)"; ok=1; }
  [ -r "$VLOG" ] || { log "$VLOG not readable"; ok=1; }
  return $ok
}

# ================================================================ Phase B: cold start cycles
# Copies of the existing scripts are run from inside B_cold/<mode>/ because they write their CSVs next to
# themselves (the originals in ~/fc-vm stay untouched and their old results are not overwritten).
phase_B() {
  local d=$1 rc=0 adapt_ok=1
  local m0; for m0 in $MODES; do mkdir -p "$d/$m0"; done
  has_mode stock && cp "$FCVM/cold_cycles_microvm_http.sh" "$d/stock/"
  has_mode shared && cp "$FCVM/cold_cycles_fc.sh" "$d/shared/"
  mkdir -p "$d/perpod"   # the per-pod adaptation below is also the base of the image one
  # per-pod: same script, pointed at fcpod-fn and at the slot VMs (fcweb1..16, /run/vhive-fcweb/<n>/fc.sock).
  # Its log greps ('Firecracker VM reachable|booted|stopped') already match the per-pod log lines.
  sed -e 's/fcweb-fn/fcpod-fn/g' \
      -e "s#pgrep -f '^/usr/local/bin/firecracker --api-sock /run/vhive-fcweb/fc.sock'#pgrep -f '^/usr/local/bin/firecracker --api-sock /run/vhive-fcweb/[0-9]+/fc.sock'#" \
      -e 's#ip link show fcweb0 >/dev/null 2>&1#ip -o link show | grep -qE ": fcweb([1-9]|1[0-6])[:@]"#g' \
      -e 's#\[ -e /run/vhive-fcweb/fc.sock \]#compgen -G "/run/vhive-fcweb/[0-9]*/fc.sock" >/dev/null#g' \
      "$FCVM/cold_cycles_fc.sh" > "$d/perpod/cold_cycles_fc.sh"
  # image: the per-pod script, pointed at real-vm instead of fcpod-fn (same slots, same log lines).
  if has_mode image; then
    mkdir -p "$d/image"
    sed -e 's/fcpod-fn/real-vm/g' "$d/perpod/cold_cycles_fc.sh" > "$d/image/cold_cycles_fc.sh"
    diff "$FCVM/cold_cycles_fc.sh" "$d/image/cold_cycles_fc.sh" > "$d/image/changes_vs_original.diff"
  fi
  # image512: the same, pointed at real-vm-512 (the 512 MiB copy of real-vm).
  if has_mode image512; then
    mkdir -p "$d/image512"
    sed -e 's/fcpod-fn/real-vm-512/g' "$d/perpod/cold_cycles_fc.sh" > "$d/image512/cold_cycles_fc.sh"
    diff "$FCVM/cold_cycles_fc.sh" "$d/image512/cold_cycles_fc.sh" > "$d/image512/changes_vs_original.diff"
  fi
  chmod +x "$d"/*/*.sh
  diff "$FCVM/cold_cycles_fc.sh" "$d/perpod/cold_cycles_fc.sh" > "$d/perpod/changes_vs_original.diff"
  if [ "$(grep -c '^>' "$d/perpod/changes_vs_original.diff")" -lt 5 ] || grep -q 'fcweb-fn\|fcweb0' "$d/perpod/cold_cycles_fc.sh"; then
    log "per-pod script adaptation incomplete, see changes_vs_original.diff"; rc=1; adapt_ok=0
  fi
  if has_mode image && { grep -q 'fcweb-fn\|fcweb0\|fcpod-fn' "$d/image/cold_cycles_fc.sh" || ! grep -q 'real-vm' "$d/image/cold_cycles_fc.sh"; }; then
    log "image script adaptation incomplete, see image/changes_vs_original.diff"; rc=1; adapt_ok=0
  fi
  if has_mode image512 && { grep -q 'fcweb-fn\|fcweb0\|fcpod-fn' "$d/image512/cold_cycles_fc.sh" || ! grep -q 'real-vm-512' "$d/image512/cold_cycles_fc.sh"; }; then
    log "image512 script adaptation incomplete, see image512/changes_vs_original.diff"; rc=1; adapt_ok=0
  fi

  local mode script svc kind
  for mode in $MODES; do
    case $mode in
      stock)  script=cold_cycles_microvm_http.sh; svc=fcmicro-fn; kind=stock;;
      shared) script=cold_cycles_fc.sh;           svc=fcweb-fn;   kind=shared;;
      perpod) script=cold_cycles_fc.sh;           svc=fcpod-fn;   kind=perpod;;
      image)  script=cold_cycles_fc.sh;           svc=real-vm;    kind=image;;
      image512) script=cold_cycles_fc.sh;         svc=real-vm-512; kind=image512;;
    esac
    { [ $mode = perpod ] || [ $mode = image ] || [ $mode = image512 ]; } && [ $adapt_ok = 0 ] && { log "skipping $mode"; continue; }
    # Everything else idle first, so all modes see the same quiet node.
    wait_zero fcmicro-fn stock 900; wait_zero fcweb-fn shared 900; wait_zero fcpod-fn perpod 900
    wait_zero_all 900
    local lim=$(( CYCLES * 300 + 600 ))
    log "$mode ($svc): $CYCLES cold cycles via $script (timeout ${lim}s)"
    local t0=$SECONDS
    ( cd "$d/$mode" && CYCLES=$CYCLES timeout -k 30 "$lim" "./$script" ) > "$d/$mode/stdout.txt" 2>&1
    local r=$?
    local csv=cold_cycles_fc.csv; [ $mode = stock ] && csv=cold_cycles_microvm_http.csv
    log "$mode finished rc=$r in $((SECONDS - t0))s; $(grep -c '^[0-9]' "$d/$mode/$csv" 2>/dev/null || echo 0) cycle rows in $csv"
    tail -n 3 "$d/$mode/stdout.txt" | sed 's/^/    /' | tee -a "$MASTER_LOG"
    [ $r = 0 ] || rc=1
  done
  # phases.py as is (it includes cycle 1; the warm-up exclusion happens in phase E)
  for mode in shared perpod image image512; do
    [ -s "$d/$mode/cold_cycles_fc_phases.csv" ] && python3 "$FCVM/phases.py" "$d/$mode" > "$d/$mode/phases.txt" 2>&1
  done
  return $rc
}

# ================================================================ Phase C: stock VM-stage breakdown
# Dedicated stock cold starts. Right after each request the two tmux panes (containerd, firecracker-containerd)
# are captured, because those daemons log only to the terminal (2000-line scrollback), not to disk.
phase_C() {
  local d=$1 c
  for c in $(seq 1 "$C_CYCLES"); do
    wait_zero fcmicro-fn stock 600 || { log "cycle $c: not idle, skipping"; continue; }
    sleep 3
    local off; off=$(wc -l < "$VLOG")
    local ts; ts=$(now)
    local out; out=$(curl -s -o /dev/null -m 60 -w '%{http_code} %{time_total}' -H "Host: fcmicro-fn.$DOMAIN" "$INGRESS")
    local tr; tr=$(now)
    sleep 4    # let the late log lines (task start, guest stdout, StartContainer) arrive
    tmux capture-pane -pJ -t "$FC_PANE" -S -2000 > "$d/c${c}_firecracker_containerd.txt"
    tmux capture-pane -pJ -t "$CTRD_PANE" -S -2000 > "$d/c${c}_containerd.txt"
    tail -n +"$off" "$VLOG" > "$d/c${c}_vhive.txt"
    local p; p=$(kubectl get pods -l serving.knative.dev/service=fcmicro-fn -o name | head -1)
    [ -n "$p" ] && kubectl get "$p" -o json > "$d/c${c}_pod.json"
    echo "cycle=$c send_epoch=$ts response_epoch=$tr http=${out% *} curl_total_s=${out#* } pod=${p#pod/}" > "$d/c${c}_meta.txt"
    log "cycle $c: HTTP ${out% *} in ${out#* }s"
  done
  python3 "$HERE/stock_breakdown.py" "$d" > "$d/stock_breakdown.stdout" 2>&1
  local r=$?; cat "$d/stock_breakdown.stdout" | tee -a "$MASTER_LOG"; return $r
}

# ================================================================ Phase D: scale-out
# One template -> one ksvc per mode; they differ only in name / guest image / guest port.
gen_yaml() {   # $1 name  $2 GUEST_IMAGE  $3 port  $4 FC_MEM_MIB
  sed -e "s#__NAME__#$1#" -e "s#__GUEST_IMAGE__#$2#" -e "s#__PORT__#$3#g" -e "s#__MEM__#$4#" "$HERE/scale-template.yaml"
}
phase_D() {
  local d=$1 rc=0 m
  declare -A NAME=([stock]=n12-stock [shared]=n12-shared [perpod]=n12-perpod [image]=n12-image [image512]=n12-image512)
  declare -A GIMG=([stock]=ghcr.io/vhive-serverless/crccheck-hello-world:latest [shared]=firecracker-vm://webvm [perpod]=firecracker-vm://pod [image]=firecracker-vm://image [image512]=firecracker-vm://image)
  declare -A PORT=([stock]=8000 [shared]=80 [perpod]=80 [image]=8000 [image512]=8000)
  # FC_MEM_MIB: 256 everywhere as before (stock ignores it: its uVM is always 512), 512 only for image512
  declare -A MEM=([stock]=256 [shared]=256 [perpod]=256 [image]=256 [image512]=512)
  for m in stock shared perpod image image512; do
    gen_yaml "${NAME[$m]}" "${GIMG[$m]}" "${PORT[$m]}" "${MEM[$m]}" > "$d/${NAME[$m]}.yaml"
  done
  { echo "# Lines that differ between the generated ksvc yamls (everything else is identical):"
    diff "$d/n12-stock.yaml" "$d/n12-shared.yaml"; echo "---- shared vs perpod"; diff "$d/n12-shared.yaml" "$d/n12-perpod.yaml"
    echo "---- stock vs image (same image, same port; only GUEST_IMAGE differs)"; diff "$d/n12-stock.yaml" "$d/n12-image.yaml"
    echo "---- image vs image512 (only FC_MEM_MIB differs: 256 -> 512)"; diff "$d/n12-image.yaml" "$d/n12-image512.yaml"
    cat <<'EOF'

# Differences that remain and are NOT visible in the yaml:
# - stock: vHive's ctriface gives every uVM 1 vCPU / 512 MiB (hard-coded); FC_VCPUS/FC_MEM_MIB are ignored.
#   shared/perpod: 1 vCPU / 256 MiB (FC_* in the yaml, same as the daemon's FCWEB_* defaults).
# - stock runs the image's own CMD (busybox httpd on :8000) inside a firecracker-containerd uVM with an agent,
#   devmapper snapshot and per-VM netns; shared/perpod boot the baked-in rootfs.ext4 (python3 http.server on :80).
# - shared: every pod of the ksvc talks to ONE VM (fcweb0); perpod: one VM per pod (slots fcweb1..16);
#   stock: one uVM per pod.
# - image: one VM per pod like perpod, but booting a rootfs built from the SAME image as stock
#   (crccheck-hello-world, busybox httpd on :8000) with fcinit as PID 1; 1 vCPU / 256 MiB like shared/perpod,
#   stock stays at 512 MiB.
# - image512: exactly image, but the VM gets 512 MiB (FC_MEM_MIB=512), the same size as the stock uVM.
EOF
  } > "$d/yaml_differences.txt"
  for m in $MODES; do
    kubectl apply -f "$d/${NAME[$m]}.yaml" >> "$d/apply.txt" 2>&1
  done
  for m in $MODES; do
    kubectl wait ksvc/"${NAME[$m]}" --for=condition=Ready --timeout=300s >> "$d/apply.txt" 2>&1 \
      && log "ksvc ${NAME[$m]} Ready" || { log "ksvc ${NAME[$m]} NOT Ready"; rc=1; }
  done
  echo "mode,conc,rps,p50_ms,p99_ms,errors,max_pods,end_pods,warmup_errors,start_epoch" > "$d/scale.csv"

  local c
  for c in $LEVELS; do
    for m in $MODES; do     # interleaved so slow drift over the night hits all modes alike
      local n=${NAME[$m]} r="$d/$m/c$c"; mkdir -p "$r"
      wait_zero fcmicro-fn stock 300; wait_zero fcweb-fn shared 300; wait_zero fcpod-fn perpod 300
      wait_zero_all 300
      local m2; for m2 in $MODES; do wait_zero "${NAME[$m2]}" "$m2" 900; done
      local st; st=$(now)
      # pod sampler (1 Hz): epoch, total pods, Ready pods
      ( while :; do
          echo "$(date +%s.%N),$(kubectl get pods -l serving.knative.dev/service=$n --no-headers 2>/dev/null | awk '{t++; split($2,a,"/"); if(a[1]==a[2]) r++} END{print t+0","r+0}')"
          sleep 1; done ) > "$r/pods.csv" &
      local sp=$!
      timeout -k 10 $((WARM_S + 60)) python3 "$FCVM/load.py" "$INGRESS_IP" 80 "$WARM_S" "$c" / "$n.$DOMAIN" > "$r/warmup.csv" 2>&1
      local t_meas; t_meas=$(now)
      timeout -k 10 $((MEASURE_S + 60)) python3 "$FCVM/load.py" "$INGRESS_IP" 80 "$MEASURE_S" "$c" / "$n.$DOMAIN" > "$r/load.csv" 2>&1
      local lr=$?
      kill $sp 2>/dev/null; wait $sp 2>/dev/null
      local res wu maxp endp
      res=$(sed -n 2p "$r/load.csv"); wu=$(sed -n 2p "$r/warmup.csv" | cut -d, -f5)
      maxp=$(awk -F, -v t="$t_meas" '$1>=t && $2>m {m=$2} END{print m+0}' "$r/pods.csv")
      endp=$(tail -1 "$r/pods.csv" | cut -d, -f2)
      if [ $lr != 0 ] || [ -z "$res" ]; then log "$m c=$c: load.py failed rc=$lr"; rc=1; res="$c,,,,"; fi
      local _c rps p50 p99 err; IFS=, read -r _c rps p50 p99 err <<<"$res"
      echo "$m,$c,$rps,$p50,$p99,$err,$maxp,$endp,$wu,$st" >> "$d/scale.csv"
      log "$m c=$c: ${rps} req/s p50 ${p50}ms p99 ${p99}ms errors ${err} pods max ${maxp} (warm-up errors ${wu})"
    done
  done
  kubectl get ksvc -o wide > "$d/ksvc_end.txt" 2>&1
  return $rc
}

# ================================================================ Phase E: analysis
phase_E() {
  python3 "$HERE/analyze.py" "$OUT_ROOT" "$1" > "$1/analyze.stdout" 2>&1
  local r=$?; tail -n 5 "$1/analyze.stdout" | tee -a "$MASTER_LOG"; return $r
}

# ---------------------------------------------------------------- dispatcher
if [ "${1:-}" = __phase ]; then
  PHASE=$2; export PHASE
  "phase_$2" "$3"; exit $?
fi

mkdir -p "$OUT_ROOT"
exec 8>"$OUT_ROOT/.night.lock"; flock -n 8 || { echo "night.sh is already running"; exit 1; }
exec 9>/tmp/sweep.lock; flock -n 9 || { echo "a sweep script holds /tmp/sweep.lock; not starting"; exit 1; }

START=$SECONDS
log "night.sh start: phases [$PHASES] CYCLES=$CYCLES C_CYCLES=$C_CYCLES LEVELS=[$LEVELS] warm ${WARM_S}s measure ${MEASURE_S}s out=$OUT_ROOT budget ${BUDGET_S}s"
declare -A RESULT=()
for P in $PHASES; do
  d="$OUT_ROOT/$(pdir "$P")"
  if [ -d "$d" ] && [ -n "$(ls -A "$d")" ]; then      # keep an older run's output, never delete
    old="$OUT_ROOT/prev-$(date +%m%d-%H%M%S)"; mkdir -p "$old"; mv "$d" "$old/"; log "moved old $(pdir "$P") to $old/"
  fi
  mkdir -p "$d"
  left=$(( BUDGET_S - (SECONDS - START) - 60 ))
  lim=$(pbudget "$P"); [ "$lim" -gt "$left" ] && lim=$left
  if [ "$lim" -lt 60 ]; then log "phase $P skipped: overall time budget used up"; RESULT[$P]=skipped; continue; fi
  log "=== phase $P ($(pdir "$P")) start, timeout ${lim}s"
  t0=$SECONDS
  timeout -k 60 "$lim" bash "$SELF" __phase "$P" "$d" > "$d/phase.out" 2>&1
  rc=$?
  case $rc in 0) RESULT[$P]=ok;; 124|137) RESULT[$P]="TIMEOUT after ${lim}s";; *) RESULT[$P]="FAILED rc=$rc";; esac
  log "=== phase $P end: ${RESULT[$P]} ($((SECONDS - t0))s)"
done
log "summary: $(for P in $PHASES; do printf '%s=%s; ' "$P" "${RESULT[$P]:-?}"; done)total $((SECONDS - START))s"
