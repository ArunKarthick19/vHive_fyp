# REPORT 1119: phase C (stock VM-stage breakdown) and phase D (scale-out) re-run, 2026-11-18 evening, node-000 (hp109)

Node time (MST, UTC-7). Same cluster as REPORT-1118 (created 08:45), same daemons (vhive-perpod PID 13414), ingress ClusterIP
**10.103.200.78** (read from kubectl by every script). Work ran 19:18–20:30; the 3-hour limit was not reached.
Branch `results-1118`, **pushed**: c92505a (phase C), ab34edb (phase D), plus the commit that adds this report.

## TL;DR
- **Phase C** (6 stock cold starts on `bb-stock`, cycle 1 = warm-up, 5 counted): all HTTP 200, identical 439-byte body. The stock VM stage
  (create-VM request → app up) is **2.660 s mean (2.589–2.701)**, of which **guest boot + agent = 2.182 s**. The end-to-end time is 4.206 s mean (3.568–4.443).
- **Stock VM memory is verified: 512 MiB, 1 vCPU.** It was read from the live Firecracker process's own API (`GET /machine-config`), and the
  firecracker-containerd create-VM log line shows the same value.
- **Phase D:** 16/16 cells ran (12 keep-alive + 4 new-connection-per-request). **0 errors in every cell**, both warm-up and measured.
  **The errors you saw before at c=8 and c=16 did not reproduce in either connection mode**, so tonight's data can neither confirm nor rule out
  connection churn as their cause (see "Churn comparison").

## What ran, in order
| step | time | result |
|---|---|---|
| phase C, `~/fc-vm/bench/phaseC.sh` | 19:19:54–19:29:21 | 6/6 cycles HTTP 200; stock_breakdown.py exit 0; memcheck done |
| commit + push phase C | 19:29 | c92505a, push OK |
| load2.py functional test (local throwaway server on 127.0.0.1, not the cluster) | ~19:30 | see "Deviations" 3 |
| phase D, `~/fc-vm/bench/phaseD.sh` | 19:30:00–20:16:54 | 16/16 cells, 0 errors, nothing skipped |
| commit + push phase D | 20:17 | ab34edb, push OK |

Nothing was re-run, and no cell or cycle was dropped.

## Exact commands
```bash
nohup bash ~/fc-vm/bench/phaseC.sh > ~/fc-vm/bench/phaseC.log 2>&1 &     # phase C (C_CYCLES=6, SVC=bb-stock)
nohup bash ~/fc-vm/bench/phaseD.sh > ~/fc-vm/bench/phaseD.log 2>&1 &     # phase D
# after each phase:
cp -r ~/fc-vm/bench/out/<phase>/. ~/fc-vm/results-1118/<phase>/  (and ~/vhive/bench/results-1118/<phase>/)
git add bench/results-1118/<phase> && git commit ... && GIT_TERMINAL_PROMPT=0 git push origin results-1118
```
- **phaseC.sh** is night-1111's `phase_C` method unchanged: wait for zero, `sleep 3`, one curl, `sleep 4`, then
  `tmux capture-pane -pJ -S -2000` of `firecracker:0.0` and `containerd:0.0`, the vhive log slice, pod json and a meta file (same file names). Then it runs
  `~/vhive/bench/night-1111/stock_breakdown.py` **unmodified** on the folder. The differences from night-1111 are: service `bb-stock` (was `fcmicro-fn`); the ingress IP is read from kubectl;
  the idle check is the stricter one from cold.sh (no pods in the namespace, no firecracker process, no fcweb tap/socket); the body sha is recorded; and the memcheck runs after the last cycle.
- **phaseD.sh** creates two new ksvcs, `d19-stock` and `d19-mine`, from `~/fc-vm/night-1112/scale-template.yaml` (`__MEM__=512` for both, `__PORT__=8000`;
  min-scale 0, max-scale 8, target concurrency 4, as in the template). `yaml_differences.txt` shows they differ only in the name and in `GUEST_IMAGE`
  (`ghcr.io/vhive-serverless/crccheck-hello-world:latest` vs `firecracker-vm://image`).
  The cell order is: for c in 1 2 4 8 16 32: stock keep-alive, mine keep-alive, and at c = 8 and 16 also stock `--new-conn`, then mine `--new-conn`.
  **Every cell starts from zero** (the same idle check, then 5 s more). Each cell runs a 30 s warm-up load2.py run (not measured), then a 30 s measured run.
  `ss -s` is saved before the warm-up, between the two runs and after the measured run. A pod sampler runs at 1 Hz.
  The client is `python3 -I load2.py 10.103.200.78 80 30 <c> / <ksvc host> [--new-conn]` on the node itself.
