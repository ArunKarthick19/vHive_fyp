# night-1112: night-1111 plus the "image" mode (same image as stock, in your per-pod Firecracker VM)

## What changed vs night-1111 (read this first)

night-1112 is a **copy** of `~/fc-vm/night-1111` (that folder is unchanged). The only additions are:

| what | where | why |
|---|---|---|
| a 4th mode **image** = ksvc `real-vm` (`~/fc-vm/real/vm.yaml`) in phase B and `n12-image` in phase D | `night.sh` | the like-for-like comparison: same image (`crccheck-hello-world`), same port 8000 and Knative settings as stock `fcmicro-fn`; only `GUEST_IMAGE` differs (`firecracker-vm://image`) |
| `MODES` knob (default `"stock shared perpod image"`) | `night.sh`, `analyze.py` | `MODES="stock image"` runs only the same-image pair, in about half the time |
| phase A runs `~/vhive/bin/fc-rootfs build <image>` (a no-op if cached) and creates `real-vm` if missing | `night.sh` phase A | so no cold start ever includes a rootfs build |
| phase B: image uses the per-pod copy of `cold_cycles_fc.sh` with `fcpod-fn` → `real-vm` (`B_cold/image/changes_vs_original.diff`) | `night.sh` phase B | same slots, same vhive log lines, so the same script measures it |
| phase D service names `n11-*` → `n12-*` (new services; the old `n11-*` stay as they are) | `night.sh` phase D | so runs do not mix |
| report: image column, notes on the VM-stage end point (TCP accept, log field `probe=tcp`) | `analyze.py` | |

`diff -r ~/fc-vm/night-1111 ~/fc-vm/night-1112` shows every change (`changes_vs_night-1111.diff` holds a copy).
Everything below is the night-1111 README with paths and names updated. Where it says "three modes", read "the modes in `MODES`".

---

# (original title) night-1112: stock vs shared vs per-pod Firecracker, overnight run

One script, `night.sh`, runs five phases (A to E) one after another and is built to finish in under 7 hours
(expected: about 2 to 2.5 hours, based on the smoke test). It only reads from the daemons. It never uses `sudo`, never restarts vhive,
containerd or firecracker-containerd, never edits the vhive source and never deletes anything. If a results folder
from an earlier run is in the way, it is moved to `prev-<time>/` instead of being deleted.

## Launch

```bash
tmux new -d -s night 'bash ~/fc-vm/night-1112/night.sh'
tmux attach -t night            # watch; detach with Ctrl-b d
tail -f ~/fc-vm/night-1112/night.log
```

The final report is `~/fc-vm/night-1112/E_analysis/REPORT.md`.

## The three modes being compared

| name in results | Knative service | what serves the request |
|---|---|---|
| **stock** | `fcmicro-fn` (phase D: `n12-stock`) | vHive's original path: firecracker-containerd boots a generic microVM with an in-guest agent, mounts the container image `crccheck-hello-world` and starts it as a container (busybox httpd, port 8000). 1 vCPU / 512 MiB. |
| **shared** | `fcweb-fn` (phase D: `n12-shared`) | your fork: vhive starts **one** plain Firecracker VM (`fcweb0`) from the baked-in `rootfs.ext4` (python http.server, port 80). Every pod of the service talks to that one VM. 1 vCPU / 256 MiB. |
| **per-pod** | `fcpod-fn` (phase D: `n12-perpod`) | your fork, branch `per-pod-vm`: same baked-in VM, but **one VM per pod** (slots `fcweb1`..`fcweb16`). 1 vCPU / 256 MiB. |

A **cold start** is a request that arrives when the service has 0 pods: Knative has to create a pod, and vhive has
to start a VM before the request can be answered. **Scale to zero**: after about 60 to 90 s with no traffic Knative
deletes the pod and vhive stops the VM. Every cold-start cycle therefore takes about 100 s, most of it waiting for that (measured in the smoke test).

## How the script is built

- `set -u`. There is no `set -e`, because a failing command must not stop the night.
- Each phase runs as a separate process: `timeout <budget> bash night.sh __phase X <dir>`. If the phase hangs, `timeout`
  kills it **and everything it started** (curl, kubectl, load.py). The exit code is logged as `ok`, `FAILED rc=N` or
  `TIMEOUT`, and the next phase starts.
