# REPORT 1119b: regression check of the drawbacks-1119 build, and a stock vHive `-snapshots` baseline (2026-11-18/19, node-000 hp109)

## Summary in plain language
- **Part A (same cold-start benchmark as last night, on the new build):** all 66 requests answered HTTP 200 with the same bytes as before; nothing failed.
  "Mine" is still faster than stock for every image, but by less than last night. The median ratio stock/mine went from 2.68× to 2.23× (busybox), from 1.96× to 1.65× (httpbin)
  and from 3.81× to 2.33× (nginx). **The stock control moved too**: +0.68 s for busybox, −0.22 s for httpbin, −0.33 s for nginx (medians). So the node or cluster
  did not behave the same as last night, and the two nights cannot be compared one to one. The VM boot itself (vhive's launch → app accepts TCP) is **unchanged**
  (busybox 122 ms, httpbin 855 ms, nginx 175–176 ms median both nights). The extra 0.3–0.6 s in "mine" is therefore outside the VM boot. **I did not measure where**, so no cause is claimed.
- **Part B (stock vHive with `-snapshots`):** snapshots **work** in this build, and log lines prove it. The first cold start boots normally. When that VM is
  removed, firecracker-containerd logs `create snapshot request ... /fccd/snapshots/snap-stock-00001/...`. **Every later cold start** sends `create VM request ... LoadSnapshot:true
  SnapshotPath:".../snap_file"`, has no fresh-boot line and no guest "httpd started". All 11 measured cycles were restores (one `LoadSnapshot:true` per cycle, inside that
  cycle's time window). Snapshot cold start: **median 1.158 s**, against 4.249 s for stock without snapshots and 1.905 s for "mine" (part A, same night).
  The snapshot took **513 MB on disk** (a 512 MiB memory file + 16 KB VM state + 6 KB patch + 144 B info). vHive deletes all snapshots at every daemon start, so it is gone again after the restore.
- The daemon was put back as it was (no `-snapshots`, same binary). The node is Ready and bb-mine answered 200.

## Provenance (`~/fc-vm/results-1119b/provenance.txt`)
| item | value |
|---|---|
| node time at start | 2026-11-18 22:08:26 MST |
| `~/vhive` branch / HEAD | `drawbacks-1119` / `8693bb2de79a6bee8d97c7e479f7365a8d64c0f6` (the running binary was built from `a00b958` at 21:45; the two later commits are docs only) |
| md5 `~/vhive/vhive-perpod` | `8fdc5b6a56e1f78fedae10fee1d36a09` (the same before part A, during part B and after the restore) |
| md5 `~/fc-vm/images/fcinit` | `9881bdfa75453889d4277195450e9643` (rootfs cache key `0d8b2d6b512e`) |
| last night (REPORT-1118) | daemon c88236a, md5 `3b09312cc0b0153263971fe823e5be79`; fcinit md5 `6676618355fef45c75d9c4a89ed9d63b` (key `0182ded729c9`) |
| cold.sh / stats.py | md5 `a770ceda…` / `256c34c9…`, **byte-identical** to the copies in `~/fc-vm/results-1118/` |
| ingress ClusterIP | 10.103.200.78 (read by cold.sh from kubectl) |
| services | bb-, httpbin-, nginx-stock/mine: the same ksvc objects as last night (generation 1, unchanged) |

## Part A: cold starts, new build vs last night (cycles 2–11, n = 10 per cell, end-to-end curl time_total in s)
Run 22:09:39 – 00:10:46 (`~/fc-vm/bench/run_a.sh`, which calls the unchanged cold.sh: busybox, then httpbin, then nginx, stock then mine within each round).
**66/66 HTTP 200, curl exit 0, no `not_idle` cycle, no rootfs build during a cold start** (counter 0 before and after). Bodies are identical to last night's (439 / 9593 / 615 B).

| workload | mode | last night median | tonight median | Δ median | last night mean | tonight mean | Δ mean | tonight min–max |
|---|---|---|---|---|---|---|---|---|
| busybox | stock | 3.565 | 4.249 | **+0.684** | 3.765 | 4.246 | +0.481 | 4.208–4.292 |
| busybox | mine | 1.328 | 1.905 | **+0.577** | 1.394 | 1.785 | +0.391 | 1.238–1.960 |
| busybox | ratio stock/mine | 2.68× | **2.23×** | | 2.70× | **2.38×** | | |
| httpbin | stock | 4.379 | 4.161 | **−0.218** | 4.342 | 4.153 | −0.189 | 4.060–4.243 |
| httpbin | mine | 2.230 | 2.520 | **+0.290** | 2.282 | 2.494 | +0.212 | 2.190–2.812 |
| httpbin | ratio stock/mine | 1.96× | **1.65×** | | 1.90× | **1.66×** | | |
| nginx | stock | 4.456 | 4.123 | **−0.333** | 4.260 | 4.115 | −0.145 | 3.988–4.188 |
| nginx | mine | 1.169 | 1.766 | **+0.597** | 1.230 | 1.765 | +0.535 | 1.670–1.845 |
| nginx | ratio stock/mine | 3.81× | **2.33×** | | 3.46× | **2.33×** | | |

VM stage of "mine" (vhive log `launchToHTTP200` = launch → app accepts TCP, cycles 2–11), min / median / max in ms:
| workload | last night | tonight |
|---|---|---|
| busybox | 121 / 122 / 134 | 119 / 122 / 126 |
| httpbin | 844 / 854 / 868 | 839 / 855 / 882 |
| nginx | 168 / 175 / 181 | 174 / 176 / 185 |

What the numbers show (observations, not causes):
1. **Stock (the control) moved**, in different directions per workload. So the node/cluster state differed between the nights, even though stock's code path is the same.
   (The new build changes only image-VM code, plus a ContainerStatus hook that leaves stock containers untouched; that was unit-tested, not timed.)
   Stock's shape also changed: last night busybox and nginx stock were **bimodal**, about 1 s apart (REPORT-1118). Tonight all three stock cells are **tight**
   (stdev 0.02–0.06 s) and sit near last night's *slow* mode.
2. **"Mine" got slower by 0.29–0.60 s (median) for all three images, while its VM stage did not change.** The added time is outside the VM boot. Where exactly is
   not measured. Plausible places are the Knative/kubelet path before the boot, and the queue-proxy readiness → activator → first proxied request path after it (where last
   night's phase C put stock's ~1 s spread). The new build also adds per-container work for image VMs at create time: a status-drive file, an exit watcher goroutine and a
   log forwarder goroutine. I did **not** time those, so I cannot say whether they contribute; the unchanged VM-stage numbers only show they are not inside launch → TCP accept.
3. "Mine" is **bimodal** tonight: busybox has 8 of 10 cycles at 1.87–1.96 s and 2 at 1.24–1.25 s; httpbin has 4 at 2.19–2.25 s and 6 at 2.47–2.81 s.
   The lower mode is close to last night's values. nginx-mine is unimodal (1.67–1.85 s).
4. Because both arms moved, **use same-night pairs for any claim**: tonight's ratios 2.2–2.4× (busybox, nginx) and 1.65× (httpbin), and last night's from REPORT-1118, each with its own caveats.

## Part B: stock vHive with `-snapshots`
### Commands (STATUS.md restart recipe both ways; same tmux session `vhive`; the binary is unchanged and was backed up first as `vhive-perpod.good-a00b958`, same md5)
```bash
kubectl apply -f ~/fc-vm/bench/snap/snap-stock.yaml        # = bb-stock.yaml with name snap-stock (diff: the name only); created under the NORMAL daemon, Ready, 1 request (200)
# wait for 0 pods / 0 firecracker, then:
tmux send-keys -t vhive C-c; ss -xl | grep -q vhive-cri || sudo rm -f /etc/vhive-cri/vhive-cri.sock
tmux send-keys -t vhive 'cd ~/vhive && sudo FCWEB_VCPUS=1 FCWEB_MEM_MIB=256 ./vhive-perpod -snapshots 2>&1 | tee -a /tmp/vhive-logs/vhive_log.txt' Enter   # up 00:13:06, node Ready, no error at start
bash ~/fc-vm/bench/snap/proof.sh 3                          # 3 manual cold starts with pane/log/disk evidence
tmux pipe-pane -t firecracker:0.0 "cat >> ~/fc-vm/bench/snap/fcctrd_pipe_cycles.log"   # copy of the pane output for per-cycle proof (00:20:10-00:38:53)
WORKLOAD=snapshot SERVICES=snap-stock CYCLES=11 OUT=~/fc-vm/bench/out-1119b/snapshot bash ~/fc-vm/bench/cold.sh
tmux pipe-pane -t firecracker:0.0                           # detach
# restore: same recipe, command without -snapshots (up 00:39:59), kubectl get nodes = Ready, curl bb-mine = 200 (1.77 s)
```

### How the snapshot is created (from the code and the logs)
- Code (`cri/firecracker/coordinator.go`): on a stock cold start, with snapshots on, `AcquireSnapshot(revision)` is tried first. If it gives nothing, the VM is **booted**
  (`orchStartVM`). When kubelet later sends RemoveContainer, `stopVM` pauses a VM that was *not* itself restored, creates a snapshot of it, resumes it, commits the snapshot and
  then stops it. The snapshot is kept in vhive's memory (keyed by revision `snap-stock-00001`), with its files in `/fccd/snapshots/<revision>/`.
  vhive logs these steps only at Debug level, and the daemon runs at Info (no `-dbg`), so the proof comes from firecracker-containerd's debug log, the files on disk,
  and the guest's output in the vhive log.
- So "after the 2nd invocation" (quickstart guide) means in practice: invocation 1 boots, its VM is snapshotted when it is removed, and **invocation 2 is the first one restored**.
- Proof cycles (`B_snap/proof/p1..p3/`):

| | request → response | firecracker-containerd | vhive log (guest output) | `/fccd/snapshots` |
|---|---|---|---|---|
| p1 (sent 00:13:24.3) | 4.867 s | `create VM request: VMID:"1-c61e464a…" MachineCfg:{MemSizeMib:512 VcpuCount:1}` **without** LoadSnapshot; `startInstance successful`; then at removal (00:15:27) `create snapshot request: VMID:"1-c61e464a…" MemFilePath:"/fccd/snapshots/snap-stock-00001/mem_file" SnapshotPath:"…/snap_file"` | `httpd started` (fresh boot) | empty before → `mem_file` 536870912 B, `snap_file` 16373 B, `patch_file` 5799 B after |
| p2 (sent 00:15:41.8) | 1.467 s | `create VM request: VMID:"2-e70a22e2…" … LoadSnapshot:true SnapshotPath:"/fccd/snapshots/snap-stock-00001/snap_file"`; **no** `startInstance`; "successfully started the VM" 134 ms after the request (p1: 2.16 s) | **no** `httpd started` | unchanged |
| p3 (sent 00:17:41.0) | 1.274 s | same as p2 (`LoadSnapshot:true`) | no `httpd started` | unchanged |

- Measured cycles (pipe-pane copy, `B_snap/fcctrd_pipe_cycles.log`, mapping in `cold_snapshot/restore_per_cycle.txt`): **11 create-VM requests, all 11 with `LoadSnapshot:true`,
  exactly one per cycle**, 0.725–0.773 s after that cycle's request was sent. 0 `startInstance successful` (no fresh boot), 0 new `create snapshot request` (restored VMs are not
  snapshotted again), and 0 `httpd started` in any cycle's vhive slice.

### Results (cycles 2–11, n = 10; bb-stock and bb-mine are from part A, the same night)
| service | what | n | mean s | median s | min s | max s | stdev s | errors |
|---|---|---|---|---|---|---|---|---|
| snap-stock | stock vHive, restored from snapshot | 10 | **1.312** | **1.158** | 1.098 | 2.119 | 0.343 | 0 |
| bb-stock | stock vHive, booted (no snapshots) | 10 | 4.246 | 4.249 | 4.208 | 4.292 | 0.021 | 0 |
| bb-mine | firecracker-vm://image, 512 MiB, booted | 10 | 1.785 | 1.905 | 1.238 | 1.960 | 0.286 | 0 |

Ratios (median): bb-stock / snap-stock = 3.67×; bb-mine / snap-stock = 1.65× (i.e. snapshot-restored stock is faster than "mine" booting from its image).
Warm-up cycle 1 of snap-stock: 1.894 s (already a restore). Two cycles were slower (c4 1.761 s, c11 2.119 s) although they were restores too (create-VM at 0.755 s and 0.773 s, like the others).
So the extra time came after the VM was created; where is not measured.

**Snapshot size on disk** (`B_snap/snapshot_files.txt`): `/fccd/snapshots/snap-stock-00001/` = **513 MB** (`du`, the same apparent and on disk, so not sparse):
`mem_file` 536,870,912 B (= the 512 MiB guest memory), `snap_file` 16,373 B, `patch_file` 5,799 B, `info_file` 144 B. One snapshot per function revision.

## Errors and anomalies (all recorded, none removed)
- None in part A or part B: 66 + 3 + 11 requests, all HTTP 200.
- Every stock scale-down logs `failed to stop firecracker-containerd VM ... forcefully terminated` (STATUS bug #2, also seen with the old binary). It appeared in part B too.
- Before part B, `/fccd/snapshots` held 36 per-VM directories (`<n>-<uuid>`, no files, 152 KB in total) that vHive creates for stock VMs even with snapshots off. They were removed by the next daemon start (see below).
- In part B the vhive log has **no** guest output at all for restored VMs (not even busybox's request log line `response:200`, which booted VMs print). I did not check why; it is only an observation.
- Part B's idle waits were 101–119 s (cycle 1: 9 s), as in part A.

## Deviations
- **`tmux pipe-pane` on the firecracker pane during the 11 snapshot cycles**, so that every cycle's restore could be proven (cold.sh itself does not capture panes, and it was not changed).
  It only copies the pane's output to a file. It does not clear or reset the pane, and it does not touch the daemon. It was not active during part A or the proof cycles.
- `snap-stock` was created under the normal daemon (it had one booted pod and one request at 00:11, before the snapshot daemon started). The snapshot daemon starts with an empty
  in-memory snapshot table, so its first cold start (p1) booted and created the snapshot.
- The part B cycles were run straight after the proof cycles under the same daemon, so the 11 cycles all used the snapshot made at 00:15:27 (from p1's VM).

## Caveats
- **n = 10 per cell; one node; one night per configuration**, and the stock control moved between the nights (see part A). Tonight's part A and part B ran ~0–2 h apart on the same daemon binary.
- **Same-node client** (curl on the node to the ingress ClusterIP), as before.
- **Memory:** every VM here has 512 MiB (stock: verified in REPORT-1119; mine: FC_MEM_MIB=512). The snapshot memory file is the full 512 MiB.
- **Snapshot restore here is stock vHive's built-in path** (Firecracker 1.13 via firecracker-containerd; no REAP/UPF, which the quickstart says is disabled). The memory file comes from the host page
  cache after the first restore (same file each time, 64 GB host RAM). I did not drop caches, so these are warm-page-cache restores.
- Snapshot correctness was checked only by the response body (same 439 bytes as booted stock). Restored VMs keep the in-memory state of the snapshotted VM (e.g. the same busybox process).
- The new build was not compared with the old build *on the same night*. That would need a daemon swap back to c88236a, which this task did not ask for. Stock's shift shows why that matters.

## Where everything is
- `~/fc-vm/results-1119b/`: `provenance.txt`; `A_cold/<busybox|httpbin|nginx>/` (cold.csv, cold.log, summary.md, cycles/, bodies/, ksvc.yaml), `run_a.sh`, `run_a.log`;
  `B_snap/` (`normal_daemon.txt`, `proof/p1..p3/`, `proof.log`, `cold_snapshot/` with cold.csv, summary.md and `restore_per_cycle.txt`, `fcctrd_pipe_cycles.log`, `snapshot_files.txt`, `restore_daemon.txt`, scripts).
- The same files are on branch `results-1118` under `bench/results-1119/` (worktree `~/fc-vm/wt-results-1118`). Commits 8a4e148 (part A) and 28c2eb8 (part B), pushed. `~/vhive` stayed on `drawbacks-1119` throughout.
- Left in place: ksvc `snap-stock` (idle at zero; it boots normally now that snapshots are off). Remove with `kubectl delete ksvc snap-stock`.
- **The snapshot itself is gone:** the restore restart at 00:39:59 emptied `/fccd/snapshots`, because vHive's `NewSnapshotManager` (`snapshotting/manager.go`) runs
  `os.RemoveAll` on the snapshot folder at every daemon start. So snapshots do not survive a vhive restart. The 513 MB size above was recorded in `B_snap/snapshot_files.txt`
  *before* that restart (`ls -la`, `du`).
