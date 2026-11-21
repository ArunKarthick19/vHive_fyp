# PROGRESS — vSwarm + 3 images, stock vs mine (started Sat 2026-11-21 13:24 SGT = Fri 22:24 MST node time; hard stop 19:24 SGT)

Start state: ~/vhive HEAD 4ba8903 (branch snapshot-spike, not switched), vhive-perpod md5 8fdc5b6a56e1f78fedae10fee1d36a09, node Ready (v1.35.3),
no pods in `default`, only firecracker-containerd running (no firecracker VMs, no fcweb taps). Ingress ClusterIP 10.103.200.78.
Results worktree: ~/fc-vm/wt-results-vswarm (new branch `results-vswarm`, created from results-1118 at 60e2646), dir `bench/results-vswarm/`.

## Phase 1 — vSwarm feasibility (22:24–22:29 MST node time, ~6 min of the 1.5 h cap)
What ran: shallow clone of vSwarm (c91a381, 2025-03-04) and vSwarm-proto into ~/fc-vm/vswarm-src; installed grpcurl v1.9.1 and ghz v0.120.0
(`go install`, into ~/fc-vm/tools). vSwarm's Knative yamls run each function as TWO containers (a `relay` gRPC sidecar on 50000 + the function on 50051);
I did NOT use the relay: each function is ONE container (the function image itself, gRPC/h2c on 50051, port name `h2c`), in the same template as
bb-stock/bb-mine (max-scale 1, containerConcurrency 0, 512 MiB for mine); -stock and -mine differ only in GUEST_IMAGE (+FC_MEM_MIB=512).
rootfs built with `~/vhive/bin/fc-rootfs build` (2.6–12 s each). All 8 services became Ready; one grpcurl request per service (through the
ingress, `-authority <ksvc host>`), sent to the pods created at deploy time (so these were WARM requests, not cold starts).
Both modes really ran in VMs at the time: 8 firecracker processes (4 firecracker-containerd uVMs + 4 /run/vhive-fcweb/<n> image VMs).

| function | image | port/proto | needs other services? | request | result |
|---|---|---|---|---|---|
| fibonacci-go | docker.io/vhiveease/fibonacci-go:latest | 50051 gRPC | no | fibonacci.Greeter/SayHello {"name":"10"} | **WORKS-BOTH** (identical body "fn: Fib: y = fib(x) \| x: 10 y: 55 \| runtime: GoLang") |
| aes-go | docker.io/vhiveease/aes-go:latest | 50051 gRPC | no | aes.Aes/ShowEncryption {"plaintext_message":"world"} | **WORKS-BOTH** (identical body) |
| auth-go | docker.io/vhiveease/auth-go:latest | 50051 gRPC | no | auth.Greeter/SayHello {"name":"allow"} | **WORKS-BOTH** (identical body) |
| fibonacci-python | docker.io/vhiveease/fibonacci-python:latest | 50051 gRPC | no | fibonacci.Greeter/SayHello {"name":"10"} | **WORKS-BOTH** (identical body, "runtime: python") |
| compression-python | docker.io/vhiveease/compression-python | 50051 gRPC | **yes: MongoDB** (`--db_addr mongodb://compression-database:27017`) | not deployed | **BLOCKED** (needs outbound network + DNS; not attempted) |
| image-rotate-go/-python | docker.io/vhiveease/image-rotate-* | 50051 gRPC | **yes: MongoDB** (`mongodb://image-rotate-database:27017`) | not deployed | **BLOCKED** (same reason; not attempted) |

Tracing is off by default in these images (zipkin only if tracing is enabled), so they make no outbound calls. Not checked: the nodejs variants,
sleeping/spinning, and the larger apps (online-shop, hotel-app, video-*, gptj, bert …), which call other services or need big models.
Files: ~/fc-vm/vswarm/{yaml/,fn.sh,p1/,logs/rootfs-*.log} → results-vswarm `bench/results-vswarm/p1/`.

