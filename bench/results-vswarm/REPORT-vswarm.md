# REPORT — vSwarm functions + HTTP images, stock vHive vs firecracker-vm://image (512 MiB)

Node-000 (hp109), one node, node time MST (UTC-7); 13:24 SGT Sat = 22:24 MST Fri. ~/vhive HEAD 4ba8903 (branch snapshot-spike, not switched,
no source change), vhive-perpod md5 8fdc5b6a56e1f78fedae10fee1d36a09, daemon not restarted. Ingress ClusterIP 10.103.200.78 (read from kubectl).
Results branch `results-vswarm` (worktree ~/fc-vm/wt-results-vswarm, dir bench/results-vswarm/). **Pushes fail**: the git credential cache expired
(`fatal: could not read Username for 'https://github.com': terminal prompts disabled`); every phase is committed locally only.

**STATUS: IN PROGRESS** — this file is updated after each function.

## Plain-language summary
*(filled in at the end)*

### Which vSwarm functions work
Four self-contained vSwarm functions work in BOTH modes, with byte-identical replies: **fibonacci-go, aes-go, auth-go, fibonacci-python**.
Each runs as ONE container (the function image, gRPC over h2c on 50051), without vSwarm's `relay` sidecar.
**compression-python** and **image-rotate (go/python)** are **BLOCKED**: they need a MongoDB service (`mongodb://compression-database:27017`,
`mongodb://image-rotate-database:27017`), i.e. outbound network + DNS, which the image-VM path does not have. Not deployed, nothing implemented.
Not examined: nodejs variants, sleeping/spinning, and the multi-service apps (online-shop, hotel-app, video-*, …) and big-model ones (bert, gptj, …).

### Phase 0 (HTTP images): busybox complete, httpbin 9/10 cells, nginx not run — stopped early at your request
See PROGRESS-vswarm.md for the tables. busybox-mine levels off at ~800 rps (stock ~1450) and its p99 at c=16 is 229 ms (stock 16 ms);
httpbin is the same in both modes (~550 rps, firecracker ~1.1 cores in both). Causes not measured.

## Method
* Same service template as ~/fc-vm/bench/yaml (max-scale 1, containerConcurrency 0, 1 vCPU; mine FC_MEM_MIB=512, stock uVM is 512 MiB);
  -stock and -mine differ only in GUEST_IMAGE (+FC_MEM_MIB). vSwarm services additionally use port 50051 named `h2c`.
* Idle check before every cell/cycle exactly as cold.sh: 0 pods in default, 0 firecracker processes, 0 fcweb taps, 0 fc.sock, 3 stable polls + 5 s.
* Cold start: coldg.sh = cold.sh with only the request line changed (curl → cold1.py, one gRPC call on a new channel, time from channel creation to reply).
* Load sweep: closed loop, c workers, 30 s warm-up (includes the cold start) then 30 s measured. HTTP: load2.py keep-alive. gRPC: ghz v0.120.0 with
  c workers and c connections. CPU: firecracker processes' CPU seconds and host busy cores over the measured window (from httpbin on).
* Time-varying: vary.py, open loop (fixed send schedule, 400-worker pool), 1, 5, 20, 5, 1 rps × 30 s each, from zero pods; per-second pods and VMs.
