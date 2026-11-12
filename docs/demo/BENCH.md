# BENCH: how to benchmark stock vs image VM (256 MiB and 512 MiB), same image for all

**What is compared.** All three run the identical image `ghcr.io/vhive-serverless/crccheck-hello-world:latest`
(busybox httpd on port 8000) with the same Knative settings. Only how the VM is made, and its memory, differ:

| mode (column in the report) | ksvc in phase B / phase D | GUEST_IMAGE | VM size | what runs |
|---|---|---|---|---|
| **stock** | `fcmicro-fn` / `n12-stock` | the image ref | 1 vCPU, **512 MiB** (hard-coded in vHive) | firecracker-containerd uVM + agent, the image mounted as a container |
| **image** | `real-vm` (`~/fc-vm/real/vm.yaml`) / `n12-image` | `firecracker-vm://image` | 1 vCPU, **256 MiB** (vhive's default) | your per-pod Firecracker VM booting a pre-built ext4 of the same image, fcinit as PID 1 |
| **image512** | `real-vm-512` (`~/fc-vm/real/vm-512.yaml`) / `n12-image512` | `firecracker-vm://image` | 1 vCPU, **512 MiB** (`FC_MEM_MIB=512`) | exactly the same as image, only with stock's memory size |

Why two image modes: **stock vs image512** is the fair, like-for-like comparison (same image AND same VM size).
**image vs image512** shows how much the VM size alone changes the numbers. Both image modes boot the same disk
(one ext4 per image, shared read-only), so the only difference between them is `FC_MEM_MIB`.
Check it yourself: `diff ~/fc-vm/real/vm.yaml ~/fc-vm/real/vm-512.yaml` shows the name, the comment and the
`FC_MEM_MIB` lines. After the run, `D_scaleout/yaml_differences.txt` shows the same for the phase-D services.

**Checked on 2026-11-11 18:44** (`~/fc-vm/real/check-512.last.log`): `VM_YAML=vm-512.yaml bash ~/fc-vm/real/check.sh` passed.
- `real-vm-512` answered with the same 439 bytes as stock (warm, and as a cold start from zero pods).
- Its VM really had 512 MiB and 1 vCPU, read from the running VM's Firecracker config `/run/vhive-fcweb/<slot>/vm-config.json`.
- It scaled to zero, and its slot was fully cleaned up.

## Why a copy of the harness (night-1112)
`~/fc-vm/night-1111/night.sh` has its three services (`fcmicro-fn`, `fcweb-fn`, `fcpod-fn`) written into the script, so it
cannot be pointed at other services with environment variables alone. night-1111 is **unchanged**. `~/fc-vm/night-1112/` is a
copy with the extra modes `image` and `image512` and a `MODES` setting that picks which modes run. Every change is listed at the top of
`~/fc-vm/night-1112/README.md`. Stock mode is untouched, so stock numbers from night-1111 and night-1112 come from the same code.

## Before you start (check each one, about 5 minutes)
1. **vhive runs the real-image-vm binary.** `ps -eo lstart,args | grep '[v]hive-perpod'` shows it running since
   Nov 11 09:06 (commit `7829a19`). Do not rebuild or restart vhive before the run. If you have to, follow
   `~/vhive/docs/STATUS.md` → "How to restart vhive".
2. **The rootfs is pre-built.** It is already in the cache. To confirm:
   ```bash
   ~/vhive/bin/fc-rootfs path ghcr.io/vhive-serverless/crccheck-hello-world:latest   # prints an .ext4 path = ready
   ```
   If it prints an error instead, run the same command with `build` instead of `path`. Phase A also runs `build` (it does
   nothing when the disk is already there), so a cold start can never include a build.
   **Do not run `make fcinit`** before the run: a new fcinit means a new disk for every image (see "Must not run" below).
3. **Optional quick check** (about 3 to 4 min each, a few requests only):
   `bash ~/fc-vm/real/check.sh` (image, 256 MiB) and `VM_YAML=vm-512.yaml bash ~/fc-vm/real/check.sh` (image512).
   Both must end with `ALL CHECKS PASSED`. check.sh deletes its services at the end, and phase A re-creates them.
4. **Quiet node:** `tmux ls` shows only `containerd`, `firecracker`, `vhive` (and your own shell), and
   `kubectl get pods` says `No resources found`.

## Launch the full run (all phases, stock + image + image512)
```bash
tmux new -d -s night 'MODES="stock image image512" bash ~/fc-vm/night-1112/night.sh'
tail -f ~/fc-vm/night-1112/night.log          # watch progress (Ctrl-C only stops tail, not the run)
```
This runs every phase in order:

| phase | what | expected time |
|---|---|---|
| A | preflight: checks, creates any missing service, makes sure the rootfs is built | about 1 min |
| B | cold starts: 11 cycles per mode (cycle 1 = warm-up, not counted), stock, then image, then image512 | about 60 min (3 × ~20 min) |
| C | stock VM stage step by step: 6 extra stock cold starts with log captures | about 11 min |
| D | scale-out: concurrency 1, 2, 4, 8, 16, 32 × 3 modes = 18 runs of 10 s warm-up + 30 s measurement, each starting from zero | about 45–60 min |
| E | report | seconds |
| **total** | | **about 2 h to 2 h 15 min; plan for 2.5 h** |

These times come from the smoke runs: one cold-start cycle takes about 95–110 s, most of it waiting for scale to zero, and one
phase-D run about 2.5–3 min. Every phase has a hard time limit; the whole run stops by itself after at most 6 h 50 min.
The first service will be busy about 1 min after launch.

Other ways to launch (same command, other settings):
```bash
# everything in night-1112: stock, shared, per-pod (baked rootfs), image, image512 (about 3.5 h)
tmux new -d -s night 'bash ~/fc-vm/night-1112/night.sh'
# cold starts + stock breakdown + report only, no scale-out (about 1 h 15 min)
tmux new -d -s night 'MODES="stock image image512" PHASES="A B C E" bash ~/fc-vm/night-1112/night.sh'
```
All other settings (`CYCLES`, `LEVELS`, `WARM_S`, `MEASURE_S`, `BUDGET_S`, ...) work as in night-1111.

## Must NOT run on the node during the benchmark
Each item would change the numbers, or make the script wait or fail:
1. **No other benchmark or sweep:** night-1111, `sweep.sh`, `cpu_vhive*.sh`, `load.py`, `hey`. night.sh refuses to start if
   `/tmp/sweep.lock` is held, but it cannot stop something you start *after* it.
2. **No requests to any function and no `kubectl apply` of services**, including `check.sh` and `regress.sh`. An extra pod or
   VM breaks the "wait until everything is at zero" step: night.sh counts *all* per-pod VMs (slots 1–16), not only its own.
3. **Do not restart, rebuild or stop vhive, containerd or firecracker-containerd**, and do not stop their tmux sessions.
4. **Do not clear or scroll-reset the `firecracker` and `containerd` tmux panes** (no `clear`, no `tmux clear-history`):
   phases B (stock) and C read their scrollback. Attaching to look is fine.
5. **Do not touch `/tmp/vhive-logs/vhive_log.txt`** (no truncating or rotating it): every cycle reads the lines added since its start.
6. **Do not run `make fcinit` or delete `~/fc-vm/images/`.** Either one causes a rootfs build inside the next image cold start.
   (Phase A only builds at the start of the run.)
7. **No heavy CPU or disk jobs** (`go build`, `go test -race`, big copies, `apt`). The node has 20 CPUs but the VMs are 1 vCPU each,
   and the timings are milliseconds, so background load shows up as noise.
8. **Do not delete the services** `fcmicro-fn`, `real-vm`, `real-vm-512` or `n12-*` while the run is going.

## Results
- `~/fc-vm/night-1112/E_analysis/REPORT.md`: tables with the columns stock | image | image512, a legend that states each mode's VM size,
  and the automatic caveats.
- `B_cold/<mode>/cold_cycles_fc.csv` and `cold_cycles_fc_phases.csv` (image, image512): the same format as per-pod.
  `phases.txt` is your `phases.py` output.
- `D_scaleout/scale.csv`, plus `n12-image.yaml`, `n12-image512.yaml` and `yaml_differences.txt`. The last one proves that stock and image differ only in
  `GUEST_IMAGE` (and the ignored `FC_MEM_MIB`), and that image and image512 differ only in `FC_MEM_MIB`.

## After the run, check that it was clean
```bash
grep '=== phase' ~/fc-vm/night-1112/night.log | tail -5                    # every phase: ok
grep -c 'built rootfs during cold start' /tmp/vhive-logs/vhive_log.txt     # write it down before the run; must not grow
grep 'probe=tcp' /tmp/vhive-logs/vhive_log.txt | tail -3                   # image boots; launchToHTTP200 = launch -> TCP accept
kubectl get pods                                                           # should be empty again
```
The run leaves `real-vm`, `real-vm-512` and the `n12-*` services in place, idle at zero pods. To remove the phase-D ones:
`kubectl delete ksvc n12-stock n12-image n12-image512`.

## What is (and is not) comparable. Put this in the FYP text.
- **Same:** image, Knative settings, port, vCPU count (1), node, ingress, measurement scripts. For image512 also the
  **memory (512 MiB)**.
- **Memory:** stock uVMs always get 512 MiB (hard-coded in vHive's ctriface; `FC_MEM_MIB` in the stock yaml is ignored). image gets
  256 MiB, image512 gets 512 MiB. Use stock vs image512 for the headline comparison, and image vs image512 to show the effect of memory size.
  One thing to expect: Firecracker gives the guest its memory lazily, so a bigger VM does not take longer to *start* by
  itself. A difference between image and image512 would most likely come from the guest kernel setting up more memory at boot.
- **VM-stage end point:** image/image512 stop the clock when the app *accepts a TCP connection* on :8000 (log `probe=tcp`); stock stops it
  when the guest prints `httpd started` (just before httpd listens). Both mean "the app is (about to be) listening".
- **Image preparation:** stock has the image layers in containerd/devmapper already; image has the ext4 pre-built. Neither
  pulls anything during a measured cold start, so build time is left out of both. Report the one-off build time separately:
  `fc-rootfs build` prints it (about 1 s for this image).
- **Page cache:** after the first cycle the ext4 is in the host page cache, so only cycle 1 (left out as warm-up) reads the disk.
  image and image512 share that disk, so image512's cycle 1 already finds it in the cache. That is one more reason cycle 1 is left out.
- **Order:** phase B runs the modes one after the other (stock, image, image512). Phase D interleaves them per concurrency level, so slow
  drift over the run affects all modes equally.