**Push failed** (22:29 MST): `fatal: could not read Username for 'https://github.com': terminal prompts disabled` — the git credential cache (cache --timeout=43200) has expired. Commit 68ba295 is local only. I will keep committing and try one push per phase; to push, re-enter the token once (e.g. `! cd ~/fc-vm/wt-results-vswarm && git push origin results-vswarm`).

## Phase 0 — load on the 3 HTTP images (22:41–23:43 MST), STOPPED EARLY AT YOUR REQUEST
What ran: `run_p0.sh` → `sweep.sh` (load2.py keep-alive, 30 s warm-up incl. the cold start + 30 s measured, every cell from zero pods, stock/mine
alternating) and `vary.py` (open-loop 1,5,20,5,1 rps × 30 s from zero pods). **busybox: complete** (10 sweep cells + both vary runs, 0 errors anywhere).
**httpbin: 9 of 10 cells** (httpbin-mine c=16 never ran; no vary runs). **nginx: not run.** httpbin's remaining cell, httpbin's vary runs and all of
nginx were **intentionally skipped at your request (CHANGE OF PLAN, 23:43 MST), not failed**: I killed only run_p0.sh (pid 3411275) and its sweep.sh
(pid 3476128) while it was waiting for idle (no load client was running), then deleted the six bb/httpbin/nginx ksvcs; the node was idle at 23:44:19.
CPU accounting (firecracker-process CPU s and host busy cores during the 30 s measured window) was added from httpbin on, so busybox has none.

| busybox (rps / p50 ms / p99 ms) | c=1 | c=2 | c=4 | c=8 | c=16 |
|---|---|---|---|---|---|
| stock | 233.5 / 4.33 / 5.07 | 524.4 / 3.80 / 4.98 | 1109.1 / 3.54 / 5.33 | 1436.0 / 5.45 / 8.97 | 1459.0 / 10.83 / 16.44 |
| mine  | 292.6 / 3.47 / 4.23 | 611.8 / 3.24 / 4.23 | 754.4 / 5.20 / 7.70 | 813.3 / 9.57 / 19.81 | 828.4 / 14.11 / **229.04** |

| httpbin (rps / p50 / p99; fc CPU s per 30 s) | c=1 | c=2 | c=4 | c=8 | c=16 |
|---|---|---|---|---|---|
| stock | 168.0 / 6.04 / 7.13; 19.7 | 477.4 / 4.14 / 5.28; 29.5 | 548.2 / 7.25 / 9.85; 33.6 | 546.6 / 14.53 / 17.77; 33.6 | 541.5 / 29.46 / 33.73; 33.6 |
| mine  | 168.0 / 6.02 / 7.13; 19.5 | 476.2 / 4.15 / 5.29; 29.1 | 559.0 / 7.09 / 9.66; 33.4 | 557.3 / 14.30 / 17.31; 33.4 | skipped (your request) |

Observations (measured, causes NOT measured): busybox-mine levels off at ~800 rps from c=4 while stock keeps rising to ~1450; mine's p99 at c=16 is 229 ms.
For httpbin both modes level off at ~550 rps from c=4 with the firecracker process at ~33.5 CPU-s per 30 s (~1.1 cores) in both. Client CPU (load2.py,
one Python process) was at most 21 s per 30 s (~0.7 core, busybox-stock c=16), so it was not pegged at one core, but a Python client near 0.7 core may
already limit the stock busybox numbers at c=8/16 — not measured further.
busybox vary (960 requests each, 0 errors): first request (the cold start) 4234 ms stock vs 1888 ms mine; after that p50 ~5 ms in every step for both;
1 pod / 1 VM throughout for both. Plots: out/p0/busybox/{rps_vs_conc,vary}.png, out/p0/httpbin/rps_vs_conc.png (partial).
Files: ~/fc-vm/vswarm/out/p0/ → results-vswarm `bench/results-vswarm/p0/`.

