# LIMITS: which images will NOT work (or work differently) in `firecracker-vm://image`

Plain-language list. "Stock" means vHive's original firecracker-containerd path. Each point says **why**, so you can
judge whether it matters for a given function. Items marked *(same as stock)* are not new limitations.

## Tested and working (2026-11-11)
| image | what it exercises | result |
|---|---|---|
| `ghcr.io/vhive-serverless/crccheck-hello-world` | busybox, `sh -c "httpd & wait"` (2 processes), root | identical bytes to stock |
| `gcr.io/knative-samples/helloworld-go` | static Go binary, distroless-style, ksvc env `TARGET` | env arrives; same answer format as stock |
| `docker.io/nginxinc/nginx-unprivileged:1.27-alpine` | `USER 101`, entrypoint script, master + worker, writes to `/etc`, `/tmp`, `/var/cache` | identical bytes to stock (after fix 73028e0) |

## What the image must be
1. **linux/amd64.** The builder picks that platform from multi-arch images. arm64-only images fail to build.
2. **It needs an Entrypoint or Cmd.** The builder refuses images that have neither.
3. **ksvc `command:` / `args:` are ignored.** Only the image's Entrypoint+Cmd run *(same as stock vHive, which also ignores them)*.
4. **One TCP port.** The app must listen on `GUEST_PORT` (or `PORT`, or 8080) on TCP. Readiness is a TCP connect,
   so plain HTTP, h2c and gRPC are all fine. UDP-only servers never become ready.

## Things missing inside the VM (and why)
5. **No outbound network, no DNS.** The guest only has a /30 link to the host (10.200.0.4n+2 → .4n+1). vhive sets up no
   NAT, and fcinit writes no `/etc/resolv.conf` (the image's own file, if any, stays). A function that calls an external API or a
   database will fail. *(Stock VMs do have egress.)* Fix later: a MASQUERADE rule for 10.200.0.0/24 plus a resolv.conf on the config drive.
6. **No Kubernetes volumes.** ConfigMap/Secret volumes, emptyDir, PVCs and the service-account token are not mounted:
   kubelet mounts them into the placeholder container on the host, not into the VM. Env vars (including `secretKeyRef` env) **do** arrive.
7. **securityContext is ignored** (runAsUser, capabilities, readOnlyRootFilesystem, seccomp profile). The user comes
   from the image's `USER`. A root app has full root inside its VM. The VM itself is the security boundary.
8. **No cgroups, no systemd.** fcinit does not mount cgroupfs. Images whose entrypoint is a service manager
   (systemd, s6 expecting cgroups) will not start. Multi-process images that just fork (shell + server, nginx, gunicorn
   workers, supervisord) **do** work, because fcinit is PID 1 and reaps every orphan.
9. **Kernel 5.10 (`hello-vmlinux.bin`), no modules (`nomodules`).** It has overlayfs, ext4, squashfs, tmpfs, devpts,
   mqueue, FUSE, seccomp, user namespaces, io_uring, netfilter, IPv6, tun and vsock. Missing: virtio-rng and an RTC
   (`random.trust_cpu=on` covers entropy, and the clock comes from kvm-clock), and xfs/btrfs/nfs. Anything needing a newer kernel
   feature (e.g. landlock, newer io_uring ops) or a module fails.
10. **File capabilities and xattrs are not copied.** The builder ignores tar xattrs (`security.capability`, ACLs).
    A binary that relies on `setcap` (e.g. a non-root `ping`, or a server binding :80 as non-root through `cap_net_bind_service`)
    loses that capability. setuid/setgid bits **are** kept.
11. **File names containing `"`, `\` or a newline** keep the builder's owner and mode instead of the image's (debugfs cannot be given
    such names). Device nodes in the image are dropped (devtmpfs provides `/dev`).
12. **rlimits are kernel defaults** (e.g. open files 1024 soft / 4096 hard). containerd usually raises this. Servers that need
    many connections may hit `EMFILE`.
13. **The time zone is UTC** unless the image sets TZ or ships /etc/localtime.

## Behaviour that differs from a normal pod
14. **Writes live in RAM.** `/` is an overlay whose upper layer is tmpfs inside the VM, so every written byte uses VM memory
    (`FC_MEM_MIB`, default 256). Writing a lot (big temp files, caches) can run the VM out of memory. Writes are lost when the
    VM stops, just like a container's writable layer.
15. **Logs are not in `kubectl logs`.** The app's stdout/stderr go to `/run/vhive-fcweb/<slot>/console.log` (readable without sudo
    while the VM runs; the next boot in the same slot truncates it). `kubectl logs` shows the placeholder container.
16. **No graceful shutdown.** Scale-to-zero stops the Firecracker process. The app's SIGTERM handler does not run. *(Stock
    also stops its VM on scale-down; I did not check whether stock delivers SIGTERM to the app first.)*
17. **A crash after start is not reported to Kubernetes.** If the app exits, fcinit powers the VM off. Kubernetes still
    sees the placeholder as Running; queue-proxy's readiness probe then fails and Knative stops routing to that pod, but nothing
    restarts it until the pod is replaced. A crash **during** boot is reported: CreateContainer fails with the console tail, and kubelet
    retries.
18. **At most 16 per-pod VMs on the node**, shared with `firecracker-vm://pod` (slots 1-16).
19. **The config drive is readable by the app** (`/dev/vdb`). It holds the same env the app already has. On the host it is
    0600 and deleted when the VM stops.

## Builder / cache
20. **First use builds the rootfs** (1–5 s for the images above, more for big images) inside the cold start, and vhive logs
    `WARN ... built rootfs during cold start`. Always pre-build: `~/vhive/bin/fc-rootfs build <image>`.
21. **The cache is never cleaned.** `~/fc-vm/images/` keeps one ext4 per (image digest, fcinit build). Rebuilding fcinit gives every image a
    new disk. Clean by hand: `rm ~/fc-vm/images/sha256-*` (then pre-build again). The empty `*.lock` files are harmless.
22. **A tag in `FC_IMAGE` costs a registry round trip per cold start** (HEAD to resolve the tag). The default path (image from the
    CRI request, named through containerd's RepoDigests) is `name@sha256:...` and needs no network. Use digests in `FC_IMAGE`.
23. **Private registries** need credentials in `~/.docker/config.json` of the user running the build (root's, if vhive builds).
