# BENCH: how to benchmark the same-image pair (stock vs image VM)

**The pair.** Both run the identical image `ghcr.io/vhive-serverless/crccheck-hello-world:latest` with the same Knative
settings and port 8000. Only `GUEST_IMAGE` differs:

| mode | ksvc | GUEST_IMAGE | what runs |
|---|---|---|---|
| stock | `fcmicro-fn` (= `~/fc-vm/real/stock.yaml` apart from the name, check with `diff ~/fc-vm/fcmicro-fn.yaml ~/fc-vm/real/stock.yaml`) | the image ref | firecracker-containerd uVM + agent, image mounted as a container |
| image | `real-vm` (`~/fc-vm/real/vm.yaml`) | `firecracker-vm://image` | your per-pod Firecracker VM booting a pre-built ext4 of the same image, fcinit as PID 1 |

## Why a copy of the harness (night-1112)
`~/fc-vm/night-1111/night.sh` hard-codes its three services (`fcmicro-fn`, `fcweb-fn`, `fcpod-fn`) in phases A, B and D, so it
cannot be pointed at `real-vm` with environment variables alone. As you asked, night-1111 is **unchanged**. `~/fc-vm/night-1112/` is a
copy with a 4th mode `image` and a `MODES` knob. Every change is listed at the top of `~/fc-vm/night-1112/README.md`, and
`~/fc-vm/night-1112/changes_vs_night-1111.diff` is the full diff. The stock mode is untouched, so night-1111's stock numbers and
night-1112's stock numbers come from the same code.

## Prerequisites (check each one, about 5 minutes)
1. **vhive runs the real-image-vm binary.**
   `git -C ~/vhive log -1 --oneline` should be on branch `real-image-vm`, and `ls -l ~/vhive/vhive-perpod` should be newer than that commit.
   Last night's swap put commit `7829a19` on the node at 09:06.
   If you rebuild: `cd ~/vhive && go build -o vhive-perpod.new .`, then restart it as described in `~/vhive/docs/STATUS.md` → "How to restart vhive".
2. **fcinit and the CLI are built** (both were built last night):
   `make -C ~/vhive/cri/firecracker fcinit fc-rootfs` puts `~/fc-vm/images/fcinit` and `~/vhive/bin/fc-rootfs` in place.
   Rebuilding fcinit changes its hash, so every image needs a new pre-build (step 3).
3. **Pre-build the rootfs (the important one):**
   ```bash
   ~/vhive/bin/fc-rootfs build ghcr.io/vhive-serverless/crccheck-hello-world:latest
   ~/vhive/bin/fc-rootfs path  ghcr.io/vhive-serverless/crccheck-hello-world:latest   # prints the .ext4 = cache hit
   ```
   night-1112's phase A runs the same `build` (a no-op when cached), so this can never be forgotten. But do it yourself once to see it work.
4. **Functional check** (about 4 min, a few requests only): `bash ~/fc-vm/real/check.sh` must end with `ALL CHECKS PASSED`.
   It deletes `real-stock`/`real-vm` at the end. Phase A of night-1112 re-creates `real-vm`.
5. **Quiet node:** no other night/sweep script running (`tmux ls`; night.sh also refuses to start if `/tmp/sweep.lock` is held),
   and all services at zero pods (`kubectl get pods` → none).

## Run
```bash
# the same-image pair only (stock + image): roughly half of a full run
tmux new -d -s night 'MODES="stock image" bash ~/fc-vm/night-1112/night.sh'

# everything: stock, shared, per-pod (baked rootfs) and image, comparable with night-1111
tmux new -d -s night 'bash ~/fc-vm/night-1112/night.sh'

# only cold starts + stock breakdown + report (no scale-out phase D)
tmux new -d -s night 'MODES="stock image" PHASES="A B C E" bash ~/fc-vm/night-1112/night.sh'

tail -f ~/fc-vm/night-1112/night.log
```
All other knobs (`CYCLES`, `LEVELS`, `WARM_S`, `MEASURE_S`, `BUDGET_S`, ...) are the same as night-1111.
Time per mode, from night-1111's smoke run: phase B ≈ 11 cycles × ~100 s ≈ 20 min. Phase D ≈ 6 levels × (40 s load + up to
~100 s waiting for zero) ≈ 15 min. A full 4-mode run should take about 3 h; `MODES="stock image"` about 1.5 h.

## Results
- `~/fc-vm/night-1112/E_analysis/REPORT.md`, with columns stock | image (| shared | perpod).
- `B_cold/image/cold_cycles_fc.csv` and `cold_cycles_fc_phases.csv`: the same format as per-pod. `phases.txt` is your `phases.py` output.
- `D_scaleout/n12-image.yaml`, `yaml_differences.txt`: proof that stock and image differ only in `GUEST_IMAGE`.

## After the run, check that it was clean
```bash
grep -c 'built rootfs during cold start' /tmp/vhive-logs/vhive_log.txt   # must not grow during the run
grep 'probe=tcp' /tmp/vhive-logs/vhive_log.txt | tail -3                 # image boots; launchToHTTP200 = launch -> TCP accept
```

## What is (and is not) comparable. Put this in the FYP text.
- **Same:** image, Knative settings, port, vCPU count (1), node, ingress, measurement scripts.
- **Memory differs:** image VMs get 256 MiB (`FC_MEM_MIB` in the template / FCWEB_MEM_MIB), while stock uVMs get 512 MiB (hard-coded in
  vHive's ctriface). To remove that variable, add `FC_MEM_MIB=512` to the image ksvc. The value is honoured in image mode.
- **VM-stage end point:** image stops the clock when the app *accepts a TCP connection* on :8000 (log `probe=tcp`); stock stops it when
  the guest prints `httpd started` (just before httpd listens). Both are "app is (about to be) listening".
- **Image preparation:** stock has the image layers in containerd/devmapper already; image has the ext4 pre-built. Neither
  pulls anything during a measured cold start, so build time is excluded from both. Report the one-off build time separately:
  `fc-rootfs build` prints it (about 1 s for this image).
- **Page cache:** after the first cycle the ext4 is in the host page cache, so cycle 1 (excluded as warm-up) is the only one that reads the disk.
