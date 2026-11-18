# REPORT 1118: cold starts, stock vHive vs firecracker-vm://image (512 MiB), 2026-11-18, node-000 (hp109)

Times are node time (MST, UTC-7). Branch `real-image-vm` at c88236a. Results branch `results-1118` (local only, **not pushed**).

## TL;DR
All steps ran. The smoke test passed on the first try. All 66 benchmark cycles (3 workloads × 2 modes × 11) returned HTTP 200, and
each stock/mine pair returned byte-identical bodies. No cycle failed, was skipped or was re-run. Statistics below cover cycles 2–11 (n = 10 per mode).
End-to-end time (curl `time_total`, one request from a fully idle node) by median: stock / mine = **2.68× (busybox)**, **3.81× (nginx)**, **1.96× (httpbin)**.

| workload | stock median s | mine median s | ratio stock/mine (median) | ratio (mean) |
|---|---|---|---|---|
| busybox (crccheck-hello-world) | 3.565 | 1.328 | 2.68× | 2.70× |
| nginx (nginx-unprivileged:1.27-alpine) | 4.456 | 1.169 | 3.81× | 3.46× |
| httpbin (kennethreitz/httpbin) | 4.379 | 2.230 | 1.96× | 1.90× |

## What ran, and what did not
| step | status | when | evidence |
|---|---|---|---|
| 1. cluster | **done** | 08:45:07–08:47:20 | `~/fc-vm/bench/setup_cluster.log`; node Ready (v1.35.3), all istio/knative pods Running |
| 2. smoke test | **passed, 1st attempt** (no retry needed) | 08:54:24–08:56:58 | `~/fc-vm/bench/check-1118.run1.log` (ALL CHECKS PASSED) |
| 3. cold.sh | written | — | `~/fc-vm/bench/cold.sh` |
| 4. six services | deployed, all Ready | 08:57 | `~/fc-vm/bench/yaml/*.yaml` |
| 5. benchmark | **done, all 3 workloads** | 09:02:00–11:04:49 | `~/fc-vm/results-1118/`, commits below, `~/results-1118.tgz` |
| 6. quiet node | kept (see deviations) | — | — |

Not done / not in scope: no push (no token); no scale-out; no per-stage breakdown of stock; night.sh was not used.

### Deviations and things you should know about (all deliberate, none silent)
1. **`setup_tool` did not exist** in `~/vhive`, so I built it the standard vHive way: `cd ~/vhive/scripts && go build -o ../setup_tool .`
   This was a binary build with no source edit. `go.work.sum` had the same md5 before and after. Running it left untracked files in `~/vhive`:
   `setup_tool` (gitignored), `configs/calico/`, `configs/knative_yamls/`, `create_one_node_cluster_common.log`, `create_one_node_cluster_error.log`.
   I left them in place, since deleting outside the results dirs is not allowed. The `ERROR:` lines in the error log are setup_tool echoing each command's stderr, which was empty. Nothing failed.
2. **The session ended while the cluster was being created.** Only my wait loop was killed. setup_tool itself ran under nohup and had
   already finished at 08:47:20 (the last step, "Cleaning up temporary directory", was Success). On your instruction I checked ps, nodes and pods.
   The cluster was complete, so I did no clean-up and no rerun.
3. **`check.sh` hard-codes the old ingress IP** (`10.106.195.52`). The new cluster's istio-ingressgateway ClusterIP is **`10.103.200.78`**.
   On your instruction I made a copy, `~/fc-vm/bench/check-1118.sh`, that differs only in the `INGRESS` and `HERE` lines. The original is untouched
   (md5 check: `~/fc-vm/bench/check.sh.orig.md5` → OK). The domain `192.168.1.240.sslip.io` was unchanged.
   ```
   13,14c13,14
   < HERE=$(cd "$(dirname "$0")" && pwd)
   < INGRESS=http://10.106.195.52/
   > HERE=$HOME/fc-vm/real
   > INGRESS=http://10.103.200.78/
   ```
4. **cold.sh reads the ClusterIP from kubectl at startup** (`kubectl -n istio-system get svc istio-ingressgateway -o jsonpath='{.spec.clusterIP}'`).
   **ClusterIP used for all three workloads: `10.103.200.78`** (logged in each `cold.log`).
5. **Extra requests outside the benchmark, before it started:** check-1118.sh (its own handful of requests); one warm request to each of the six services
   at 08:58, to check that each pair gives identical bodies (they did); and a **1-cycle harness test** of cold.sh at 08:58–09:01 (bb-stock 4.15 s, bb-mine 1.82 s, both HTTP 200),
   kept in `~/fc-vm/bench/out/_harness_test/`. **None of these is in the results.** Nothing else ran during 09:02–11:04 apart from read-only checks
   (`tail`, and `kubectl get pods`, `pgrep` and `ls` once at 10:02, when I wrongly thought the run had stalled. It had not).
