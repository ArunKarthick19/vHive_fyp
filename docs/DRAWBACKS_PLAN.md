# DRAWBACKS_PLAN: fixing known drawbacks of `firecracker-vm://image`

Branch `drawbacks-1119` (from `real-image-vm`). Progress log: `~/fc-vm/DRAWBACKS-LOG.md`.

## 1. Crash detection (implemented on this branch)

### How it works today
- A Knative pod has two containers. For `firecracker-vm://image`, vhive's CRI service (`cri/firecracker/service.go`) does two things in
  `CreateContainer(user-container)`. It asks containerd to create the normal **placeholder** user container (it runs the same image *on the host*
  and is never sent traffic), and it boots the pod's own Firecracker VM (`podvm.go`, `firecracker.go`). queue-proxy is then pointed at the VM.
- Inside the VM, `fcinit` (PID 1) starts the app. When the app exits, fcinit logs `app exited (status N)` on the serial console and reboots.
  With `reboot=k` the Firecracker process then exits.
- **Nobody watches for that exit.** `firecrackerVM.boot` only closes an internal `exited` channel. The kubelet asks containerd about the
  *placeholder* (`ContainerStatus` is forwarded unchanged by `cri/proxy.go`), and the placeholder is still running. So the pod stays `Running`,
  queue-proxy's probe starts failing, and nothing restarts the app.

### What changes
1. **fcinit records the exit (guest, `cmd/fcinit`, `guest/`).** Each image VM gets a third virtio disk, a 4 KiB writable **status drive** (`/dev/vdc`,
   host file `/run/vhive-fcweb/<slot>/exit-status.bin`, mode 0600, zeroed at boot). When the app exits, fcinit writes
   `{"exitCode":3,"reason":"Exited"}` (a signal gives `128+signo`, as for containers) to it, fsyncs it, logs it on the console and powers off.
   A failure of fcinit itself writes `reason: InitError`. The format is a pure function in package `guest` (encode/decode/wait-status mapping).
   *Why a drive and not a console line:* the app's output goes to the same console, so an app could print a fake line. The drive is written only by fcinit.
2. **vhive notices the exit (`podvm.go`).** For VMs booted with the status drive (image VMs only), a goroutine waits on the Firecracker
   process's exit channel. If the exit was not caused by vhive's own teardown (StopPodSandbox or a failed create), it reads the status drive and the console tail
   and calls the service.
3. **vhive tells Kubernetes (`service.go`, `cri/proxy.go`).** The service remembers which user container belongs to the sandbox, records
   the exit for that container ID, and **stops the placeholder** through containerd (`StopContainer`, timeout 0). containerd then really reports the container as EXITED
   (so `ListContainers`/PLEG see it). `ContainerStatus` is patched for *that container ID only*: `exitCode` = the app's code, `reason` = `Error` (or
   `Completed` for 0, `VMExited` if the drive was empty, e.g. kernel panic), `message` = a short console tail. kubelet then applies the pod's restartPolicy
   (`Always` for Knative): it creates a new user container in the same sandbox. vhive's existing code boots a fresh VM in the same slot for it, so `RESTARTS` goes up
   and `kubectl describe` shows `Last State: Terminated, Exit Code: 3`.
4. **What does not change:** `firecracker-vm://webvm`, `firecracker-vm://pod` and the stock path never get a status drive, no watcher and no patched status.
   Their Firecracker configs stay byte-identical (golden test). `cri/proxy.go` patches only through an optional interface that only image-VM container IDs hit.

### How it is tested
- **Unit (no root):** guest exit-status encode/decode and wait-status → code mapping; VM config JSON with the status drive (and unchanged golden files for
  the baked modes); podVMs watcher with a fake VM (exit → one report; teardown → no report; a newer boot is not confused with an old exit; baked mode → no watcher);
  the service records the exit, stops the right placeholder, and patches only EXITED statuses of tracked IDs.
