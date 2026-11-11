# DESIGN: running a real container image in a per-pod Firecracker VM

Branch `real-image-vm`. Goal: one Knative service image X runs **either** the stock vHive way
(firecracker-containerd) **or** in your own per-pod Firecracker VM, using the same image and giving the same answer.

## The idea in one paragraph
A container image is just a stack of tar files (layers) plus a small JSON config (entrypoint, cmd, env, workdir, user).
We (1) flatten the layers once into an **ext4 disk image**, cached by the image's digest, (2) add a tiny
**init program** (`fcinit`) to that disk which does what a container runtime would do (set up mounts, network, env,
user, working directory, then start Entrypoint+Cmd), and (3) boot the per-pod VM from that disk instead of the
hand-made `rootfs.ext4`. The disk is shared **read-only** by all VMs of that image, and every VM gets its own
in-memory writable layer on top.

## New mode and how it is selected
- New marker `GUEST_IMAGE=firecracker-vm://image`. The existing markers keep their exact meaning:
  `firecracker-vm://webvm` (shared VM, slot 0) and `firecracker-vm://pod` (per-pod, baked-in disk or `FC_ROOTFS`).
  A new marker was chosen, rather than changing `firecracker-vm://pod`, so that `fcpod-fn` and its benchmarks behave exactly as before.
- **Which image:** (1) `FC_IMAGE` in the ksvc env, if set, or else (2) the image of the CRI CreateContainer request, which is the same
  `image:` field that the stock path uses. Kubelet usually sends an image **ID** (`sha256:<config digest>`) and not a name,
  so vhive asks containerd's ImageService (`ImageStatus`) for that image's `RepoDigests`, which gives `name@sha256:...`.
- **Port:** `GUEST_PORT` from the ksvc env (the same variable the stock path uses), or else `PORT`, or else 8080. queue-proxy is
  pointed at `guestIP:GUEST_PORT`, exactly like the stock path.
- Same slots (1–16), tap, /30 network, teardown in StopPodSandbox and FC_VCPUS/FC_MEM_MIB sizing as `firecracker-vm://pod`.
  All of that code is reused.

## 1. Image → rootfs builder (`cri/firecracker/rootfs`, CLI `cmd/fc-rootfs`)
1. Resolve the reference to a **manifest digest** (for linux/amd64) with go-containerregistry, which is already in the module graph.
2. Look up the cache: `$FC_IMAGE_CACHE/<digest>-<init hash>.ext4` (default `~/fc-vm/images`). The fcinit hash is part of the
   key, so changing fcinit automatically gives a new disk and an old disk is never used with a new init.
3. On a miss: flatten the layers (`mutate.Extract` applies whiteouts, i.e. files deleted by later layers), unpack them into a staging
   folder **as a normal user**, and record each entry's real owner, mode and type from the tar headers. Add `/.fcinit/init`, `/.fcinit/image.json`
   (entrypoint, cmd, env, workdir, user) and the empty mount points. Then run `mkfs.ext4 -d staging` (no loop mount, so no root needed) and fix
   owners and modes with one `debugfs -w` script. The fix is needed because an unprivileged unpack makes every file yours.
   Device nodes in the tar are skipped, since devtmpfs provides /dev in the guest.
4. Build into a temp file and `rename()` it into place, so a half-built disk is never seen. An `flock` makes concurrent
   builds of the same image wait for each other.
- **Pre-build:** `fc-rootfs build <image-ref>` (prints the disk path). Run it before benchmarking so that no cold start includes a
  build. If vhive misses the cache, it builds the disk itself (it works but is slow) and logs a loud `WARN ... built rootfs during cold start`.
