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

## Phase 2 top-up — fibonacci-python, 2 more cycles per mode (03:05–03:30 MST), labelled, separate dir out/p2/fibpy-topup/
All 4 OK, same reply. stock 4.680, 4.755 s; mine 2.889, 3.551 s (VM stage 1.788/1.789 s). Combined with the main run's cycles 2–4 (n=5 per mode):
stock median 4.761 / mean 4.771 (4.680–4.835), mine median 2.804 / mean 2.928 (2.696–3.551), ratio 1.70× median / 1.63× mean.
(coldg.sh flags the top-up's cycle 1 as warm-up; I counted it, because the image was already cached and pods had been through many cold starts.)
End state 03:57 MST: 0 pods in default, no firecracker VMs, no load processes. The 8 vSwarm ksvcs (fibgo/fibpy/aesgo/authgo -stock/-mine) are left
deployed (scaled to zero); bb/httpbin/nginx ksvcs were deleted at your request; d19-* and snap-stock untouched. Final report: ~/fc-vm/REPORT-vswarm.md.

# Second run — aes-go then auth-go, stock vs mine (started Sun 2026-11-22 11:37 SGT = Sat 20:37 MST node time; hard stop 15:07 SGT = 00:07 MST)
Start state (20:37 MST): ~/vhive HEAD 4ba8903 (snapshot-spike, not switched, no source change), vhive-perpod md5 8fdc5b6a56e1f78fedae10fee1d36a09,
daemons not restarted (tmux containerd/firecracker/vhive from Nov 18). 0 pods in default, 0 firecracker VMs. aesgo/authgo -stock/-mine ksvcs still
deployed from the first run (rev 00001, Ready) — same yamls (`yaml/aesgo-*.yaml`, `yaml/authgo-*.yaml`), not recreated, template unchanged.
New harness pieces (old scripts untouched): `run_r2.sh` (runner per function), `pidwatch.py` (logs each firecracker process appear/exit, 20 ms poll,
runs only during the cold-start part), `fccdcap.sh` (read-only `tmux capture-pane -J` of the firecracker-containerd pane every 15 s — that daemon
logs only to its pane, 2000-line history; no keys sent), `mem.sh` (one pod Ready + 5 s, ps RSS), `sweep2.sh` (= sweep.sh + one `ps` snapshot right
after each measured 30 s window). Output: ~/fc-vm/vswarm/out/r2/<fn>/{cold,mem,load}/.
Command: `nohup env DEADLINE=<23:55 MST> bash ~/fc-vm/vswarm/run_r2.sh aesgo > out-r2-aesgo.log 2>&1 &` (20:39:22 MST).

## Second run — aes-go (20:39:22–21:48:29 MST), complete: 16 cold cycles, memory, 10 sweep cells, 2 vary runs; nothing failed, nothing re-run
Request aes.Aes/ShowEncryption {"plaintext_message":"world"}. All 16 cold-start replies were gRPC OK and byte-identical in both modes (122 B,
sha256/16 7bfd0cc3ed50de86: `fn: AES | plaintext: defaultplaintext | ciphertext: d80e9ec83a6acbef3d909876e79904a9 | runtime: golang`).

**(a) Cold start** (coldg.sh, 8 cycles per mode, stock then mine each round, cycle 1 = warm-up; idle waits 99–138 s):
| aesgo (cycles 2–8, n=7) | mean s | median s | min s | max s | stdev s |
|---|---|---|---|---|---|
| stock | 4.076 | 4.079 | 3.984 | 4.181 | 0.060 |
| mine | 1.742 | 1.752 | 1.409 | 1.964 | 0.167 |
| ratio stock/mine | 2.34× | 2.33× | | | |
All cycles (s): stock 4.459(w) 4.081 3.984 4.035 4.079 4.181 4.104 4.068; mine 1.701(w) 1.730 1.769 1.751 1.752 1.964 1.409 1.818.
Mine's VM stage (vhive `Firecracker VM booted ... launchToHTTP200`): 131.9–142.4 ms over all 8 cycles (= launch → TCP accept of the gRPC port).
Stock has no comparable per-stage line in the vhive Info log (not measured).

**(c) Memory** (RSS of the firecracker process, `ps -eo pid,ppid,rss,etimes,args`):
| | stock | mine |
|---|---|---|
| one pod Ready + 5 s, idle (mem.sh) | **184,040 KiB** — pid 626514, VM `--id 51-33c133b0-2945-4a` (21:09:38.950) | **61,784 KiB** — pid 630314, `/run/vhive-fcweb/1/fc.sock` (21:11:27.847) |
| end of the c=16 measured window, VM up 60 s | **189,696 KiB** — pid 670422, VM `56-785caeab-7148-4d` (21:36:13.898) | **66,888 KiB** — pid 676832, `/run/vhive-fcweb/1/fc.sock` (21:39:56.682) |
(also end of c=1: stock 189,180 KiB VM 52-1fa22dd3-eb4c-4a; mine 67,400 KiB pid 638706.) Both guests are configured with 512 MiB; RSS counts only
host pages the VM has touched. Identification: the stock firecracker's parent is **containerd-shim-aws-firecracker** (e.g. pid 626501, RSS 28,996 KiB,
not counted above), whose parent is firecracker-containerd (pid 13374) — i.e. a grandchild, not a direct child, of firecracker-containerd. Mine's parent
is vhive-perpod (pid 1251110). Why the RSS differs was not measured.