- **load2.py**: N threads, each with one persistent HTTP/1.1 keep-alive connection. A worker reconnects only after an error or when the server says it will close.
  The per-request timeout is 10 s. `--new-conn` sends `Connection: close` and opens a new connection for every request; nothing else changes.
  Every attempt is counted once, as ok (200) or as one of: `connect_timeout`, `read_timeout`, `refused`, `reset:ECONNRESET`, `reset:RemoteDisconnected`, `reset:EPIPE`,
  `http_<code>` or `other:<exception class>`. After a connection error the worker sleeps 10 ms (so that a refused port cannot become a busy loop); the failed request is not retried.
  p50/p99 use nearest rank over 200 responses only. Latency runs from just before sending (including connect when there is one) to the last body byte. rps = ok / elapsed.

## Phase C: stock VM-stage breakdown (bb-stock, crccheck-hello-world, 1 vCPU / 512 MiB)
Output of `stock_breakdown.py` (statistics over cycles 2–6; cycle 1 is the warm-up):

| step | n | mean s | min s | max s | what it covers |
|---|---|---|---|---|---|
| k8s: request -> sandbox ready | 5 | 0.651 | 0.636 | 0.665 | activator, autoscaler, scheduler, kubelet, pause sandbox |
| pre-VM: sandbox -> create VM request | 5 | 0.005 | 0.004 | 0.005 | kubelet CreateContainer -> vhive; vhive network setup + image lookup |
| VM1 VMM launch | 5 | 0.052 | 0.052 | 0.053 | shim start, firecracker process, machine/boot/drive/NIC config |
| VM2 guest boot + agent | 5 | 2.182 | 2.101 | 2.202 | kernel + systemd + firecracker-containerd agent |
| VM3 container create (vhive) | 5 | 0.008 | 0.006 | 0.009 | vhive NewContainer: container metadata, snapshot request |
| VM4 rootfs snapshot | 5 | 0.079 | 0.074 | 0.089 | devmapper thin snapshot of the cached image + hot-plug |
| VM5 task create | 5 | 0.305 | 0.274 | 0.322 | agent mounts rootfs, creates the container task in the guest |
| VM6 task start -> app up | 5 | 0.034 | 0.030 | 0.037 | task start, busybox httpd prints "httpd started" |
| **VM stage total (create VM -> app up)** | 5 | **2.660** | 2.589 | 2.701 | |
| post-VM: app up -> response | 5 | 0.889 | 0.209 | 1.183 | placeholder UC + queue-proxy start, readiness, first proxied request |
| end-to-end | 5 | 4.206 | 3.568 | 4.443 | client view (from the log timestamps) |

Per cycle (s; curl `time_total` from the meta files; "qp−app" = queue-proxy StartContainer returned, minus app up):

| cycle | curl total | k8s | VM stage | post-VM | qp−app |
|---|---|---|---|---|---|
| 1 (warm-up) | 4.294 | 0.709 | 2.615 | 0.976 | 0.189 |
| 2 | 3.556 | 0.653 | 2.701 | **0.209** | 0.170 |
| 3 | 4.428 | 0.654 | 2.684 | 1.098 | 0.177 |
| 4 | 4.379 | 0.636 | 2.648 | 1.102 | 0.177 |
| 5 | 4.431 | 0.665 | 2.589 | 1.183 | 0.190 |
| 6 | 4.175 | 0.649 | 2.678 | 0.855 | 0.173 |

**Observation (measured, not a cause):** the ~1 s bimodality in stock end-to-end time seen in REPORT-1118 also shows up here (cycle 2 = 3.56 s, the others 4.2–4.4 s).
In these 6 cycles, **all of the spread is in "post-VM: app up → response"** (0.21 s vs 0.86–1.18 s). k8s and the VM stage are flat (±0.06 s), and queue-proxy's
StartContainer returns 0.17–0.19 s after app up in every cycle. So the variable part lies between queue-proxy's StartContainer returning and the
response arriving. The logs captured here do not show what happens in that interval (readiness probing and activator forwarding are candidates, but I did not measure them).

### Stock VM memory: how it was checked (`C_stock_breakdown/memcheck.txt`)
Done right after cycle 6's capture, while its pod and VM were running (19:29:20):
1. `pgrep -f '^/usr/local/bin/firecracker --api-sock firecracker.sock'` → exactly one process, PID 690325, owned by root, started 19:29:12
   (`--id 45-a0b23ab6-f446-46`). This was unprivileged.
