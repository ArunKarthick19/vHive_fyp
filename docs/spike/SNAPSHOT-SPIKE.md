# SNAPSHOT-SPIKE: restoring a Firecracker snapshot of an image VM vs booting it (2026-11-19, node-000 hp109)

Branch `snapshot-spike` (from drawbacks-1119 `8693bb2`), notes and scripts only; no daemon or repo code changed. Work ran 01:12–01:18 node time, outside Kubernetes,
in spare slot 30, with no benchmark running and without touching the daemon or slots 1–16.

## Summary in plain language
- **With the exact VM layout the daemon uses today, restore does not work.** The Firecracker build on this node (`/usr/local/bin/firecracker` v1.13.1, vHive's fork)
  **panics** when loading a snapshot of an image VM: `Firecracker panicked at src/vmm/src/persist.rs:328:5: assertion left == right failed, left: 3, right: 2`.
  An image VM has **3 disks** (image rootfs, config drive, status drive); the fork's restore only accepts **2** (the layout of stock vHive's microVMs). The fork's load API
  also *requires* a `container_snapshot_path` field that upstream Firecracker does not have. Creating the snapshot works; loading it crashes the Firecracker process.
- **With 2 disks (the same VM without the new status drive) restore works, and it is about 10× (busybox) to 85× (httpbin) faster than booting**, measured the same way
  (process start → the app accepts a TCP connection, 10 runs each):

| image | boot (median, min–max) | restore (median, min–max) |
|---|---|---|
| busybox (crccheck-hello-world) | **117.2 ms** (107.7–124.8) | **10.9 ms** (8.7–11.2) |
| httpbin | **838.0 ms** (815.3–847.5) | **9.9 ms** (7.7–10.3) |

- **"Accepts TCP" is not "serves fast"**, and the numbers above are with the snapshot's memory file in the host page cache. Time to the first full HTTP 200 (5 runs):
  - busybox: warm restore 15 ms, but **232 ms when the memory file must come from disk** (slower than a warm boot, 121 ms);
  - httpbin: warm restore 32 ms, cold-cache restore 590 ms, both still well below a warm boot (1.49 s; gunicorn accepts TCP at about 0.86 s but answers later).
- The restored VM **really serves**: same body as a booted VM (busybox 439 B, httpbin 9593 B, same sha256), and still serving 3 s later. Its **clock is stale**:
  it restarts at the snapshot's wall-clock time (the `Date` header lagged the host by the age of the snapshot, 2–6 s here). Network (same tap, IP and MAC) worked unchanged.
- The snapshot is **513 MB per VM** on disk (a 512 MiB memory file, not sparse, plus about 16 KB of VM state). Creating it took 1.7–2.0 s, and pausing the VM under 1 ms.
- **What it means for the project:** fast restores of image VMs need either a 2-disk layout (e.g. move the config and status data elsewhere: the kernel cmdline, MMDS or vsock)
  or a Firecracker build whose restore accepts 3 disks (e.g. upstream Firecracker, which this node does not have; **not tested**). Even then, a restored VM needs a clock fix and
  per-VM config (env, hostname) injected *after* restore. A snapshot of one VM carries that VM's config drive contents and its in-memory state.

## How the daemon launches an image VM (what was copied)
From `cri/firecracker/firecracker.go` (`fcwebConfigJSON`, `writeConfig`, `createFcwebTap`) and the daemon's leftover `/run/vhive-fcweb/1/vm-config.json`:
- `firecracker --api-sock <dir>/fc.sock --config-file <dir>/vm-config.json`, stdout/stderr → `<dir>/console.log`, own session.
- Kernel `/var/lib/firecracker-containerd/runtime/hello-vmlinux.bin`, boot args `console=ttyS0 quiet acpi=off random.trust_cpu=on reboot=k panic=1 pci=off nomodules ro
  i8042.noaux i8042.nomux i8042.nopnp i8042.dumbkbd init=/.fcinit/init fcweb.ip=<guest>/30 fcweb.gw=<host>`.
- Drives: `rootfs` = the image's ext4 from `~/fc-vm/images` (read-only, root); `config` = `guest-config.bin` (pod env JSON zero-padded to 4 KiB, read-only, `/dev/vdb`);
  `status` = `exit-status.bin` (4 KiB zeroed, **writable**, `/dev/vdc`, added on drawbacks-1119).
- NIC `eth0`, MAC `06:00:` + guest IP in hex, on tap `fcweb<n>` (host end `/30` address plus a static ARP entry for the guest). 1 vCPU / 512 MiB here (as bb-mine), logger Warning.
- Slot formula (`slots.go`): host `10.200.0.(4n+1)`, guest `10.200.0.(4n+2)`. **Slot 30** = tap `fcweb30`, host `10.200.0.121/30`, guest `10.200.0.122`,
  MAC `06:00:0a:c8:00:7a`, dir `/run/vhive-fcweb/30` (the daemon only uses slots 1–16). The spike created the tap the same way (`ip tuntap`, `ip addr`, `ip link up`, `ip neigh ... permanent`).