- Rejected: *loop-mount + cp* (needs root and leaves mounts behind if it crashes). *Read from containerd's content store*
  (needs root, ties us to containerd's namespaces and snapshotter; pulling from the registry once per digest is simpler).
  *`mkfs.ext4 -d image.tar`* (needs e2fsprogs 1.47.1, and the node has 1.47.0). *squashfs* (smaller and the kernel has it, but
  `mksquashfs` is not installed and installing packages is off-limits; ext4 works with the tools we already have).

## 2. Guest init `fcinit` (`cmd/fcinit`, static Go, CGO_ENABLED=0)
PID 1. In order: mount a tmpfs and an **overlay** (lower = the read-only disk, upper = tmpfs) and chroot into it. Mount
`/proc`, `/sys`, `/dev` (devtmpfs), `/dev/pts`, `/dev/shm`. Read the per-VM config. Bring up `lo` and `eth0` with netlink (address and
default route), set the hostname and add it to `/etc/hosts`. Build the environment (image env, then pod env on top: the same precedence
Docker and containerd use). Resolve the user (`uid[:gid]` or a name looked up in the image's `/etc/passwd`/`/etc/group`). `chdir`
WorkingDir. Start Entrypoint+Cmd as a **child** with stdout/stderr on the serial console. Then loop on `wait4(-1)` to reap every zombie,
and forward SIGTERM/SIGINT to the app. When the app exits, `sync` and `reboot()`, which makes Firecracker exit (`reboot=k`).
fcinit stays PID 1 rather than exec'ing the app because many images (including our target, `sh -c "httpd & wait"`) never reap
orphans, and the kernel kills the VM if PID 1 exits.

**Config delivery (evaluated):**
| option | used for | why / why not |
|---|---|---|
| baked into the rootfs (`/.fcinit/image.json`) | **image config** | it is per image, like the disk, so it is cached together with the disk. |
| kernel cmdline | **network** (`fcweb.ip=`, `fcweb.gw=`, already used) | tiny, per slot, known before boot. Not used for env: x86 5.10 limits the cmdline to 2048 bytes, values with spaces need quoting, and the kernel passes unknown `k=v` words on to init's env. |
| per-VM **config drive** (2nd read-only virtio disk, raw JSON padded to 4 KiB, `/run/vhive-fcweb/<n>/guest.json`) | **pod env, port** | no size limit, no network needed, readable in microseconds as `/dev/vdb`. |
| Firecracker MMDS | rejected | the guest needs a network stack plus an HTTP (and v2 token) exchange before it can even read its config, and the config file needs an `mmds-config`. That is more moving parts for the same data. |

## 3. Writable root: read-only shared disk + tmpfs overlay
One ext4 per image, attached `is_read_only` to every VM. Inside, fcinit makes `/` an overlay with a tmpfs upper layer, so
the **whole** filesystem looks writable to the app, and writes live in the VM's own RAM and vanish when it stops. This is the same semantics
as a container's writable layer, which is also thrown away when the container is removed. Why: no per-VM disk copy (copying a disk on every cold start would add
time and host I/O that grow with the image size), no cleanup of disk copies, and the host page cache holds the image once for all VMs.
Cost: writes count against the VM's memory (FC_MEM_MIB). Rejected: *copy the disk per VM* (slow, uses disk);
*tmpfs only on /tmp,/run* (apps that write anywhere else fail with EROFS); *per-VM scratch ext4 as the upper layer* (needs mkfs or a template copy per VM,
and it is only worth it for apps that write a lot, so it is noted as future work).

## 4. Integration points in `cri/firecracker`
- `fcwebConfig` gains `Init`, `Port`, `ProbeTCP`, `GuestConfig` (all comparable strings/bools, so `cfg != p.cfg` still works).
  `fcwebConfigJSON` adds the config drive only when `GuestConfig` is set, and `init=` follows `Init`. The baked-in modes produce byte-identical
  configs, which is checked by the existing golden test.
- Readiness for image mode is a **TCP connect** to GUEST_PORT (the same thing Knative's `tcpSocket` probe checks), because a real app may not speak
  HTTP/1 on `/`. The baked modes keep the HTTP probe.
- `createPodVMUserContainer` receives a "prepare" function per mode. Image mode resolves the ref, gets the disk from the cache or builds it, and
  serialises the pod env. Everything after that (slot, boot, failure cleanup, queue-proxy env, StopPodSandbox) is the existing per-pod code.

## 5. Testing
Unit tests run without root: builder (synthetic in-memory images: whiteouts, symlinks, hardlinks, owners and modes, checked with `debugfs`; cache hit/miss),
fcinit pure parts (env merge, user and PATH resolution, config parsing), config JSON (golden files for the old modes, new drive for image mode), service routing
(image mode uses the resolver and port; webvm and pod modes are unchanged). End to end: `~/fc-vm/real/check.sh`.