- Phase time limits: A 15 min, B 2 h 30, C 40 min, D 3 h 10, E 10 min. These add up to 6 h 45. There is also an overall
  limit of 6 h 50 (`BUDGET_S`): a phase never gets more time than is left, and is skipped if less than a minute remains.
- Every line in `night.log` starts with a date, time and phase, for example `2026-11-11 23:04:10 [B] ...`.
  Each phase also writes its full console output to `<phase dir>/phase.out`.
- Two locks: `night-1112/.night.lock` stops a second copy of night.sh, and `/tmp/sweep.lock` (the same lock your
  sweep scripts use) stops it running at the same time as `sweep.sh` / `cpu_vhive*.sh`.
- Settings you can change with environment variables: `CYCLES` (11), `C_CYCLES` (6), `LEVELS` ("1 2 4 8 16 32"),
  `WARM_S` (10), `MEASURE_S` (30), `PHASES` ("A B C D E"), `OUT_ROOT` (~/fc-vm/night-1112), `BUDGET_S` (24600).

Files in this folder:

| file | role |
|---|---|
| `night.sh` | the whole run (phases A to D, calls the two Python scripts) |
| `scale-template.yaml` | the single template the three phase-D services are made from |
| `stock_breakdown.py` | phase C: reads the captured logs and computes the stock sub-steps |
| `analyze.py` | phase E: builds `summary.csv` and `REPORT.md` |
| `smoke/` | output of the short test run made before handing over, not real results: A, B and C with 2 cycles each, D with only concurrency 1 (3 s warm-up, 5 s measurement), then E |

## Phase A: preflight (`A_preflight/`, about 1 to 5 min)

Checks the setup is ready and records the state before anything runs.

1. `kubectl get ksvc`: lists the Knative services (`ksvc_before.txt`).
2. **Is the ingress reachable?** It sends one `curl` to `http://10.106.195.52/` *without* a Host header. The gateway
   answers 404, which proves it is up without waking any function (`ingress.txt`).
3. **Stock baseline:** if `fcmicro-fn` does not exist, it creates it from `~/fc-vm/fcmicro-fn.yaml`. It then waits up
   to 5 min for `fcmicro-fn`, `fcweb-fn` and `fcpod-fn` to report Ready. Creating a service makes Knative start one pod,
   which then scales back to zero; phase B waits for that.
4. **Versions** (`versions.txt`): the branch, commit and `git status` of `~/vhive`, plus the size, date and sha256 of the
   `vhive-perpod` binary. Note that git cannot tell whether the binary was built from that exact commit; compare the
   binary's date with the commit date.
5. **The daemon's FCWEB_\* settings** (`fcweb_env.txt`, `daemons.txt`). They are read from the visible command line
   `sudo FCWEB_VCPUS=1 FCWEB_MEM_MIB=256 ./vhive-perpod`. The usual source, `/proc/<pid>/environ`, belongs to root and
   cannot be read without sudo.
6. Checks that the tmux panes `firecracker` and `containerd` and the vhive log file exist, because phases B and C read
   them (`log_sources.txt`).

## Phase B: cold starts (`B_cold/{stock,shared,perpod}/`, about 1 h)

For each mode, in the order stock, shared, per-pod: **11 cold-start cycles**. Cycle 1 is a warm-up and is left out
of the statistics in phase E (it is still in the CSVs). Before each mode, the script waits until all three
services are at zero, so every mode starts on a quiet node.

It reuses your scripts, but runs **copies** placed inside `B_cold/<mode>/`. Your scripts write their CSVs next to
themselves, so running the originals would overwrite your old results in `~/fc-vm`.

- **stock:** `cold_cycles_microvm_http.sh`, copied unchanged.
- **shared:** `cold_cycles_fc.sh`, copied unchanged.
- **per-pod:** `cold_cycles_fc.sh` with four automatic edits, recorded in `perpod/changes_vs_original.diff`:
  the service name `fcweb-fn` becomes `fcpod-fn`; the VM process check looks for `/run/vhive-fcweb/<n>/fc.sock`
  instead of the shared socket; the tap check looks for `fcweb1..16` instead of `fcweb0`; and the socket check
  looks for `/run/vhive-fcweb/<n>/fc.sock`. The vhive log lines it reads (`Firecracker VM reachable / booted / stopped`)
  already match the per-pod log lines. If the edits do not apply cleanly, per-pod is skipped and the problem is logged.

