# PROGRESS — vSwarm + 3 images, stock vs mine (started Sat 2026-11-21 13:24 SGT = Fri 22:24 MST node time; hard stop 19:24 SGT)

Start state: ~/vhive HEAD 4ba8903 (branch snapshot-spike, not switched), vhive-perpod md5 8fdc5b6a56e1f78fedae10fee1d36a09, node Ready (v1.35.3),
no pods in `default`, only firecracker-containerd running (no firecracker VMs, no fcweb taps). Ingress ClusterIP 10.103.200.78.
Results worktree: ~/fc-vm/wt-results-vswarm (new branch `results-vswarm`, created from results-1118 at 60e2646), dir `bench/results-vswarm/`.

## Phase 1 — vSwarm feasibility (22:24–22:40 MST, ~16 min of the 1.5 h cap)
What ran: shallow clone of vSwarm (c91a381, 2025-03-04) and vSwarm-proto into ~/fc-vm/vswarm-src; installed grpcurl v1.9.1 and ghz v0.120.0
(`go install`, into ~/fc-vm/tools). vSwarm's Knative yamls run each function as TWO containers (a `relay` gRPC sidecar on 50000 + the function on 50051);
I did NOT use the relay: each function is ONE container (the function image itself, gRPC/h2c on 50051, port name `h2c`), in the same template as
bb-stock/bb-mine (max-scale 1, containerConcurrency 0, 512 MiB for mine); -stock and -mine differ only in GUEST_IMAGE (+FC_MEM_MIB=512).
rootfs built with `~/vhive/bin/fc-rootfs build` (2.6–12 s each). All 8 services became Ready; one grpcurl request per service (through the
ingress, `-authority <ksvc host>`), sent to the pods created at deploy time (so these were WARM requests, not cold starts).
Both modes really ran in VMs at the time: 8 firecracker processes (4 firecracker-containerd uVMs + 4 /run/vhive-fcweb/<n> image VMs).

| function | image | port/proto | needs other services? | request | result |
|---|---|---|---|---|---|
| fibonacci-go | docker.io/vhiveease/fibonacci-go:latest | 50051 gRPC | no | fibonacci.Greeter/SayHello {"name":"10"} | **WORKS-BOTH** (identical body "fn: Fib: y = fib(x) \| x: 10 y: 55 \| runtime: GoLang") |
| aes-go | docker.io/vhiveease/aes-go:latest | 50051 gRPC | no | aes.Aes/ShowEncryption {"plaintext_message":"world"} | **WORKS-BOTH** (identical body) |
| auth-go | docker.io/vhiveease/auth-go:latest | 50051 gRPC | no | auth.Greeter/SayHello {"name":"allow"} | **WORKS-BOTH** (identical body) |
| fibonacci-python | docker.io/vhiveease/fibonacci-python:latest | 50051 gRPC | no | fibonacci.Greeter/SayHello {"name":"10"} | **WORKS-BOTH** (identical body, "runtime: python") |
| compression-python | docker.io/vhiveease/compression-python | 50051 gRPC | **yes: MongoDB** (`--db_addr mongodb://compression-database:27017`) | not deployed | **BLOCKED** (needs outbound network + DNS; not attempted) |
| image-rotate-go/-python | docker.io/vhiveease/image-rotate-* | 50051 gRPC | **yes: MongoDB** (`mongodb://image-rotate-database:27017`) | not deployed | **BLOCKED** (same reason; not attempted) |

Tracing is off by default in these images (zipkin only if tracing is enabled), so they make no outbound calls. Not checked: the nodejs variants,
sleeping/spinning, and the larger apps (online-shop, hotel-app, video-*, gptj, bert …), which call other services or need big models.
Files: ~/fc-vm/vswarm/{yaml/,fn.sh,p1/,logs/rootfs-*.log} → results-vswarm `bench/results-vswarm/p1/`.