2. Its API socket is relative (`firecracker.sock`) inside the root-only shim directory, so it was reached through the process's cwd. This was the **one sudo command**:
   `sudo -n curl -s --unix-socket /proc/690325/cwd/firecracker.sock http://localhost/machine-config` (GET, read-only) returned
   **`{"vcpu_count":1,"mem_size_mib":512,"smt":false,"track_dirty_pages":false,"huge_pages":"None"}`**.
3. A cross-check in the firecracker-containerd debug log captured for the same cycle, for the same VM ID:
   `create VM request: VMID:"45-a0b23ab6-f446-46" MachineCfg:{MemSizeMib:512 VcpuCount:1}`.
   (The script's own grep for this line used the wrong brackets, `<…>` instead of `{…}`, and found nothing. I re-ran the corrected grep on the saved capture
   afterwards and appended the result, marked as added later.)
→ **Stock uVM = 512 MiB, 1 vCPU**, the same as mine (`FC_MEM_MIB=512`). This closes the "unverified" caveat in REPORT-1118 for this node and binary.

## Phase D: scale-out, busybox, stock vs mine (512 MiB), 30 s warm-up + 30 s measured, each cell from zero
### Keep-alive (load2.py default)
| c | stock rps | stock p50 / p99 ms | stock pods max | mine rps | mine p50 / p99 ms | mine pods max | mine/stock rps | errors (both) |
|---|---|---|---|---|---|---|---|---|
| 1 | 246.1 | 4.27 / 5.09 | 1 | 310.7 | 3.34 / 4.02 | 1 | 1.26 | 0 / 0 |
| 2 | 548.2 | 3.65 / 5.00 | 1 | 622.4 | 3.19 / 4.15 | 1 | 1.14 | 0 / 0 |
| 4 | 1314.3 | 3.04 / 4.53 | 2 | 1222.6 | 3.21 / 4.91 | 2 | 0.93 | 0 / 0 |
| 8 | 2687.6 | 2.86 / 5.06 | 3 | 1993.3 | 3.85 / 7.51 | 3 | 0.74 | 0 / 0 |
| 16 | 3300.0 | 4.61 / 9.56 | 6 | 2761.3 | 5.55 / 12.24 | 5 | 0.84 | 0 / 0 |
| 32 | 3259.1 | 9.65 / 19.17 | 8 | 3084.8 | 9.96 / 21.06 | 8 | 0.95 | 0 / 0 |

Warm-up runs (which include the scale-up from zero): **0 errors in all 16**. Warm-up rps is in `scale.csv`.
"Pods max" is the most pods seen during the measured 30 s (1 Hz sampler).

### Churn comparison: keep-alive vs a new connection per request, c = 8 and 16
| mode | c | connection | rps | p50 / p99 ms | errors (by type) | connections opened | TIME_WAIT before / mid / after | client CPU s (30 s run) |
|---|---|---|---|---|---|---|---|---|
| stock | 8 | keep-alive | 2687.6 | 2.86 / 5.06 | 0 | 8 | 439 / 383 / 367 | 23.3 |
| stock | 8 | new-conn | 2305.4 | 3.34 / 5.64 | 0 | 69,173 | 444 / 422 / 352 | 39.0 |
| mine | 8 | keep-alive | 1993.3 | 3.85 / 7.51 | 0 | 8 | 435 / 439 / 406 | 21.2 |
| mine | 8 | new-conn | 1934.4 | 3.95 / 7.65 | 0 | 58,043 | 438 / 413 / 360 | 33.3 |
| stock | 16 | keep-alive | 3300.0 | 4.61 / 9.56 | 0 | 16 | 428 / 386 / 344 | 34.6 |
| stock | 16 | new-conn | 2484.3 | 6.20 / 11.24 | 0 | 74,543 | 418 / 392 / 364 | 51.6 |
| mine | 16 | keep-alive | 2761.3 | 5.55 / 12.24 | 0 | 16 | 427 / 423 / 406 | 26.1 |
| mine | 16 | new-conn | 2467.2 | 6.26 / 11.67 | 0 | 74,030 | 424 / 414 / 344 | 49.4 |