6. In the two `*-mine.yaml` files, sed also changed the **comment** line `# ~/vhive/bin/fc-rootfs build <image>` to the workload's image. It is a comment only.
   The other comments still mention vm.yaml and crccheck, because I copied the templates as they are.
7. The vhive daemon, containerd and firecracker-containerd were not touched (vhive-perpod PID 13414, running since 08:38). The tmux sessions are still
   `containerd`, `firecracker` and `vhive`. A fourth session, `claude`, is the Claude Code session itself (created 08:50), not a benchmark session.

## Exact commands
```bash
# 1. cluster
cd ~/vhive/scripts && go build -o ../setup_tool .
cd ~/vhive && nohup ./setup_tool create_one_node_cluster firecracker > ~/fc-vm/bench/setup_cluster.log 2>&1 &
# 2. smoke
cd ~/fc-vm/bench && VM_YAML=vm-512.yaml nohup bash check-1118.sh > check-1118.run1.log 2>&1
# 4. services (generated from the templates with sed; see "Manifests")
for f in ~/fc-vm/bench/yaml/*.yaml; do kubectl apply -f $f; done
# 5. benchmark (one driver, nohup, no tmux)
cd ~/vhive && git checkout -b results-1118
cd ~/fc-vm/bench && nohup bash run_all.sh > run_all.log 2>&1 &
```
`run_all.sh` runs, in order:
`WORKLOAD=<w> SERVICES="<p>-stock <p>-mine" CYCLES=11 OUT=~/fc-vm/bench/out/<w> timeout <left> bash cold.sh` for busybox/bb, nginx/nginx, httpbin/httpbin.
After each workload it runs `stats.py`, copies the results to `~/fc-vm/results-1118/<w>/` and `~/vhive/bench/results-1118/<w>/`, commits them locally, and rebuilds `~/results-1118.tgz`.
It had a hard deadline of 14:40 (about 6 h after the start). It was not needed: the run ended at 11:04.

### cold.sh, per cycle (all in `~/fc-vm/bench/cold.sh`)
1. **Wait for idle:** 0 pods in `default` (so neither service of the pair has a pod), 0 `firecracker` processes (stock and mine), 0 `fcweb*` taps,
   0 `/run/vhive-fcweb/*/fc.sock`. This must hold on 3 polls in a row, 2 s apart, then 5 s settle, then a recheck (it starts over if anything reappeared).
   After 600 s without idle, the cycle would be written as `not_idle` with no request. **That never happened.**
2. **One request:** `curl -s -m 300 -H "Host: <ksvc URL host>" http://10.103.200.78/`. It records the epoch send time, node time (ISO), HTTP code, curl exit,
   `time_total`, body bytes and sha256.
3. It saves the vhive log lines written during the cycle and a pod snapshot, and copies the image VM's `launchToHTTP200` (= launch → TCP accept) into the CSV.
Rounds alternate: stock c1, mine c1, stock c2, mine c2, … Cycle 1 has `warmup=1`. It is kept in the CSV and left out of the statistics.