- **End to end:** a test image `dev.local/fcexit:1` (crccheck plus a guarded cmd): inside the VM (`/.fcinit` exists) it serves for 20 s, then `exit 3`;
  the host placeholder (no `/.fcinit`) serves forever, so it cannot fake the result. The same service is run with the **old** binary (expect: stays Running,
  0 restarts) and the **new** binary (expect: restarts, Exit Code 3 in describe). Before every push: `go test -race ./cri/firecracker/...`, `check-1118.sh` with
  `VM_YAML=vm-512.yaml`, and `regress.sh`.

**Result (verified 2026-11-18, logs in `~/fc-vm/drawbacks/`).** The old binary left the pod `Running` with 0 restarts for 120 s after the app exited.
With the new one, the test app's `exit 3` gives `RESTARTS` 1, 2, 3, then `Error` and `CrashLoopBackOff`. `kubectl describe` shows
`Last State: Terminated, Reason: Error, Exit Code: 3` with the console tail. Killing the VM's Firecracker from outside gives `Reason: VMExited, Exit Code: 255`.
Scale-to-zero (check-1118.sh) is *not* reported as a crash.

## 2. Logs: `kubectl logs` (implemented on this branch)

### What kubelet needs
kubelet sends `CreateContainer` with `config.log_path` (`user-container/<attempt>.log`) relative to the sandbox's `log_directory`
(`/var/log/pods/<ns>_<pod>_<uid>/`). It reads the container's log from the full path that `ContainerStatus.log_path` returns, in the CRI log format,
one record per line: `<RFC3339Nano time> <stdout|stderr> <F|P> <text>` (`P` = partial line). When the file reaches `containerLogMaxSize`, kubelet
renames it and calls `ReopenContainerLog`. `kubectl logs -f` follows the file. There is no CRI streaming call for logs; the file is the whole contract.
Before this branch, that file was written by containerd **for the host placeholder**, so `kubectl logs` showed the placeholder's output, not the VM's.

### What was implemented (smallest step that gives correct output)
A per-container **log forwarder** in vhive (`cri/firecracker/vmlogs.go`) polls the VM's `console.log` every 250 ms and appends its new lines in CRI format
to that same file. Firecracker's own lines are dropped and CRs are removed; lines over 16 KiB are split into `P` records. The file is opened for each write with `O_APPEND`, so after kubelet's
rotation the next write goes to the new file (no `ReopenContainerLog` handling needed). For a freshly booted VM the forwarder copies the whole console (fcinit's
start-up lines included). For a container that reuses a running VM, it copies from the console's current end. It is drained and stopped when the app exits, before the
placeholder is stopped, so `kubectl logs --previous` has the last lines, and also at StopPodSandbox. Baked-in modes and stock are untouched.

### What is still missing (documented, not done)
- The **placeholder's own output** is still in the same file, interleaved (e.g. a second `httpd started`). Fixing it needs the placeholder's log
  redirected (rewrite `log_path` in the forwarded CreateContainer and patch it back in ContainerStatus) or the placeholder replaced by a no-op image.
- **stdout and stderr are not separated:** fcinit gives the app one pipe. Two pipes plus a tag per console line would fix it.
- **Not live while vhive is down:** lines written while vhive restarts are copied when it is back only if the forwarder is restarted. Today, a vhive restart loses the
  forwarders of running VMs (like every other in-memory state of the per-pod VMs).
- `console.log` itself still grows without a bound during a VM's life and is truncated on the next boot in the slot (LIMITS #15). A size cap would mean a
  rotating writer between Firecracker's stdout and the file. That is not done.

## 3. Outbound network and DNS (design only, not implemented)

### What is missing today (checked read-only on node-000, 2026-11-18)
- **Routing:** the guest has `10.200.0.4n+2/30` with its gateway on the host tap `fcweb<n>`. The host forwards (`net.ipv4.ip_forward = 1`, FORWARD policy ACCEPT behind
  Calico's and kube-proxy's chains), but **no NAT rule covers 10.200.0.0/24** (`iptables-save | grep 10.200` is empty). Replies from the
  internet or from other pods cannot find their way back to 10.200.0.x, so no connection succeeds.