**(d) Teardown** (stop request → VM process exit, every cold-start cycle, `cold/teardown.md`):
* stock, from firecracker-containerd's pane log: `DEBU[...] stop VM: VMID:"<id>"` → next `WARN[...] firecracker exited: signal: killed`:
  **5.051, 5.066, 5.063, 5.062, 5.052, 5.053, 5.057, 5.056 s** (8/8 VMs, 43…50). My PID watcher saw the process gone 0.007–0.044 s after the exit line.
  The log shows the same sequence every time (example `cold/stock-stop-example-vm44.txt`): `gracefully shutdown VM` → 5.00 s later
  `failed to wait VM error="context deadline exceeded"` → `forcefully terminate VM` → `firecracker exited: signal: killed`; then 5 s more until
  `shim has been terminated` / `shim disconnected`; vhive logs `failed to stop firecracker-containerd VM ... forcefully terminated VM` at that point.
  So every stock VM was killed after the graceful-shutdown wait ran out; why the guest did not exit within it was not measured.
* mine: vhive logs only one line, at the end: `per-pod Firecracker VM stopped ... took=<d>`; `took` is vhive's own timer started just before
  SIGTERM and stopped after the process is gone AND the tap/socket are removed. There is **no log line for the process-exit instant**; I take it from
  my PID watcher (`pidwatch.py`, 20 ms poll, not a daemon log). took = **0.082, 0.101, 0.076, 0.071, 0.074, 0.098, 0.068, 0.080 s**; stop start
  (line − took) → process gone (pidwatch): **0.064, 0.051, 0.060, 0.063, 0.068, 0.043, 0.050, 0.034 s** (±0.02 s poll resolution).