What one cycle does (this is what your scripts already do):
1. Wait until the service has no pod and no VM.
2. Note the current length of the vhive log, then send one `curl` through the ingress and time it.
3. Read the VM timings from the vhive log (shared/per-pod) or from the firecracker-containerd tmux pane (stock),
   and the pod timestamps from `kubectl`.
4. Wait for scale to zero, and measure how long the pod and then the VM took to disappear.

Afterwards your `phases.py` is run, unchanged, on shared and per-pod (`phases.txt`). It includes cycle 1, because the
original script does. Phase E recomputes the same split without cycle 1, and for stock as well.

Output per mode: `cold_cycles_*.csv` (one row per cycle), `cold_cycles_*_phases.csv` (timestamps), `cold_cycles_*.log`,
`stdout.txt`; for stock also `run/microvm_http/` (pane captures per cycle and 10 warm requests).

## Phase C: the stock VM stage, step by step (`C_stock_breakdown/`, about 10 min)

Goal: split stock's ~2.66 s "VM stage" into its parts. Where are the timestamps?

| source | on disk? | timestamps? | what is in it |
|---|---|---|---|
| vhive log `/tmp/vhive-logs/vhive_log.txt` | **yes** | yes | guest output (`httpd started`). vhive's own StartVM steps (`Creating a new container`, `...task`, `Starting the task`) are **debug level, and vhive runs at info level, so they are not there**. |
| firecracker-containerd | **no**, only the tmux pane `firecracker` (2000 lines of scrollback) | yes, debug level | VM creation, boot, agent, snapshot, drive hot-plug, task creation |
| containerd | **no**, only the tmux pane `containerd` (2000 lines) | yes, info level | the CRI calls: pod sandbox, CreateContainer, StartContainer |

Because two of the three sources exist only in terminal scrollback, phase C makes **6 dedicated stock cold starts**
(cycle 1 = warm-up). Four seconds after each response it saves both panes (`tmux capture-pane`, read only) and the new
part of the vhive log (`c<N>_*.txt`, `c<N>_pod.json`). `stock_breakdown.py` then finds these lines and subtracts
their timestamps:

| step | from line → to line | what happens |
|---|---|---|
| k8s | request → `RunPodSandbox ... returns sandbox id` | Knative activator/autoscaler, scheduler, kubelet, pause container |
| pre-VM | sandbox ready → `create VM request` | kubelet asks vhive for the user container; vhive sets up the VM network and looks up the image |
| VM1 VMM launch | `create VM request` → `startInstance successful` | shim starts, firecracker process configured (kernel, drives, NIC) and started |
| VM2 guest boot + agent | `startInstance successful` → `successfully started the VM` | guest kernel + systemd boot, firecracker-containerd agent answers |
| VM3 container create | `successfully started the VM` → `prepare snapshot` | vhive's NewContainer call |
| VM4 rootfs snapshot ("unpack") | `prepare snapshot` → `PatchGuestDrive successful` | devmapper thin snapshot of the cached image, hot-plugged into the VM as a disk |
| VM5 task create | `PatchGuestDrive` → `successfully created task` | agent mounts the disk and creates the container inside the VM |
| VM6 task start → app up | `successfully created task` → `httpd started` (vhive log) | task started, busybox httpd starts |
| VM stage total | `create VM request` → `httpd started` | the "~2.66 s" |
| post-VM | `httpd started` → response | placeholder container + queue-proxy start, readiness check, the first request is proxied |

**What the logs cannot tell you (stated in `breakdown.md`, not guessed):**
- **Image pull** has no timestamp: vhive pulls/looks up the image *before* `create VM request`, and that is logged only at debug
  level. The image is already cached on the node, so no download happens; whatever time the lookup takes is inside "pre-VM".
- "Unpack" in this setup is not a tar extraction: devmapper makes a copy-on-write snapshot of the already-unpacked image
  (VM4).
- If a line is missing in a cycle (for example, it scrolled out of the pane), that cell is left empty and the step is listed
  as missing.

Output: `breakdown.csv` (one row per cycle, all timestamps and durations), `breakdown.md` (table + explanation).

## Phase D: scale-out under load (`D_scaleout/`, about 45 to 60 min)

**Matched services from one template.** `scale-template.yaml` is filled in three times; only the name, `GUEST_IMAGE` and
the port change. Everything else is identical: max-scale 8, min-scale 0, target concurrency 4 per pod,
`containerConcurrency: 0`, the same TCP readiness probe, the same CPU/memory requests and limits, and
`FC_VCPUS=1 FC_MEM_MIB=256`. The services are `n12-stock`, `n12-shared` and `n12-perpod`. Your existing services are not
changed. `yaml_differences.txt` shows the `diff` of the three files and lists the differences that are not visible in YAML
(stock uVM size is hard-coded at 512 MiB; different guest program; one shared VM vs one VM per pod).