- **DNS:** fcinit writes no `/etc/resolv.conf`. crccheck's image has none, and httpbin's is empty. The host's own resolver is `127.0.0.53` (systemd-resolved),
  which the guest cannot use anyway. Pods normally get `nameserver 10.96.0.10` (CoreDNS) plus `search <ns>.svc.cluster.local svc.cluster.local cluster.local`
  and `options ndots:5`. kubelet hands vhive exactly this in the CRI request: `CreateContainerRequest.sandbox_config.dns_config` (servers, searches, options).
- **What Kubernetes/Knative expect:** a pod can reach Services (ClusterIP, DNS names), other pods and the internet, subject to NetworkPolicy. Knative itself needs nothing
  from the user container except the inbound port. Ingress already works (queue-proxy → guest IP).
- Stock vHive gives its uVMs egress through `networking/networking.go`: a netns per VM, SNAT inside it, and an nftables `masquerade` on the host uplink.

### Options
| | what | pros | cons | effort |
|---|---|---|---|---|
| **A. Host NAT + resolv.conf** (recommended first step) | per slot at tap creation: `iptables -t nat -A POSTROUTING -s 10.200.0.4n+2/32 ! -d 10.200.0.0/24 -j MASQUERADE` plus an anti-spoof rule `-i fcweb<n> ! -s <guestIP> -j DROP`. Removed in cleanup and by the startup sweep. vhive copies `dns_config` onto the config drive and fcinit writes `/etc/resolv.conf` from it | small, mirrors stock vHive, ClusterIP Services work (kube-proxy DNATs in PREROUTING for packets from the tap), CoreDNS works | traffic leaves as the **node's** IP: NetworkPolicy, which Calico applies on the pod's `cali*` interface, does **not** apply to the VM; other pods see the node, not the pod IP | about 1 day: rule lifecycle + sweep + tests (resolv.conf part: 2–3 h) |
| B. VM inside the pod's network namespace | create the tap in the placeholder sandbox's netns and redirect eth0↔tap with tc (like Kata Containers) | the VM *is* the pod IP: NetworkPolicy, Services and source IP are all correct | queue-proxy shares that netns and must reach the app differently; slot networking, probes and teardown change; large | 1–2 weeks |
| C. Proxy on the host (HTTP CONNECT / SOCKS on the tap) | only proxied traffic leaves | very easy to control | apps must be proxy-aware; not transparent; no DNS | 1–2 days |

### Security implications (for A)
- **The VM is root inside its guest** and can send any packet on its tap. Without the anti-spoof rule it could use another slot's guest IP, or any source.
- **NetworkPolicy is bypassed** (traffic is masqueraded as the node), and so are egress restrictions a cluster may rely on.
- **The node's own services become reachable**, as from any host process: kubelet `:10250`, etcd `:2379`/`:2380` (on this single-node cluster), the Kubernetes API,
  and the cloud metadata address if there is one. INPUT rules for `-i fcweb+` (allow only the established/related traffic for probes, plus DNS to CoreDNS through the
  Service IP) should come with the NAT rules.
- Rate limiting and connection tracking limits per tap are optional hardening.
- Stock vHive's uVMs have the same exposure today (their egress is also masqueraded on the host), so option A does not add a new class of risk compared to stock. It is still a change from today's
  image VMs, which cannot reach anything.

### How it would be tested
Unit: rule generation per slot (pure), resolv.conf rendering from `DNSConfig` (pure). End to end: an image whose app runs `nslookup kubernetes.default` and
`wget -qO- http://<Service>` and prints the results. Check the rules are gone after scale-to-zero and after the startup sweep. Run regress plus a NetworkPolicy
test documenting that the policy is *not* applied (until option B).