- Rootfs disks used: busybox `sha256-0404ca69…-0d8b2d6b512e.ext4` and httpbin `sha256-599fe5e5…-0d8b2d6b512e.ext4` (current fcinit, md5 `9881bdfa…`).

## Method
- Scripts (on the branch, `docs/spike/`): `snapshot_spike.py` (boots, snapshot, restores, verification), `probe_container_path.py`, `http_and_coldcache.py`. All were run as root
  (`sudo python3 -I …`), like the daemon, which runs Firecracker as root.
- **Timing (identical for both sides, same idea as vhive's `launchToHTTP200` with `probe=tcp`):** t0 just before the Firecracker process is spawned; t1 = first successful
  TCP connect to guest:port, with a 20 ms connect timeout, polled every 5 ms.
  - boot = spawn `firecracker --api-sock S --config-file C` → TCP accept.
  - restore = spawn `firecracker --api-sock S` → wait for the API socket → `PUT /snapshot/load` with `resume_vm: true` → TCP accept.
  The restore figure therefore includes process start (about 3–4 ms), the load call (3–5 ms) and resume → accept (2–3 ms).
- Snapshot: boot once, wait 1 s, `GET /` (200), `PATCH /vm {"state":"Paused"}`, `PUT /snapshot/create {"snapshot_type":"Full", "snapshot_path", "mem_file_path"}`, then kill.
  Every restore loads that same snapshot into a new Firecracker process, using the same tap, IP, run directory and drive paths.
- Snapshot and memory files were on the local ext3 disk (`~/fc-vm/spike/…`, `/dev/sda3`, the same disk as the daemon's `/fccd/snapshots`), not on tmpfs.

## Results
### 1–4, busybox
| step | result |
|---|---|
| boot, daemon's exact 3-drive config (10×) | median **118.8** ms, min 113.5, max 126.6 (`spike/busybox/`) |
| snapshot of the 3-drive VM | created OK: `snap_file` 16,289 B, `mem_file` 536,870,912 B (du 513M); create 1.69 s |
| restore of the 3-drive snapshot | **FAILED, 0/20 + 0/2.** Without `container_snapshot_path`: HTTP 400 `missing field container_snapshot_path`. With it (any value): **Firecracker panics** (`persist.rs:328`, `left: 3, right: 2`) and drops the API connection |
| boot, 2-drive variant (no status drive) (10×) | median **117.2** ms, min 107.7, max 124.8 |
| snapshot of the 2-drive VM | `snap_file` 16,063 B, `mem_file` 536,870,912 B (513M); pause 0.8 ms, create 2.02 s |
| restore, `mem_backend: {backend_type: File}` (10×) | median **10.9** ms, min 8.7, max 11.2 |
| restore, legacy `mem_file_path` (10×) | median **10.6** ms, min 9.3, max 11.5 |
| restored VM serves? | yes: HTTP 200, 439 B, the same sha256 (`d05318e5…`) as the booted VM; still 200 three seconds later; 0 failures |

### 5, httpbin (the same steps)
| step | result |
|---|---|
| boot, 3-drive (1×, to record the restore failure) | 836.5 ms; snapshot created; restore: **the same panic** (`left: 3, right: 2`), 0/2 |
| boot, 2-drive (10×) | median **838.0** ms, min 815.3, max 847.5 |
| snapshot (2-drive) | `snap_file` 16,063 B, `mem_file` 536,870,912 B (513M); create 1.77 s |
| restore, `mem_backend` File (10×) | median **9.9** ms, min 7.7, max 10.3 |
| restore, legacy `mem_file_path` (10×) | median **9.9** ms, min 7.8, max 10.3 |
| restored VM serves? | yes: HTTP 200, 9593 B, the same sha256 (`7c7b091e…`); 200 again after 3 s; 0 failures |

### Supplement: first full HTTP 200, and a cold page cache (2-drive variant, 5 runs each, ms)
"cold" = `sync; echo 3 > /proc/sys/vm/drop_caches` right before each run (the rootfs for a boot, or the memory file for a restore, must then come from disk).

| image | condition | to TCP accept: median (min–max) | to first HTTP 200: median (min–max) |
|---|---|---|---|
| busybox | boot, warm | 119.0 (113.9–125.0) | 121.2 (116.1–131.5) |
| busybox | boot, cold | 299.8 (295.7–302.3) | 302.4 (298.2–305.0) |
| busybox | restore, warm | 11.2 (10.3–145.0*) | 15.4 (14.3–202.7*) |
| busybox | restore, cold | 175.6 (165.3–197.1) | 231.8 (225.2–249.5) |
| httpbin | boot, warm | 860.9 (822.1–1341.8) | 1489.6 (1454.8–2098.8) |
| httpbin | boot, cold | 1406.7 (1402.9–1425.4) | 2155.5 (2149.3–2190.9) |
| httpbin | restore, warm | 10.7 (9.7–133.0*) | 32.4 (30.4–560.5*) |
| httpbin | restore, cold | 159.6 (158.4–177.9) | 590.2 (582.1–608.1) |

\* The maximum of each "restore, warm" series is its **first** run, which came right after the cold-boot series had dropped the page cache, so that run was not really warm.
I kept it in the table. Without it the warm restores are 10–12 ms (TCP) and 14–16 / 30–33 ms (HTTP).

### Options the Firecracker build offers (what was tried)
- Load fields in this build (`struct LoadSnapshotConfig with 8 elements`, from the binary's strings): `snapshot_path`, `mem_file_path` (legacy), `mem_backend`
  {`backend_type`: `File` | `Uffd`, `backend_path`}, `enable_diff_snapshots`, `resume_vm`, `network_overrides`, `container_snapshot_path` (**vHive-specific and required**).
- **File** (via `mem_backend` and via the legacy `mem_file_path`): both work, with the same speed (the table above). Both mmap the memory file and fault pages in on demand.
- **Uffd**: the API accepts it, but it needs a userfaultfd page-fault handler process listening on a Unix socket. Without one: `Failed to connect to UDS ... No such file or directory`.
  **Not tested** (no handler on the node).
- Snapshot type `Full` was used. Diff snapshots (`enable_diff_snapshots` / `track_dirty_pages`) were **not tested**.
- `container_snapshot_path`: firecracker-containerd passes the per-VM devmapper device of the container rootfs there (seen in REPORT-1119b: `ContainerSnapshotPath:"/dev/mapper/fc-dev-thinpool-snap-137"`).
  For the 2-drive image VM, loading with the rootfs path *or* a non-existent path both succeeded, and `GET /vm/config` afterwards showed both drive paths unchanged.
  **What the fork does with this field for these VMs is not determined.** The spike passed the config drive's own path.

## Things that look stale or broken after restore
- **Clock:** the guest's wall clock restarts at the snapshot time. busybox's `Date` header showed 08:15:20 while the host was at 08:15:22 and, on a later restore, 08:15:26.
  httpbin showed 08:16:18 against 08:16:20 and 08:16:24. The guest has no NTP, so the gap equals the snapshot's age. Anything time-based (TLS validity, tokens, logs) would be wrong.
- **Per-VM identity is frozen:** env, hostname (`spike30-<name>`) and the pod config were read once at boot. A restored VM keeps the values of the VM that was snapshotted, so
  using one snapshot for several pods would give them the same hostname and env (not tested with a different env, only implied by how fcinit works).
- **Network:** fine here, because the restore used the same tap, IP and MAC as the snapshot. A restore into a different slot would need `network_overrides` plus a guest IP change. **Not tested.**
- **Status drive / crash detection:** the 2-drive variant has no status drive, so the drawbacks-1119 crash reporting (exit code via `/dev/vdc`) would not work for it as is.
- Nothing else broke in 40 restores: 0 failures, every verification request returned 200, and no errors were in the console logs apart from the expected snapshot-load messages.

## Failures (all of them)
1. 3-drive restore without `container_snapshot_path`: HTTP 400 `missing field container_snapshot_path` (20/20, busybox, first run).
2. 3-drive restore with `container_snapshot_path`: **Firecracker panics** (`persist.rs:328`, `left: 3, right: 2`), 2/2 busybox, 2/2 httpbin, and also the Uffd probe on the 3-drive snapshot.
3. `probe_container_path.py`'s first version crashed itself when Firecracker dropped the connection after that panic. No stray process was left (checked with `pgrep -x firecracker`).
4. Uffd load without a handler: HTTP 400 (expected; recorded as "option exists, untested").

## Caveats
- **One node, one session, n = 10** (n = 5 for the supplement). The 2-drive variant is **not** what the daemon runs today: it drops the status drive (and so crash detection).
  The boot times of the 2- and 3-drive configs were about the same (117 vs 119 ms for busybox).
- Warm-cache restores read a memory file that was just written (it was in the page cache). Real cold starts after a long idle period would be closer to the "cold" rows.
- Restores loaded the same snapshot into the same slot each time. Concurrency, many snapshots and other slots were not tested.
- The Firecracker process ran outside vhive (no Kubernetes, no queue-proxy, no placeholder), so these are VM-level times only, not end-to-end cold starts.
  Compare them with vhive's `launchToHTTP200` (busybox about 122 ms, httpbin about 855 ms in REPORT-1119b), which they match for boots.
- `drop_caches` was used (node-wide, harmless with no workload running).

## Clean-up (verified)
Tap `fcweb30` deleted (and its address and ARP entry with it), `/run/vhive-fcweb/30` removed, no Firecracker process left, all `mem_file`/`snap_file` copies deleted (2.5 GB).
The small results (JSON, logs, console excerpts, 120 KB) are in `~/fc-vm/spike/` and on the branch under `docs/spike/results/`. The daemon (pid 1251110, no `-snapshots`) and slots 1–16 were not touched.