**Load generator.** `~/fc-vm/load.py`, the same one `cpu_vhive*.sh` uses, so `hey` is not needed. It is a *closed loop*:
C workers each send a request, wait for the answer, and immediately send the next (new TCP connection each time, 5 s timeout).
So the request rate is whatever the system can sustain at that concurrency.

For each concurrency **1, 2, 4, 8, 16, 32**, and for each mode in turn (stock, shared, per-pod, then the next concurrency;
interleaving means any slow drift during the night affects all modes equally):
1. Wait until every service involved is at **zero pods and zero VMs**, so every run starts cold in the same way.
2. Start a pod counter (`pods.csv`, once per second: total pods and Ready pods).
3. **10 s warm-up** at that concurrency (`warmup.csv`). This absorbs the cold start and lets Knative add pods. Not counted.
4. **30 s measurement** (`load.csv`): requests/s, p50 and p99 latency in ms, number of errors.
5. Record the highest pod count during the measurement and the count at the end.

Result: `scale.csv` with columns `mode, conc, rps, p50_ms, p99_ms, errors, max_pods, end_pods, warmup_errors, start_epoch`.
The three `n12-*` services are left in place (idle at zero pods). To remove them later:
`kubectl delete ksvc n12-stock n12-shared n12-perpod`.

## Phase E: analysis (`E_analysis/`, seconds)

`analyze.py` reads B, C and D and writes:
- `summary.csv`: one row per (section, mode, metric) with n, mean, min, max.
- `REPORT.md`: the tables:
  - **B** cold start per mode: before the VM stage / VM stage / after / total / curl end-to-end / time to scale to zero /
    VM stop time. For stock the VM stage is `create VM request` → `httpd started`. For shared/per-pod it is
    vhive's acquire start → `Firecracker VM reachable` (the same split `phases.py` makes).
  - **C** the stock sub-steps.
  - **D** req/s, p50, p99, errors and pods by concurrency and mode.
  - **Caveats**, generated automatically: the three fixed ones you asked for (one trivial workload; 256 vs 512 MiB;
    baked-in program vs real container image), plus the different VM-stage end points, small samples and the cached image. It
    also adds caveats found in the data: fewer cycles than planned, non-200 responses, more than one stock uVM per cold start,
    load errors, max-scale reached, and any phase that failed or timed out.

## Expected timeline

| phase | expected | limit |
|---|---|---|
| A | 1–5 min | 15 min |
| B | ~1 h (33 cycles × ~100 s, plus waits between modes) | 2 h 30 |
| C | ~10 min | 40 min |
| D | ~45–60 min (18 runs × ~2–2.5 min) | 3 h 10 |
| E | < 1 min | 10 min |
| **total** | **~2–2.5 h** | **6 h 50** |

## After the run, check

1. `grep '=== phase' night.log`: every phase should say `ok`.
2. `E_analysis/REPORT.md`, especially the caveats list.
3. `B_cold/*/stdout.txt` if a mode has fewer than 10 usable cycles.

## Smoke test (2026-11-11, 06:58–07:20)

`smoke/` holds a short run: A, B and C with 2 cycles each, D at concurrency 1 only, and E. All phases passed (A 9 s, B 800 s,
D 389 s). The first attempt at C failed because `stock_breakdown.py` hit a containerd line with a relative timestamp
(`WARN[0000]`). The parser now skips such lines and was re-run on the same captured logs; every step was found. Caveat 11 in
`smoke/E_analysis/REPORT.md` is that first failure, which shows the failure reporting works. Single-cycle values (n = 1, not results):

| | stock | shared | per-pod |
|---|---|---|---|
| cold end-to-end (curl) | 3.93 s | 1.57 s | 1.70 s |
| VM stage | 2.74 s | 0.36 s | 0.36 s |
| VM stop call | 10.0 s | 0.10 s | 0.08 s |

Stock VM stage (phase C, cycle 2): VMM launch 0.09 s, **guest boot + agent 2.20 s**, container create 0.01 s, rootfs snapshot
0.11 s, task create 0.33 s, task start → httpd 0.03 s.
