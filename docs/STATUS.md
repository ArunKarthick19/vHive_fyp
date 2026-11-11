# STATUS — real-image-vm

**DEADLINE: 4:00 PM node time (Utah, UTC-7), Wed 2026-11-11 = 7:00 AM Singapore, Thu 2026-11-12.**
By then all docs, this file and the final commit must be finished. (The original "stop at 6:00 AM node time"
in IMPLEMENTATION_BRIEF.md was replaced by this deadline, which you sent as a correction at 08:31 node time.)

## Time plan (node time)
| step | box | until |
|---|---|---|
| 0. brief, status, branch | 10 min | 08:40 |
| 1. DESIGN.md, commit | 35 min | 09:15 |
| 2. image→rootfs builder + tests | 1 h 45 | 11:00 |
| 3. guest init (fcinit) + tests | 1 h 15 | 12:15 |
| 4. cri/firecracker integration + regression tests | 1 h 15 | 13:30 |
| 5. e2e demo (~/fc-vm/real) | 1 h 30 | 15:00 |
| 6. BENCH.md | 20 min | 15:20 |
| 7. LIMITS.md, final STATUS.md, final commit | 40 min | 16:00 |

Hard rule: no new code after 15:20. If a step runs over its box, note the blocker here and switch to the simplest workable alternative.

## Log
- 08:31 started. Branch `real-image-vm` created off `per-pod-vm` (4d02a14).
- 08:35 facts found: the guest kernel is 5.10.0 and includes overlayfs, squashfs, devtmpfs, tmpfs and vsock.
  The tools on the node are e2fsprogs 1.47.0 (`mkfs.ext4 -d` works, but it cannot read a tarball directly), debugfs, gcc and Go 1.26.
  go-containerregistry v0.20.3 is in the Go module cache.

## Done
- (nothing committed yet)

## In progress
- Step 1: DESIGN.md

## Next
- Step 2: builder

## Blockers
- none

## Decisions
- see DESIGN.md (filled in as I go)
