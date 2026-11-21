# REPORT — vSwarm functions + HTTP images: stock vHive vs firecracker-vm://image (512 MiB)

Node-000 (hp109), one node, node time MST (UTC-7): 22:24 MST Fri = 13:24 SGT Sat; hard stop 04:24 MST = 19:24 SGT.
~/vhive HEAD 4ba8903 (branch snapshot-spike; not switched, no source change), vhive-perpod md5 8fdc5b6a56e1f78fedae10fee1d36a09, daemon not
restarted. Ingress ClusterIP 10.103.200.78 (read from kubectl each run). Results: branch `results-vswarm` (worktree ~/fc-vm/wt-results-vswarm,
dir `bench/results-vswarm/`), commits 68ba295 … (see the end). **No push succeeded**: the git credential cache had expired
(`fatal: could not read Username for 'https://github.com': terminal prompts disabled`, every phase); all commits are local only.

## Plain-language summary

**Which vSwarm functions work.** Four self-contained vSwarm functions work in both modes with byte-identical replies:
**fibonacci-go, fibonacci-python, aes-go, auth-go** (each run as one container, the function image itself, gRPC over h2c on port 50051,
without vSwarm's `relay` sidecar). Two are **blocked**: **compression-python** and **image-rotate** (go/python) need a MongoDB service
(`mongodb://compression-database:27017`, `mongodb://image-rotate-database:27017`), i.e. outbound network and DNS, which the image-VM path
does not have. They were not deployed; no networking was implemented. Not examined: nodejs variants, sleeping/spinning, the multi-service apps.

**What was measured** (everything that ran is reported; nothing was re-run or dropped):
* **Cold start** (one request from a fully idle node; cycle 1 = warm-up, not in the stats):

  | function | n (after warm-up) | stock median s | mine median s | ratio stock/mine (median / mean) |
  |---|---|---|---|---|
  | fibonacci-go | **6** | 3.944 | 1.683 | 2.34× / 2.32× |
  | fibonacci-python (main run) | **3** | 4.826 | 2.698 | 1.79× / 1.76× |
  | fibonacci-python main (n=3) + top-up (2) | **5** | 4.761 | 2.804 | 1.70× / 1.63× |
  | aes-go, auth-go | 0 — not run (time) | | | |

  fibonacci-python has fewer cycles than you asked for (n≥5): my `timeout 3000` around its run was too short for its 6.5-minute cycles
  (see "Things that went wrong"); a separately labelled 2-cycle top-up was run at the end.
* **Load, vSwarm (gRPC, ghz)**: for fibonacci-go and fibonacci-python **stock and mine are the same within a few percent at every concurrency**
  (fibonacci-go ~2,570 rps at c=16 in both; fibonacci-python levels off at ~1,650–1,700 rps from c=8 in both). Latency p50/p99 also match.
* **Load, HTTP images (load2.py)**: **busybox**: mine levels off at ~800–830 rps from c=4 while stock rises to ~1,450 rps; mine's p99 at c=16 is
  229 ms (stock 16 ms). **httpbin**: the same in both modes (~550 rps from c=4, firecracker ~1.1 cores in both); its c=16-mine cell was not run.
  **nginx: not run.** Phase 0 was stopped early **at your request** (not a failure).
* **Time-varying open-loop load** (1, 5, 20, 5, 1 rps, 30 s each, from zero pods; busybox, fibonacci-go, fibonacci-python): **0 errors**
  in all 6 runs (960 requests each), 1 pod and 1 VM throughout. The only difference is the very first request (the cold start):
  busybox 4.23 s stock vs 1.89 s mine, fibonacci-go 3.78 s vs 1.31 s, fibonacci-python 4.80 s vs 2.74 s. After that p50 is ~5 ms in every step for both.
* **The "errors" in the gRPC sweeps are a client artifact, not failures**: each measured cell has about c errors, all
  `Unavailable … use of closed network connection` (one per client connection; one `Canceled`), produced when ghz closes its own connections at the
  end of its `-z` duration. They are reported in the CSVs and tables as they are.
* **Is the client the bottleneck?** Measured client CPU: ghz ≤ 48.8 CPU-s per 30 s (~1.6 cores, fibonacci-go c=16), host ≤ 8.3 of 20 cores busy,
  so the node was not CPU-saturated. load2.py (one Python process) reached ~0.7 core (21 CPU-s / 30 s) in busybox-stock c=16, which may already
  limit that cell; not measured further. At the plateaus the firecracker process used ~1.1 cores in both modes (httpbin, fibonacci-go c=16) and
  ~1.25 cores (fibonacci-python c=16); whether the 1-vCPU guest is the limit, and why busybox-mine levels off at ~800 rps, was **not measured**.

**Plots** (in `bench/results-vswarm/`): `p0/busybox/rps_vs_conc.png`, `p0/busybox/vary.png`, `p0/httpbin/rps_vs_conc.png` (partial, no c=16 mine),
`p3/fibgo/rps_vs_conc.png`, `p3/fibgo/vary.png`, `p3/fibpy/rps_vs_conc.png`, `p3/fibpy/vary.png`.

### Things that went wrong or differ from the plan (all deliberate or recorded, none silent)
1. **Pushes failed** every time (expired credential cache). To push: `! cd ~/fc-vm/wt-results-vswarm && git push origin results-vswarm`.
2. **Phase 0 stopped at your request** at 23:43 (busybox complete; httpbin 9/10 cells, no vary; nginx not run). I killed only run_p0.sh and its
   sweep.sh (it was waiting for idle; no load running), deleted bb/httpbin/nginx -stock/-mine ksvcs; node idle at 23:44:19.
3. **fibonacci-python pods take 300 s to terminate (measured)**: the Python server does not exit on SIGTERM; with `timeoutSeconds: 300` Knative sets
   `terminationGracePeriodSeconds: 300`, and kubelet waited the full 300 s (pod `Terminating`, user-container still running 3.5 min after the delete).
   Each fibpy cycle/cell therefore needed 371–393 s of idle wait (fibonacci-go: 99–159 s). Template NOT changed.
4. **fibonacci-python Phase 2 has n=3, my mistake**: run_p2b.sh ran it under `timeout 3000`; it was killed at 01:01:33 before cycle 5.
   Top-up: 2 more cycles per mode at 03:05–03:30 in `p2/fibpy-topup/` (its cycle 1 is flagged warm-up by the script; see the table above
   for how it is counted).
5. **Phase 2 ran over its 1 h cap** (23:44–01:01) so fibpy could get its cycles; Phase 2 used 7 cycles for fibgo (not 11) to fit at all.
   To do that I killed the runner script run_p2.sh during fibgo (its coldg.sh kept running untouched) and continued with run_p2b.sh.
6. **aes-go and auth-go: no cold-start or load data.** Phase 3 started aes-go at 03:05:10; I stopped it at 03:05:30 (still waiting for idle, nothing
   measured) to give the time to the fibpy top-up, per your priority.
7. Phase 0 CPU columns (fc_cpu_s, host_busy_cores) exist from httpbin on; I added them (atomic replace of sweep.sh) after busybox had started.
8. Requests outside the measured runs: one grpcurl request per vSwarm service at deploy time (warm pods), harness tests in `~/fc-vm/vswarm/_test/`
   (fibgo gRPC sweep + vary at 22:30–22:39; one vary.py HTTP test on bb-mine at 22:40). None are in the results.

## Results in detail
Per-phase tables, per-cell numbers and notes: `PROGRESS-vswarm.md` (also in the results dir). Raw data: `p0/<wl>/sweep.csv`, `p0/<wl>/vary-<mode>/`,
`p2/<fn>/cold.csv` + `summary.md` + `bodies/`, `p3/<fn>/sweep.csv`, `p3/<fn>/c<c>-<mode>.{warmup,load}.json` (ghz), `p3/<fn>/vary-<mode>/`.

### Cold start, all cycles
fibonacci-go (s): stock 3.694(w) 3.950 3.926 4.052 4.100 3.938 3.900; mine 1.580(w) 1.708 1.855 1.634 1.600 1.657 1.816. VM stage (launchToHTTP200) 132.6–138.6 ms.
fibonacci-python (s): stock 4.717(w) 4.826 4.835 4.761; mine 3.478(w) 2.696 2.698 2.804. VM stage (launchToHTTP200) 1.76–1.80 s: the Python server takes ~1.8 s to accept TCP inside the VM.
Top-up (03:05–03:30, separate run, both cycles counted, all OK, same reply sha 6b353fb5e0c790b9): stock 4.680 4.755; mine 2.889 **3.551** (mine's slowest cycle of all; its VM stage was a normal 1.789 s, so the extra ~0.75 s was outside the VM boot — not investigated further).

### Load sweeps (rps / p50 ms / p99 ms)
| | c=1 | c=2 | c=4 | c=8 | c=16 |
|---|---|---|---|---|---|
| busybox stock | 233.5 / 4.33 / 5.07 | 524.4 / 3.80 / 4.98 | 1109.1 / 3.54 / 5.33 | 1436.0 / 5.45 / 8.97 | 1459.0 / 10.83 / 16.44 |
| busybox mine | 292.6 / 3.47 / 4.23 | 611.8 / 3.24 / 4.23 | 754.4 / 5.20 / 7.70 | 813.3 / 9.57 / 19.81 | 828.4 / 14.11 / 229.04 |
| httpbin stock | 168.0 / 6.04 / 7.13 | 477.4 / 4.14 / 5.28 | 548.2 / 7.25 / 9.85 | 546.6 / 14.53 / 17.77 | 541.5 / 29.46 / 33.73 |
| httpbin mine | 168.0 / 6.02 / 7.13 | 476.2 / 4.15 / 5.29 | 559.0 / 7.09 / 9.66 | 557.3 / 14.30 / 17.31 | not run (Phase 0 stopped) |
| fibonacci-go stock | 259.5 / 3.66 / 4.76 | 468.0 / 4.12 / 5.52 | 845.0 / 4.43 / 7.27 | 2052.1 / 3.57 / 6.44 | 2566.6 / 5.81 / 10.73 |
| fibonacci-go mine | 264.0 / 3.60 / 4.69 | 447.8 / 4.35 / 5.81 | 854.4 / 4.38 / 7.09 | 2087.9 / 3.51 / 6.36 | 2585.7 / 5.80 / 10.64 |
| fibonacci-python stock | 236.7 / 4.04 / 5.13 | 474.6 / 4.04 / 5.44 | 1169.8 / 3.14 / 5.09 | 1635.0 / 4.62 / 7.11 | 1643.0 / 9.47 / 12.93 |
| fibonacci-python mine | 239.3 / 4.00 / 5.10 | 468.8 / 4.07 / 5.37 | 1192.0 / 3.08 / 4.96 | 1694.6 / 4.46 / 6.87 | 1703.9 / 9.12 / 12.56 |
HTTP cells: 0 errors. gRPC cells: ≈ c "closed network connection" errors each (client artifact, above).

### Time-varying runs (960 requests each, 0 errors; p50 / p99 ms per step 1, 5, 20, 5, 1 rps)
| run | first (cold) request ms | 1 rps | 5 | 20 | 5 | 1 |
|---|---|---|---|---|---|---|
| busybox stock | 4234 | 4.93 / 4234 | 5.35 / 6.22 | 4.90 / 5.94 | 5.38 / 7.02 | 4.85 / 5.22 |
| busybox mine | 1888 | 5.22 / 1888 | 5.05 / 7.16 | 4.54 / 5.38 | 4.96 / 6.54 | 5.03 / 6.41 |
| fibonacci-go stock | 3781 | 5.25 / 3781 | 5.22 / 6.20 | 4.71 / 5.87 | 5.30 / 6.70 | 5.08 / 6.28 |
| fibonacci-go mine | 1311 | 5.11 / 1311 | 5.29 / 6.04 | 4.63 / 5.70 | 5.19 / 6.09 | 4.87 / 5.33 |
| fibonacci-python stock | 4797 | 5.66 / 4797 | 5.72 / 6.42 | 5.14 / 6.19 | 5.62 / 8.20 | 5.47 / 7.82 |
| fibonacci-python mine | 2740 | 5.57 / 2740 | 5.68 / 6.41 | 5.07 / 6.26 | 5.57 / 6.50 | 5.36 / 5.88 |
(The 1-rps p99 of the first step is the cold-start request itself.) Pods and VMs stayed at 1 in every run (max-scale 1).

## Method
* Services: same template as ~/fc-vm/bench/yaml (max-scale 1, containerConcurrency 0, timeoutSeconds 300, 1 vCPU; mine FC_MEM_MIB=512, stock uVM
  512 MiB); -stock/-mine differ only in GUEST_IMAGE (+FC_MEM_MIB). vSwarm: image docker.io/vhiveease/<fn>:latest, containerPort 50051 named h2c,
  GUEST_PORT 50051; yamls in `p1/yaml/`. rootfs: `~/vhive/bin/fc-rootfs build docker.io/vhiveease/<fn>:latest`.
* Requests: fibonacci {"name":"10"} (vSwarm relay default), aes {"plaintext_message":"world"}, auth {"name":"allow"}; protos from vSwarm-proto.
* Idle check before every cycle/cell, as cold.sh: 0 pods in default, 0 firecracker processes, 0 fcweb taps, 0 fc.sock; 3 stable polls + 5 s.
* Cold start: `coldg.sh` = cold.sh with only the request line changed (curl → `cold1.py`, one gRPC call on a new channel; time from channel creation
  to the reply; status OK instead of HTTP 200); stock then mine in each round; stats with `statsg.py` (stats.py with "OK" for "200").
* Sweep (`sweep.sh`): per c in 1,2,4,8,16, stock then mine, each from zero pods: 30 s warm-up (includes the cold start) + 30 s measured.
  HTTP: `~/fc-vm/bench/load2.py` keep-alive (one connection per worker). gRPC: ghz v0.120.0 `-c c --connections c -z 30s --timeout 60s`.
  CPU over the measured window: firecracker processes (utime+stime from /proc) and host busy cores (/proc/stat); client CPU (load2.py rusage / ghz `time`).
* Time-varying (`vary.py`): open loop (fixed schedule, 400-worker pool; latency from the scheduled send time), HTTP keep-alive per worker or one shared
  gRPC channel; per-second sent/ok/errors/p50 and pods/VMs (kubectl + pgrep each second).

## Exact commands
```bash
# setup
cd ~/vhive && git worktree add -b results-vswarm ~/fc-vm/wt-results-vswarm results-1118
git clone --depth 1 https://github.com/vhive-serverless/vSwarm.git        # in ~/fc-vm/vswarm-src (c91a381)
git clone --depth 1 https://github.com/vhive-serverless/vSwarm-proto.git
GOBIN=~/fc-vm/tools go install github.com/fullstorydev/grpcurl/cmd/grpcurl@v1.9.1 github.com/bojand/ghz/cmd/ghz@v0.120.0
sudo apt-get install -y python3-pip python3-venv; python3 -m venv ~/fc-vm/tools/venv
~/fc-vm/tools/venv/bin/pip install matplotlib grpcio protobuf grpcio-tools   # stubs: grpc_tools.protoc → ~/fc-vm/vswarm/pb
for i in fibonacci-go aes-go auth-go fibonacci-python; do ~/vhive/bin/fc-rootfs build docker.io/vhiveease/$i:latest; done
kubectl apply -f ~/fc-vm/vswarm/yaml/
# phase 1 request (per service)
grpcurl -plaintext -import-path <proto dir> -proto <fn>.proto -authority <ksvc host> -d '<json>' 10.103.200.78:80 <service/method>
# phase 0
nohup env DEADLINE=<now+88min> bash ~/fc-vm/vswarm/run_p0.sh > ~/fc-vm/vswarm/out-p0.log 2>&1 &
kubectl delete ksvc bb-stock bb-mine httpbin-stock httpbin-mine nginx-stock nginx-mine          # after your stop request
# phase 2
nohup env FNS="fibgo fibpy" CYCLES=7 DEADLINE=<now+60min> bash ~/fc-vm/vswarm/run_p2.sh > out-p2.log 2>&1 &   # then run_p2b.sh (fibpy CYCLES=6)
nohup env FN=fibpy WORKLOAD=fibpy SERVICES="fibpy-stock fibpy-mine" CYCLES=2 OUT=~/fc-vm/vswarm/out/p2/fibpy-topup timeout 2400 bash coldg.sh &
# phase 3
nohup bash ~/fc-vm/vswarm/chain_p3.sh > out-p3.log 2>&1 &     # run_p3.sh: KIND=grpc sweep.sh per fn, then plot.py
```
Scripts: `bench/results-vswarm/scripts/` (sweep.sh, vary.py, plot.py, coldg.sh, cold1.py, statsg.py, fn.sh, run_p0/p2/p3.sh) and `p2/run_p2b.sh`, `p2/chain_p3.sh`.
