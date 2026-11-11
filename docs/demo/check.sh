#!/usr/bin/env bash
# check.sh — functional check that one image answers the same in stock vHive and in a per-pod Firecracker VM.
#
#   bash ~/fc-vm/real/check.sh            # apply both, curl each, compare, wait for scale to zero, delete
#   SKIP_ZERO=1 bash ~/fc-vm/real/check.sh   # skip the scale-to-zero wait (about 2 min)
#   KEEP=1 bash ~/fc-vm/real/check.sh        # leave both services in place at the end
# After scale to zero it sends one more request to each service, i.e. a real cold start from zero pods, and
# compares that answer too. Responses are kept in ~/fc-vm/real/out/check-<time>/.
#
# No sudo, no load: a handful of requests per service. Exit code 0 = everything passed.
set -u
HERE=$(cd "$(dirname "$0")" && pwd)
INGRESS=http://10.106.195.52/
DOMAIN=default.192.168.1.240.sslip.io
IMAGE=ghcr.io/vhive-serverless/crccheck-hello-world:latest
REQS=${REQS:-3}
OUT=$HERE/out/check-$(date +%m%d-%H%M%S)
mkdir -p "$OUT"
fails=0
say()  { echo "$(date '+%T') $*"; }
pass() { say "PASS: $*"; }
fail() { say "FAIL: $*"; fails=$((fails+1)); }

# 0. Same image in both manifests (the whole point).
img_stock=$(grep -m1 '^ *image:' "$HERE/stock.yaml" | awk '{print $2}')
img_vm=$(grep -m1 '^ *image:' "$HERE/vm.yaml" | awk '{print $2}')
[ "$img_stock" = "$img_vm" ] && pass "identical image in both manifests: $img_vm" || fail "images differ: $img_stock vs $img_vm"

# 1. The VM path needs a pre-built rootfs, or its first cold start includes the build.
if [ -x ~/vhive/bin/fc-rootfs ]; then
  if p=$(~/vhive/bin/fc-rootfs path "$IMAGE" 2>/dev/null); then pass "rootfs pre-built: $p"
  else say "WARN: rootfs not pre-built; vhive will build it during the first cold start (run: ~/vhive/bin/fc-rootfs build $IMAGE)"; fi
fi

# 2. Apply and wait for Ready.
for s in stock vm; do kubectl apply -f "$HERE/$s.yaml" >/dev/null || fail "kubectl apply $s.yaml"; done
for s in real-stock real-vm; do
  if kubectl wait ksvc/$s --for=condition=Ready --timeout=180s >/dev/null 2>&1; then pass "ksvc $s Ready"
  else fail "ksvc $s not Ready after 180 s"; kubectl get ksvc $s; fi
done

# 3. A few requests each; keep the bodies.
for s in real-stock real-vm; do
  for i in $(seq "$REQS"); do
    code=$(curl -s -m 60 -o "$OUT/$s.$i" -w '%{http_code} %{time_total}' -H "Host: $s.$DOMAIN" "$INGRESS")
    say "$s request $i: HTTP $code s"
    [ "${code%% *}" = 200 ] || fail "$s request $i: HTTP $code"
  done
done

# 4. Compare: every body identical, across requests and across the two paths.
ref="$OUT/real-stock.1"
same=1
for f in "$OUT"/real-*.*; do cmp -s "$ref" "$f" || { same=0; say "differs: $(basename "$f")"; diff <(cat "$ref") <(cat "$f") | head -5; }; done
if [ "$same" = 1 ] && [ -s "$ref" ]; then pass "all $((2*REQS)) responses identical ($(wc -c < "$ref") bytes, sha256 $(sha256sum < "$ref" | cut -c1-16))"
else fail "responses differ or are empty (kept in $OUT)"; fi

# Where did the VM pod run? (slot from the vhive log)
slot=$(grep 'per-pod Firecracker VM reachable' /tmp/vhive-logs/vhive_log.txt | tail -1 | sed -n 's/.*slot=\([0-9]*\).*/\1/p')

# 5. Scale to zero: pods disappear, and for the VM path its firecracker, tap and API socket too.
if [ "${SKIP_ZERO:-0}" != 1 ]; then
  say "waiting for scale to zero (no traffic; Knative needs about 60-90 s)"
  t0=$SECONDS
  while :; do
    np=$(kubectl get pods -l 'serving.knative.dev/service in (real-stock,real-vm)' --no-headers 2>/dev/null | wc -l)
    [ "$np" = 0 ] && break
    [ $((SECONDS-t0)) -ge 300 ] && break
    sleep 3
  done
  if [ "$np" = 0 ]; then pass "both services at zero pods after $((SECONDS-t0)) s"; else fail "$np pods left after 300 s"; fi
  if [ -n "$slot" ]; then
    sleep 3
    left=""
    pgrep -f "firecracker --api-sock /run/vhive-fcweb/$slot/fc.sock" >/dev/null && left="$left firecracker"
    ip link show "fcweb$slot" >/dev/null 2>&1 && left="$left tap:fcweb$slot"
    [ -e "/run/vhive-fcweb/$slot/fc.sock" ] && left="$left fc.sock"
    [ -z "$left" ] && pass "VM in slot $slot fully torn down (no firecracker, tap or socket)" || fail "slot $slot leftovers:$left"
    grep 'per-pod Firecracker VM stopped' /tmp/vhive-logs/vhive_log.txt | tail -1 | sed 's/^/  vhive: /' | cut -c1-200
  fi

  # 5b. Cold start from zero: one request each, same answer expected.
  for s in real-stock real-vm; do
    code=$(curl -s -m 120 -o "$OUT/$s.cold" -w '%{http_code} %{time_total}' -H "Host: $s.$DOMAIN" "$INGRESS")
    say "$s cold request (from zero pods): HTTP $code s"
    [ "${code%% *}" = 200 ] || fail "$s cold request: HTTP $code"
    cmp -s "$ref" "$OUT/$s.cold" && pass "$s cold answer identical" || fail "$s cold answer differs"
  done
  grep 'per-pod Firecracker VM reachable' /tmp/vhive-logs/vhive_log.txt | tail -1 | sed 's/^/  vhive: /' | cut -c1-220
  slot=$(grep 'per-pod Firecracker VM reachable' /tmp/vhive-logs/vhive_log.txt | tail -1 | sed -n 's/.*slot=\([0-9]*\).*/\1/p')
fi

# 6. Delete.
if [ "${KEEP:-0}" != 1 ]; then
  for s in stock vm; do kubectl delete -f "$HERE/$s.yaml" --wait=true >/dev/null 2>&1 || fail "delete $s.yaml"; done
  pass "deleted real-stock and real-vm"
  # Deleting a running pod must tear its VM down as well.
  t0=$SECONDS
  while [ $((SECONDS-t0)) -lt 120 ] && kubectl get pods -l 'serving.knative.dev/service in (real-stock,real-vm)' --no-headers 2>/dev/null | grep -q .; do sleep 3; done
  if [ -n "$slot" ]; then
    sleep 3
    if pgrep -f "firecracker --api-sock /run/vhive-fcweb/$slot/fc.sock" >/dev/null || ip link show "fcweb$slot" >/dev/null 2>&1; then
      fail "after delete: VM or tap of slot $slot still present"
    else pass "after delete: slot $slot clean"; fi
  fi
fi

say "responses kept in $OUT"
if [ "$fails" = 0 ]; then say "ALL CHECKS PASSED"; else say "$fails CHECK(S) FAILED"; fi
exit $((fails > 0))