**(b) Load** (ghz, 30 s warm-up incl. cold start + 30 s measured, each cell from zero pods; rps / p50 ms / p99 ms; fc CPU-s per 30 s):
| aesgo | c=1 | c=2 | c=4 | c=8 | c=16 |
|---|---|---|---|---|---|
| stock | 260.3 / 3.68 / 4.73; 11.9 | 462.5 / 4.15 / 5.64; 14.9 | 858.6 / 4.37 / 7.02; 23.6 | 2054.4 / 3.56 / 6.42; 32.2 | 2564.7 / 5.83 / 10.78; 33.5 |
| mine  | 263.8 / 3.61 / 4.66; 11.3 | 474.3 / 4.05 / 5.42; 14.3 | 874.6 / 4.26 / 7.21; 23.3 | 2103.1 / 3.48 / 6.36; 31.8 | 2591.0 / 5.78 / 10.67; 32.7 |
Mine is within 0…+3% of stock in every cell. Measured-window errors: stock 0, 2, 4, 7, 13; mine 1, 2, 3, 8, 12 — all `Unavailable … use of closed
network connection` except one `Canceled: the client connection is closing` (mine c=4), the same ghz end-of-run connection-close artifact as in the
first run. Client (ghz) CPU ≤ 48.6 s per 30 s; host busy ≤ 8.25 of 20 cores.
vary (open loop 1,5,20,5,1 rps; 960 sent, 960 OK, 0 errors, 0 late sends, max 1 pod / 1 VM in both): first request (cold) **3602 ms stock vs
2024 ms mine**; afterwards p50 4.6–5.4 ms, p99 ≤ 6.7 ms (max ≤ 10.5 ms) in every step for both. Mine's vary cold request (2.02 s) is slower than
any of its 8 coldg cycles (1.41–1.96 s); cause not investigated (vary uses one shared channel and measures from the scheduled send time).
Plots: `r2/aesgo/load/rps_vs_conc.png`, `r2/aesgo/load/vary.png`. Files: `bench/results-vswarm/r2/aesgo/` (fccd-pane.raw gzipped + deduplicated copy).
Push after aes-go (21:5x MST): **succeeded** (2d5678d..9f9a6f1 → origin/results-vswarm; this also pushed the first run's earlier local-only commits).

## Second run — auth-go (21:48:38–22:58:34 MST), complete: 16 cold cycles, memory, 10 sweep cells, 2 vary runs; nothing failed, nothing re-run
Request auth.Greeter/SayHello {"name":"allow"}. All 16 cold-start replies gRPC OK and byte-identical in both modes (341 B, sha256/16 65cddb0024a32feb,
`fn: Auth | token: allow | resp: {principalId:user policyDocument:{... Effect:Allow ...}} ... | runtime: golang`).

**(a) Cold start** (8 cycles per mode, cycle 1 = warm-up; idle waits 91–125 s):
| authgo (cycles 2–8, n=7) | mean s | median s | min s | max s | stdev s |
|---|---|---|---|---|---|
| stock | 4.060 | 4.073 | 3.974 | 4.126 | 0.059 |
| mine | 1.647 | 1.633 | 1.587 | 1.732 | 0.052 |
| ratio stock/mine | 2.46× | 2.49× | | | |
All cycles (s): stock 4.006(w) 4.021 4.086 4.073 4.126 4.126 3.974 4.013; mine 1.811(w) 1.732 1.680 1.621 1.587 1.598 1.633 1.679.
Mine's VM stage (launchToHTTP200): 131.5–139.8 ms over all 8 cycles.

**(c) Memory** (firecracker RSS):
| | stock | mine |
|---|---|---|
| one pod Ready + 5 s, idle | **183,036 KiB** — pid 759776, VM `66-7804ed0b-9ef6-4b` (22:19:47.995) | **60,300 KiB** — pid 763660, `/run/vhive-fcweb/1/fc.sock` (22:21:38.904) |
| end of the c=16 measured window | **188,604 KiB** — pid 805495, VM `71-578c9e60-af84-45` (22:47:14.312) | **65,644 KiB** — pid 810188, `/run/vhive-fcweb/1/fc.sock` (22:50:01.399) |
(end of c=1: stock 187,556 KiB VM 67-642facc1-17d9-4d; mine 65,824 KiB pid 773740.) Parents as for aes-go (stock: containerd-shim-aws-firecracker,
child of firecracker-containerd; mine: vhive-perpod).

**(d) Teardown** (same log lines and method as aes-go; `cold/teardown.md`):
* stock `stop VM` → `firecracker exited: signal: killed`: **5.073, 5.060, 5.064, 5.057, 5.062, 5.057, 5.063, 5.062 s** (8/8, VMs 58…65; same
  graceful-shutdown-timeout → forced-kill sequence every time). PID watcher: process gone 0.015–0.046 s after the exit line.
* mine took: **0.099, 0.085, 0.094, 0.087, 0.088, 0.091, 0.074, 0.092 s**; stop start → process gone (pidwatch): **0.082, 0.062, 0.073, 0.062, 0.064,
  0.072, 0.055, 0.050 s**.
* **Excluded, labelled in teardown.md**: one more mine teardown at 21:49:58 (took 0.073 s) was the **aes-go vary pod's VM** (pid 690626, already running
  when the watchers started at 21:48:38, ~10 s after aes-go's vary run ended); coldg.sh waited 91 s for it to go before auth-go cycle 1.

**(b) Load** (rps / p50 ms / p99 ms; fc CPU-s per 30 s):
| authgo | c=1 | c=2 | c=4 | c=8 | c=16 |
|---|---|---|---|---|---|
| stock | 256.0 / 3.72 / 4.77; 12.2 | 459.6 / 4.19 / 5.66; 15.5 | 856.5 / 4.34 / 7.33; 24.1 | 2004.7 / 3.66 / 6.48; 32.9 | 2534.8 / 5.89 / 10.97; 33.8 |
| mine  | 261.6 / 3.65 / 4.70; 11.6 | 440.7 / 4.41 / 5.88; 15.0 | 859.8 / 4.34 / 7.28; 23.7 | 2050.7 / 3.57 / 6.40; 32.1 | 2565.8 / 5.83 / 10.67; 33.3 |
Mine vs stock: −4.1% at c=2 (440.7 vs 459.6), +0.4…+2.3% elsewhere. Measured-window errors: stock 1, 2, 4, 8, 15; mine 1, 2, 4, 8, 16 — all
`Unavailable … use of closed network connection` (ghz end-of-run artifact; 116 over all auth-go warm-up + measured runs, no other error type).
Client CPU ≤ 48.8 s per 30 s; host busy ≤ 8.28 of 20 cores.
vary (960 sent, 960 OK, 0 errors, 0 late, max 1 pod / 1 VM both): first (cold) request **4007 ms stock vs 1431 ms mine**; afterwards p50 4.7–5.5 ms,
p99 ≤ 7.3 ms (max ≤ 9.5 ms). Plots: `r2/authgo/load/{rps_vs_conc,vary}.png`. Files: `bench/results-vswarm/r2/authgo/`.
End state: 0 pods in default, no firecracker VMs, no load processes; the 8 vSwarm ksvcs remain deployed (scaled to zero).