## Manifests (busybox pair shown; the other pairs differ only in name, image, GUEST_PORT, containerPort)
Changed from the templates: name; image (and stock's GUEST_IMAGE, which is the image ref); GUEST_PORT; containerPort (busybox keeps 8000; nginx 8080; httpbin 80).
`diff ~/fc-vm/real/stock.yaml bb-stock.yaml` shows only the name. `diff ~/fc-vm/real/vm-512.yaml bb-mine.yaml` shows only the name.

`bb-stock.yaml`
```yaml
# Stock vHive path: firecracker-containerd boots its generic microVM and runs the image as a container.
# Identical image and Knative settings to vm.yaml; only GUEST_IMAGE differs (stock vHive reads the image to run from it).
apiVersion: serving.knative.dev/v1
kind: Service
metadata:
  name: bb-stock
  namespace: default
spec:
  template:
    metadata:
      annotations:
        autoscaling.knative.dev/max-scale: "1"
    spec:
      containerConcurrency: 0
      containers:
      - env:
        - name: GUEST_PORT
          value: "8000"
        - name: GUEST_IMAGE
          value: ghcr.io/vhive-serverless/crccheck-hello-world:latest
        image: ghcr.io/vhive-serverless/crccheck-hello-world:latest
        name: user-container
        ports:
        - containerPort: 8000
          protocol: TCP
        readinessProbe:
          successThreshold: 1
          tcpSocket:
            port: 0
      enableServiceLinks: false
      timeoutSeconds: 300
```
`bb-mine.yaml`
```yaml
# Same as vm.yaml (your per-pod Firecracker VM running the SAME image as stock), but the VM gets 512 MiB of memory,
# like the stock firecracker-containerd uVM (512 MiB, hard-coded in vHive's ctriface). vm.yaml (real-vm) keeps
# vhive's default of 256 MiB, so the benchmark has image-256 and image-512 next to stock.
# The ONLY difference from vm.yaml: the name and the FC_MEM_MIB env (check with: diff vm.yaml vm-512.yaml).
# Pre-build first (shared with real-vm, same image = same disk):
#   ~/vhive/bin/fc-rootfs build ghcr.io/vhive-serverless/crccheck-hello-world:latest
apiVersion: serving.knative.dev/v1
kind: Service
metadata:
  name: bb-mine
  namespace: default
spec:
  template:
    metadata:
      annotations:
        autoscaling.knative.dev/max-scale: "1"
    spec:
      containerConcurrency: 0
      containers:
      - env:
        - name: GUEST_PORT
          value: "8000"
        - name: GUEST_IMAGE
          value: firecracker-vm://image
        - name: FC_MEM_MIB
          value: "512"
        image: ghcr.io/vhive-serverless/crccheck-hello-world:latest
        name: user-container
        ports:
        - containerPort: 8000
          protocol: TCP
        readinessProbe:
          successThreshold: 1
          tcpSocket:
            port: 0
      enableServiceLinks: false
      timeoutSeconds: 300
```

## Results: end-to-end seconds (curl time_total), cycles 2–11
### busybox (ghcr.io/vhive-serverless/crccheck-hello-world:latest, port 8000), 09:02–09:43
| mode | n | mean | median | min | max | stdev |
|---|---|---|---|---|---|---|
| stock (bb-stock) | 10 | 3.765 | 3.565 | 3.530 | 4.591 | 0.417 |
| mine (bb-mine, 512 MiB) | 10 | 1.394 | 1.328 | 1.258 | 1.899 | 0.191 |
| **ratio stock/mine** | | **2.70×** | **2.68×** | | | |

Warm-up c1: stock 3.924, mine 1.265. Body 439 B, sha256 d05318e5f8904725…, all 22 identical. Image VM launch→TCP accept: 121–134 ms.

### nginx (docker.io/nginxinc/nginx-unprivileged:1.27-alpine, port 8080), 09:43–10:23
| mode | n | mean | median | min | max | stdev |
|---|---|---|---|---|---|---|
| stock (nginx-stock) | 10 | 4.260 | 4.456 | 3.444 | 4.493 | 0.426 |
| mine (nginx-mine, 512 MiB) | 10 | 1.230 | 1.169 | 1.025 | 2.007 | 0.281 |
| **ratio stock/mine** | | **3.46×** | **3.81×** | | | |

Warm-up c1: stock 4.124, mine 1.225. Body 615 B, sha256 fb47468a2cd3953c…, all 22 identical. Image VM launch→TCP accept: 168–181 ms.

### httpbin (docker.io/kennethreitz/httpbin:latest, port 80), 10:23–11:04
| mode | n | mean | median | min | max | stdev |
|---|---|---|---|---|---|---|
| stock (httpbin-stock) | 10 | 4.342 | 4.379 | 4.167 | 4.488 | 0.109 |
| mine (httpbin-mine, 512 MiB) | 10 | 2.282 | 2.230 | 2.206 | 2.793 | 0.180 |
| **ratio stock/mine** | | **1.90×** | **1.96×** | | | |

Warm-up c1: stock 4.118, mine 2.232. Body 9593 B, sha256 7c7b091e007ce3f2…, all 22 identical. Image VM launch→TCP accept: 844–868 ms.

Per-cycle rows (including the warm-up) are in each `summary.md` and `cold.csv`.

## Errors
**None.** 66/66 HTTP 200 with curl exit 0. No `not_idle` cycles. No pod restarts in any snapshot. No `level=warn` or `level=error` lines in the vhive log
slices of any cycle. `built rootfs during cold start` count: 0 before, 0 after (no cold start included a disk build).

## Odd things, not explained (I did not measure their causes)
1. **Stock is bimodal, about 1 s apart.** bb-stock: 8 of 10 at 3.53–3.66 s, and c2 = 4.59 and c10 = 4.52. nginx-stock: 8 of 10 at 4.42–4.49 s, and c4 = 3.46 and c6 = 3.44.
   httpbin-stock: 4.17–4.49, no clear split. The step is close to 1 s, which *might* point to a 1 s poll or retry somewhere in the stock path, but I did not measure that.
   It affects the means (and therefore the mean vs median ratios, e.g. nginx 3.46× vs 3.81×).
2. **One slow outlier per workload on the mine side:** bb-mine c4 1.899 s, nginx-mine c9 2.007 s, httpbin-mine c10 2.793 s (about +0.6–0.8 s over that mode's median).
   In those cycles, vhive's `launchToHTTP200` was normal (124.6 ms, 172.7 ms, 864.5 ms), so the extra time was spent outside the span that the vhive log measures. Where exactly is not measured.
3. **Some idle waits were longer:** 146–159 s instead of about 101–110 s, on bb-mine c4, c6, c8 and httpbin-mine c5, c7. These waits include the previous stock pod/uVM going away.
   cold.sh only logs *what* blocked idle when it times out, so the blocking item was not recorded. The idle wait is not part of the measured time.
   bb-mine c4 (idle 153 s) is also the bb-mine outlier, but c6 and c8 were near normal (1.49, 1.42 s), so I see no clear link.
4. Leftover `/run/vhive-fcweb/{2,3}/vm-config.json` files (no socket, no process, no tap) from the pods created when the services were first deployed.
   They do not affect the idle check. Observation only.
5. At 08:57 (before the run), a stock `firecracker` process outlived its pod for a few seconds. This matches STATUS bug #2 (stock uVM gone 3–25 s after its pod), which is why cold.sh waits for no firecracker process.

## Arithmetic observation (not a cause)
For mine, end-to-end minus vhive's launch→TCP accept leaves about 1.2 s (busybox, 1.33 − 0.12), 1.0 s (nginx) and 1.4 s (httpbin) outside the VM boot.
I did not break stock down tonight, so I cannot say which stages make up stock's 3.5–4.5 s.

## Caveats (please carry these into the FYP text)
- **Small n:** 10 timed cycles per mode per workload. Medians and ratios are point estimates, and I computed no confidence intervals.
- **One node, one night, one cluster** (hp109, cluster created 08:45). There is no repetition on another node or day.
- **512 MiB both:** mine is verified at 512 MiB/1 vCPU from the live VM config (check-1118 for the busybox image, and the live httpbin-mine VM after the run).
  Stock's 512 MiB is **not re-verified tonight**: it rests on BENCH.md (hard-coded in vHive ctriface).
- **Same-node client:** curl ran on the node itself, to the ingress ClusterIP, so client and server share the CPU and no real network is in the path.
- **End point differs slightly:** the mine readiness probe is TCP accept (`probe=tcp`). For the end-to-end number, both sides are measured the same way: a full HTTP 200 response via Istio ingress.
- **Pre-warmed images:** stock has the image in containerd; mine has a pre-built ext4 (built before tonight; build time not included). Cycle 1 (warm-up) also warms the page cache.
- **Cold = from zero pods with no VM left**, scale-to-zero via Knative defaults. The order is stock→mine within each round, never reversed. Drift hits both, but a systematic "goes second" effect cannot be ruled out.
- Workloads ran one after another (busybox, nginx, httpbin), so comparisons *across* workloads also contain time-of-run differences.
- The harness is new (cold.sh). The old `cold_cycles_*.sh`/`phases.py` were lost, so these numbers are not directly comparable to earlier runs.

## Where everything is
- `~/fc-vm/results-1118/<workload>/`: `cold.csv`, `cold.log`, `summary.md`, the two yaml files, `ksvc.txt`, `bodies/`, `cycles/` (per-cycle pod snapshot + vhive log slice).
  Plus `cold.sh`, `stats.py`, `run_all.sh`, `check-1118.sh` and `check-1118.run1.log` at the top level.
- `~/results-1118.tgz` (a tar of `~/fc-vm/results-1118`, rebuilt after each workload and again with this report).
- Git, `~/vhive` branch `results-1118` (local, not pushed): 1650c7f busybox, 8079188 nginx, 0181195 httpbin, plus a final commit that adds this report.
  Only files under `bench/results-1118/` are committed. The uncommitted `go.work.sum` change from before tonight is left as it was.
- Raw driver log: `~/fc-vm/bench/run_all.log`. Cluster log: `~/fc-vm/bench/setup_cluster.log`.
- The six services are still deployed and idle (scaled to zero). To remove them: `kubectl delete -f ~/fc-vm/bench/yaml/`.
