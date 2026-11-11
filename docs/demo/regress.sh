#!/usr/bin/env bash
# regress.sh — the existing modes still work with the new vhive binary: one cold request each to
#   fcweb-fn   (shared VM, firecracker-vm://webvm, baked rootfs)
#   fcpod-fn   (per-pod VM, firecracker-vm://pod, baked rootfs)
#   fcmicro-fn (stock vHive / firecracker-containerd)
# then wait for scale to zero and check that no VM, tap or socket is left. Read-only towards the
# services (does not apply or delete them). No sudo. Exit 0 = all passed.
set -u
INGRESS=http://10.106.195.52/
DOMAIN=default.192.168.1.240.sslip.io
fails=0
say()  { echo "$(date '+%T') $*"; }
pass() { say "PASS: $*"; }
fail() { say "FAIL: $*"; fails=$((fails+1)); }
pods() { kubectl get pods -l serving.knative.dev/service="$1" --no-headers 2>/dev/null | wc -l; }
wait_zero() { local t0=$SECONDS; while [ "$(pods "$1")" != 0 ] && [ $((SECONDS-t0)) -lt 300 ]; do sleep 3; done; [ "$(pods "$1")" = 0 ]; }

for svc in fcweb-fn fcpod-fn fcmicro-fn; do
  wait_zero "$svc" || { fail "$svc not at zero before the test"; continue; }
  body=$(curl -s -m 120 -w '\n%{http_code} %{time_total}' -H "Host: $svc.$DOMAIN" "$INGRESS")
  code=$(tail -1 <<<"$body")
  say "$svc cold request: HTTP $code s; body: $(head -c 60 <<<"$body" | tr '\n' ' ')"
  [ "${code%% *}" = 200 ] && pass "$svc answered" || fail "$svc: HTTP $code"
  wait_zero "$svc" && pass "$svc back at zero pods" || fail "$svc did not scale to zero in 300 s"
  sleep 3
  case $svc in
    fcweb-fn)
      # The shared VM is released in RemoveContainer, which kubelet sends when it garbage-collects the
      # pod's containers: observed 0-50 s after the pod disappears (existing behaviour, not new code).
      t0=$SECONDS
      while pgrep -f '^/usr/local/bin/firecracker --api-sock /run/vhive-fcweb/fc.sock' >/dev/null && [ $((SECONDS-t0)) -lt 180 ]; do sleep 2; done
      say "shared VM gone $((SECONDS-t0)) s after the pod (waits for kubelet's RemoveContainer)"
      left=""; pgrep -f '^/usr/local/bin/firecracker --api-sock /run/vhive-fcweb/fc.sock' >/dev/null && left="$left firecracker"
      ip link show fcweb0 >/dev/null 2>&1 && left="$left fcweb0"; [ -e /run/vhive-fcweb/fc.sock ] && left="$left fc.sock"
      [ -z "$left" ] && pass "shared VM torn down" || fail "shared VM leftovers:$left";;
    fcpod-fn)
      n=$(pgrep -fc '^/usr/local/bin/firecracker --api-sock /run/vhive-fcweb/[0-9]+/fc.sock'); t=$(ip -br link | grep -c '^fcweb[1-9]')
      [ "$n" = 0 ] && [ "$t" = 0 ] && pass "per-pod VM torn down" || fail "per-pod leftovers: $n firecracker, $t taps";;
    fcmicro-fn)
      # Stock vHive stops its microVM asynchronously, ~20-30 s after the pod is gone (and logs
      # "failed to stop ... forcefully terminated", also with the old binary).
      t0=$SECONDS
      while [ "$(pgrep -fc '^/usr/local/bin/firecracker --api-sock firecracker.sock')" != 0 ] && [ $((SECONDS-t0)) -lt 60 ]; do sleep 3; done
      n=$(pgrep -fc '^/usr/local/bin/firecracker --api-sock firecracker.sock')
      [ "$n" = 0 ] && pass "stock microVM stopped $((SECONDS-t0)) s after the pod" || fail "$n stock microVM(s) still running 60 s after the pod";;
  esac
done
[ "$fails" = 0 ] && say "ALL REGRESSION CHECKS PASSED" || say "$fails REGRESSION CHECK(S) FAILED"
exit $((fails > 0))