What the data shows:
- **No errors of any type in any of the 8 runs (or their warm-ups)**, with or without per-request connections. So the earlier c=8/c=16 errors did **not reproduce**
  under either behaviour tonight. This experiment therefore does not show that churn causes them, and it does not show that it doesn't. They may depend on something
  that differed (the lost load.py's exact behaviour and error counting, the previous node/cluster, the 10 s warm-up then vs 30 s now, the 256 MiB mine VMs then); none of that was tested.
- New-conn lowered rps in all four pairs: stock −14% (c=8) and −25% (c=16); mine −3% (c=8) and −11% (c=16). It raised the client's CPU time by about 1.6–1.9×. These are single runs (n = 1 per cell).
- **Host TIME_WAIT did not grow** (it stayed between 344 and 480 in every cell, even with ~74k connections in 30 s). The node had about 430 TIME_WAIT sockets at baseline.
  `ss -s` here sees only the **host network namespace**. The Istio ingress gateway (Envoy) runs in its own pod namespace, and TIME_WAIT sits on whichever side closes first.
  I did not measure which side closed first or count sockets inside the gateway pod, so I cannot say where those connections' TIME_WAIT states went.

## Deviations, slips and anomalies (all recorded)
1. **Rule slip: `python3 -m py_compile load2.py` at 19:20:27, during phase C.** That compiles bytecode, so it breaks the "no compiling while a benchmark runs" rule.
   It took milliseconds and fell inside cycle 2's idle wait. Cycle 1's request (sent 19:20:01.9, response 19:20:06.2) and capture had finished by 19:20:10, and cycle 2's request was sent at 19:21:49.5, so no request was in flight. The `__pycache__` was deleted.
   I left cycle 2 in the data, as the rules require. (Cycle 2 is also the fast 3.56 s cycle, and I see no plausible way a millisecond compile ~90 s earlier could cause that, but I did not test it.)
2. **memcheck grep pattern bug** (`<…>` vs `{…}`): fixed afterwards on the saved capture; see the memcheck section. The live API reading is unaffected.
3. **load2.py was tested before phase D** against a throwaway local Python HTTP server (127.0.0.1:18080/18081, 1–2 s runs, under 20 s in total, about 19:30, outside
   both phases). This checked keep-alive reuse (4 connections for 197 requests), `--new-conn`, `http_404` counting and `refused` counting.
4. Phase C cycle 1 had an idle wait of only 4 s: the node had been idle since REPORT-1118's run. Its "warm-up" is therefore a cold start after ~8 h of idle.
5. Idle waits before the phase D cells were 103–166 s (mine new-conn c=16 was the longest at 166 s; mine keep-alive c=2 and c=4 were 137–142 s). This is not part of any measurement, and the blocking item was not logged.
6. `git push` worked with the cached credentials. The remote branch already had last night's commits (8efe478) before the first push tonight.
7. `d19-stock` and `d19-mine` are left deployed at zero pods (min-scale 0). To remove them: `kubectl delete ksvc d19-stock d19-mine`.

## Caveats
- **n = 1 per phase D cell** and **n = 5 for the phase C statistics.** There are no confidence intervals. Differences of ~10% between single cells may be noise.
- **The client is on the same node** (Python, threads). At c ≥ 16 it used ~1.2–1.7 CPU-seconds per second (26–52 s of CPU in a 30 s run), so the client competes with the
  VMs and Envoy for the node's 20 CPUs. **I did not measure whether the client was the bottleneck.** The flattening of rps at c=16→32 for both modes may be
  the client, Envoy, the autoscaler cap (max-scale 8) or the functions; it was not separated.
- **One node, one cluster, one evening.** The cell order was always stock before mine at each level.
- Scale-out uses the template's Knative settings (target concurrency 4, max-scale 8). Different settings would give different pod counts and rps.
- **The warm-up is 30 s** (night-1111 used 10 s), and load2.py differs from the lost load.py. So these phase D numbers are **not directly comparable** to night-1111's phase D.
- The phase C sub-steps come from daemon log timestamps (firecracker-containerd debug, containerd info, vhive). Steps without a log line (image lookup, vhive's internal StartVM steps) are lumped together, as stock_breakdown.py explains.
- The memory check reads one live stock VM (cycle 6). The value comes from vHive's create-VM request, so it is expected to be the same for every stock VM, but I checked only one.

## Where everything is
- `~/fc-vm/results-1118/C_stock_breakdown/`: c1–c6 raw captures (`*_firecracker_containerd.txt`, `*_containerd.txt`, `*_vhive.txt`, `*_pod.json`, `*_meta.txt`, `*_body`),
  `breakdown.csv`, `breakdown.md`, `stock_breakdown.stdout`, `memcheck.txt`, `phaseC.sh`, `phaseC.log`.
- `~/fc-vm/results-1118/D_scaleout/`: `scale.csv`, the two yamls, `yaml_differences.txt`, per cell `<mode>/<conn>/c<N>/` (`load.csv`, `load.json`,
  `warmup.csv`, `warmup.json`, `pods.csv`, `ss_before/mid/after.txt`, stderr files, all empty), `phaseD.sh`, `phaseD.log`, `load2.py`, `ksvc_end.txt`.
- The same files are in `~/vhive/bench/results-1118/` (branch `results-1118`, pushed) and in `~/results-1118.tgz`.