## Phase 2 — cold starts, fibonacci-go (23:44–00:11 MST)
Plan change (yours): Phase 0 stopped, straight to vSwarm. Because the idle wait per cycle was 100–160 s, 11 cycles per mode for fibgo AND fibpy
would not fit in 1 h; I ran **fibgo with 7 cycles (n=6 after warm-up)** and fibpy with 6 cycles (n=5), letting fibpy finish even if Phase 2 runs
~10 min past its cap. To do that I killed the runner script run_p2.sh (pid 3519757) during fibgo cycle 2 (its coldg.sh kept running untouched) and
continued with run_p2b.sh. All 14 fibgo cycles: gRPC OK, identical reply in both modes (sha256/16 8f3171cb13a2651b, "x: 10 y: 55"), no failures.

| fibgo cold start (cycles 2–7, n=6) | mean s | median s | min s | max s |
|---|---|---|---|---|
| stock | 3.978 | 3.944 | 3.900 | 4.100 |
| mine | 1.712 | 1.683 | 1.600 | 1.855 |
| ratio stock/mine | 2.32× | 2.34× | | |
Warm-up (cycle 1): stock 3.694 s, mine 1.580 s. Mine's VM stage (vhive log launchToHTTP200) was 132.6–138.6 ms in every cycle.
Files: out/p2/fibgo/{cold.csv,cold.log,summary.md,bodies/,cycles/}.

### Phase 2 note, fibonacci-python (00:43 MST): each cycle waits ~6.5 min for the node to go idle — measured cause
fibpy idle waits were 393 s and 371 s (fibgo: 99–159 s). At 00:42–00:43 the fibpy-stock pod was `Terminating` with queue-proxy not ready but
**user-container still running ~3.5 min after the deletion request**; the pod has `terminationGracePeriodSeconds: 300` (Knative copies it from
`timeoutSeconds: 300` in our template) and `deletionTimestamp` = request + 300 s. So the Python gRPC server does not exit on SIGTERM and kubelet
waits the full 300 s before killing it. This affects both modes (the pod and its containers are the same; mine's placeholder also runs the image).
I did NOT change the template (that would change the method versus every earlier run); fibpy continues to 6 cycles (n=5) per your priority, which
pushes Phase 2 to ~01:25 MST and makes each fibpy load cell in Phase 3 ~6 min longer. Consequence: aes-go/auth-go will probably get little or no Phase 3 time.

## Phase 2 — cold starts, fibonacci-python (00:11–01:01 MST): ONLY n=3 (below your n>=5 minimum) — my mistake
run_p2b.sh wrapped coldg.sh in `timeout 3000` (50 min), sized for ~110 s cycles; with the 6.5-min fibpy cycles (see the SIGTERM note above) the
timeout killed coldg.sh at 01:01:33 while it was waiting for idle before cycle 5 stock. So fibpy got cycles 1–4 (warm-up + n=3 per mode); every
cycle completed OK and both modes returned the same reply (sha256/16 6b353fb5e0c790b9). Cycles 5–6 were NOT run. If time remains after Phase 3 I will
run 2 more cycles per mode as a clearly labelled top-up in a separate directory (not merged silently).

| fibpy cold start (cycles 2–4, n=3) | mean s | median s | min s | max s |
|---|---|---|---|---|
| stock | 4.807 | 4.826 | 4.761 | 4.835 |
| mine | 2.733 | 2.698 | 2.696 | 2.804 |
| ratio stock/mine | 1.76× | 1.79× | | |
Warm-up (cycle 1): stock 4.717 s, mine 3.478 s. Mine's VM stage (launchToHTTP200) 1.76–1.80 s: the Python server takes ~1.8 s to accept TCP inside
the VM (fibgo: ~135 ms). aes-go and auth-go: no Phase 2 cycles were run (Phase 2 time used up).
Phase 3 started automatically at 01:01:35 (deadline 03:31:35 MST).

## Phase 3 — gRPC load, fibonacci-go (01:01–01:38 MST): complete (10 sweep cells + 2 vary runs)
ghz, c workers on c connections, 30 s warm-up (incl. cold start) + 30 s measured, every cell from zero pods.
| fibgo (rps / p50 ms / p99 ms; fc CPU s per 30 s) | c=1 | c=2 | c=4 | c=8 | c=16 |
|---|---|---|---|---|---|
| stock | 259.5 / 3.66 / 4.76; 11.9 | 468.0 / 4.12 / 5.52; 15.0 | 845.0 / 4.43 / 7.27; 23.5 | 2052.1 / 3.57 / 6.44; 32.1 | 2566.6 / 5.81 / 10.73; 33.3 |
| mine  | 264.0 / 3.60 / 4.69; 11.3 | 447.8 / 4.35 / 5.81; 14.4 | 854.4 / 4.38 / 7.09; 23.0 | 2087.9 / 3.51 / 6.36; 31.8 | 2585.7 / 5.80 / 10.64; 32.6 |
Stock and mine are within ~4% of each other in every cell. **Errors**: each measured cell has errors = c (c=4 mine: 3, c=8 mine: 7), every one
`Unavailable: error reading from server: read tcp <client>-><ingress>:80: use of closed network connection`, one per client connection: ghz closing its
own connections when the -z duration ends, not server failures (OK counts are 7786…77562). Client CPU (ghz): up to 48.8 s per 30 s (~1.6 cores) at c=16,
host busy 8.3 of 20 cores, so the node was not CPU-saturated; firecracker reached ~33 CPU-s per 30 s (~1.1 cores) at c=16 in both modes. Whether the
1-vCPU VM is the limit was not measured further.
vary (open loop 1,5,20,5,1 rps; 960 requests each, 0 errors, 1 pod / 1 VM throughout): first request (cold) 3781 ms stock vs 1311 ms mine; afterwards
p50 4.6–5.3 ms and p99 ≤ 8.9 ms in every step for both. Plots: out/p3/fibgo/{rps_vs_conc,vary}.png.

## Phase 3 — gRPC load, fibonacci-python (01:38–03:05 MST): complete (10 sweep cells + 2 vary runs); aes-go and auth-go NOT run
| fibpy (rps / p50 ms / p99 ms; fc CPU s per 30 s) | c=1 | c=2 | c=4 | c=8 | c=16 |
|---|---|---|---|---|---|
| stock | 236.7 / 4.04 / 5.13; 12.4 | 474.6 / 4.04 / 5.44; 23.2 | 1169.8 / 3.14 / 5.09; 30.8 | 1635.0 / 4.62 / 7.11; 36.7 | 1643.0 / 9.47 / 12.93; 37.3 |
| mine  | 239.3 / 4.00 / 5.10; 11.9 | 468.8 / 4.07 / 5.37; 22.7 | 1192.0 / 3.08 / 4.96; 30.3 | 1694.6 / 4.46 / 6.87; 36.7 | 1703.9 / 9.12 / 12.56; 37.6 |
Mine is within −1%…+4% of stock in every cell; both level off at ~1650–1700 rps from c=8. Errors again ≈ c per cell (Unavailable "use of closed network
connection", plus one `Canceled` in stock c=2), i.e. ghz closing its connections at the end of -z. Client (ghz) CPU ≤ 37 s per 30 s (~1.2 cores);
host busy ≤ 7.3 of 20 cores. Idle waits were 371–382 s per cell (the 300 s SIGTERM grace period, see Phase 2 note).
vary (960 requests each, 0 errors, 1 pod / 1 VM): first request (cold) 4797 ms stock vs 2740 ms mine; afterwards p50 5.1–5.7 ms, p99 ≤ 8.2 ms for both.
Plots: out/p3/fibpy/{rps_vs_conc,vary}.png.
**aes-go / auth-go**: Phase 3 started aes-go at 03:05:10; I stopped it at 03:05:30 while aesgo-stock c=1 was still waiting for idle (nothing measured),
to give the time to the fibpy Phase 2 top-up (your priority: fibgo and fibpy completely first). auth-go was never started. Neither has Phase 2 data either.
